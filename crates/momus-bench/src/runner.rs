use crate::config::BenchConfig;
use crate::modes::run_mode;
use crate::report::BenchReport;
use anyhow::Result;
use momus_core::ast::TestPlan;
use std::time::Instant;

/// Execute a benchmark against a test plan.
///
/// Runs the plan's steps under load according to the benchmark configuration.
/// Returns a `BenchReport` with latency histograms and error statistics.
///
/// # Errors
///
/// Returns an error if the plan cannot be loaded or the HTTP client fails to initialize.
pub async fn run_bench(plan: &TestPlan, config: &BenchConfig) -> Result<BenchReport> {
    let _start = Instant::now();
    let _ = plan; // TODO: use plan steps in v0.2.0

    tracing::info!(
        "Starting benchmark '{}' in {:?} mode",
        plan.name,
        config.mode
    );

    let mut report = run_mode(&config.mode).await?;
    report.duration_secs = _start.elapsed().as_secs_f64();

    Ok(report)
}
