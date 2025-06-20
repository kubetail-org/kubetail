use crate::services::ClusterAgent;
use tonic::transport::Server;
use types::cluster_agent::FILE_DESCRIPTOR_SET;
use types::cluster_agent::log_metadata_service_server;
use types::cluster_agent::log_records_service_server;
mod services;

#[tokio::main]
async fn main() -> eyre::Result<()> {
    let server = ClusterAgent {};

    let (agent_health_reporter, agent_health_service) = tonic_health::server::health_reporter();

    agent_health_reporter
        .set_serving::<log_metadata_service_server::LogMetadataServiceServer<ClusterAgent>>()
        .await;

    let reflection_service = tonic_reflection::server::Builder::configure()
        .register_encoded_file_descriptor_set(FILE_DESCRIPTOR_SET)
        .build_v1()?;

    Server::builder()
        .add_service(agent_health_service)
        .add_service(reflection_service)
        .add_service(log_metadata_service_server::LogMetadataServiceServer::new(
            server.clone(),
        ))
        .add_service(log_records_service_server::LogRecordsServiceServer::new(
            server,
        ))
        .serve("[::1]:50051".parse()?)
        .await
        .unwrap();

    Ok(())
}
