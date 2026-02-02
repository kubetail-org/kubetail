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

#[cfg(not(test))]
use k8s_openapi::api::authorization::v1::{
    ResourceAttributes, SelfSubjectAccessReview, SelfSubjectAccessReviewSpec,
};

use kube::Config;

#[cfg(not(test))]
use kube::{Api, Client, api::PostParams, config::AuthInfo};
use tonic::{Status, metadata::MetadataMap};

use moka::future::Cache;
#[cfg(not(test))]
use sha2::{Digest, Sha256};
use std::time::Duration;

/// Key for the authorization cache: (token_hash, namespace, verb)
#[derive(Clone, Debug, Eq, PartialEq, Hash)]
pub struct CacheKey {
    token_hash: [u8; 32],
    namespace: String,
    verb: String,
}

/// Value for the authorization cache: allowed (true/false)
pub type CacheValue = bool;

/// Type alias for the authorization cache
pub type AuthCache = Cache<CacheKey, CacheValue>;

/// Creates a process-scoped authorization cache
#[cfg(test)]
pub fn create_auth_cache() -> AuthCache {
    Cache::builder()
        .max_capacity(10_000)
        .time_to_live(Duration::from_secs(30))
        .build()
}

/// Creates a process-scoped authorization cache
#[cfg(not(test))]
fn create_auth_cache() -> AuthCache {
    Cache::builder()
        .max_capacity(10_000)
        .time_to_live(Duration::from_secs(30))
        .build()
}

#[allow(dead_code)]
#[derive(Debug, Clone)]
pub struct Authorizer {
    k8s_config: Config,
    auth_cache: AuthCache,
}

/// Checks that the the k8s doing the request has proper rights to access the log files.
#[cfg(not(test))]
impl Authorizer {
    /// Creates a new Authorizer that can be shared across requests.
    pub async fn new() -> Result<Self, Status> {
        let k8s_config = Config::infer().await.map_err(|error| {
            Status::new(
                tonic::Code::Unknown,
                format!("unable to infer k8s config {error}"),
            )
        })?;
        Ok(Self {
            k8s_config,
            auth_cache: create_auth_cache(),
        })
    }

    fn extract_token(request_metadata: &MetadataMap) -> Result<String, Status> {
        request_metadata
            .get("authorization")
            .and_then(|token| token.to_str().ok())
            .ok_or_else(|| {
                Status::new(
                    tonic::Code::Unauthenticated,
                    "authentication token not found",
                )
            })
            .map(|token| token.to_owned())
    }

    fn hash_token(token: &str) -> [u8; 32] {
        let mut hasher = Sha256::new();
        hasher.update(token.as_bytes());
        hasher.finalize().into()
    }

    /// Checks if the request is authorized by calling the k8s API.
    pub async fn is_authorized(
        &self,
        request_metadata: &MetadataMap,
        namespaces: &[String],
        verb: &str,
    ) -> Result<(), Status> {
        let token = Self::extract_token(request_metadata)?;
        let token_hash = Self::hash_token(&token);

        let mut k8s_config = self.k8s_config.clone();
        k8s_config.auth_info = AuthInfo {
            token: Some(token.into()),
            ..Default::default()
        };

        let client = Client::try_from(k8s_config)
            .map_err(|error| Status::new(tonic::Code::Unauthenticated, error.to_string()))?;

        let access_reviews: Api<SelfSubjectAccessReview> = Api::all(client);

        let namespaces_to_check: Vec<Option<String>> = if namespaces.is_empty() {
            vec![None]
        } else {
            namespaces.iter().map(|ns| Some(ns.clone())).collect()
        };

        for namespace_opt in namespaces_to_check {
            let namespace_key = namespace_opt.clone().unwrap_or_default();

            let cache_key = CacheKey {
                token_hash,
                namespace: namespace_key.clone(),
                verb: verb.to_owned(),
            };

            if let Some(allowed) = self.auth_cache.get(&cache_key).await {
                if !allowed {
                    return Err(Status::new(
                        tonic::Code::PermissionDenied,
                        format!(
                            "permission denied: `{verb} pods/log` in namespace `{}`",
                            namespace_opt.as_deref().unwrap_or("all")
                        ),
                    ));
                }
                continue;
            }

            let access_review = SelfSubjectAccessReview {
                spec: SelfSubjectAccessReviewSpec {
                    resource_attributes: Some(ResourceAttributes {
                        namespace: namespace_opt.clone(),
                        group: None,
                        verb: Some(verb.to_owned()),
                        resource: Some("pods".to_owned()),
                        subresource: Some("log".to_owned()),
                        ..ResourceAttributes::default()
                    }),
                    non_resource_attributes: None,
                },
                ..SelfSubjectAccessReview::default()
            };

            let response = access_reviews
                .create(&PostParams::default(), &access_review)
                .await
                .map_err(|error| {
                    Status::new(
                        tonic::Code::Unknown,
                        format!("failed to authenticate {error}"),
                    )
                })?;

            let allowed = response.status.as_ref().is_some_and(|s| s.allowed);

            self.auth_cache.insert(cache_key.clone(), allowed).await;

            if !allowed {
                return Err(Status::new(
                    tonic::Code::PermissionDenied,
                    format!(
                        "permission denied: `{verb} pods/log` in namespace `{}`",
                        namespace_opt.as_deref().unwrap_or("all")
                    ),
                ));
            }
        }

        Ok(())
    }
}

#[cfg(test)]
impl Authorizer {
    pub async fn new() -> Result<Self, Status> {
        Ok(Self {
            k8s_config: Config::new(http::Uri::from_static("http://k8s.url")),
            auth_cache: create_auth_cache(),
        })
    }

    pub async fn is_authorized(
        &self,
        _request_metadata: &MetadataMap,
        _namespaces: &[String],
        _verb: &str,
    ) -> Result<(), Status> {
        Ok(())
    }
}
