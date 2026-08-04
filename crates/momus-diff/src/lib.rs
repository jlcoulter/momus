//! Regression/diff testing — compare API responses between environments.
//!
//! Runs the same test plan against two environments (e.g. staging vs production)
//! and reports differences in responses: body changes, new/missing fields,
//! status code changes, header differences.
//!
//! # Example
//!
//! ```rust,ignore
//! use momus_diff::{DiffConfig, run_diff};
//! use momus_core::ast::TestPlan;
//!
//! let plan: TestPlan = serde_json::from_str(r#"{"name":"test","base_url":"http://localhost","steps":[]}"#).unwrap();
//! let config = DiffConfig {
//!     baseline_url: "https://api-v1.example.com".into(),
//!     target_url: "https://api-v2.example.com".into(),
//!     ..Default::default()
//! };
//! let report = run_diff(&plan, &config).await.unwrap();
//! println!("Changes: {} added, {} removed, {} modified", report.fields_added, report.fields_removed, report.fields_modified);
//! ```

pub mod config;
pub mod report;
pub mod runner;

pub use config::*;
pub use report::*;
pub use runner::*;
