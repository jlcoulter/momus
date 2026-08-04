//! Contract testing — validate API responses against OpenAPI/GraphQL schemas.
//!
//! Runs a test plan and checks each response against its declared schema.
//! Reports compliance percentage, missing fields, type mismatches, and
//! undocumented fields.
//!
//! # Example
//!
//! ```rust,ignore
//! use momus_contract::{ContractConfig, run_contract};
//! use momus_core::ast::TestPlan;
//!
//! let plan: TestPlan = serde_json::from_str(r#"{"name":"test","base_url":"http://localhost","steps":[]}"#).unwrap();
//! let config = ContractConfig {
//!     spec_path: "openapi.yaml".into(),
//!     ..Default::default()
//! };
//! let report = run_contract(&plan, &config).await.unwrap();
//! println!("Compliance: {:.1}%", report.compliance_pct);
//! ```

pub mod config;
pub mod report;
pub mod runner;

pub use config::*;
pub use report::*;
pub use runner::*;
