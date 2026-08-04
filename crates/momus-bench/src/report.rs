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

impl BenchReport {
    /// Render the report as a self-contained HTML page.
    pub fn to_html(&self) -> String {
        let error_pct = self.error_rate * 100.0;
        let pass_pct = if self.total_requests > 0 {
            ((self.total_requests - self.error_count) as f64 / self.total_requests as f64) * 100.0
        } else {
            100.0
        };

        format!(
            r#"<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Benchmark Report — {mode}</title>
<style>
  * {{ box-sizing: border-box; margin: 0; padding: 0; }}
  body {{ font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; background: #f5f7fa; color: #1a1a2e; padding: 2rem; }}
  .container {{ max-width: 900px; margin: 0 auto; }}
  h1 {{ font-size: 1.6rem; margin-bottom: 0.25rem; }}
  .subtitle {{ color: #666; margin-bottom: 1.5rem; }}
  .summary {{ display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 1rem; margin-bottom: 2rem; }}
  .card {{ background: #fff; border-radius: 8px; padding: 1rem; box-shadow: 0 1px 3px rgba(0,0,0,0.08); }}
  .card .label {{ font-size: 0.75rem; text-transform: uppercase; color: #888; letter-spacing: 0.5px; }}
  .card .value {{ font-size: 1.5rem; font-weight: 700; margin-top: 0.25rem; }}
  .card .value.green {{ color: #22c55e; }}
  .card .value.red {{ color: #ef4444; }}
  .card .value.blue {{ color: #3b82f6; }}
  .card .value.amber {{ color: #f59e0b; }}
  table {{ width: 100%; border-collapse: collapse; background: #fff; border-radius: 8px; overflow: hidden; box-shadow: 0 1px 3px rgba(0,0,0,0.08); }}
  th {{ background: #f0f2f5; text-align: left; padding: 0.75rem 1rem; font-size: 0.8rem; text-transform: uppercase; color: #666; letter-spacing: 0.5px; }}
  td {{ padding: 0.75rem 1rem; border-top: 1px solid #e5e7eb; }}
  tr:hover td {{ background: #f9fafb; }}
  .bar {{ display: inline-block; height: 8px; border-radius: 4px; background: #3b82f6; }}
  .bar-container {{ display: flex; align-items: center; gap: 0.5rem; }}
  .bar-label {{ font-size: 0.8rem; color: #666; min-width: 2.5rem; text-align: right; }}
  .footer {{ margin-top: 1.5rem; font-size: 0.8rem; color: #999; text-align: center; }}
</style>
</head>
<body>
<div class="container">
  <h1>Benchmark Report</h1>
  <p class="subtitle">Mode: {mode} &middot; {total_requests} requests in {duration_secs:.1}s</p>

  <div class="summary">
    <div class="card">
      <div class="label">Throughput</div>
      <div class="value blue">{requests_per_sec:.0} <span style="font-size:0.8rem;font-weight:400;color:#888;">req/s</span></div>
    </div>
    <div class="card">
      <div class="label">P50 Latency</div>
      <div class="value">{p50_ms:.1} <span style="font-size:0.8rem;font-weight:400;color:#888;">ms</span></div>
    </div>
    <div class="card">
      <div class="label">P99 Latency</div>
      <div class="value">{p99_ms:.1} <span style="font-size:0.8rem;font-weight:400;color:#888;">ms</span></div>
    </div>
    <div class="card">
      <div class="label">Success Rate</div>
      <div class="value green">{pass_pct:.1}%</div>
    </div>
    <div class="card">
      <div class="label">Errors</div>
      <div class="value red">{error_count} <span style="font-size:0.8rem;font-weight:400;color:#888;">({error_pct:.1}%)</span></div>
    </div>
    <div class="card">
      <div class="label">Duration</div>
      <div class="value">{duration_secs:.0} <span style="font-size:0.8rem;font-weight:400;color:#888;">s</span></div>
    </div>
  </div>

  <table>
    <thead>
      <tr><th>Metric</th><th>Value</th><th></th></tr>
    </thead>
    <tbody>
      <tr><td>Average Latency</td><td>{avg_ms:.1} ms</td><td><div class="bar-container"><span class="bar-label">{avg_ms:.0}%</span><span class="bar" style="width:{avg_ms_percent:.0}%;"></span></div></td></tr>
      <tr><td>Minimum Latency</td><td>{min_ms:.1} ms</td><td><div class="bar-container"><span class="bar-label">{min_ms:.0}%</span><span class="bar" style="width:{min_ms_percent:.0}%;"></span></div></td></tr>
      <tr><td>Maximum Latency</td><td>{max_ms:.1} ms</td><td><div class="bar-container"><span class="bar-label">100%</span><span class="bar" style="width:100%;background:#ef4444;"></span></div></td></tr>
      <tr><td>P50 (Median)</td><td>{p50_ms:.1} ms</td><td><div class="bar-container"><span class="bar-label">{p50_ms:.0}%</span><span class="bar" style="width:{p50_ms_percent:.0}%;"></span></div></td></tr>
      <tr><td>P90</td><td>{p90_ms:.1} ms</td><td><div class="bar-container"><span class="bar-label">{p90_ms:.0}%</span><span class="bar" style="width:{p90_ms_percent:.0}%;"></span></div></td></tr>
      <tr><td>P95</td><td>{p95_ms:.1} ms</td><td><div class="bar-container"><span class="bar-label">{p95_ms:.0}%</span><span class="bar" style="width:{p95_ms_percent:.0}%;"></span></div></td></tr>
      <tr><td>P99</td><td>{p99_ms:.1} ms</td><td><div class="bar-container"><span class="bar-label">{p99_ms:.0}%</span><span class="bar" style="width:{p99_ms_percent:.0}%;"></span></div></td></tr>
    </tbody>
  </table>

  <div class="footer">Generated by Momus</div>
</div>
</body>
</html>"#,
            mode = self.mode,
            total_requests = self.total_requests,
            duration_secs = self.duration_secs,
            requests_per_sec = self.requests_per_sec,
            p50_ms = self.p50_ms,
            p90_ms = self.p90_ms,
            p95_ms = self.p95_ms,
            p99_ms = self.p99_ms,
            avg_ms = self.avg_ms,
            min_ms = self.min_ms,
            max_ms = self.max_ms,
            error_count = self.error_count,
            error_pct = error_pct,
            pass_pct = pass_pct,
            p50_ms_percent = (self.p50_ms / self.max_ms.max(1.0)) * 100.0,
            p90_ms_percent = (self.p90_ms / self.max_ms.max(1.0)) * 100.0,
            p95_ms_percent = (self.p95_ms / self.max_ms.max(1.0)) * 100.0,
            p99_ms_percent = (self.p99_ms / self.max_ms.max(1.0)) * 100.0,
            avg_ms_percent = (self.avg_ms / self.max_ms.max(1.0)) * 100.0,
            min_ms_percent = (self.min_ms / self.max_ms.max(1.0)) * 100.0,
        )
    }
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

    #[test]
    fn test_bench_report_to_html() {
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
        let html = report.to_html();
        assert!(html.contains("<!DOCTYPE html>"));
        assert!(html.contains("Benchmark Report"));
        assert!(html.contains("steady"));
        assert!(html.contains("1000"));
        assert!(html.contains("45.0"));
        assert!(html.contains("99.8%")); // success rate
        assert!(html.contains("</html>"));
    }
}
