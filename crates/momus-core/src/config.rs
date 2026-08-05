/// Generic configuration for running test plans.
///
/// This module provides a common configuration struct that can be loaded
/// from a TOML file and merged with CLI overrides. Each engine crate
/// (bench, fuzz, chaos, etc.) extends this with its own specific config.
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::path::PathBuf;

// ---------------------------------------------------------------------------
// Top-level config — a single TOML file with sections per crate
// ---------------------------------------------------------------------------

/// Top-level configuration loaded from `config.toml`.
///
/// Each crate's config lives in its own TOML section. CLI flags override
/// the corresponding fields after loading.
#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct MomusConfig {
    /// Global defaults shared by all commands.
    #[serde(default)]
    pub global: GlobalConfig,
    /// `momus run` / `momus validate` settings.
    #[serde(default)]
    pub run: RunConfig,
    /// `momus bench` settings.
    #[serde(default)]
    pub bench: BenchConfig,
    /// `momus fuzz` settings.
    #[serde(default)]
    pub fuzz: FuzzConfig,
    /// `momus chaos` settings.
    #[serde(default)]
    pub chaos: ChaosConfig,
    /// `momus contract` settings.
    #[serde(default)]
    pub contract: ContractConfig,
    /// `momus guard` settings.
    #[serde(default)]
    pub guard: GuardConfig,
    /// `momus diff` settings.
    #[serde(default)]
    pub diff: DiffConfig,
    /// `momus plan` settings.
    #[serde(default)]
    pub plan: PlanConfig,
}

impl MomusConfig {
    /// Load a `MomusConfig` from a TOML file path.
    ///
    /// Returns the default config (all sections empty) if the file doesn't exist.
    pub fn load(path: &str) -> anyhow::Result<Self> {
        let content = std::fs::read_to_string(path)?;
        let config: MomusConfig = toml::from_str(&content)?;
        Ok(config)
    }

    /// Load a `MomusConfig` from a TOML file, returning default if file not found.
    /// Logs a warning if the file exists but cannot be parsed.
    pub fn load_optional(path: &str) -> Self {
        match std::fs::read_to_string(path) {
            Ok(content) => match toml::from_str(&content) {
                Ok(config) => config,
                Err(e) => {
                    tracing::warn!(
                        "Config file '{}' has invalid TOML: {}. Using defaults.",
                        path,
                        e
                    );
                    Self::default()
                }
            },
            Err(_) => Self::default(),
        }
    }
}

/// Global defaults inherited by all commands unless overridden by a
/// command-specific section or CLI flag.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct GlobalConfig {
    /// Base URL for all requests (overrides the plan's base_url).
    #[serde(default)]
    pub base_url: Option<String>,
    /// Default headers sent with every request.
    #[serde(default)]
    pub headers: HashMap<String, String>,
    /// Request timeout in seconds.
    #[serde(default = "default_timeout")]
    pub timeout_secs: u64,
}

impl Default for GlobalConfig {
    fn default() -> Self {
        Self {
            base_url: None,
            headers: HashMap::new(),
            timeout_secs: default_timeout(),
        }
    }
}

// ---------------------------------------------------------------------------
// Run / Validate config
// ---------------------------------------------------------------------------

/// Common configuration for `momus run` and `momus validate`.
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

// ---------------------------------------------------------------------------
// Bench config
// ---------------------------------------------------------------------------

/// Benchmark execution mode.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(tag = "type")]
pub enum BenchMode {
    /// Fixed concurrency for a fixed duration.
    Steady {
        /// Number of concurrent workers.
        concurrency: usize,
        /// Duration in seconds (0 = one-shot, run each step once).
        duration_secs: u64,
    },
    /// Ramp concurrency upward until error rate or latency threshold is breached.
    MaxThroughput {
        /// Starting concurrency.
        min_concurrency: usize,
        /// Maximum concurrency to try.
        max_concurrency: usize,
        /// Concurrency increment per step.
        step: usize,
        /// Duration per step in seconds.
        step_duration_secs: u64,
        /// Error rate threshold (0.0–1.0) that triggers stop.
        max_error_rate: f64,
        /// Latency P99 threshold in ms that triggers stop.
        max_p99_ms: u64,
    },
    /// Sustained load at fixed concurrency for hours.
    Soak {
        /// Number of concurrent workers.
        concurrency: usize,
        /// Duration in seconds.
        duration_secs: u64,
    },
}

