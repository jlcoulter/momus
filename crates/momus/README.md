# momus

[![Crates.io](https://img.shields.io/crates/v/momus.svg)](https://crates.io/crates/momus)
[![Docs.rs](https://img.shields.io/docsrs/momus)](https://docs.rs/momus)

**Generic API test harness with a composable assertion AST (umbrella crate).**

## What is this?

`momus` is the umbrella crate for the Momus project. It re-exports all sub-crates (`momus-core`, `momus-mock`, `momus-convert`, `momus-bench`, `momus-fuzz`, `momus-chaos`, `momus-contract`, `momus-guard`, `momus-diff`) and provides convenience APIs for building and running test plans programmatically.

## Key Features

- **Re-exports** — all Momus sub-crates available under a single dependency
- **Builder API** — `TestPlanBuilder`, `RequestStepBuilder`, `SequenceStepBuilder` for constructing test plans in code
- **Prelude** — common types re-exported via `momus::prelude::*`
- **Convenience Functions** — `load_plan()`, `parse_plan()`, `validate_plan()`

## Usage

```rust
use momus::prelude::*;
use momus::builder::*;

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let plan = TestPlanBuilder::new("health check")
        .base_url("http://localhost:8080")
        .step(
            request("health")
                .get("/health")
                .assert(Assertion::Status(200))
                .assert(Assertion::valid_json())
                .build(),
        )
        .build();

    let report = momus_core::engine::runner::execute_plan(&plan).await?;
    println!("{}", report);
    Ok(())
}
```

### Crate Structure

| Crate | Purpose |
|-------|---------|
| `momus-core` | AST types, assertion evaluation, plan runner, template resolution, transport adapter, script engine |
| `momus-mock` | Configurable mock HTTP server with stateful CRUD store |
| `momus-convert` | Convert API descriptions (curl, HAR, OpenAPI, Postman, GraphQL, gRPC, FHIR) into test plans |
| `momus-bench` | Load testing: steady, max-throughput, soak modes |
| `momus-fuzz` | Payload mutation: boundary, encoding, type mismatch, cardinality |
| `momus-chaos` | Chaos engineering: network, service, resource, state faults |
| `momus-contract` | Contract testing: validate responses against OpenAPI/GraphQL specs |
| `momus-guard` | Security scanning: auth, CORS, info leaks, exposed endpoints |
| `momus-diff` | Regression/diff testing: compare responses between environments |
| `momus-cli` | CLI binary |
| `momus` (this) | Umbrella crate with re-exports, builder, and convenience APIs |

---

Part of the [Momus](https://github.com/jlcoulter/momus) project — a generic API test harness with a composable assertion AST.
