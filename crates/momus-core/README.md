# momus-core

[![Crates.io](https://img.shields.io/crates/v/momus-core.svg)](https://crates.io/crates/momus-core)
[![Docs.rs](https://img.shields.io/docsrs/momus-core)](https://docs.rs/momus-core)

**Generic API test harness — AST types, assertion evaluation, plan runner, template resolution.**

## What is this?

`momus-core` is the foundational crate of the Momus ecosystem. It defines the core data model (AST types for test plans, steps, and assertions), the assertion evaluation engine, the plan runner, template resolution for dynamic values, the transport adapter trait for multi-protocol support, and the Rhai-based script engine.

## Key Features

- **Composable Assertion AST** — `all_of`, `any_of`, `not`, `status`, `status_in`, `header`, `body_length`, `content_type`, `valid_json`, `json_path`, `schema`
- **JSONPath Predicates** — `exists`, `not_exists`, `eq`, `not_eq`, `cmp`, `length`, `count`, `every`, `some`, `schema`
- **Step Types** — `request`, `sequence`, `parallel`, `script`, `noop`
- **Template Resolution** — `{base_url}`, `{steps.<name>.<field>}` substitution in URLs, headers, and bodies
- **Transport Adapter** — `TransportAdapter` trait with built-in `HttpAdapter` (reqwest-based)
- **Script Engine** — Rhai-based scripting for custom logic
- **JSON Schema Validation** — via `jsonschema` crate
- **Plan Runner** — executes test plans and produces structured `RunReport`

## Usage

```rust
use momus_core::ast::*;
use momus_core::engine::runner;
use momus_core::transport::HttpAdapter;

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let plan: TestPlan = serde_json::from_str(r#"{
        "name": "health check",
        "base_url": "http://localhost:8080",
        "steps": [
            {
                "type": "request",
                "name": "health",
                "method": "GET",
                "url": "/health",
                "assert": [
                    { "status": 200 },
                    { "json_path": { "path": "$.status", "predicate": { "eq": "ok" } } }
                ]
            }
        ]
    }"#)?;

    let report = runner::execute_plan(&plan).await?;
    println!("{}", report);
    Ok(())
}
```

---

Part of the [Momus](https://github.com/jlcoulter/momus) project — a generic API test harness with a composable assertion AST.
