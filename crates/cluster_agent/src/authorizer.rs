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

#[cfg(not(test))]
use secrecy::ExposeSecret;

#[cfg(not(test))]
use moka::future::Cache;
#[cfg(not(test))]
use sha2::{Digest, Sha256};
#[cfg(not(test))]
use std::sync::Arc;
#[cfg(not(test))]
use std::time::Duration;

// Cache key: (token_hash, namespace, verb)
#[cfg(not(test))]
type CacheKey = (String, String, String);

// Cache value: authorization result (allowed)
#[cfg(not(test))]
type CacheValue = bool;

#[allow(dead_code)]
pub struct Authorizer {
    k8s_config: Config,
    #[cfg(not(test))]
    auth_cache: Arc<Cache<CacheKey, CacheValue>>,
}

/// Checks that the the k8s doing the request has proper rights to access the log files.
#[cfg(not(test))]
impl Authorizer {
    /// Default cache TTL in seconds (5 minutes)
    const DEFAULT_CACHE_TTL_SECS: u64 = 300;

    /// Default maximum cache size
    const DEFAULT_CACHE_MAX_CAPACITY: u64 = 10_000;

    /// Creates a new Authorizer, using the k8s authorization token to construct the proper
    /// client set during authorization.
    pub async fn new(request_metadata: &MetadataMap) -> Result<Self, Status> {
        let token = request_metadata
            .get("authorization")
            .and_then(|token| token.to_str().ok())
            .ok_or_else(|| {
                Status::new(
                    tonic::Code::Unauthenticated,
                    "authentication token not found",
                )
            })?
            .to_owned();

        let mut k8s_config = Config::infer().await.map_err(|error| {
            Status::new(
                tonic::Code::Unknown,
                format!("unable to infer k8s config {error}"),
            )
        })?;

        k8s_config.auth_info = AuthInfo {
            token: Some(token.into()),
            ..Default::default()
        };

        let auth_cache = Arc::new(
            Cache::builder()
                .max_capacity(Self::DEFAULT_CACHE_MAX_CAPACITY)
                .time_to_live(Duration::from_secs(Self::DEFAULT_CACHE_TTL_SECS))
                .build(),
        );

        Ok(Self {
            k8s_config,
            auth_cache,
        })
    }

    /// Hashes a token for use as a cache key
    fn hash_token(token: &str) -> String {
        let mut hasher = Sha256::new();
        hasher.update(token.as_bytes());
        format!("{:x}", hasher.finalize())
    }

    /// Checks if the request is authorized by calling the k8s API.
    /// Results are cached to reduce API calls.
    pub async fn is_authorized(
        &self,
        mut namespaces: &Vec<String>,
        verb: &str,
    ) -> Result<(), Status> {
        let client = Client::try_from(self.k8s_config.clone())
            .map_err(|error| Status::new(tonic::Code::Unauthenticated, error.to_string()))?;

        // Extract and hash the token for cache key
        let token_hash = self
            .k8s_config
            .auth_info
            .token
            .as_ref()
            .map(|t| Self::hash_token(t.expose_secret()))
            .unwrap_or_default();

        // Default to all namespaces if no namespace is provided.
        let empty_namespace = vec![String::new()];
        if namespaces.is_empty() {
            namespaces = &empty_namespace;
        }

        let access_reviews: Api<SelfSubjectAccessReview> = Api::all(client);
        for namespace in namespaces {
            let cache_key = (token_hash.clone(), namespace.clone(), verb.to_string());

            // Check cache first
            if let Some(allowed) = self.auth_cache.get(&cache_key).await {
                if !allowed {
                    return Err(Status::new(
                        tonic::Code::Unauthenticated,
                        format!("permission denied: `{verb} pods/log` in namespace `{namespace}`"),
                    ));
                }
                continue;
            }

            // Cache miss - check with k8s API
            let access_review = SelfSubjectAccessReview {
                spec: SelfSubjectAccessReviewSpec {
                    resource_attributes: Some(ResourceAttributes {
                        namespace: Some(namespace.to_owned()),
                        group: None,
                        verb: Some(verb.to_owned()),
                        resource: Some("pods/log".to_owned()),
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

            let allowed = response.status.is_some() && response.status.unwrap().allowed;

            // Store result in cache
            self.auth_cache.insert(cache_key, allowed).await;

            if !allowed {
                return Err(Status::new(
                    tonic::Code::Unauthenticated,
                    format!("permission denied: `{verb} pods/log` in namespace `{namespace}`"),
                ));
            }
        }

        Ok(())
    }
}

#[cfg(test)]
impl Authorizer {
    pub async fn new(_request_metadata: &MetadataMap) -> Result<Self, Status> {
        Ok(Self {
            k8s_config: Config::new(http::Uri::from_static("http://k8s.url")),
        })
    }

    pub async fn is_authorized(self, _namespaces: &Vec<String>, _verb: &str) -> Result<(), Status> {
        Ok(())
    }
}

#[cfg(all(test, not(test)))]
mod tests {
    use super::*;
    use std::time::Duration;

    #[tokio::test]
    async fn test_hash_token_produces_consistent_hash() {
        let token1 = "my-secret-token";
        let token2 = "my-secret-token";
        let token3 = "different-token";

        let hash1 = Authorizer::hash_token(token1);
        let hash2 = Authorizer::hash_token(token2);
        let hash3 = Authorizer::hash_token(token3);

        assert_eq!(hash1, hash2);
        assert_ne!(hash1, hash3);
    }

    #[tokio::test]
    async fn test_cache_respects_ttl() {
        let cache = Cache::builder()
            .max_capacity(100)
            .time_to_live(Duration::from_millis(100))
            .build();

        let key = (
            "token_hash".to_string(),
            "namespace".to_string(),
            "get".to_string(),
        );
        cache.insert(key.clone(), true).await;

        assert_eq!(cache.get(&key).await, Some(true));

        tokio::time::sleep(Duration::from_millis(150)).await;

        assert_eq!(cache.get(&key).await, None);
    }

    #[tokio::test]
    async fn test_cache_respects_max_capacity() {
        let cache = Cache::builder()
            .max_capacity(2)
            .time_to_live(Duration::from_secs(60))
            .build();

        cache
            .insert(
                ("hash1".to_string(), "ns1".to_string(), "get".to_string()),
                true,
            )
            .await;
        cache
            .insert(
                ("hash2".to_string(), "ns2".to_string(), "get".to_string()),
                true,
            )
            .await;

        cache.run_pending_tasks().await;

        cache
            .insert(
                ("hash3".to_string(), "ns3".to_string(), "get".to_string()),
                true,
            )
            .await;

        cache.run_pending_tasks().await;

        assert_eq!(cache.entry_count(), 2);
    }
}
