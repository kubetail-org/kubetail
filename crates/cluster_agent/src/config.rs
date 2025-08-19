use std::{error::Error, io, net::SocketAddr, path::PathBuf};

use regex::Regex;
use serde::Deserialize;
use tokio::fs;

#[derive(Debug)]
pub struct Config {
    pub address: SocketAddr,
    pub logging: LoggingConfig,
    pub tls: TlsConfig,
}

#[derive(Deserialize, Debug)]
struct ConfigInternal {
    #[serde(rename(deserialize = "addr"))]
    address: String,
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
    pub cert_file: PathBuf,

    #[serde(rename(deserialize = "key-file"))]
    pub key_file: PathBuf,

    #[serde(rename(deserialize = "ca-file"))]
    pub ca_file: PathBuf,

    #[serde(rename(deserialize = "client-auth"))]
    pub client_auth: PathBuf,
}

impl Config {
    pub async fn parse(path: &PathBuf) -> Result<Self, Box<dyn Error + 'static>> {
        let config = fs::read_to_string(path).await?;
        let full_config: FullConfig = serde_yml::from_str(&config)?;

        let logging = full_config.cluster_agent.logging;

        if logging.enabled && (logging.level.is_empty() || logging.format.is_empty()) {
            return Err(Box::new(io::Error::new(
                io::ErrorKind::InvalidInput,
                "Logging level and format should be supplied when logging is enabled",
            )));
        }

        let tls = full_config.cluster_agent.tls;

        if tls.enabled
            && (tls.cert_file.as_os_str().is_empty() || tls.key_file.as_os_str().is_empty())
        {
            return Err(Box::new(io::Error::new(
                io::ErrorKind::InvalidInput,
                "Cert file and key file should be supplied when tls is enabled",
            )));
        }

        Ok(Self {
            address: Self::parse_address(&full_config.cluster_agent.address)?,
            logging,
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
}
