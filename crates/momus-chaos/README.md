# momus-chaos

[![Crates.io](https://img.shields.io/crates/v/momus-chaos.svg)](https://crates.io/crates/momus-chaos)
[![Docs.rs](https://img.shields.io/docsrs/momus-chaos)](https://docs.rs/momus-chaos)

**Chaos engineering engine — network faults, service faults, resource pressure, state faults.**

## What is this?

`momus-chaos` injects infrastructure-level faults into a running system and verifies that the system self-heals, degrades gracefully, or fails safely. It runs a test plan concurrently with configurable chaos experiments and reports on system behavior under failure conditions.

## Key Features

- **Network Faults** — latency injection, packet loss simulation, connection resets
- **Service Faults** — kill processes, crash dependencies, return 5xx from upstream
- **Resource Faults** — CPU pressure, memory exhaustion, disk full simulation
- **State Faults** — clock skew, corrupted data, leader election disruption
- **Configurable Duration** — per-experiment duration and cooldown periods
- **Structured Reports** — per-experiment results with pass/fail and observations

## Usage

```rust
use momus_chaos::{ChaosConfig, ChaosExperiment, run_chaos};
use momus_core::ast::TestPlan;

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let plan: TestPlan = serde_json::from_str(r#"{
        "name": "resilience test",
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

    let config = ChaosConfig {
        experiments: vec![
            ChaosExperiment::NetworkLatency {
                endpoint: "/api/slow".into(),
                delay_ms: 5000,
                duration_secs: 30,
            },
            ChaosExperiment::ServiceCrash {
                service: "auth-service".into(),
                duration_secs: 15,
            },
        ],
        ..Default::default()
    };

    let reports = run_chaos(&plan, &config).await?;
    for report in &reports {
        println!("{}", report);
    }
    Ok(())
}
```

---

Part of the [Momus](https://github.com/jlcoulter/momus) project — a generic API test harness with a composable assertion AST.
