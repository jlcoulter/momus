use crate::config::BenchMode;
use crate::report::BenchReport;
use anyhow::Result;

/// Run a benchmark in the given mode.
///
/// Dispatches to the appropriate mode-specific runner.
pub async fn run_mode(mode: &BenchMode) -> Result<BenchReport> {
    match mode {
        BenchMode::Steady {
            concurrency,
            duration_secs,
        } => run_steady(*concurrency, *duration_secs).await,
        BenchMode::MaxThroughput {
            min_concurrency,
            max_concurrency,
            step,
            step_duration_secs,
            max_error_rate,
            max_p99_ms,
        } => {
            run_max_throughput(
                *min_concurrency,
                *max_concurrency,
                *step,
                *step_duration_secs,
                *max_error_rate,
                *max_p99_ms,
            )
            .await
        }
        BenchMode::Soak {
            concurrency,
            duration_secs,
        } => run_soak(*concurrency, *duration_secs).await,
    }
}

/// Steady mode: fixed concurrency for a fixed duration.
async fn run_steady(concurrency: usize, duration_secs: u64) -> Result<BenchReport> {
    // TODO: implement in v0.2.0
    let _ = (concurrency, duration_secs);
    Ok(BenchReport {
        mode: "steady".into(),
        total_requests: 0,
        duration_secs: 0.0,
        p50_ms: 0.0,
        p90_ms: 0.0,
        p95_ms: 0.0,
        p99_ms: 0.0,
        avg_ms: 0.0,
        min_ms: 0.0,
        max_ms: 0.0,
        error_count: 0,
        error_rate: 0.0,
        requests_per_sec: 0.0,
    })
}

/// Max-throughput mode: ramp concurrency until thresholds are breached.
async fn run_max_throughput(
    min_concurrency: usize,
    max_concurrency: usize,
    step: usize,
    step_duration_secs: u64,
    max_error_rate: f64,
    max_p99_ms: u64,
) -> Result<BenchReport> {
    // TODO: implement in v0.2.0
    let _ = (
        min_concurrency,
        max_concurrency,
        step,
        step_duration_secs,
        max_error_rate,
        max_p99_ms,
    );
    Ok(BenchReport {
        mode: "max_throughput".into(),
        total_requests: 0,
        duration_secs: 0.0,
        p50_ms: 0.0,
        p90_ms: 0.0,
        p95_ms: 0.0,
        p99_ms: 0.0,
        avg_ms: 0.0,
        min_ms: 0.0,
        max_ms: 0.0,
        error_count: 0,
        error_rate: 0.0,
        requests_per_sec: 0.0,
    })
}

/// Soak mode: sustained load at fixed concurrency for hours.
async fn run_soak(concurrency: usize, duration_secs: u64) -> Result<BenchReport> {
    // TODO: implement in v0.2.0
    let _ = (concurrency, duration_secs);
    Ok(BenchReport {
        mode: "soak".into(),
        total_requests: 0,
        duration_secs: 0.0,
        p50_ms: 0.0,
        p90_ms: 0.0,
        p95_ms: 0.0,
        p99_ms: 0.0,
        avg_ms: 0.0,
        min_ms: 0.0,
        max_ms: 0.0,
        error_count: 0,
        error_rate: 0.0,
        requests_per_sec: 0.0,
    })
}
