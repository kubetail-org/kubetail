use std::{
    error::Error,
    io,
    net::SocketAddr,
    path::{Path, PathBuf},
};

use config::builder::DefaultState;
use regex::Regex;
use serde::Deserialize;

#[derive(Debug)]
pub struct Config {
    pub address: SocketAddr,
    pub logs_dir: PathBuf,
    pub logging: LoggingConfig,
    pub tls: TlsConfig,
}

#[derive(Deserialize, Debug)]
struct ConfigInternal {
    #[serde(rename(deserialize = "addr"))]
    address: String,
    #[serde(rename(deserialize = "container-logs-dir"))]
    logs_dir: PathBuf,
    logging: LoggingConfig,
    tls: TlsConfig,
}

#[derive(Deserialize, Debug)]
struct FullConfig {
    #[serde(rename(deserialize = "cluster-agent"))]
    cluster_agent: ConfigInternal,
}

#[derive(Deserialize, Debug)]
pub struct LoggingConfig {
    pub enabled: bool,
    pub level: String,
    pub format: String,
}

#[derive(Deserialize, Debug)]
pub struct TlsConfig {
    pub enabled: bool,

    #[serde(rename(deserialize = "cert-file"))]
    pub cert_file: Option<PathBuf>,

    #[serde(rename(deserialize = "key-file"))]
    pub key_file: Option<PathBuf>,

    #[serde(rename(deserialize = "ca-file"))]
    pub ca_file: Option<PathBuf>,

    #[serde(rename(deserialize = "client-auth"))]
    pub client_auth: Option<String>,
}

impl Config {
    pub fn parse(path: &Path) -> Result<Self, Box<dyn Error + 'static>> {
        let settings = Self::builder_with_defaults()?
            .add_source(config::File::with_name(&path.to_string_lossy()))
            .build()?;

        let full_config: FullConfig = settings.try_deserialize()?;
        let tls = full_config.cluster_agent.tls;

        if tls.enabled {
            if tls.cert_file.is_none() || tls.key_file.is_none() {
                return Err(Box::new(io::Error::new(
                    io::ErrorKind::InvalidInput,
                    "Cert file and key file should be supplied when tls is enabled",
                )));
            }

            if let Some(client_auth) = &tls.client_auth {
                if client_auth == "require-and-verify" && tls.ca_file.is_none() {
                    return Err(Box::new(io::Error::new(
                        io::ErrorKind::InvalidInput,
                        "Trusted certificates should be supplied for require-and-verify",
                    )));
                }
            }
        }

        Ok(Self {
            address: Self::parse_address(&full_config.cluster_agent.address)?,
            logs_dir: full_config.cluster_agent.logs_dir,
            logging: full_config.cluster_agent.logging,
            tls,
        })
    }

    fn parse_address(address: &str) -> Result<SocketAddr, Box<dyn Error + 'static>> {
        let shorthand_regex = Regex::new(r"^:(?<socket>\d+)$").unwrap();

        if let Some(captures) = shorthand_regex.captures(address) {
            let socket_str = format!("[::]:{}", &captures["socket"]);

            return Ok(socket_str.parse()?);
        }

        Ok(address.parse()?)
    }

    fn builder_with_defaults() -> Result<config::ConfigBuilder<DefaultState>, config::ConfigError> {
        config::Config::builder()
            .set_default("cluster-agent.addr", "[::]:50051")?
            .set_default("cluster-agent.container-logs-dir", "/var/log/containers")?
            .set_default("cluster-agent.logging.enabled", true)?
            .set_default("cluster-agent.logging.level", "info")?
            .set_default("cluster-agent.logging.format", "json")?
            .set_default("cluster-agent.tls.enabled", false)
    }
}
