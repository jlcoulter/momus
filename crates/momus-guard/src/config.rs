use serde::{Deserialize, Serialize};

/// Configuration for a security scan.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct GuardConfig {
    /// Base URL override.
    #[serde(default)]
    pub base_url: Option<String>,
    /// Whether to check for missing security headers.
    #[serde(default = "default_true")]
    pub check_headers: bool,
    /// Whether to check CORS configuration.
    #[serde(default = "default_true")]
    pub check_cors: bool,
    /// Whether to check for information leakage in error responses.
    #[serde(default = "default_true")]
    pub check_leaks: bool,
    /// Whether to check for exposed internal endpoints.
    #[serde(default = "default_true")]
    pub check_exposed: bool,
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

impl Default for GuardConfig {
    fn default() -> Self {
        Self {
            base_url: None,
            check_headers: true,
            check_cors: true,
            check_leaks: true,
            check_exposed: true,
            timeout_secs: default_timeout(),
        }
    }
}
