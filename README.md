# Momus

[![CI](https://github.com/jlcoulter/momus/actions/workflows/ci.yml/badge.svg)](https://github.com/jlcoulter/momus/actions/workflows/ci.yml)
[![Crates.io](https://img.shields.io/crates/v/momus.svg)](https://crates.io/crates/momus)
[![Docs.rs](https://img.shields.io/docsrs/momus)](https://docs.rs/momus)
[![Rust](https://img.shields.io/badge/rust-1.88+-blue.svg)](https://www.rust-lang.org)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)

**Generic API test harness with a composable assertion AST.**

Momus is a domain-agnostic test runner for HTTP APIs. Tests are defined as a JSON plan — a tree of steps (requests, sequences, parallel blocks) with composable assertions on responses. No DSL, no vendor lock-in.

## Quick Start

```bash
# Install from source
cargo install momus

# Validate a test plan
momus validate examples/health-check.json

# Run against a server
momus run examples/health-check.json --base-url http://localhost:8080

# Start a mock server for testing
momus mock --port 8091
```

## Example Test Plan

```json
{
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
}
```

## Assertion AST

Assertions form a composable tree:

| Node | Purpose |
|------|---------|
| `all_of` | All sub-assertions must pass (AND) |
| `any_of` | At least one sub-assertion must pass (OR) |
| `not` | Sub-assertion must NOT pass |
| `status` | Expected HTTP status code |
| `status_in` | Status must be in a set |
| `header` | Header present/absent/equals/contains/regex |
| `body_length` | Response body size constraints |
| `content_type` | Content-Type header match |
| `valid_json` | Response must be valid JSON |
| `json_path` | JSONPath query with predicate |
| `schema` | JSON Schema validation (WIP) |

### JSONPath Predicates

| Predicate | Purpose |
|-----------|---------|
| `exists` | Path must exist |
| `not_exists` | Path must NOT exist |
| `eq` | First result equals value |
| `not_eq` | First result does not equal value |
| `cmp` | Numeric comparison (gt/lt/ge/le) |
| `length` | Result array length (eq/min/max/range) |
| `count` | Result count (eq/min/max/range) |
| `every` | Every result satisfies sub-predicate |
| `some` | At least one result satisfies sub-predicate |
| `schema` | Match result against JSON Schema (WIP) |

## Step Types

| Step | Purpose |
|------|---------|
| `request` | Single HTTP request with assertions |
| `sequence` | Ordered sub-steps with state passing via `{steps.<name>.*}` |
| `parallel` | Concurrent sub-steps |
| `script` | Custom logic (future) |
| `noop` | Placeholder / disabled test |

### Template Resolution

URLs, headers, and bodies support template substitution:

- `{base_url}` — the configured base URL
- `{steps.<name>.id}` — the `id` field from a saved step response
- `{steps.<name>.<field.path>}` — any field from a saved step response

## Multi-Step Sequence Example

```json
{
  "name": "create then read",
  "base_url": "http://localhost:8080",
  "steps": [
    {
      "type": "sequence",
      "name": "crud",
      "steps": [
        {
          "type": "request",
          "name": "create",
          "method": "POST",
          "url": "/api/items",
          "body": { "name": "test" },
          "assert": [{ "status": 201 }],
          "save_as": "create_item"
        },
        {
          "type": "request",
          "name": "read",
          "method": "GET",
          "url": "/api/items/{steps.create_item.id}",
          "assert": [
            { "status": 200 },
            { "json_path": { "path": "$.name", "predicate": { "eq": "test" } } }
          ]
        }
      ]
    }
  ]
}
```

## Library Usage

Add Momus to your `Cargo.toml`:

```toml
[dependencies]
momus = "0.1"
```

Use the library to build and run test plans programmatically:

```rust
use momus::ast::*;
use momus::engine::runner;

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let plan = TestPlan {
        name: "health check".into(),
        base_url: "http://localhost:8080".into(),
        default_headers: std::collections::HashMap::new(),
        steps: vec![
            Step::Request(RequestStep {
                name: "health".into(),
                method: Method::Get,
                url: "/health".into(),
                headers: std::collections::HashMap::new(),
                body: None,
                assert: vec![
                    Assertion::Status(200),
                    Assertion::json_path_eq("$.status", serde_json::json!("ok")),
                ],
                save_as: String::new(),
                soft_fail: false,
            }),
        ],
        setup: vec![],
        teardown: vec![],
    };

    let report = runner::execute_plan(&plan).await?;
    println!("{}", report);
    Ok(())
}
```

## CLI

```text
Usage: momus <COMMAND>

Commands:
  run       Run a test plan from a JSON file
  validate  Validate a test plan JSON file
  mock      Start a mock server for testing
```

### `momus run`

```bash
# Run with default output directory
momus run plan.json

# Override base URL
momus run plan.json --base-url http://other-server:3000

# Custom output directory
momus run plan.json --output ./results
```

### `momus validate`

```bash
momus validate plan.json
# ✓ Valid test plan: 'health check'
#   Total tests: 3
#   Steps: 1
#   Setup steps: 1
#   Teardown steps: 1
```

### `momus mock`

```bash
momus mock --port 8091
# Momus mock server listening on http://127.0.0.1:8091
```

## Project Structure

```
src/
├── main.rs              # CLI entry point
├── lib.rs               # Public API
├── ast/
│   ├── mod.rs           # TestPlan, Step, RequestStep, SequenceStep, TestResult, RunReport
│   └── assertion.rs     # Composable assertion AST
├── engine/
│   ├── mod.rs
│   ├── evaluator.rs     # Assertion evaluation engine
│   ├── runner.rs        # Plan executor (setup → steps → teardown)
│   └── templates.rs     # {base_url}, {steps.<name>.*} template resolution
└── mock/
    └── mod.rs           # Configurable mock HTTP server
```

## Development

```bash
# Run all tests
cargo test --all-targets

# Check formatting
cargo fmt --check

# Lint
cargo clippy --all-targets -- -D warnings
```

## License

Apache 2.0
