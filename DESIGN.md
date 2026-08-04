# Momus Design

## Origin

Momus is the generalization of [fhir-autotest](https://github.com/jlcoulter/fhir-autotest) — a tool that parses FHIR Implementation Guide packages and automatically generates conformance test suites from CapabilityStatements, StructureDefinitions, and SearchParameters. The core pipeline was:

```
IG Package → Parse → Resolve Profiles → Generate Resources → Generate Tests → Execute → Validate → Report
```

That pipeline is not FHIR-specific. Every API testing workflow follows the same shape: understand the schema, generate valid input, derive test cases from the spec, execute them, assert the responses, and report the results. Momus extracts the universal engine and defines a contract (the AST) that domain-specific frontends compile into.

## Crate Map

The Momus workspace is organized as a set of crates, each with a single responsibility. The dependency direction is always toward `momus-core` — frontends depend on the core, never the other way.

```
momus/                          # workspace root
├── crates/
│   ├── momus-core/             # AST types, assertion evaluation, plan runner, template resolution
│   ├── momus-mock/             # Configurable mock HTTP server
│   ├── momus-bench/            # Load testing: steady, max-throughput, soak modes
│   ├── momus-fuzz/             # Payload mutation: boundary, encoding, type mismatch, cardinality
│   ├── momus-openapi/          # OpenAPI 3.x frontend: spec → TestPlan
│   ├── momus-fhir/             # FHIR frontend: IG package → TestPlan (extracted from fhir-autotest)
│   └── momus-cli/              # CLI binary: run, validate, mock, bench, fuzz
└── examples/
    ├── health-check.json
    └── petstore.json
```

### momus-core

The foundation. No CLI, no config file parsing, no protocol-specific logic. Just the AST and the engine that executes it.

**AST types** (`momus::ast`):
- `TestPlan` — top-level: name, base_url, default_headers, steps, setup, teardown
- `Step` — tagged union: `Request`, `Sequence`, `Parallel`, `Script`, `Noop`
- `RequestStep` — method, url, headers, body, assertions, save_as, soft_fail
- `SequenceStep` — name, steps, continue_on_failure
- `Assertion` — composable tree: `AllOf`, `AnyOf`, `Not`, `Status`, `StatusIn`, `Header`, `BodyLength`, `ContentType`, `ValidJson`, `JsonPath`, `Schema`, `ResponseTime`
- `TestResult`, `TestGroupResult`, `RunReport` — output types

**Engine** (`momus::engine`):
- `runner::execute_plan` — walks the step tree, resolves `{base_url}` and `{steps.<name>.*}` templates, dispatches HTTP requests, evaluates assertions, collects results
- `evaluator::evaluate_assertions` — evaluates the assertion tree against a response
- `templates::resolve_url`, `resolve_body`, `resolve_headers` — template substitution

**Key design decisions:**
- No transport trait yet — HTTP is the only protocol. A `TransportAdapter` trait is the right abstraction but implementing it for gRPC/GraphQL is a crate's worth of work per protocol.
- Template substitution (`{steps.<name>.*}`) replaces DAG-based dependency resolution. It's simpler, more flexible, and handles the same cases.
- The `Schema` assertion variant exists but delegates to nothing yet. Wiring in the `jsonschema` crate is the next implementation task.
- The `ResponseTime` assertion variant exists but delegates to nothing yet. It will assert that a response completed within a maximum duration.

### momus-mock

A configurable mock HTTP server for testing. Extracted from the FHIR project's `mock_server.rs`.

- Route-based response matching (`"GET /path"` → canned JSON response)
- Custom handler functions for dynamic responses
- Request recording for verification
- Graceful shutdown

### momus-bench

Load testing engine. Extracted from the FHIR project's `crates/benchmark/`.

Takes a `TestPlan` and runs it under load. The execution model is fundamentally different from the assertion runner — concurrent stateless fire-and-forget rather than sequential stateful execution.

**Modes:**
- **Steady** — fixed concurrency for a fixed duration (or one-shot)
- **Max-throughput** — ramp concurrency upward until error rate or latency threshold is breached
- **Soak** — sustained load at fixed concurrency for hours

**Features:**
- Warmup phase (N requests before recording)
- HDR histogram latency recording (P50/P90/P95/P99 per group and overall)
- Per-group statistics
- Report generation: JSON summary, full results JSON, text report, HTML dashboard
- Signal handling (Ctrl+C graceful shutdown)

**What gets replaced from the FHIR version:**
- `fhir_autotest::generate::model::TestPlan` → `momus::ast::TestPlan`
- `fhir_autotest::config::models::TestConfig` → `momus_bench::BenchConfig`
- FHIR-specific data ensure/cleanup → generic setup/teardown from the TestPlan

### momus-fuzz

Payload mutation engine. Extracted from the FHIR project's `crates/fhir-autotest-fuzz/`.

Takes a valid JSON payload and produces mutated variants. The `Mutator` trait is the extension point:

```rust
pub trait Mutator: Send + Sync {
    fn name(&self) -> &'static str;
    fn mutate(&self, base: &serde_json::Value, seed: u64) -> serde_json::Value;
}
```

**Built-in mutators:**
- **Boundary** — empty strings, very long strings, zero/negative/NaN numbers, extreme dates, null values
- **Encoding** — JSON injection, deeply nested objects, duplicate keys, unicode normalization attacks, null bytes
- **Type mismatch** — string where number expected, array where object expected, boolean where string expected
- **Cardinality** — remove required fields, duplicate array elements, add unexpected fields, empty arrays
- **Search param** — SQL injection, format string exploits, path traversal, extremely long values

The fuzzer generates a valid resource, applies each mutator, sends the variant to the server, and classifies the response (passed, rejected, error, leak). It detects information leakage (stack traces, SQL errors, path disclosure) in error responses.

### momus-openapi

An OpenAPI 3.x frontend. Walks the spec's paths and produces a `TestPlan`.

- Generates CRUD sequences for each path pattern (`POST /users` → `GET /users/{id}` → `PUT /users/{id}` → `DELETE /users/{id}`)
- Extracts response schemas for `JsonPath` and `Schema` assertions
- Generates boundary tests from parameter constraints (min/max, pattern, enum)
- Generates negative tests (required field missing, type violation)

### momus-fhir

The FHIR frontend. This is what fhir-autotest becomes after refactoring — a crate that depends on `momus-core` and compiles FHIR artifacts into `TestPlan` JSON.

The existing fhir-autotest codebase stays intact as the reference implementation. The refactoring path is:
1. Replace the inline `TestCase`/`TestPlan` types with `momus::ast::*`
2. Replace the inline assertion types with `momus::ast::Assertion`
3. Replace the inline HTTP executor with `momus::engine::runner`
4. Replace the inline mock server with `momus_mock`
5. The FHIR-specific code (IG parsing, profile resolution, resource generation, conformance test generation) stays in `momus-fhir`

### momus-cli

The CLI binary. Thin wrapper that dispatches to the appropriate crate:

```
momus run plan.json                    # momus-core: execute a test plan
momus validate plan.json               # momus-core: parse + validate a test plan
momus mock [--port 8091]               # momus-mock: start a mock server
momus bench plan.json [--concurrency N] # momus-bench: load test
momus fuzz plan.json [--iterations N]   # momus-fuzz: fuzz test
momus openapi spec.yaml                # momus-openapi: convert spec → plan
momus fhir package.tgz                 # momus-fhir: convert IG → plan
```

## What Momus Is Not

### Not a fuzzer

The `momus-fuzz` crate generates mutated payloads, but it is not a coverage-guided fuzzer (AFL-style). It applies schema-aware mutations to valid payloads and classifies server responses. True coverage-guided fuzzing is a separate engineering problem with its own tooling (cargo-fuzz, libFuzzer). Momus-fuzz is a mutation testing tool that happens to share the HTTP transport with the rest of Momus.

### Not multi-protocol (yet)

gRPC, GraphQL, and Protobuf each require fundamentally different transport and serialization. A `TransportAdapter` trait is the right abstraction, but implementing it for each protocol is a crate's worth of work per protocol. Start with HTTP/REST and prove the architecture before expanding.

## Future State: End-to-End Workflow

```
# 1. Generate a test plan from an API spec
momus openapi petstore.yaml > plan.json

# 2. Validate the plan
momus validate plan.json

# 3. Run the tests
momus run plan.json --base-url http://localhost:8080

# 4. Load test
momus bench plan.json --concurrency 50 --duration 60

# 5. Fuzz test
momus fuzz plan.json --iterations 10000

# 6. Or all at once with a FHIR IG package
momus fhir my-ig.tgz --mock
```

Each step is a separate crate with a single responsibility. They compose via the `TestPlan` JSON format — the universal contract between frontends and engines.
