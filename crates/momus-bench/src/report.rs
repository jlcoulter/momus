use serde::{Deserialize, Serialize};

/// Results of a benchmark run.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct BenchReport {
    /// Mode name ("steady", "max_throughput", "soak").
    pub mode: String,
    /// Total requests sent.
    pub total_requests: u64,
    /// Wall-clock duration in seconds.
    pub duration_secs: f64,
    /// Median latency in milliseconds.
    pub p50_ms: f64,
    /// 90th percentile latency in milliseconds.
    pub p90_ms: f64,
    /// 95th percentile latency in milliseconds.
    pub p95_ms: f64,
    /// 99th percentile latency in milliseconds.
    pub p99_ms: f64,
    /// Average latency in milliseconds.
    pub avg_ms: f64,
    /// Minimum latency in milliseconds.
    pub min_ms: f64,
    /// Maximum latency in milliseconds.
    pub max_ms: f64,
    /// Number of failed/errored requests.
    pub error_count: u64,
    /// Error rate (0.0–1.0).
    pub error_rate: f64,
    /// Throughput in requests per second.
    pub requests_per_sec: f64,
}

impl std::fmt::Display for BenchReport {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        writeln!(f, "── Benchmark Report ({}) ──", self.mode)?;
        writeln!(f, "  Total requests: {}", self.total_requests)?;
        writeln!(f, "  Duration: {:.1}s", self.duration_secs)?;
        writeln!(f, "  Throughput: {:.0} req/s", self.requests_per_sec)?;
        writeln!(f, "  Latency:")?;
        writeln!(f, "    P50: {:.1}ms", self.p50_ms)?;
        writeln!(f, "    P90: {:.1}ms", self.p90_ms)?;
        writeln!(f, "    P95: {:.1}ms", self.p95_ms)?;
        writeln!(f, "    P99: {:.1}ms", self.p99_ms)?;
        writeln!(f, "    Avg: {:.1}ms", self.avg_ms)?;
        writeln!(f, "    Min: {:.1}ms", self.min_ms)?;
        writeln!(f, "    Max: {:.1}ms", self.max_ms)?;
        writeln!(
            f,
            "  Errors: {} ({:.1}%)",
            self.error_count,
            self.error_rate * 100.0
        )?;
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_bench_report_display() {
        let report = BenchReport {
            mode: "steady".into(),
            total_requests: 1000,
            duration_secs: 30.0,
            p50_ms: 45.0,
            p90_ms: 120.0,
            p95_ms: 200.0,
            p99_ms: 500.0,
            avg_ms: 60.0,
            min_ms: 5.0,
            max_ms: 1200.0,
            error_count: 2,
            error_rate: 0.002,
            requests_per_sec: 33.3,
        };
        let output = report.to_string();
        assert!(output.contains("1000"));
        assert!(output.contains("45.0"));
        assert!(output.contains("0.2%"));
    }
}
