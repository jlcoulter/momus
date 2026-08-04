//! Momus — generic API test harness with a composable assertion AST.
//!
//! This is the umbrella crate that re-exports all Momus sub-crates
//! and provides convenience APIs for building and running test plans.
//!
//! # Quick Start
//!
//! ```rust
//! use momus::prelude::*;
//! use momus::builder::*;
//!
//! let plan = TestPlanBuilder::new("health check")
//!     .base_url("http://localhost:8080")
//!     .step(
//!         request("health")
//!             .get("/health")
//!             .assert(Assertion::Status(200))
//!             .assert(Assertion::valid_json())
//!             .build(),
//!     )
//!     .build();
//!
//! assert_eq!(plan.total_tests(), 1);
//! ```
//!
//! # Crate Structure
//!
//! | Crate | Purpose |
//! |-------|---------|
//! | `momus-core` | AST types, assertion evaluation, plan runner, template resolution |
//! | `momus-mock` | Configurable mock HTTP server |
//! | `momus-convert` | Convert API descriptions (OpenAPI, Postman, HAR, cURL, etc.) into test plans |
//! | `momus-bench` | Load testing engine — steady, max-throughput, soak modes |
//! | `momus-fuzz` | Payload mutation engine — boundary, encoding, type mismatch, cardinality |
//! | `momus-cli` | CLI binary (`momus run`, `momus validate`, `momus mock`, `momus bench`, `momus fuzz`, `momus convert`) |
//! | `momus` (this) | Umbrella crate with re-exports, builder, and convenience APIs |

pub mod builder;
pub mod prelude;

/// Convenience function to load a `TestPlan` from a JSON file.
///
/// # Errors
///
/// Returns an error if the file cannot be read or the JSON is invalid.
pub fn load_plan(path: &str) -> anyhow::Result<momus_core::ast::TestPlan> {
    let content = std::fs::read_to_string(path)?;
    let plan: momus_core::ast::TestPlan = serde_json::from_str(&content)?;
    Ok(plan)
}

/// Convenience function to load a `TestPlan` from a JSON string.
///
/// # Errors
///
/// Returns an error if the string is not valid JSON or does not match the `TestPlan` schema.
pub fn parse_plan(json: &str) -> anyhow::Result<momus_core::ast::TestPlan> {
    let plan: momus_core::ast::TestPlan = serde_json::from_str(json)?;
    Ok(plan)
}

/// Convenience function to validate a test plan without running it.
///
/// Returns `Ok(())` if the plan is structurally valid.
///
/// # Errors
///
/// Returns an error if the plan is invalid (e.g., empty name, no steps).
pub fn validate_plan(plan: &momus_core::ast::TestPlan) -> anyhow::Result<()> {
    if plan.name.is_empty() {
        anyhow::bail!("Plan name must not be empty");
    }
    if plan.steps.is_empty() && plan.setup.is_empty() && plan.teardown.is_empty() {
        anyhow::bail!("Plan must have at least one step, setup, or teardown");
    }
    Ok(())
}

pub use momus_bench;
pub use momus_chaos;
pub use momus_contract;
pub use momus_convert;
pub use momus_core;
pub use momus_diff;
pub use momus_fuzz;
pub use momus_guard;
pub use momus_mock;

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_load_plan_from_json() {
        let json = r#"{
            "name": "test",
            "base_url": "http://localhost",
            "steps": [
                {
                    "type": "request",
                    "name": "health",
                    "method": "GET",
                    "url": "/health",
                    "assert": [{ "status": 200 }]
                }
            ]
        }"#;

        let plan = parse_plan(json).unwrap();
        assert_eq!(plan.name, "test");
        assert_eq!(plan.total_tests(), 1);
    }

    #[test]
    fn test_validate_plan_ok() {
        let plan = momus_core::ast::TestPlan {
            name: "test".into(),
            base_url: "http://localhost".into(),
            default_headers: std::collections::HashMap::new(),
            steps: vec![momus_core::ast::Step::Request(
                momus_core::ast::RequestStep {
                    name: "r1".into(),
                    method: momus_core::ast::Method::Get,
                    url: "/".into(),
                    headers: std::collections::HashMap::new(),
                    body: None,
                    assert: vec![],
                    save_as: String::new(),
                    soft_fail: false,
                },
            )],
            setup: vec![],
            teardown: vec![],
        };
        assert!(validate_plan(&plan).is_ok());
    }

    #[test]
    fn test_validate_plan_empty_name() {
        let plan = momus_core::ast::TestPlan {
            name: "".into(),
            base_url: "http://localhost".into(),
            default_headers: std::collections::HashMap::new(),
            steps: vec![],
            setup: vec![],
            teardown: vec![],
        };
        assert!(validate_plan(&plan).is_err());
    }

    #[test]
    fn test_validate_plan_no_steps() {
        let plan = momus_core::ast::TestPlan {
            name: "empty".into(),
            base_url: "http://localhost".into(),
            default_headers: std::collections::HashMap::new(),
            steps: vec![],
            setup: vec![],
            teardown: vec![],
        };
        assert!(validate_plan(&plan).is_err());
    }
}
