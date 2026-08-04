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
    let start = Instant::now();

    let base_url = config.base_url.as_deref().unwrap_or(&plan.base_url);

    tracing::info!(
        "Starting benchmark '{}' in {:?} mode against {}",
        plan.name,
        config.mode,
        base_url
    );

    let mut report = run_mode(&config.mode, plan, base_url).await?;
    report.duration_secs = start.elapsed().as_secs_f64();

    Ok(report)
}
