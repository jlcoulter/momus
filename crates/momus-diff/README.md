# momus-diff

[![Crates.io](https://img.shields.io/crates/v/momus-diff.svg)](https://crates.io/crates/momus-diff)
[![Docs.rs](https://img.shields.io/docsrs/momus-diff)](https://docs.rs/momus-diff)

**Regression/diff testing — compare API responses between environments.**

## What is this?

`momus-diff` runs the same test plan against two environments (e.g. staging vs production, or v1 vs v2) and reports differences in responses. It detects body changes, new or missing fields, status code changes, and header differences — making it ideal for regression testing and API migration validation.

## Key Features

- **Multi-Environment Comparison** — run the same plan against baseline and target URLs
- **Body Diff** — detect added, removed, and modified fields in JSON responses
- **Status Code Changes** — flag endpoints where status codes differ between environments
- **Header Differences** — compare response headers between environments
- **Structured Reports** — per-endpoint diff summaries with field-level granularity

## Usage

```rust
use momus_diff::{DiffConfig, run_diff};
use momus_core::ast::TestPlan;

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let plan: TestPlan = serde_json::from_str(r#"{
        "name": "regression check",
        "base_url": "http://localhost:8080",
        "steps": [
            {
                "type": "request",
                "name": "get_users",
                "method": "GET",
                "url": "/users",
                "assert": [{ "status": 200 }]
            }
        ]
    }"#)?;

    let config = DiffConfig {
        baseline_url: "https://api-v1.example.com".into(),
        target_url: "https://api-v2.example.com".into(),
        ..Default::default()
    };

    let report = run_diff(&plan, &config).await?;
    println!("Fields added: {}", report.fields_added);
    println!("Fields removed: {}", report.fields_removed);
    println!("Fields modified: {}", report.fields_modified);
    for change in &report.changes {
        println!("  {}: {}", change.endpoint, change.description);
    }
    Ok(())
}
```

---

Part of the [Momus](https://github.com/jlcoulter/momus) project — a generic API test harness with a composable assertion AST.
