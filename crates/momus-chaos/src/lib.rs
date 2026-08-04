//! Chaos engineering engine for Momus test plans.
//!
//! Injects infrastructure-level faults into a running system and verifies
//! that the system self-heals, degrades gracefully, or fails safely.
//!
//! # Experiment Types
//!
//! - **Network faults** — latency injection, packet loss, connection resets
//! - **Service faults** — kill processes, crash dependencies, return 5xx from upstream
//! - **Resource faults** — CPU pressure, memory exhaustion, disk full
//! - **State faults** — clock skew, corrupted data, leader election disruption
//!
//! # Example
//!
//! ```rust,ignore
//! use momus_chaos::{ChaosConfig, ChaosExperiment, run_chaos};
//! use momus_core::ast::TestPlan;
//!
//! let plan: TestPlan = serde_json::from_str(r#"{"name":"test","base_url":"http://localhost","steps":[]}"#).unwrap();
//! let config = ChaosConfig {
//!     experiments: vec![
//!         ChaosExperiment::NetworkLatency {
//!             endpoint: "/api/slow".into(),
//!             delay_ms: 5000,
//!             duration_secs: 30,
//!         },
//!     ],
//!     ..Default::default()
//! };
//! let report = run_chaos(&plan, &config).await.unwrap();
//! println!("{}", report);
//! ```

pub mod config;
pub mod experiments;
pub mod report;
pub mod runner;

pub use config::*;
pub use experiments::*;
pub use report::*;
pub use runner::*;
