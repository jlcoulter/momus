use crate::config::ChaosConfig;
use crate::experiments::run_experiment;
use crate::report::ChaosReport;
use anyhow::Result;
use momus_core::ast::TestPlan;
use std::time::Instant;

/// Execute a chaos run against a test plan.
///
/// Runs each experiment in sequence, with a configurable interval between them.
/// Returns a list of reports, one per experiment.
///
/// # Errors
///
/// Returns an error if the HTTP client fails to initialize.
pub async fn run_chaos(plan: &TestPlan, config: &ChaosConfig) -> Result<Vec<ChaosReport>> {
    let _start = Instant::now();
    let _ = plan; // TODO: use plan steps in v0.2.0

    tracing::info!(
        "Starting chaos run on '{}' with {} experiment(s)",
        plan.name,
        config.experiments.len()
    );

    let mut reports = Vec::new();

    for (i, experiment) in config.experiments.iter().enumerate() {
        tracing::info!(
            "Running experiment {}/{}: {:?}",
            i + 1,
            config.experiments.len(),
            experiment
        );

        let report = run_experiment(experiment, config.timeout_secs).await?;
        reports.push(report);

        // Wait between experiments
        if i + 1 < config.experiments.len() {
            tokio::time::sleep(std::time::Duration::from_secs(config.interval_secs)).await;
        }
    }

    Ok(reports)
}
