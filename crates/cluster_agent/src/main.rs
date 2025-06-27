use tonic::transport::Server;
use types::cluster_agent::FILE_DESCRIPTOR_SET;
use types::cluster_agent::log_metadata_service_server::LogMetadataServiceServer;
use types::cluster_agent::log_records_service_server::LogRecordsServiceServer;
mod logmetadata;
mod logrecords;

use logmetadata::LogMetadata;
use logrecords::LogRecords;

#[tokio::main]
async fn main() -> eyre::Result<()> {
    let (_, agent_health_service) = tonic_health::server::health_reporter();

    let reflection_service = tonic_reflection::server::Builder::configure()
        .register_encoded_file_descriptor_set(FILE_DESCRIPTOR_SET)
        .build_v1()?;

    Server::builder()
        .add_service(agent_health_service)
        .add_service(reflection_service)
        .add_service(LogMetadataServiceServer::new(LogMetadata {}))
        .add_service(LogRecordsServiceServer::new(LogRecords {}))
        .serve("[::]:50051".parse()?)
        .await
        .unwrap();

    Ok(())
}
