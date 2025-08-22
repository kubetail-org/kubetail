use std::{
    error::Error,
    io,
    net::{AddrParseError, SocketAddr},
    path::{Path, PathBuf},
};

use config::{File, FileFormat, builder::DefaultState};
use regex::Regex;
use serde::Deserialize;
use tokio::fs;

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
    pub async fn parse(
        path: &Path,
        overrides: Vec<(String, String)>,
    ) -> Result<Self, Box<dyn Error + 'static>> {
        let config_content = fs::read_to_string(path).await?;
        let config_content = subst::substitute(&config_content, &subst::Env)?;
        let format = Self::get_format(path)?;

        let mut settings = Self::builder_with_defaults()?;

        for (config_key, config_value) in overrides {
            settings =
                settings.set_override("cluster-agent.".to_owned() + &config_key, config_value)?;
        }

        let settings = settings
            .add_source(File::from_str(&config_content, format))
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

            if let Some(client_auth) = &tls.client_auth
                && client_auth == "require-and-verify"
                && tls.ca_file.is_none()
            {
                return Err(Box::new(io::Error::new(
                    io::ErrorKind::InvalidInput,
                    "Trusted certificates should be supplied for require-and-verify",
                )));
            }
        }

        Ok(Self {
            address: Self::parse_address(&full_config.cluster_agent.address)?,
            logs_dir: full_config.cluster_agent.logs_dir,
            logging: full_config.cluster_agent.logging,
            tls,
        })
    }

    fn parse_address(address: &str) -> Result<SocketAddr, AddrParseError> {
        let shorthand_regex = Regex::new(r"^:(?<socket>\d+)$").unwrap();

        if let Some(captures) = shorthand_regex.captures(address) {
            let socket_str = format!("[::]:{}", &captures["socket"]);

            return socket_str.parse();
        }

        address.parse()
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

    fn get_format(path: &Path) -> Result<FileFormat, Box<io::Error>> {
        let extension = path
            .extension()
            .map(|os_str| os_str.to_string_lossy().to_lowercase());

        match extension.as_deref() {
            Some("toml") => Ok(FileFormat::Toml),
            Some("json") => Ok(FileFormat::Json),
            Some("yaml" | "yml") => Ok(FileFormat::Yaml),
            Some("ron") => Ok(FileFormat::Ron),
            Some("ini") => Ok(FileFormat::Ini),
            Some("json5") => Ok(FileFormat::Json5),
            _ => Err(Box::new(io::Error::new(
                io::ErrorKind::NotFound,
                format!(
                    "configuration file \"{}\" is not of a registered file format",
                    path.to_string_lossy()
                ),
            ))),
        }
    }
}
