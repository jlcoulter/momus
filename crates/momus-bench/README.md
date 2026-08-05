# momus-bench

[![Crates.io](https://img.shields.io/crates/v/momus-bench.svg)](https://crates.io/crates/momus-bench)
[![Docs.rs](https://img.shields.io/docsrs/momus-bench)](https://docs.rs/momus-bench)

**Load testing engine — steady, max-throughput, and soak modes.**

## What is this?

`momus-bench` takes a Momus test plan and runs it under load. It supports three load testing modes: steady-state concurrency, max-throughput ramping, and long-duration soak testing. Results include latency percentiles (P50, P90, P99), error rates, and throughput metrics.

## Key Features

- **Steady Mode** — fixed concurrency for a fixed duration (or one-shot)
- **Max-Throughput Mode** — ramp concurrency upward until error rate or latency threshold is breached
- **Soak Mode** — sustained load at fixed concurrency for extended durations
- **Latency Percentiles** — P50, P90, P99, P99.9, min, max, mean
- **Error Tracking** — per-status-code error counts and rates
- **HTML Reports** — rich visual reports with charts
- **Warmup Phase** — configurable warmup requests before measurement

## Usage

```rust
use momus_bench::{BenchConfig, BenchMode, run_bench};
use momus_core::ast::TestPlan;

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let plan: TestPlan = serde_json::from_str(r#"{
        "name": "load test",
        "base_url": "http://localhost:8080",
        "steps": [
            {
                "type": "request",
                "name": "health",
                "method": "GET",
                "url": "/health",
                "assert": [{ "status": 200 }]
            }
        ]
    }"#)?;

    let config = BenchConfig {
        mode: BenchMode::Steady {
            concurrency: 50,
            duration_secs: 30,
        },
        warmup_requests: 5,
        ..Default::default()
    };

    let report = run_bench(&plan, &config).await?;
    println!("P50: {}ms, P99: {}ms", report.p50_ms, report.p99_ms);
    println!("Total requests: {}", report.total_requests);
    println!("Error rate: {:.1}%", report.error_rate_pct);
    Ok(())
}
```

---

Part of the [Momus](https://github.com/jlcoulter/momus) project — a generic API test harness with a composable assertion AST.
