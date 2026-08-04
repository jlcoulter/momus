use serde::{Deserialize, Serialize};

/// Configuration for a contract validation run.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ContractConfig {
    /// Path to the API spec file (OpenAPI YAML/JSON or GraphQL SDL).
    pub spec_path: String,
    /// Base URL override.
    #[serde(default)]
    pub base_url: Option<String>,
    /// Whether to fail on undocumented endpoints.
    #[serde(default)]
    pub strict: bool,
    /// Request timeout in seconds.
    #[serde(default = "default_timeout")]
    pub timeout_secs: u64,
}

fn default_timeout() -> u64 {
    30
}

impl Default for ContractConfig {
    fn default() -> Self {
        Self {
            spec_path: String::new(),
            base_url: None,
            strict: false,
            timeout_secs: default_timeout(),
        }
    }
}
