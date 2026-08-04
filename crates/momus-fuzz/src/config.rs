use serde::{Deserialize, Serialize};

/// Configuration for a fuzz run.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct FuzzConfig {
    /// Number of mutations to generate per input.
    #[serde(default = "default_iterations")]
    pub iterations: usize,
    /// Which mutators to apply (empty = all).
    #[serde(default)]
    pub mutators: Vec<String>,
    /// Base URL override.
    #[serde(default)]
    pub base_url: Option<String>,
    /// Request timeout in seconds.
    #[serde(default = "default_timeout")]
    pub timeout_secs: u64,
}

fn default_iterations() -> usize {
    1000
}

fn default_timeout() -> u64 {
    30
}

impl Default for FuzzConfig {
    fn default() -> Self {
        Self {
            iterations: default_iterations(),
            mutators: vec![],
            base_url: None,
            timeout_secs: default_timeout(),
        }
    }
}
