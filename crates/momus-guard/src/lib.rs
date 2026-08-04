//! Security scanning — check for missing auth, CORS misconfig, info leaks, exposed endpoints.
//!
//! Runs a test plan and inspects responses for common security issues:
//!
//! - Missing or weak authentication headers
//! - CORS misconfiguration (permissive origins, credentials with wildcard)
//! - Information leakage (stack traces, SQL errors, path disclosure in error bodies)
//! - Exposed internal endpoints
//! - Missing security headers (HSTS, CSP, X-Content-Type-Options)
//!
//! # Example
//!
//! ```rust,ignore
//! use momus_guard::{GuardConfig, run_guard};
//! use momus_core::ast::TestPlan;
//!
//! let plan: TestPlan = serde_json::from_str(r#"{"name":"test","base_url":"http://localhost","steps":[]}"#).unwrap();
//! let config = GuardConfig::default();
//! let report = run_guard(&plan, &config).await.unwrap();
//! println!("Issues found: {}", report.issues.len());
//! ```

pub mod config;
pub mod report;
pub mod runner;

pub use config::*;
pub use report::*;
pub use runner::*;
