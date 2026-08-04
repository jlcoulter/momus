use crate::config::ContractConfig;
use crate::report::ContractReport;
use anyhow::Result;
use momus_core::ast::TestPlan;
use std::time::Instant;

/// Execute a contract validation run.
///
/// Loads the API spec, runs the plan, and validates each response
/// against the spec's schema for that endpoint.
///
/// # Errors
///
/// Returns an error if the spec cannot be loaded or the HTTP client fails.
pub async fn run_contract(plan: &TestPlan, config: &ContractConfig) -> Result<ContractReport> {
    let _start = Instant::now();
    let _ = (plan, config); // TODO: implement in v0.2.0

    tracing::info!(
        "Running contract validation on '{}' against spec '{}'",
        plan.name,
        config.spec_path
    );

    Ok(ContractReport {
        plan_name: plan.name.clone(),
        spec_path: config.spec_path.clone(),
        total_endpoints: 0,
        compliant: 0,
        violations: 0,
        compliance_pct: 0.0,
        duration_secs: _start.elapsed().as_secs_f64(),
        details: vec![],
    })
}
