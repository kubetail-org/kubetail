use k8s_openapi::api::authorization::v1::{
    ResourceAttributes, SelfSubjectAccessReview, SelfSubjectAccessReviewSpec,
};
use kube::{Api, Client, Config, api::PostParams, config::AuthInfo};
use tonic::{Status, metadata::MetadataMap};

pub struct Authorizer {
    k8s_config: Config,
}

/// Checks that the the k8s doing the request has proper rights to access the log files.
#[cfg(not(test))]
impl Authorizer {
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

        Ok(Self { k8s_config })
    }

    /// Checks if the request is authorized by calling the k8s API.
    pub async fn is_authorized(
        &self,
        mut namespaces: &Vec<String>,
        verb: &str,
    ) -> Result<(), Status> {
        let client = Client::try_from(self.k8s_config.clone())
            .map_err(|error| Status::new(tonic::Code::Unauthenticated, error.to_string()))?;

        // Default to all namespaces if no namespace is provided.
        let empty_namespace = vec![String::new()];
        if namespaces.is_empty() {
            namespaces = &empty_namespace;
        }

        let access_reviews: Api<SelfSubjectAccessReview> = Api::all(client);
        for namespace in namespaces {
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

            if response.status.is_none() || !response.status.unwrap().allowed {
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
            k8s_config: Config::infer().await.unwrap(),
        })
    }

    pub async fn is_authorized(self, _namespaces: &Vec<String>, _verb: &str) -> Result<(), Status> {
        Ok(())
    }
}
