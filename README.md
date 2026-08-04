# Momus

[![CI](https://github.com/jlcoulter/momus/actions/workflows/ci.yml/badge.svg)](https://github.com/jlcoulter/momus/actions/workflows/ci.yml)
[![Crates.io](https://img.shields.io/crates/v/momus.svg)](https://crates.io/crates/momus)
[![Docs.rs](https://img.shields.io/docsrs/momus)](https://docs.rs/momus)
[![Rust](https://img.shields.io/badge/rust-1.88+-blue.svg)](https://www.rust-lang.org)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)

**Generic API test harness with a composable assertion AST.**

Momus is a domain-agnostic test runner for HTTP APIs. Tests are defined as a JSON plan — a tree of steps (requests, sequences, parallel blocks) with composable assertions on responses. No DSL, no vendor lock-in.

Momus is the generalization of [fhir-autotest](https://github.com/jlcoulter/fhir-autotest) — a FHIR Implementation Guide conformance test suite. The core pipeline (parse spec → generate resources → generate tests → execute → validate → report) is universal. Momus extracts the engine and makes it domain-agnostic, with FHIR as one of many supported input formats via `momus convert fhir`.

> **Status:** v0.1.0 — Core engine, mock server, and CLI are functional. The assertion runner, template resolution, and fuzz mutators are implemented. Load testing, chaos engineering, contract testing, security scanning, diff testing, and all API description converters (curl, HAR, OpenAPI, FHIR, etc.) are scaffolded with stubs — implementation is in progress for v0.2.0. See [DESIGN.md](DESIGN.md) and [FEATURES.md](FEATURES.md) for the full roadmap.

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

# Convert a cURL command into a test plan (coming in v0.2.0)
# momus convert curl 'curl https://api.example.com/health'

# Load test a plan (coming in v0.2.0)
# momus bench examples/health-check.json --concurrency 50 --duration 30

# Fuzz test a plan (coming in v0.2.0)
# momus fuzz examples/health-check.json --iterations 1000
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
                .assert(Assertion::json_path_eq("$.status", serde_json::json!("ok")))
                .build(),
        )
        .build();

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
  bench     Load test a plan (steady, max-throughput, or soak)
  fuzz      Fuzz test a plan with payload mutations
  convert   Convert an API description into a test plan
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
```

### `momus mock`

```bash
momus mock --port 8091
# Momus mock server listening on http://127.0.0.1:8091
```

### `momus bench`

```bash
# Steady load: 50 concurrent users for 60 seconds
momus bench plan.json --concurrency 50 --duration 60

# Override base URL
momus bench plan.json --concurrency 100 --duration 30 --base-url http://staging:8080
```

### `momus fuzz`

```bash
# Generate 5000 mutations
momus fuzz plan.json --iterations 5000

# Override base URL
momus fuzz plan.json --iterations 10000 --base-url http://staging:8080
```

### `momus convert`

```bash
# Convert a cURL command into a test plan
momus convert curl 'curl -X POST https://api.example.com/users -H "Content-Type: application/json" -d "{\"name\":\"test\"}"'

# Convert a HAR file (browser DevTools export) into a test plan
momus convert har traffic.har

# Convert an OpenAPI spec (coming in v0.2.0)
momus convert openapi spec.yaml

# Convert a Postman collection (coming in v0.2.0)
momus convert postman collection.json
```

## Project Structure

```
momus/                          # workspace root
├── crates/
│   ├── momus/                  # Umbrella crate: re-exports, builder, prelude
│   ├── momus-core/             # AST types, assertion evaluation, plan runner, template resolution
│   ├── momus-mock/             # Configurable mock HTTP server
│   ├── momus-convert/          # Convert API descriptions into test plans
│   │   ├── curl.rs             #   cURL command → TestPlan
│   │   ├── har.rs              #   HAR file → TestPlan
│   │   ├── openapi.rs          #   OpenAPI 3.x → TestPlan (WIP)
│   │   ├── postman.rs          #   Postman Collection → TestPlan (WIP)
│   │   ├── graphql.rs          #   GraphQL SDL → TestPlan (WIP)
│   │   ├── grpc.rs             #   gRPC proto → TestPlan (WIP)
│   │   └── fhir.rs             #   FHIR IG → TestPlan (WIP)
│   ├── momus-bench/            # Load testing: steady, max-throughput, soak modes
│   ├── momus-fuzz/             # Payload mutation: boundary, encoding, type mismatch, cardinality
│   ├── momus-chaos/            # Chaos engineering: network, service, resource, state faults
│   ├── momus-contract/         # Contract testing: validate responses against OpenAPI/GraphQL specs
│   ├── momus-guard/            # Security scanning: auth, CORS, info leaks, exposed endpoints
│   ├── momus-diff/             # Regression/diff testing: compare responses between environments
│   └── momus-cli/              # CLI binary
├── examples/
│   ├── health-check.json
│   └── crud-sequence.json
├── DESIGN.md
└── README.md
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
