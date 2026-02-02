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
use std::path::PathBuf;
use std::str::FromStr;
use std::sync::Arc;

use clap::{ArgAction, arg, command, value_parser};
use tokio::signal::ctrl_c;
use tokio::signal::unix::{SignalKind, signal};
use tokio_util::sync::CancellationToken;
use tokio_util::task::TaskTracker;
use tonic::transport::{Certificate, Identity, Server, ServerTlsConfig};
use tracing::info;
use types::cluster_agent::FILE_DESCRIPTOR_SET;
use types::cluster_agent::log_metadata_service_server::LogMetadataServiceServer;
use types::cluster_agent::log_records_service_server::LogRecordsServiceServer;

mod authorizer;
mod config;
mod log_metadata;
mod log_records;
use authorizer::Authorizer;
use log_metadata::LogMetadataImpl;
use log_records::LogRecordsImpl;

use crate::config::{Config, LoggingConfig, TlsConfig};

#[tokio::main]
async fn main() -> Result<(), Box<dyn Error>> {
    let config = parse_config().await?;

    configure_logging(&config.logging)?;

    let authorizer = Authorizer::new().await?;

    let (_, agent_health_service) = tonic_health::server::health_reporter();
    let reflection_service = tonic_reflection::server::Builder::configure()
        .register_encoded_file_descriptor_set(FILE_DESCRIPTOR_SET)
        .build_v1()?;
    let task_tracker = TaskTracker::new();
    let root_ctx = CancellationToken::new();

    let mut server = enable_tls(Server::builder(), &config.tls)?;

    info!("Starting cluster-agent on {}", config.address);

    server
        .add_service(agent_health_service)
        .add_service(reflection_service)
        .add_service(LogMetadataServiceServer::new(LogMetadataImpl::new(
            root_ctx.clone(),
            task_tracker.clone(),
            config.logs_dir.clone(),
            authorizer.clone(),
        )))
        .add_service(LogRecordsServiceServer::new(LogRecordsImpl::new(
            root_ctx.clone(),
            task_tracker.clone(),
            config.logs_dir.clone(),
            authorizer.clone(),
        )))
        .serve_with_shutdown(config.address, shutdown(root_ctx))
        .await?;

    task_tracker.close();
    task_tracker.wait().await;

    info!("Shutdown completed.");

    Ok(())
}

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

fn enable_tls(server: Server, tls_config: &TlsConfig) -> Result<Server, Box<dyn Error>> {
    if !tls_config.enabled {
        return Ok(server);
    }

    let cert_file = tls_config
        .cert_file
        .as_ref()
        .ok_or("TLS cert file path is required when TLS is enabled")?;
    let key_file = tls_config
        .key_file
        .as_ref()
        .ok_or("TLS key file path is required when TLS is enabled")?;
    let cert = read_to_string(cert_file)?;
    let key = read_to_string(key_file)?;

    let server_identity = Identity::from_pem(cert, key);

    let mut server_tls_config = ServerTlsConfig::new().identity(server_identity);

    #[allow(clippy::collapsible_if)]
    if let Some(client_auth) = &tls_config.client_auth {
        if client_auth == "require-and-verify" {
            let ca_file = tls_config
                .ca_file
                .as_ref()
                .ok_or("TLS CA file path is required for client verification")?;
            let client_ca_cert = read_to_string(ca_file)?;

            let client_ca_cert = Certificate::from_pem(client_ca_cert);

            server_tls_config = server_tls_config.client_ca_root(client_ca_cert);
        }
    }

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

async fn shutdown(ctx_token: CancellationToken) {
    let mut term = signal(SignalKind::terminate()).expect("Failed to register SIGTERM handler");

    tokio::select! {
        _ = ctrl_c()  => {
            info!("SIGINT received, initiating shutdown..");
            ctx_token.cancel();
        },
        _ = term.recv() => {
            info!("SIGTERM received, initiating shutdown..");
            ctx_token.cancel();
        },
    }
}
