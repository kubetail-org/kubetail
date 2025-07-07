use tokio::signal::{ctrl_c, unix::*};
use tokio::sync::broadcast::{self, Sender};
use tokio_util::task::TaskTracker;
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

    tracing_subscriber::fmt()
        .with_max_level(tracing::Level::WARN)
        .init();

    let (term_tx, _term_rx) = broadcast::channel(1);

    let task_tracker = TaskTracker::new();

    Server::builder()
        .add_service(agent_health_service)
        .add_service(reflection_service)
        .add_service(LogMetadataServiceServer::new(LogMetadata {}))
        .add_service(LogRecordsServiceServer::new(LogRecords::new(
            term_tx.clone(),
            task_tracker.clone(),
        )))
        .serve_with_shutdown("[::]:50051".parse()?, shutdown(term_tx))
        .await
        .unwrap();

    task_tracker.close();
    task_tracker.wait().await;

    println!("Shutdown completed.");

    Ok(())
}

async fn shutdown(term_tx: Sender<()>) {
    let mut term = signal(SignalKind::terminate()).unwrap();

    tokio::select! {
        _ = ctrl_c()  => {
            println!("SIGINT received, initiating shutdown..");
            let _ = term_tx.send(());
        },
        _ = term.recv() => {
            println!("SIGTERM received, initiating shutdown..");
            let _ = term_tx.send(());
        },
    }
}
