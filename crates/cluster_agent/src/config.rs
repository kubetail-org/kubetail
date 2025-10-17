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

            #[allow(clippy::collapsible_if)]
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

#[cfg(test)]
mod tests {
    use super::*;
    use serial_test::serial;
    use std::io::Write;
    use tempfile::NamedTempFile;

    fn create_config_file(content: &str, extension: &str) -> NamedTempFile {
        let mut file = tempfile::Builder::new()
            .suffix(extension)
            .tempfile()
            .expect("Failed to create temp file");

        file.write_all(content.as_bytes())
            .expect("Failed to write to file");

        file.flush().expect("Failed to flush file");

        file
    }

    #[tokio::test]
    #[serial]
    async fn test_parse_basic_yaml_config() {
        let config_content = r#"cluster-agent:
  addr: "127.0.0.1:8080"
  container-logs-dir: "/test/logs"
  logging:
    enabled: true
    level: "debug"
    format: "json"
  tls:
    enabled: false
"#;
        let file = create_config_file(config_content, ".yaml");

        let config = Config::parse(file.path(), vec![])
            .await
            .expect("Failed to parse config");

        assert_eq!(config.address.to_string(), "127.0.0.1:8080");
        assert_eq!(config.logs_dir, PathBuf::from("/test/logs"));
        assert!(config.logging.enabled);
        assert_eq!(config.logging.level, "debug");
        assert_eq!(config.logging.format, "json");
        assert!(!config.tls.enabled);
    }

    #[tokio::test]
    #[serial]
    async fn test_parse_shorthand_address() {
        let config_content = r#"cluster-agent:
  addr: ":9090"
  container-logs-dir: "/logs"
  logging:
    enabled: true
    level: "info"
    format: "json"
  tls:
    enabled: false
"#;
        let file = create_config_file(config_content, ".yaml");

        let config = Config::parse(file.path(), vec![])
            .await
            .expect("Failed to parse config");

        assert_eq!(config.address.to_string(), "[::]:9090");
    }

    #[tokio::test]
    #[serial]
    async fn test_parse_with_overrides() {
        let config_content = r#"cluster-agent:
  addr: "127.0.0.1:8080"
  container-logs-dir: "/logs"
  logging:
    enabled: true
    level: "info"
    format: "json"
  tls:
    enabled: false
"#;
        let file = create_config_file(config_content, ".yaml");

        let overrides = vec![
            ("addr".to_string(), ":5555".to_string()),
            ("logging.level".to_string(), "trace".to_string()),
        ];

        let config = Config::parse(file.path(), overrides)
            .await
            .expect("Failed to parse config");

        assert_eq!(config.address.to_string(), "[::]:5555");
        assert_eq!(config.logging.level, "trace");
    }

    #[tokio::test]
    #[serial]
    async fn test_parse_json_format() {
        let config_content = r#"{
  "cluster-agent": {
    "addr": "0.0.0.0:3000",
    "container-logs-dir": "/var/logs",
    "logging": {
      "enabled": false,
      "level": "error",
      "format": "text"
    },
    "tls": {
      "enabled": false
    }
  }
}"#;
        let file = create_config_file(config_content, ".json");

        let config = Config::parse(file.path(), vec![])
            .await
            .expect("Failed to parse config");

        assert_eq!(config.address.to_string(), "0.0.0.0:3000");
        assert!(!config.logging.enabled);
    }

    #[tokio::test]
    #[serial]
    async fn test_parse_toml_format() {
        let config_content = r#"[cluster-agent]
addr = ":7070"
container-logs-dir = "/toml/logs"

[cluster-agent.logging]
enabled = true
level = "info"
format = "json"

[cluster-agent.tls]
enabled = false
"#;
        let file = create_config_file(config_content, ".toml");

        let config = Config::parse(file.path(), vec![])
            .await
            .expect("Failed to parse config");

        assert_eq!(config.address.to_string(), "[::]:7070");
    }

    #[tokio::test]
    #[serial]
    async fn test_tls_enabled_without_cert_files_fails() {
        let config_content = r#"cluster-agent:
  addr: ":8080"
  container-logs-dir: "/logs"
  logging:
    enabled: true
    level: "info"
    format: "json"
  tls:
    enabled: true
"#;
        let file = create_config_file(config_content, ".yaml");

        let result = Config::parse(file.path(), vec![]).await;

        assert!(result.is_err());
        let err = result.unwrap_err();
        assert!(
            err.to_string().contains("Cert file and key file")
                || err.to_string().contains("should be supplied")
        );
    }

    #[tokio::test]
    #[serial]
    async fn test_tls_require_and_verify_without_ca_fails() {
        let config_content = r#"cluster-agent:
  addr: ":8080"
  container-logs-dir: "/logs"
  logging:
    enabled: true
    level: "info"
    format: "json"
  tls:
    enabled: true
    cert-file: "/path/to/cert.pem"
    key-file: "/path/to/key.pem"
    client-auth: "require-and-verify"
"#;
        let file = create_config_file(config_content, ".yaml");

        let result = Config::parse(file.path(), vec![]).await;

        assert!(result.is_err());
        let err = result.unwrap_err();
        assert!(
            err.to_string().contains("Trusted certificates")
                || err.to_string().contains("should be supplied")
        );
    }

    #[tokio::test]
    #[serial]
    async fn test_tls_enabled_with_all_files_succeeds() {
        let config_content = r#"cluster-agent:
  addr: ":8080"
  container-logs-dir: "/logs"
  logging:
    enabled: true
    level: "info"
    format: "json"
  tls:
    enabled: true
    cert-file: "/path/to/cert.pem"
    key-file: "/path/to/key.pem"
    ca-file: "/path/to/ca.pem"
    client-auth: "require-and-verify"
"#;
        let file = create_config_file(config_content, ".yaml");

        let config = Config::parse(file.path(), vec![])
            .await
            .expect("Failed to parse config");

        assert!(config.tls.enabled);
        assert_eq!(
            config.tls.cert_file,
            Some(PathBuf::from("/path/to/cert.pem"))
        );
        assert_eq!(config.tls.key_file, Some(PathBuf::from("/path/to/key.pem")));
        assert_eq!(config.tls.ca_file, Some(PathBuf::from("/path/to/ca.pem")));
        assert_eq!(
            config.tls.client_auth,
            Some("require-and-verify".to_string())
        );
    }

    #[tokio::test]
    #[serial]
    async fn test_parse_with_defaults() {
        let config_content = r#"cluster-agent:
  tls:
    enabled: false
"#;
        let file = create_config_file(config_content, ".yaml");

        let config = Config::parse(file.path(), vec![])
            .await
            .expect("Failed to parse config");

        assert_eq!(config.address.to_string(), "[::]:50051");
        assert_eq!(config.logs_dir, PathBuf::from("/var/log/containers"));
        assert!(config.logging.enabled);
        assert_eq!(config.logging.level, "info");
        assert_eq!(config.logging.format, "json");
    }

    #[tokio::test]
    #[serial]
    async fn test_invalid_file_format_fails() {
        let file = tempfile::Builder::new()
            .suffix(".invalid")
            .tempfile()
            .expect("Failed to create temp file");

        let result = Config::parse(file.path(), vec![]).await;

        assert!(result.is_err());
        let err = result.unwrap_err();
        assert!(err.to_string().contains("not of a registered file format"));
    }
}