/// Configuration for a benchmark run.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct BenchConfig {
    /// Execution mode.
    pub mode: BenchMode,
    /// Number of warmup requests before recording (0 = no warmup).
    #[serde(default)]
    pub warmup_requests: usize,
    /// Request timeout in seconds.
    #[serde(default = "default_timeout")]
    pub timeout_secs: u64,
    /// Base URL override (overrides the plan's base_url).
    #[serde(default)]
    pub base_url: Option<String>,
    /// Output directory for results.
    #[serde(default = "default_output")]
    pub output: PathBuf,
}

impl Default for BenchConfig {
    fn default() -> Self {
        Self {
            mode: BenchMode::Steady {
                concurrency: 10,
                duration_secs: 30,
            },
            warmup_requests: 0,
            timeout_secs: default_timeout(),
            base_url: None,
            output: default_output(),
        }
    }
}

// ---------------------------------------------------------------------------
// Fuzz config
// ---------------------------------------------------------------------------

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
    /// Output directory for results.
    #[serde(default = "default_output")]
    pub output: PathBuf,
}

fn default_iterations() -> usize {
    1000
}

impl Default for FuzzConfig {
    fn default() -> Self {
        Self {
            iterations: default_iterations(),
            mutators: vec![],
            base_url: None,
            timeout_secs: default_timeout(),
            output: default_output(),
        }
    }
}

// ---------------------------------------------------------------------------
// Chaos config
// ---------------------------------------------------------------------------

/// A single chaos experiment to run.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub enum ChaosExperiment {
    // -- Network faults ------------------------------------------------------
    /// Inject artificial latency into requests to a specific endpoint.
    NetworkLatency {
        /// Endpoint path pattern (e.g. "/api/slow").
        endpoint: String,
        /// Additional delay in milliseconds.
        delay_ms: u64,
        /// How long the fault is active in seconds.
        duration_secs: u64,
    },

    /// Simulate connection resets for a percentage of requests.
    ConnectionReset {
        /// Endpoint path pattern.
        endpoint: String,
        /// Percentage of requests to reset (0–100).
        reset_pct: u8,
        /// How long the fault is active in seconds.
        duration_secs: u64,
    },

    /// Drop a percentage of requests (simulate packet loss).
    PacketLoss {
        /// Endpoint path pattern.
        endpoint: String,
        /// Percentage of requests to drop (0–100).
        drop_pct: u8,
        /// How long the fault is active in seconds.
        duration_secs: u64,
    },

    // -- Service faults ------------------------------------------------------
    /// Return a specific HTTP status code for a matching endpoint.
    ServiceError {
        /// Endpoint path pattern.
        endpoint: String,
        /// HTTP status code to return.
        status: u16,
        /// How long the fault is active in seconds.
        duration_secs: u64,
    },

    /// Simulate a downstream service being unreachable.
    ServiceDown {
        /// Endpoint path pattern.
        endpoint: String,
        /// How long the fault is active in seconds.
        duration_secs: u64,
    },

    // -- Resource faults -----------------------------------------------------
    /// Simulate CPU pressure (busy loop on N cores).
    CpuPressure {
        /// Number of cores to saturate.
        cores: usize,
        /// Duration in seconds.
        duration_secs: u64,
    },

    /// Simulate memory pressure (allocate N MB).
    MemoryPressure {
        /// Megabytes to allocate.
        mb: usize,
        /// Duration in seconds.
        duration_secs: u64,
    },

    // -- State faults --------------------------------------------------------
    /// Simulate clock skew (N seconds ahead/behind).
    ClockSkew {
        /// Offset in seconds (positive = ahead, negative = behind).
        offset_secs: i64,
        /// Duration in seconds.
        duration_secs: u64,
    },
}

/// Configuration for a chaos run.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ChaosConfig {
    /// List of experiments to run (sequentially).
    #[serde(default)]
    pub experiments: Vec<ChaosExperiment>,
    /// Base URL override.
    #[serde(default)]
    pub base_url: Option<String>,
    /// How long to wait between experiments (seconds).
    #[serde(default = "default_interval")]
    pub interval_secs: u64,
    /// Request timeout in seconds.
    #[serde(default = "default_timeout")]
    pub timeout_secs: u64,
    /// Output directory for results.
    #[serde(default = "default_output")]
    pub output: PathBuf,
}

fn default_interval() -> u64 {
    5
}

impl Default for ChaosConfig {
    fn default() -> Self {
        Self {
            experiments: vec![],
            base_url: None,
            interval_secs: default_interval(),
            timeout_secs: default_timeout(),
            output: default_output(),
        }
    }
}

