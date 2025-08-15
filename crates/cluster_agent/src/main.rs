use std::error::Error;
use std::fs::read_to_string;

use tokio::signal::ctrl_c;
use tokio::signal::unix::{SignalKind, signal};
use tokio::sync::broadcast::{self, Sender};
use tokio_util::task::TaskTracker;
use tonic::transport::{Certificate, Identity, Server, ServerTlsConfig};
use tracing::info;
use types::cluster_agent::FILE_DESCRIPTOR_SET;
use types::cluster_agent::log_metadata_service_server::LogMetadataServiceServer;
use types::cluster_agent::log_records_service_server::LogRecordsServiceServer;

mod log_metadata;
mod log_records;
use log_metadata::LogMetadataImpl;
use log_records::LogRecordsImpl;

#[tokio::main]
async fn main() -> Result<(), Box<dyn Error>> {
    tracing_subscriber::fmt()
        .with_max_level(tracing::Level::INFO)
        .init();

    let (_, agent_health_service) = tonic_health::server::health_reporter();
    let reflection_service = tonic_reflection::server::Builder::configure()
        .register_encoded_file_descriptor_set(FILE_DESCRIPTOR_SET)
        .build_v1()?;
    let (term_tx, _term_rx) = broadcast::channel(1);
    let task_tracker = TaskTracker::new();
    let tls_config = build_tls_config()?;

    Server::builder()
        .tls_config(tls_config)?
        .add_service(agent_health_service)
        .add_service(reflection_service)
        .add_service(LogMetadataServiceServer::new(LogMetadataImpl::new(
            term_tx.clone(),
            task_tracker.clone(),
        )))
        .add_service(LogRecordsServiceServer::new(LogRecordsImpl::new(
            term_tx.clone(),
            task_tracker.clone(),
        )))
        .serve_with_shutdown("[::]:50051".parse()?, shutdown(term_tx))
        .await
        .unwrap();

    task_tracker.close();
    task_tracker.wait().await;

    info!("Shutdown completed.");

    Ok(())
}

fn build_tls_config() -> Result<ServerTlsConfig, Box<dyn Error>> {
    let cert = read_to_string("/etc/kubetail/tls.crt")?;
    let key = read_to_string("/etc/kubetail/tls.key")?;
    let server_identity = Identity::from_pem(cert, key);

    let client_ca_cert = read_to_string("/etc/kubetail/ca.crt")?;
    let client_ca_cert = Certificate::from_pem(client_ca_cert);

    Ok(ServerTlsConfig::new()
        .identity(server_identity)
        .client_ca_root(client_ca_cert))
}

async fn shutdown(term_tx: Sender<()>) {
    let mut term = signal(SignalKind::terminate()).unwrap();

    tokio::select! {
        _ = ctrl_c()  => {
            info!("SIGINT received, initiating shutdown..");
            let _ = term_tx.send(());
        },
        _ = term.recv() => {
            info!("SIGTERM received, initiating shutdown..");
            let _ = term_tx.send(());
        },
    }
}
