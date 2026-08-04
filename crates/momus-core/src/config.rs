/// Generic configuration for running test plans.
///
/// This module provides a common configuration struct that can be loaded
/// from a TOML file and merged with CLI overrides. Each engine crate
/// (bench, fuzz, chaos, etc.) extends this with its own specific config.
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::path::PathBuf;

/// Common configuration for all momus commands.
///
/// This is the base config that can be loaded from a TOML file.
/// CLI flags override specific fields after loading.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RunConfig {
    /// Base URL for all requests (overrides the plan's base_url).
    #[serde(default)]
    pub base_url: Option<String>,

    /// Output directory for results.
    #[serde(default = "default_output")]
    pub output: PathBuf,

    /// Default headers sent with every request.
    #[serde(default)]
    pub headers: HashMap<String, String>,

    /// Request timeout in seconds.
    #[serde(default = "default_timeout")]
    pub timeout_secs: u64,
}

fn default_output() -> PathBuf {
    PathBuf::from("./output")
}

fn default_timeout() -> u64 {
    30
}

impl Default for RunConfig {
    fn default() -> Self {
        Self {
            base_url: None,
            output: default_output(),
            headers: HashMap::new(),
            timeout_secs: default_timeout(),
        }
    }
}

impl RunConfig {
    /// Load a RunConfig from a TOML file.
    ///
    /// Returns the default config if the file doesn't exist (optional config).
    pub fn load(path: &str) -> anyhow::Result<Self> {
        let content = std::fs::read_to_string(path)?;
        let config: RunConfig = toml::from_str(&content)?;
        Ok(config)
    }

    /// Load a RunConfig from a TOML file, returning default if file not found.
    pub fn load_optional(path: &str) -> Self {
        std::fs::read_to_string(path)
            .ok()
            .and_then(|content| toml::from_str(&content).ok())
            .unwrap_or_default()
    }

    /// Merge CLI overrides into this config.
    ///
    /// CLI values take precedence over file values.
    pub fn merge(&mut self, base_url: Option<String>, output: Option<PathBuf>) {
        if let Some(url) = base_url {
            self.base_url = Some(url);
        }
        if let Some(out) = output {
            self.output = out;
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parse_config_toml() {
        let toml = r#"
base_url = "http://localhost:8080"
output = "./test-output"
timeout_secs = 60

[headers]
Authorization = "Bearer test-token"
"#;
        let config: RunConfig = toml::from_str(toml).unwrap();
        assert_eq!(config.base_url, Some("http://localhost:8080".to_string()));
        assert_eq!(config.output, PathBuf::from("./test-output"));
        assert_eq!(config.timeout_secs, 60);
        assert_eq!(
            config.headers.get("Authorization").unwrap(),
            "Bearer test-token"
        );
    }

    #[test]
    fn parse_config_defaults() {
        let toml = r#"
base_url = "http://localhost:8080"
"#;
        let config: RunConfig = toml::from_str(toml).unwrap();
        assert_eq!(config.base_url, Some("http://localhost:8080".to_string()));
        assert_eq!(config.output, PathBuf::from("./output"));
        assert_eq!(config.timeout_secs, 30);
        assert!(config.headers.is_empty());
    }

    #[test]
    fn merge_overrides() {
        let mut config = RunConfig {
            base_url: Some("http://original".to_string()),
            output: PathBuf::from("./original"),
            headers: HashMap::new(),
            timeout_secs: 30,
        };
        config.merge(
            Some("http://override".to_string()),
            Some(PathBuf::from("./override")),
        );
        assert_eq!(config.base_url, Some("http://override".to_string()));
        assert_eq!(config.output, PathBuf::from("./override"));
    }

    #[test]
    fn merge_partial() {
        let mut config = RunConfig {
            base_url: Some("http://original".to_string()),
            output: PathBuf::from("./original"),
            headers: HashMap::new(),
            timeout_secs: 30,
        };
        config.merge(None, Some(PathBuf::from("./override")));
        assert_eq!(config.base_url, Some("http://original".to_string()));
        assert_eq!(config.output, PathBuf::from("./override"));
    }
}
