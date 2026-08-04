use crate::config::FuzzConfig;
use crate::mutators::{all_mutators, mutator_by_name};
use crate::report::FuzzReport;
use anyhow::Result;
use momus_core::ast::TestPlan;
use std::time::Instant;

/// Execute a fuzz run against a test plan.
///
/// Generates mutated payloads from the plan's request bodies,
/// sends them to the server, and classifies responses.
///
/// # Errors
///
/// Returns an error if the HTTP client fails to initialize.
pub async fn run_fuzz(plan: &TestPlan, config: &FuzzConfig) -> Result<FuzzReport> {
    let _start = Instant::now();
    let _ = plan; // TODO: use plan steps in v0.2.0

    // Select mutators
    let mutators: Vec<Box<dyn crate::Mutator>> = if config.mutators.is_empty() {
        all_mutators()
    } else {
        config
            .mutators
            .iter()
            .filter_map(|name| mutator_by_name(name))
            .collect()
    };

    tracing::info!(
        "Starting fuzz run on '{}' with {} mutator(s) and {} iteration(s)",
        plan.name,
        mutators.len(),
        config.iterations
    );

    let _ = mutators; // TODO: apply mutators in v0.2.0

    Ok(FuzzReport {
        plan_name: plan.name.clone(),
        total_mutations: 0,
        passed: 0,
        rejected: 0,
        errors: 0,
        leaks: 0,
        duration_secs: _start.elapsed().as_secs_f64(),
        mutators_applied: vec![],
    })
}
