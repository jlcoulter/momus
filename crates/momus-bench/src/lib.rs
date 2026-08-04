//! Load testing engine for Momus test plans.
//!
//! Takes a `TestPlan` and runs it under load. Three modes:
//!
//! - **Steady** — fixed concurrency for a fixed duration (or one-shot)
//! - **Max-throughput** — ramp concurrency upward until error rate or latency threshold is breached
//! - **Soak** — sustained load at fixed concurrency for hours
//!
//! # Example
//!
//! ```rust,ignore
//! use momus_bench::{BenchConfig, BenchMode, run_bench};
//! use momus_core::ast::TestPlan;
//!
//! let plan: TestPlan = serde_json::from_str(r#"{"name":"test","base_url":"http://localhost","steps":[]}"#).unwrap();
//! let config = BenchConfig {
//!     mode: BenchMode::Steady { concurrency: 10, duration_secs: 30 },
//!     warmup_requests: 5,
//!     ..Default::default()
//! };
//! let report = run_bench(&plan, &config).await.unwrap();
//! println!("P50: {}ms, P99: {}ms", report.p50_ms, report.p99_ms);
//! ```

pub mod config;
pub mod modes;
pub mod report;
pub mod runner;

pub use config::*;
pub use modes::*;
pub use report::*;
pub use runner::*;
