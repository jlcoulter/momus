# Momus

[![CI](https://github.com/jlcoulter/momus/actions/workflows/ci.yml/badge.svg)](https://github.com/jlcoulter/momus/actions/workflows/ci.yml)
[![Crates.io](https://img.shields.io/crates/v/momus.svg)](https://crates.io/crates/momus)
[![Docs.rs](https://img.shields.io/docsrs/momus)](https://docs.rs/momus)
[![Rust](https://img.shields.io/badge/rust-1.88+-blue.svg)](https://www.rust-lang.org)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)

**Generic API test harness with a composable assertion AST.**

Momus is a domain-agnostic test runner for HTTP APIs. Tests are defined as a JSON plan — a tree of steps (requests, sequences, parallel blocks) with composable assertions on responses. No DSL, no vendor lock-in.

Momus is the generalization of [fhir-autotest](https://github.com/jlcoulter/fhir-autotest) — a FHIR Implementation Guide conformance test suite. The core pipeline (parse spec → generate resources → generate tests → execute → validate → report) is universal. Momus extracts the engine and makes it domain-agnostic, with FHIR as one of many supported input formats via `momus convert fhir`.

> **Status:** v0.3.0 — All 10 CLI commands are implemented and functional. The core engine, mock server, assertion runner, template resolution, and all sub-crates (bench, fuzz, chaos, contract, guard, diff) are fully implemented. All 7 API description converters (curl, HAR, OpenAPI, Postman, GraphQL, gRPC, FHIR) are complete. See [DESIGN.md](DESIGN.md) for the full architecture.

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

# Convert a cURL command into a test plan
momus convert curl 'curl https://api.example.com/health'

# Load test a plan
momus bench examples/health-check.json --concurrency 50 --duration 30

# Fuzz test a plan
momus fuzz examples/health-check.json --iterations 1000

# Run chaos experiments
momus chaos examples/health-check.json

# Validate responses against an API spec
momus contract examples/health-check.json --spec openapi.yaml

# Security scan a plan
momus guard examples/health-check.json

# Diff responses between two environments
momus diff examples/health-check.json --baseline https://api-v1.example.com --target https://api-v2.example.com
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
- `{env.VAR}` — the value of environment variable `VAR`
- `{random.uuid}` — a random UUID v4 string (each occurrence produces a different value)
- `{random.int}` — a random integer (0..=i64::MAX)
- `{random.int(N,M)}` — a random integer in the range [N, M]
- `{random.string}` — a random alphanumeric string of length 8
- `{random.string(N)}` — a random alphanumeric string of length N

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
momus = "0.3"
```

Use the library to build and run test plans programmatically:

```rust
use momus::prelude::*;
use momus::builder::*;
use momus_core::transport::HttpAdapter;

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

    // Use the transport adapter for custom HTTP clients
    let adapter = HttpAdapter::new();
    let request = momus_core::transport::TransportRequest {
        method: momus_core::ast::Method::Get,
        url: "http://localhost:8080/health".into(),
        headers: std::collections::HashMap::new(),
        body: None,
    };
    let response = adapter.send(&request).await?;
    println!("Status: {}, Body: {:?}", response.status_code, response.body);

    // Or use the built-in plan runner
    let report = momus_core::engine::runner::execute_plan(&plan).await?;
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
  chaos     Run chaos experiments against a plan
  convert   Convert an API description into a test plan
  contract  Validate API responses against an OpenAPI/GraphQL spec
  guard     Security scan a plan for common vulnerabilities
  diff      Diff responses between two environments
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

### `momus chaos`

```bash
# Run chaos experiments against a plan
momus chaos plan.json

# Override base URL
momus chaos plan.json --base-url http://staging:8080
```

### `momus convert`

```bash
# Convert a cURL command into a test plan
momus convert curl 'curl -X POST https://api.example.com/users -H "Content-Type: application/json" -d "{\"name\":\"test\"}"'

# Convert a HAR file (browser DevTools export) into a test plan
momus convert har traffic.har

# Convert an OpenAPI spec into a test plan
momus convert openapi spec.yaml

# Convert a Postman collection into a test plan
momus convert postman collection.json

# Convert a GraphQL schema into a test plan
momus convert graphql schema.graphql

# Convert a gRPC proto into a test plan
momus convert grpc service.proto

# Convert a FHIR IG package into a test plan
momus convert fhir ig.tar.gz
```

### `momus contract`

```bash
# Validate responses against an OpenAPI spec
momus contract plan.json --spec openapi.yaml

# Validate responses against a GraphQL schema
momus contract plan.json --spec schema.graphql
```

### `momus guard`

```bash
# Security scan a plan
momus guard plan.json

# Override base URL
momus guard plan.json --base-url http://staging:8080
```

### `momus diff`

```bash
# Diff responses between two environments
momus diff plan.json --baseline https://api-v1.example.com --target https://api-v2.example.com
```

## Project Structure

```text
momus/                          # workspace root
├── crates/
│   ├── momus/                  # Umbrella crate: re-exports, builder, prelude
│   ├── momus-core/             # AST types, assertion evaluation, plan runner, template resolution, transport adapter, script engine
│   ├── momus-mock/             # Configurable mock HTTP server with stateful CRUD store
│   ├── momus-convert/          # Convert API descriptions into test plans
│   │   ├── curl.rs             #   cURL command → TestPlan
│   │   ├── har.rs              #   HAR file → TestPlan
│   │   ├── openapi.rs          #   OpenAPI 3.x → TestPlan
│   │   ├── postman.rs          #   Postman Collection → TestPlan
│   │   ├── graphql.rs          #   GraphQL SDL → TestPlan
│   │   ├── grpc.rs             #   gRPC proto → TestPlan
│   │   └── fhir/               #   FHIR IG → TestPlan
│   │       ├── mod.rs
│   │       ├── package.rs
│   │       ├── profile.rs
│   │       ├── profile_resolver.rs
│   │       ├── resource_gen.rs
│   │       ├── capability.rs
│   │       ├── search_param.rs
│   │       ├── operation.rs
│   │       ├── assertions.rs
│   │       ├── validator.rs
│   │       ├── planner.rs
│   │       ├── test_model.rs
│   │       ├── valuesets.rs
│   │       ├── bulk_data.rs
│   │       ├── bulk_loader.rs
│   │       └── hcpd.rs
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

## FHIR Converter

The FHIR converter (`momus convert fhir`) transforms FHIR Implementation Guide packages into comprehensive conformance test plans. It is the most sophisticated converter in Momus, reflecting its origins in [fhir-autotest](https://github.com/jlcoulter/fhir-autotest).

### Capabilities

- **Package Parsing** — extract and parse FHIR IG packages (tar.gz) with NPM-style dependency resolution
- **Profile Resolution** — resolve FHIR profiles, extensions, and value sets from the IG package and external sources
- **Resource Generation** — generate valid FHIR resources conforming to profile definitions
- **Search Parameter Testing** — generate tests for all defined search parameters
- **Operation Testing** — generate tests for custom FHIR operations
- **Capability Statement Validation** — verify server CapabilityStatement against IG requirements
- **Assertion Generation** — generate FHIRPath-based assertions for profile conformance
- **Bulk Data Testing** — support for FHIR Bulk Data Export ($export) testing
- **HCPD Integration** — Health Care Provider Directory (HCPD) specific test generation

### Usage

```bash
# Convert a FHIR IG package into a test plan
momus convert fhir hl7.fhir.us.core-6.1.0.tar.gz

# Save to a file
momus convert fhir ig.tar.gz -o fhir-tests.json

# Run the generated tests
momus run fhir-tests.json --base-url https://fhir.example.com
```

## Development

See [CONTRIBUTING.md](CONTRIBUTING.md) for the full development guide, including
setup, build steps, test/lint workflow, commit conventions, and PR workflow.

```bash
# Run all tests
cargo test --all-targets

# Check formatting
cargo fmt --check

# Lint
cargo clippy --all-targets -- -D warnings
```

## Security

See [SECURITY.md](SECURITY.md) for our security policy and instructions for reporting vulnerabilities.

## Community

This project is governed by a [Code of Conduct](CODE_OF_CONDUCT.md).
By participating, you agree to uphold its terms.

## License

Apache 2.0