// ---------------------------------------------------------------------------
// Contract config
// ---------------------------------------------------------------------------

/// Configuration for a contract validation run.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ContractConfig {
    /// Path to the API spec file (OpenAPI YAML/JSON or GraphQL SDL).
    #[serde(default)]
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
    /// Output directory for results.
    #[serde(default = "default_output")]
    pub output: PathBuf,
}

impl Default for ContractConfig {
    fn default() -> Self {
        Self {
            spec_path: String::new(),
            base_url: None,
            strict: false,
            timeout_secs: default_timeout(),
            output: default_output(),
        }
    }
}

// ---------------------------------------------------------------------------
// Guard config
// ---------------------------------------------------------------------------

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
    /// Output directory for results.
    #[serde(default = "default_output")]
    pub output: PathBuf,
}

fn default_true() -> bool {
    true
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
            output: default_output(),
        }
    }
}

// ---------------------------------------------------------------------------
// Diff config
// ---------------------------------------------------------------------------

/// Configuration for a diff run.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DiffConfig {
    /// Baseline environment URL (e.g. production).
    #[serde(default)]
    pub baseline_url: String,
    /// Target environment URL (e.g. staging, new deployment).
    #[serde(default)]
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
    /// Output directory for results.
    #[serde(default = "default_output")]
    pub output: PathBuf,
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
            output: default_output(),
        }
    }
}

// ---------------------------------------------------------------------------
// Plan config
// ---------------------------------------------------------------------------

/// Configuration for `momus plan`.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PlanConfig {
    /// Output directory for the plan display (default: ./output).
    #[serde(default = "default_output")]
    pub output: PathBuf,
}

impl Default for PlanConfig {
    fn default() -> Self {
        Self {
            output: default_output(),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parse_run_config_toml() {
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
    fn parse_run_config_defaults() {
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

    // -- MomusConfig (multi-section) tests -----------------------------------

    #[test]
    fn parse_momus_config_empty() {
        let toml = "";
        let config: MomusConfig = toml::from_str(toml).unwrap();
        assert!(config.global.base_url.is_none());
        assert!(config.run.base_url.is_none());
        assert!(config.bench.base_url.is_none());
        assert!(config.fuzz.base_url.is_none());
        assert!(config.chaos.base_url.is_none());
        assert!(config.contract.base_url.is_none());
        assert!(config.guard.base_url.is_none());
        assert!(config.diff.baseline_url.is_empty());
    }

    #[test]
    fn parse_momus_config_full() {
        let toml = r#"
[global]
base_url = "http://global:8080"
timeout_secs = 60

[run]
output = "./run-output"

[bench]
warmup_requests = 100
mode = { type = "Steady", concurrency = 20, duration_secs = 60 }

[fuzz]
iterations = 5000

[chaos]
interval_secs = 10

[contract]
spec_path = "./api.yaml"
strict = true

[guard]
check_headers = false

[diff]
baseline_url = "https://prod.example.com"
target_url = "https://staging.example.com"
"#;
        let config: MomusConfig = toml::from_str(toml).unwrap();
        assert_eq!(
            config.global.base_url,
            Some("http://global:8080".to_string())
        );
        assert_eq!(config.global.timeout_secs, 60);
        assert_eq!(config.run.output, PathBuf::from("./run-output"));
        assert_eq!(config.bench.warmup_requests, 100);
        assert_eq!(config.fuzz.iterations, 5000);
        assert_eq!(config.chaos.interval_secs, 10);
        assert_eq!(config.contract.spec_path, "./api.yaml");
        assert!(config.contract.strict);
        assert!(!config.guard.check_headers);
        assert_eq!(config.diff.baseline_url, "https://prod.example.com");
        assert_eq!(config.diff.target_url, "https://staging.example.com");
    }

    #[test]
    fn parse_momus_config_global_fallback() {
        // When a section is missing, its defaults apply.
        let toml = r#"
[global]
base_url = "http://global:8080"
"#;
        let config: MomusConfig = toml::from_str(toml).unwrap();
        assert_eq!(
            config.global.base_url,
            Some("http://global:8080".to_string())
        );
        assert_eq!(config.run.output, PathBuf::from("./output"));
        assert_eq!(config.bench.warmup_requests, 0);
        assert_eq!(config.fuzz.iterations, 1000);
        assert_eq!(config.chaos.interval_secs, 5);
        assert!(!config.contract.strict);
        assert!(config.guard.check_headers);
        assert!(config.diff.baseline_url.is_empty());
    }
}
