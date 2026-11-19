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
pub struct LoggingConfig {
    pub enabled: bool,
    pub level: String,
    pub format: String,
}

#[derive(Deserialize, Debug)]
pub struct TlsConfig {
    #[serde(rename(deserialize = "cert-file"))]
    pub cert_file: PathBuf,

    #[serde(rename(deserialize = "key-file"))]
    pub key_file: PathBuf,

    #[serde(rename(deserialize = "ca-file"))]
    pub ca_file: PathBuf,

    #[serde(rename(deserialize = "allowed-names"), default)]
    pub allowed_names: Vec<String>,
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
            settings = settings.set_override(config_key, config_value)?;
        }

        let settings = settings
            .add_source(File::from_str(&config_content, format))
            .build()?;

        let cfg: ConfigInternal = settings.try_deserialize()?;

        Ok(Self {
            address: Self::parse_address(&cfg.address)?,
            logs_dir: cfg.logs_dir,
            logging: cfg.logging,
            tls: cfg.tls,
        })
    }

    fn parse_address(address: &str) -> Result<SocketAddr, AddrParseError> {
        let shorthand_regex = Regex::new(r"^:(?<socket>\d+)$")
            .expect("Invalid shorthand regex pattern - this is a developer error");

        if let Some(captures) = shorthand_regex.captures(address) {
            let socket_str = format!("[::]:{}", &captures["socket"]);

            return socket_str.parse();
        }

        address.parse()
    }

    fn builder_with_defaults() -> Result<config::ConfigBuilder<DefaultState>, config::ConfigError> {
        config::Config::builder()
            .set_default("addr", "[::]:50051")?
            .set_default("container-logs-dir", "/var/log/containers")?
            .set_default("logging.enabled", true)?
            .set_default("logging.level", "info")?
            .set_default("logging.format", "json")
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

    /// Indented YAML body for the `tls:` block; the caller writes the `tls:`
    /// key.
    const VALID_TLS_BLOCK: &str = r#"  cert-file: "/path/to/cert.pem"
  key-file: "/path/to/key.pem"
  ca-file: "/path/to/ca.pem"
"#;

    /// Standard non-TLS scaffolding (addr/logs-dir/logging) so each test only
    /// has to vary what it cares about.
    const BASE_YAML: &str = "addr: \":8080\"\ncontainer-logs-dir: \"/logs\"\nlogging:\n  enabled: true\n  level: \"info\"\n  format: \"json\"\n";

    fn full_yaml() -> String {
        format!("{BASE_YAML}tls:\n{VALID_TLS_BLOCK}")
    }

    #[tokio::test]
    #[serial]
    async fn test_parse_basic_yaml_config() {
        let content = format!(
            "addr: \"127.0.0.1:8080\"\ncontainer-logs-dir: \"/test/logs\"\nlogging:\n  enabled: true\n  level: \"debug\"\n  format: \"json\"\ntls:\n{VALID_TLS_BLOCK}"
        );
        let file = create_config_file(&content, ".yaml");
        let config = Config::parse(file.path(), vec![])
            .await
            .expect("parse config");
        assert_eq!(config.address.to_string(), "127.0.0.1:8080");
        assert_eq!(config.logs_dir, PathBuf::from("/test/logs"));
        assert!(config.logging.enabled);
        assert_eq!(config.logging.level, "debug");
        assert_eq!(config.logging.format, "json");
    }

    #[tokio::test]
    #[serial]
    async fn test_parse_shorthand_address() {
        let content = format!(
            "addr: \":9090\"\ncontainer-logs-dir: \"/logs\"\nlogging:\n  enabled: true\n  level: \"info\"\n  format: \"json\"\ntls:\n{VALID_TLS_BLOCK}"
        );
        let file = create_config_file(&content, ".yaml");
        let config = Config::parse(file.path(), vec![])
            .await
            .expect("parse config");
        assert_eq!(config.address.to_string(), "[::]:9090");
    }

    #[tokio::test]
    #[serial]
    async fn test_parse_with_overrides() {
        let file = create_config_file(&full_yaml(), ".yaml");
        let overrides = vec![
            ("addr".to_string(), ":5555".to_string()),
            ("logging.level".to_string(), "trace".to_string()),
        ];
        let config = Config::parse(file.path(), overrides)
            .await
            .expect("parse config");
        assert_eq!(config.address.to_string(), "[::]:5555");
        assert_eq!(config.logging.level, "trace");
    }

    #[tokio::test]
    #[serial]
    async fn test_parse_json_format() {
        let content = r#"{
  "addr": "0.0.0.0:3000",
  "container-logs-dir": "/var/logs",
  "logging": {"enabled": false, "level": "error", "format": "json"},
  "tls": {
    "cert-file": "/path/to/cert.pem",
    "key-file": "/path/to/key.pem",
    "ca-file": "/path/to/ca.pem"
  }
}"#;
        let file = create_config_file(content, ".json");
        let config = Config::parse(file.path(), vec![])
            .await
            .expect("parse config");
        assert_eq!(config.address.to_string(), "0.0.0.0:3000");
        assert!(!config.logging.enabled);
    }

    #[tokio::test]
    #[serial]
    async fn test_parse_toml_format() {
        let content = r#"addr = ":7070"
container-logs-dir = "/toml/logs"

[logging]
enabled = true
level = "info"
format = "json"

[tls]
cert-file = "/path/to/cert.pem"
key-file = "/path/to/key.pem"
ca-file = "/path/to/ca.pem"
"#;
        let file = create_config_file(content, ".toml");
        let config = Config::parse(file.path(), vec![])
            .await
            .expect("parse config");
        assert_eq!(config.address.to_string(), "[::]:7070");
    }

    #[tokio::test]
    #[serial]
    async fn test_loaded_tls_files() {
        let file = create_config_file(&full_yaml(), ".yaml");
        let config = Config::parse(file.path(), vec![])
            .await
            .expect("parse config");
        assert_eq!(config.tls.cert_file, PathBuf::from("/path/to/cert.pem"));
        assert_eq!(config.tls.key_file, PathBuf::from("/path/to/key.pem"));
        assert_eq!(config.tls.ca_file, PathBuf::from("/path/to/ca.pem"));
    }

    #[tokio::test]
    #[serial]
    async fn test_missing_tls_file_fails() {
        let all = ["cert-file", "key-file", "ca-file"];
        for missing in &all {
            let tls_lines: String = all
                .iter()
                .filter(|k| **k != *missing)
                .map(|k| format!("  {k}: \"/path/to/{k}.pem\"\n"))
                .collect();
            let content = format!("{BASE_YAML}tls:\n{tls_lines}");
            let file = create_config_file(&content, ".yaml");
            let err = Config::parse(file.path(), vec![])
                .await
                .expect_err(&format!("expected error when {missing} is missing"));
            let msg = err.to_string();
            assert!(
                msg.contains(missing),
                "error for missing {missing} should mention it; got: {msg}"
            );
        }
    }

    #[tokio::test]
    #[serial]
    async fn test_parse_with_defaults() {
        let content = format!("tls:\n{VALID_TLS_BLOCK}");
        let file = create_config_file(&content, ".yaml");
        let config = Config::parse(file.path(), vec![])
            .await
            .expect("parse config");
        assert_eq!(config.address.to_string(), "[::]:50051");
        assert_eq!(config.logs_dir, PathBuf::from("/var/log/containers"));
        assert!(config.logging.enabled);
        assert_eq!(config.logging.level, "info");
        assert_eq!(config.logging.format, "json");
    }

    #[tokio::test]
    #[serial]
    async fn test_allowed_names_defaults_to_empty() {
        let file = create_config_file(&full_yaml(), ".yaml");
        let config = Config::parse(file.path(), vec![])
            .await
            .expect("parse config");
        assert!(config.tls.allowed_names.is_empty());
    }

    #[tokio::test]
    #[serial]
    async fn test_allowed_names_parses_list() {
        let content = format!(
            "tls:\n{VALID_TLS_BLOCK}  allowed-names:\n    - \"kubetail-cluster-api\"\n    - \"other-trusted-proxy\"\n"
        );
        let file = create_config_file(&content, ".yaml");
        let config = Config::parse(file.path(), vec![])
            .await
            .expect("parse config");
        assert_eq!(
            config.tls.allowed_names,
            vec![
                "kubetail-cluster-api".to_string(),
                "other-trusted-proxy".to_string(),
            ]
        );
    }

    #[tokio::test]
    #[serial]
    async fn test_allowed_names_empty_list_when_explicitly_set() {
        let content = format!("tls:\n{VALID_TLS_BLOCK}  allowed-names: []\n");
        let file = create_config_file(&content, ".yaml");
        let config = Config::parse(file.path(), vec![])
            .await
            .expect("parse config");
        assert!(config.tls.allowed_names.is_empty());
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
        let err = result.expect_err("config parse should fail for unknown extension");
        assert!(err.to_string().contains("not of a registered file format"));
    }
}
