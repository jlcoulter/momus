use serde::{Deserialize, Serialize};

/// Configuration for a diff run.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DiffConfig {
    /// Baseline environment URL (e.g. production).
    pub baseline_url: String,
    /// Target environment URL (e.g. staging, new deployment).
    pub target_url: String,
    /// Whether to diff response headers.
    #[serde(default = "default_true")]
    pub diff_headers: bool,
    /// Whether to diff response bodies.
    #[serde(default = "default_true")]
    pub diff_bodies: bool,
    /// Whether to diff status codes.
    #[serde(default = "default_true")]
    pub diff_status: bool,
    /// Request timeout in seconds.
    #[serde(default = "default_timeout")]
    pub timeout_secs: u64,
}

fn default_true() -> bool {
    true
}

fn default_timeout() -> u64 {
    30
}

impl Default for DiffConfig {
    fn default() -> Self {
        Self {
            baseline_url: String::new(),
            target_url: String::new(),
            diff_headers: true,
            diff_bodies: true,
            diff_status: true,
            timeout_secs: default_timeout(),
        }
    }
}
