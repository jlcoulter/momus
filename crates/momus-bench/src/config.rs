use serde::{Deserialize, Serialize};

/// Benchmark execution mode.
#[derive(Debug, Clone, Serialize, Deserialize)]
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
}

fn default_timeout() -> u64 {
    30
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
        }
    }
}
