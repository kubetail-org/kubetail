// Copyright 2024 The Kubetail Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

use k8s_openapi::api::authorization::v1::{
    ResourceAttributes, SubjectAccessReview, SubjectAccessReviewSpec,
};
use tonic::Status;

use crate::auth::Identity;
use moka::future::Cache;
use std::sync::Arc;
use std::time::Duration;

/// Performs a Kubernetes `SubjectAccessReview` for a single SAR.
#[tonic::async_trait]
pub trait AccessReviewer: std::fmt::Debug + Send + Sync + 'static {
    async fn review(&self, sar: SubjectAccessReview) -> Result<bool, Status>;
}

#[derive(Debug)]
struct KubeAccessReviewer {
    api: kube::Api<SubjectAccessReview>,
}

#[tonic::async_trait]
impl AccessReviewer for KubeAccessReviewer {
    async fn review(&self, sar: SubjectAccessReview) -> Result<bool, Status> {
        let response = self
            .api
            .create(&kube::api::PostParams::default(), &sar)
            .await
            .map_err(|error| {
                Status::new(
                    tonic::Code::Unknown,
                    format!("failed to authenticate {error}"),
                )
            })?;
        Ok(response.status.as_ref().is_some_and(|s| s.allowed))
    }
}

/// Always-allow reviewer used by tests in other modules that need to
/// construct an `Authorizer` without a live cluster.
#[cfg(test)]
#[derive(Debug)]
pub(crate) struct AlwaysAllowReviewer;

#[cfg(test)]
#[tonic::async_trait]
impl AccessReviewer for AlwaysAllowReviewer {
    async fn review(&self, _sar: SubjectAccessReview) -> Result<bool, Status> {
        Ok(true)
    }
}

/// Key for the authorization cache: (identity, namespace, verb)
#[derive(Clone, Debug, Eq, PartialEq, Hash)]
pub struct CacheKey {
    identity: Arc<Identity>,
    namespace: String,
    verb: String,
}

type AuthCache = Cache<CacheKey, bool>;

fn create_auth_cache() -> AuthCache {
    Cache::builder()
        .max_capacity(10_000)
        .time_to_live(Duration::from_secs(30))
        .build()
}

#[derive(Debug, Clone)]
pub struct Authorizer {
    reviewer: Arc<dyn AccessReviewer>,
    auth_cache: AuthCache,
}

/// Build a `SubjectAccessReview` for an identity asking to perform `verb`
/// on `pods/log` in the given namespace (`None` = cluster-scoped).
pub fn build_sar(identity: &Identity, namespace: Option<&str>, verb: &str) -> SubjectAccessReview {
    let extra = if identity.extras.is_empty() {
        None
    } else {
        Some(
            identity
                .extras
                .iter()
                .map(|(k, v)| (k.clone(), v.iter().cloned().collect()))
                .collect(),
        )
    };
    let groups = if identity.groups.is_empty() {
        None
    } else {
        Some(identity.groups.iter().cloned().collect())
    };
    SubjectAccessReview {
        spec: SubjectAccessReviewSpec {
            user: Some(identity.user.clone()),
            groups,
            extra,
            resource_attributes: Some(ResourceAttributes {
                namespace: namespace.map(str::to_owned),
                verb: Some(verb.to_owned()),
                resource: Some("pods".to_owned()),
                subresource: Some("log".to_owned()),
                ..ResourceAttributes::default()
            }),
            non_resource_attributes: None,
            uid: None,
        },
        ..SubjectAccessReview::default()
    }
}

fn permission_denied(verb: &str, namespace: Option<&str>) -> Status {
    let target_ns = namespace.unwrap_or("all");

    Status::new(
        tonic::Code::PermissionDenied,
        format!(
            "permission denied: `{verb} pods/log` in namespace `{}`",
            target_ns
        ),
    )
}

impl Authorizer {
    pub fn with_reviewer(reviewer: Arc<dyn AccessReviewer>) -> Self {
        Self {
            reviewer,
            auth_cache: create_auth_cache(),
        }
    }

