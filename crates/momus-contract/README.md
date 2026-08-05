# momus-contract

[![Crates.io](https://img.shields.io/crates/v/momus-contract.svg)](https://crates.io/crates/momus-contract)
[![Docs.rs](https://img.shields.io/docsrs/momus-contract)](https://docs.rs/momus-contract)

**Contract testing — validate API responses against OpenAPI/GraphQL schemas.**

## What is this?

`momus-contract` runs a test plan and validates each response against its declared schema. It reports compliance percentage, missing fields, type mismatches, and undocumented fields, ensuring that your API implementation stays faithful to its specification.

## Key Features

- **OpenAPI Schema Validation** — validate responses against OpenAPI 3.x spec schemas
- **GraphQL Schema Validation** — validate responses against GraphQL SDL type definitions
- **Compliance Percentage** — overall score of how well responses match the spec
- **Detailed Diagnostics** — missing fields, type mismatches, undocumented fields, constraint violations
- **Per-Endpoint Reporting** — breakdown of compliance by endpoint and status code

## Usage

```rust
use momus_contract::{ContractConfig, run_contract};
use momus_core::ast::TestPlan;

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let plan: TestPlan = serde_json::from_str(r#"{
        "name": "contract test",
        "base_url": "http://localhost:8080",
        "steps": [
            {
                "type": "request",
                "name": "get_user",
                "method": "GET",
                "url": "/users/123",
                "assert": [{ "status": 200 }]
            }
        ]
    }"#)?;

    let config = ContractConfig {
        spec_path: "openapi.yaml".into(),
        ..Default::default()
    };

    let report = run_contract(&plan, &config).await?;
    println!("Compliance: {:.1}%", report.compliance_pct);
    for issue in &report.issues {
        println!("  - {}: {}", issue.endpoint, issue.message);
    }
    Ok(())
}
```

---

Part of the [Momus](https://github.com/jlcoulter/momus) project — a generic API test harness with a composable assertion AST.
