use crate::config::DiffConfig;
use crate::report::DiffReport;
use anyhow::Result;
use momus_core::ast::TestPlan;
use std::time::Instant;

/// Execute a diff run between two environments.
///
/// Runs the plan against both `baseline_url` and `target_url`,
/// then compares responses field-by-field.
///
/// # Errors
///
/// Returns an error if the HTTP client fails to initialize.
pub async fn run_diff(plan: &TestPlan, config: &DiffConfig) -> Result<DiffReport> {
    let _start = Instant::now();
    let _ = (plan, config); // TODO: implement in v0.2.0

    tracing::info!(
        "Running diff on '{}': baseline={}, target={}",
        plan.name,
        config.baseline_url,
        config.target_url
    );

    Ok(DiffReport {
        plan_name: plan.name.clone(),
        baseline_url: config.baseline_url.clone(),
        target_url: config.target_url.clone(),
        total_endpoints: 0,
        identical: 0,
        different: 0,
        fields_added: 0,
        fields_removed: 0,
        fields_modified: 0,
        duration_secs: _start.elapsed().as_secs_f64(),
        diffs: vec![],
    })
}