    pub async fn is_authorized(
        &self,
        identity: &Arc<Identity>,
        namespaces: &[String],
        verb: &str,
    ) -> Result<(), Status> {
        let namespaces_to_check: Vec<Option<&str>> = if namespaces.is_empty() {
            vec![None]
        } else {
            namespaces.iter().map(|ns| Some(ns.as_str())).collect()
        };

        for namespace in namespaces_to_check {
            let cache_key = CacheKey {
                identity: identity.clone(),
                namespace: namespace.unwrap_or("").to_owned(),
                verb: verb.to_owned(),
            };

            let allowed = if let Some(cached) = self.auth_cache.get(&cache_key).await {
                cached
            } else {
                let sar = build_sar(identity, namespace, verb);
                let allowed = self.reviewer.review(sar).await?;
                self.auth_cache.insert(cache_key, allowed).await;
                allowed
            };

            if !allowed {
                return Err(permission_denied(verb, namespace));
            }
        }

        Ok(())
    }
}

impl Authorizer {
    pub async fn new() -> Result<Self, Status> {
        let cfg = kube::Config::infer().await.map_err(|error| {
            Status::new(
                tonic::Code::Unknown,
                format!("unable to infer k8s config {error}"),
            )
        })?;
        let client = kube::Client::try_from(cfg)
            .map_err(|error| Status::new(tonic::Code::Unauthenticated, error.to_string()))?;
        let api: kube::Api<SubjectAccessReview> = kube::Api::all(client);
        Ok(Self::with_reviewer(Arc::new(KubeAccessReviewer { api })))
    }
}

#[cfg(test)]
mod tests {
    #![allow(clippy::unwrap_used)]

    use super::*;
    use std::collections::{BTreeMap, BTreeSet};

    fn id(user: &str) -> Identity {
        Identity {
            user: user.to_owned(),
            groups: BTreeSet::new(),
            extras: BTreeMap::new(),
        }
    }

    #[test]
    fn build_sar_user_only() {
        let sar = build_sar(&id("alice"), Some("default"), "get");
        assert_eq!(sar.spec.user.as_deref(), Some("alice"));
        assert!(sar.spec.groups.is_none());
        assert!(sar.spec.extra.is_none());
        let attrs = sar.spec.resource_attributes.as_ref().unwrap();
        assert_eq!(attrs.namespace.as_deref(), Some("default"));
        assert_eq!(attrs.verb.as_deref(), Some("get"));
        assert_eq!(attrs.resource.as_deref(), Some("pods"));
        assert_eq!(attrs.subresource.as_deref(), Some("log"));
    }

    #[test]
    fn build_sar_with_groups_and_extras() {
        let identity = Identity {
            user: "alice".into(),
            groups: BTreeSet::from(["devs".to_string(), "system:authenticated".to_string()]),
            extras: BTreeMap::from([(
                "scopes".to_string(),
                BTreeSet::from(["read".to_string(), "write".to_string()]),
            )]),
        };
        let sar = build_sar(&identity, Some("ns"), "list");
        assert_eq!(sar.spec.groups.as_ref().unwrap().len(), 2);
        let extra = sar.spec.extra.as_ref().unwrap();
        assert_eq!(
            extra.get("scopes").unwrap(),
            &vec!["read".to_string(), "write".to_string()]
        );
    }

    #[test]
    fn build_sar_cluster_scoped_when_namespace_none() {
        let sar = build_sar(&id("alice"), None, "get");
        let attrs = sar.spec.resource_attributes.as_ref().unwrap();
        assert!(attrs.namespace.is_none());
    }

    fn key(identity: Identity) -> CacheKey {
        CacheKey {
            identity: Arc::new(identity),
            namespace: "ns".to_owned(),
            verb: "get".to_owned(),
        }
    }

    #[test]
    fn cache_key_equal_for_reordered_groups() {
        let a = Identity {
            user: "alice".into(),
            groups: BTreeSet::from(["x".to_string(), "y".to_string()]),
            extras: BTreeMap::new(),
        };
        let b = Identity {
            user: "alice".into(),
            groups: ["y", "x"].iter().map(|s| s.to_string()).collect(),
            extras: BTreeMap::new(),
        };
        assert_eq!(key(a.clone()), key(b.clone()));

        use std::hash::{BuildHasher, Hash, Hasher};
        let state = std::collections::hash_map::RandomState::new();
        let mut h1 = state.build_hasher();
        let mut h2 = state.build_hasher();
        key(a).hash(&mut h1);
        key(b).hash(&mut h2);
        assert_eq!(h1.finish(), h2.finish());
    }

