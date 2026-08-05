# momus-fuzz

[![Crates.io](https://img.shields.io/crates/v/momus-fuzz.svg)](https://crates.io/crates/momus-fuzz)
[![Docs.rs](https://img.shields.io/docsrs/momus-fuzz)](https://docs.rs/momus-fuzz)

**Payload mutation engine — boundary, encoding, type mismatch, cardinality mutators.**

## What is this?

`momus-fuzz` takes a valid JSON payload and produces mutated variants to test API robustness. It applies configurable mutators that exercise edge cases: boundary values, encoding tricks, type mismatches, and cardinality changes. The `Mutator` trait is the extension point for custom mutation strategies.

## Key Features

- **Boundary Mutator** — extreme values (empty strings, max integers, nulls, very large numbers)
- **Encoding Mutator** — Unicode injection, escaped characters, mixed encodings
- **Type Mismatch Mutator** — swap types (string→number, object→array, etc.)
- **Cardinality Mutator** — duplicate fields, remove fields, add extra array elements
- **Deterministic Mutation** — same `(base, seed)` always produces the same mutation
- **Configurable Iterations** — control how many mutations to generate and test
- **Per-Mutation Reporting** — track which mutations caused failures

## Usage

```rust
use momus_fuzz::{Mutator, mutators::BoundaryMutator, FuzzConfig, run_fuzz};
use momus_core::ast::TestPlan;
use serde_json::json;

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    // Use a mutator directly
    let mutator = BoundaryMutator;
    let base = json!({"name": "test", "count": 5});
    let mutated = mutator.mutate(&base, 42);
    println!("Original: {}", base);
    println!("Mutated:  {}", mutated);

    // Or run a full fuzz session against a test plan
    let plan: TestPlan = serde_json::from_str(r#"{
        "name": "fuzz test",
        "base_url": "http://localhost:8080",
        "steps": [
            {
                "type": "request",
                "name": "create",
                "method": "POST",
                "url": "/api/items",
                "body": {"name": "test", "count": 5},
                "assert": [{ "status": 201 }]
            }
        ]
    }"#)?;

    let config = FuzzConfig {
        iterations: 1000,
        ..Default::default()
    };

    let report = run_fuzz(&plan, &config).await?;
    println!("{}", report);
    Ok(())
}
```

---

Part of the [Momus](https://github.com/jlcoulter/momus) project — a generic API test harness with a composable assertion AST.
