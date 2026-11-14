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

use std::error::Error;
use std::fs::read_to_string;
use std::future::Future;
use std::path::PathBuf;
use std::str::FromStr;
use std::sync::Arc;

use clap::{ArgAction, arg, command, value_parser};
use tokio::signal::ctrl_c;
use tokio::signal::unix::{SignalKind, signal};
use tokio_util::sync::CancellationToken;
use tokio_util::task::TaskTracker;
use tonic::transport::{Certificate, Identity, Server, ServerTlsConfig};
use tonic_health::server::HealthReporter;
use tracing::info;
use types::cluster_agent::FILE_DESCRIPTOR_SET;
use types::cluster_agent::log_metadata_service_server::LogMetadataServiceServer;
use types::cluster_agent::log_records_service_server::LogRecordsServiceServer;

mod auth;
mod authorizer;
mod config;
mod health;
mod log_metadata;
mod log_records;
use authorizer::Authorizer;
use log_metadata::LogMetadataImpl;
use log_records::LogRecordsImpl;

use crate::config::{Config, LoggingConfig, TlsConfig};

// coverage:ignore-start the entry point runs only as the deployed binary
#[tokio::main]
async fn main() -> Result<(), Box<dyn Error>> {
    let mut config = parse_config().await?;

    configure_logging(&config.logging)?;

    let authorizer = Authorizer::new().await?;

    let (health_reporter, agent_health_service) = tonic_health::server::health_reporter();
    let reflection_service = tonic_reflection::server::Builder::configure()
        .register_encoded_file_descriptor_set(FILE_DESCRIPTOR_SET)
        .build_v1()?;
    let task_tracker = TaskTracker::new();
    let root_ctx = CancellationToken::new();

    let mut server = configure_tls(Server::builder(), &config.tls)?;

    info!("Starting cluster-agent on {}", config.address);

    let allowed_names = Arc::new(std::mem::take(&mut config.tls.allowed_names));
    let log_metadata_svc = LogMetadataServiceServer::with_interceptor(
        LogMetadataImpl::new(
            root_ctx.clone(),
            task_tracker.clone(),
            config.logs_dir.clone(),
            authorizer.clone(),
        ),
        auth::interceptor(allowed_names.clone()),
    );
    let log_records_svc = LogRecordsServiceServer::with_interceptor(
        LogRecordsImpl::new(
            root_ctx.clone(),
            task_tracker.clone(),
            config.logs_dir.clone(),
            authorizer.clone(),
        ),
        auth::interceptor(allowed_names.clone()),
    );

    health::set_serving(&health_reporter).await;

    server
        .add_service(agent_health_service)
        .add_service(reflection_service)
        .add_service(log_metadata_svc)
        .add_service(log_records_svc)
        .serve_with_shutdown(
            config.address,
            shutdown(wait_for_signal(), root_ctx, health_reporter),
        )
        .await?;

    task_tracker.close();
    task_tracker.wait().await;

    info!("Shutdown completed.");

    Ok(())
}
// coverage:ignore-stop

#[allow(clippy::cognitive_complexity)]
async fn parse_config() -> Result<Config, Box<dyn Error + 'static>> {
    let matches = command!()
        .arg(
            arg!(
                -c --config <FILE> "Configuration file path"
            )
            .required(true)
            .value_parser(value_parser!(PathBuf)),
        )
        .arg(
            arg!(-p --param <CONFIG_PAIR> "Configuration overrides")
                .action(ArgAction::Append)
                .value_parser(parse_overrides),
        )
        .arg(arg!(-a --addr <ADDRESS> "Address to listen for connections"))
        .get_matches();

    let config_path = matches
        .get_one::<PathBuf>("config")
        .expect("config argument is required");
    let mut overrides: Vec<(String, String)> = matches
        .get_many("param")
        .map_or_else(Vec::new, |params| params.cloned().collect());

    if let Some(address) = matches.get_one::<String>("addr") {
        overrides.push(("addr".to_owned(), address.to_owned()));
    }

    let config = Config::parse(config_path, overrides).await?;

    Ok(config)
}

fn parse_overrides(param: &str) -> Result<(String, String), String> {
    if let Some((name, value)) = param.split_once(':') {
        Ok((name.to_owned(), value.to_owned()))
    } else {
        Err(
            "configuration should have format <config name>:<value>, i.e. logging.level:debug"
                .to_owned(),
        )
    }
}

fn configure_tls(server: Server, tls_config: &TlsConfig) -> Result<Server, Box<dyn Error>> {
    let cert = read_to_string(&tls_config.cert_file)?;
    let key = read_to_string(&tls_config.key_file)?;
    let ca = read_to_string(&tls_config.ca_file)?;

    let server_tls_config = ServerTlsConfig::new()
        .identity(Identity::from_pem(cert, key))
        .client_ca_root(Certificate::from_pem(ca));

    server.tls_config(server_tls_config).map_err(Into::into)
}

fn configure_logging(logging_config: &LoggingConfig) -> Result<(), Box<dyn Error>> {
    if !logging_config.enabled {
        return Ok(());
    }

    let sub_builder =
        tracing_subscriber::fmt().with_max_level(tracing::Level::from_str(&logging_config.level)?);

    if logging_config.format == "pretty" {
        sub_builder.pretty().init();
    } else {
        sub_builder.json().init();
    }

    Ok(())
}

/// Waits for `signal`, then reports not serving before cancelling `ctx_token`,
/// so a health check sees the drain before the listener closes.
async fn shutdown(
    signal: impl Future<Output = ()>,
    ctx_token: CancellationToken,
    health_reporter: HealthReporter,
) {
    signal.await;
    health::set_not_serving(&health_reporter).await;
    ctx_token.cancel();
}

// coverage:ignore-start waits on a real process signal
async fn wait_for_signal() {
    let mut term = signal(SignalKind::terminate()).expect("Failed to register SIGTERM handler");

    tokio::select! {
        _ = ctrl_c()  => info!("SIGINT received, initiating shutdown.."),
        _ = term.recv() => info!("SIGTERM received, initiating shutdown.."),
    }
}
// coverage:ignore-stop

#[cfg(test)]
mod tests {
    #![allow(clippy::unwrap_used)]

    use tonic::Request;
    use tonic_health::pb::HealthCheckRequest;
    use tonic_health::pb::health_check_response::ServingStatus;
    use tonic_health::pb::health_server::Health;
    use tonic_health::server::HealthService;

    use super::*;

    #[tokio::test]
    async fn shutdown_reports_not_serving_and_cancels() {
        let reporter = HealthReporter::new();
        health::set_serving(&reporter).await;
        let ctx = CancellationToken::new();

        shutdown(std::future::ready(()), ctx.clone(), reporter.clone()).await;

        let response = HealthService::from_health_reporter(reporter)
            .check(Request::new(HealthCheckRequest::default()))
            .await
            .unwrap();
        assert_eq!(response.into_inner().status(), ServingStatus::NotServing);
        assert!(ctx.is_cancelled());
    }
}
