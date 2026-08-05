# momus-guard

[![Crates.io](https://img.shields.io/crates/v/momus-guard.svg)](https://crates.io/crates/momus-guard)
[![Docs.rs](https://img.shields.io/docsrs/momus-guard)](https://docs.rs/momus-guard)

**Security scanning — check for missing auth, CORS misconfig, info leaks, exposed endpoints.**

## What is this?

`momus-guard` runs a test plan and inspects responses for common security issues. It checks for missing or weak authentication, CORS misconfiguration, information leakage in error responses, exposed internal endpoints, and missing security headers.

## Key Features

- **Authentication Checks** — missing or weak auth headers on protected endpoints
- **CORS Misconfiguration** — permissive origins, credentials with wildcard origins
- **Information Leakage** — stack traces, SQL errors, path disclosure in error bodies
- **Exposed Endpoints** — internal-only endpoints accessible externally
- **Security Headers** — missing HSTS, CSP, X-Content-Type-Options, X-Frame-Options
- **Structured Reports** — categorized issues with severity levels and remediation guidance

## Usage

```rust
use momus_guard::{GuardConfig, run_guard};
use momus_core::ast::TestPlan;

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let plan: TestPlan = serde_json::from_str(r#"{
        "name": "security scan",
        "base_url": "http://localhost:8080",
        "steps": [
            {
                "type": "request",
                "name": "public_endpoint",
                "method": "GET",
                "url": "/health",
                "assert": [{ "status": 200 }]
            },
            {
                "type": "request",
                "name": "admin_endpoint",
                "method": "GET",
                "url": "/admin/users",
                "assert": [{ "status": 401 }]
            }
        ]
    }"#)?;

    let config = GuardConfig::default();
    let report = run_guard(&plan, &config).await?;
    println!("Issues found: {}", report.issues.len());
    for issue in &report.issues {
        println!("  [{}] {}: {}", issue.severity, issue.category, issue.message);
    }
    Ok(())
}
```

---

Part of the [Momus](https://github.com/jlcoulter/momus) project — a generic API test harness with a composable assertion AST.
