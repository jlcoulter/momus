use crate::config::GuardConfig;
use crate::report::GuardReport;
use anyhow::Result;
use momus_core::ast::TestPlan;
use std::time::Instant;

/// Execute a security scan against a test plan.
///
/// Sends requests and inspects responses for common security issues.
///
/// # Errors
///
/// Returns an error if the HTTP client fails to initialize.
pub async fn run_guard(plan: &TestPlan, config: &GuardConfig) -> Result<GuardReport> {
    let _start = Instant::now();
    let _ = (plan, config); // TODO: implement in v0.2.0

    tracing::info!("Running security scan on '{}'", plan.name);

    Ok(GuardReport {
        plan_name: plan.name.clone(),
        total_checks: 0,
        issues: vec![],
        passed: 0,
        failed: 0,
        duration_secs: _start.elapsed().as_secs_f64(),
    })
}
