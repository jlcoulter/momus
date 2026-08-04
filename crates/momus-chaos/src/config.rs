use serde::{Deserialize, Serialize};

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
}

fn default_interval() -> u64 {
    5
}

fn default_timeout() -> u64 {
    30
}

impl Default for ChaosConfig {
    fn default() -> Self {
        Self {
            experiments: vec![],
            base_url: None,
            interval_secs: default_interval(),
            timeout_secs: default_timeout(),
        }
    }
}