    use std::sync::atomic::{AtomicUsize, Ordering};

    #[derive(Debug)]
    struct CountingReviewer {
        count: AtomicUsize,
        allow: bool,
    }

    impl CountingReviewer {
        fn new(allow: bool) -> Self {
            Self {
                count: AtomicUsize::new(0),
                allow,
            }
        }
        fn calls(&self) -> usize {
            self.count.load(Ordering::SeqCst)
        }
    }

    #[tonic::async_trait]
    impl AccessReviewer for CountingReviewer {
        async fn review(&self, _sar: SubjectAccessReview) -> Result<bool, Status> {
            self.count.fetch_add(1, Ordering::SeqCst);
            Ok(self.allow)
        }
    }

    #[tokio::test]
    async fn cache_hit_avoids_second_review() {
        let reviewer = Arc::new(CountingReviewer::new(true));
        let auth = Authorizer::with_reviewer(reviewer.clone());
        let id = Arc::new(id("alice"));
        let ns = vec!["ns".to_string()];
        auth.is_authorized(&id, &ns, "get").await.unwrap();
        auth.is_authorized(&id, &ns, "get").await.unwrap();
        assert_eq!(reviewer.calls(), 1);
    }

    #[tokio::test]
    async fn cache_miss_per_namespace() {
        let reviewer = Arc::new(CountingReviewer::new(true));
        let auth = Authorizer::with_reviewer(reviewer.clone());
        let id = Arc::new(id("alice"));
        auth.is_authorized(&id, &["a".to_string()], "get")
            .await
            .unwrap();
        auth.is_authorized(&id, &["b".to_string()], "get")
            .await
            .unwrap();
        assert_eq!(reviewer.calls(), 2);
    }

    #[tokio::test]
    async fn reordered_groups_share_cache_entry() {
        let reviewer = Arc::new(CountingReviewer::new(true));
        let auth = Authorizer::with_reviewer(reviewer.clone());
        let a = Arc::new(Identity {
            user: "alice".into(),
            groups: BTreeSet::from(["x".to_string(), "y".to_string()]),
            extras: BTreeMap::new(),
        });
        let b = Arc::new(Identity {
            user: "alice".into(),
            groups: ["y", "x"].iter().map(|s| s.to_string()).collect(),
            extras: BTreeMap::new(),
        });
        let ns = vec!["ns".to_string()];
        auth.is_authorized(&a, &ns, "get").await.unwrap();
        auth.is_authorized(&b, &ns, "get").await.unwrap();
        assert_eq!(reviewer.calls(), 1);
    }

    #[tokio::test]
    async fn deny_decision_is_cached() {
        let reviewer = Arc::new(CountingReviewer::new(false));
        let auth = Authorizer::with_reviewer(reviewer.clone());
        let id = Arc::new(id("alice"));
        let ns = vec!["ns".to_string()];
        let r1 = auth.is_authorized(&id, &ns, "get").await;
        let r2 = auth.is_authorized(&id, &ns, "get").await;
        assert_eq!(r1.unwrap_err().code(), tonic::Code::PermissionDenied);
        assert_eq!(r2.unwrap_err().code(), tonic::Code::PermissionDenied);
        assert_eq!(reviewer.calls(), 1);
    }

    #[test]
    fn cache_key_equal_for_reordered_extras() {
        let a = Identity {
            user: "alice".into(),
            groups: BTreeSet::new(),
            extras: BTreeMap::from([(
                "scopes".to_string(),
                BTreeSet::from(["read".to_string(), "write".to_string()]),
            )]),
        };
        let b = Identity {
            user: "alice".into(),
            groups: BTreeSet::new(),
            extras: BTreeMap::from([(
                "scopes".to_string(),
                ["write", "read"].iter().map(|s| s.to_string()).collect(),
            )]),
        };
        assert_eq!(key(a), key(b));
    }
}
