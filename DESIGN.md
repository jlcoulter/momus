# Momus Design

## Origin

Momus is the generalization of [fhir-autotest](https://github.com/jlcoulter/fhir-autotest) — a tool that parses FHIR Implementation Guide packages and automatically generates conformance test suites from CapabilityStatements, StructureDefinitions, and SearchParameters. The core pipeline was:

```
IG Package → Parse → Resolve Profiles → Generate Resources → Generate Tests → Execute → Validate → Report
```

That pipeline is not FHIR-specific. Every API testing workflow follows the same shape: understand the schema, generate valid input, derive test cases from the spec, execute them, assert the responses, and report the results. Momus extracts the universal engine and defines a contract (the AST) that domain-specific frontends compile into.

## Crate Map

The Momus workspace is organized as a set of crates, each with a single responsibility. The dependency direction is always toward `momus-core` — every other crate depends on the core, never the other way.

```
momus/                          # workspace root
├── crates/
│   ├── momus/                  # Umbrella crate: re-exports, builder, prelude
│   ├── momus-core/             # AST types, assertion evaluation, plan runner, template resolution
│   ├── momus-mock/             # Configurable mock HTTP server
│   ├── momus-convert/          # Convert API descriptions into test plans
│   │   ├── curl.rs             #   cURL command → TestPlan (v0.1.0)
│   │   ├── har.rs              #   HAR file → TestPlan (v0.1.0)
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
│   └── momus-cli/              # CLI binary: run, validate, mock, bench, fuzz, chaos, contract, guard, diff, convert
└── examples/
    ├── health-check.json
    └── crud-sequence.json
```

### momus (umbrella)

The top-level `momus` crate re-exports all sub-crates and provides convenience APIs:

- **`prelude`** — re-exports common types (`TestPlan`, `Step`, `Assertion`, `Method`, `RunReport`, `runner`)
- **`builder`** — programmatic plan construction (`TestPlanBuilder`, `RequestStepBuilder`, `SequenceStepBuilder`)
- **`load_plan` / `parse_plan` / `validate_plan`** — convenience functions

Users who want the full toolkit depend on `momus`. Users who only need the AST depend on `momus-core`.

### momus-core

The foundation. No CLI, no config file parsing, no protocol-specific logic. Just the AST and the engine that executes it.

**AST types** (`momus_core::ast`):
- `TestPlan` — top-level: name, base_url, default_headers, steps, setup, teardown
- `Step` — tagged union: `Request`, `Sequence`, `Parallel`, `Script`, `Noop`
- `RequestStep` — method, url, headers, body, assertions, save_as, soft_fail
- `SequenceStep` — name, steps, continue_on_failure
- `Assertion` — composable tree: `AllOf`, `AnyOf`, `Not`, `Status`, `StatusIn`, `Header`, `BodyLength`, `ContentType`, `ValidJson`, `JsonPath`, `Schema`, `ResponseTime`
- `TestResult`, `TestGroupResult`, `RunReport` — output types

**Engine** (`momus_core::engine`):
- `runner::execute_plan` — walks the step tree, resolves `{base_url}` and `{steps.<name>.*}` templates, dispatches HTTP requests, evaluates assertions, collects results
- `evaluator::evaluate_assertions` — evaluates the assertion tree against a response
- `templates::resolve_url`, `resolve_body`, `resolve_headers` — template substitution

**Key design decisions:**
- No transport trait yet — HTTP is the only protocol. A `TransportAdapter` trait is the right abstraction but implementing it for gRPC/GraphQL is a crate's worth of work per protocol.
- Template substitution (`{steps.<name>.*}`) replaces DAG-based dependency resolution. It's simpler, more flexible, and handles the same cases.
- The `Schema` assertion variant exists but delegates to nothing yet. Wiring in the `jsonschema` crate is the next implementation task.
- The `ResponseTime` assertion variant exists but delegates to nothing yet. It will assert that a response completed within a maximum duration.

### momus-mock

A configurable mock HTTP server for testing.

- Route-based response matching (`"GET /path"` → canned JSON response)
- Custom handler functions for dynamic responses
- Request recording for verification
- Graceful shutdown

### momus-convert

Converts API descriptions into `TestPlan` JSON. Each format is a feature-gated module:

| Module | Format | Status |
|--------|--------|--------|
| `curl` | cURL command string | ✅ v0.1.0 — full parser |
| `har` | HAR (HTTP Archive) file | ✅ v0.1.0 — full converter |
| `openapi` | OpenAPI 3.x YAML/JSON | 🔜 Stub |
| `postman` | Postman Collection v2.1 | 🔜 Stub |
| `graphql` | GraphQL SDL / introspection | 🔜 Stub |
| `grpc` | gRPC proto file | 🔜 Stub |
| `fhir` | FHIR IG package | 🔜 Stub |

The converters are modules inside a single crate rather than individual crates because they share the same interface (`fn convert(input: &str) -> Result<TestPlan>`) and dependency set. They can graduate to their own crates when implementations warrant it.

**cURL parser** handles: `-X`/`--request`, `-H`/`--header`, `-d`/`--data`/`--data-raw`/`--data-binary`, URL, `--max-time`, `-u`/`--user` (basic auth), and `-b`/`--cookie`.

**HAR converter** reads HAR 1.2 format, extracts each request/response pair, and produces a `RequestStep` with a `Status` assertion matching the recorded status code. The generated plan is a starting point — users add `JsonPath`, `Schema`, and `ResponseTime` assertions on top.

### momus-bench

Load testing engine. Takes a `TestPlan` and runs it under load. The execution model is fundamentally different from the assertion runner — concurrent stateless fire-and-forget rather than sequential stateful execution.

**Modes:**
- **Steady** — fixed concurrency for a fixed duration (or one-shot)
- **Max-throughput** — ramp concurrency upward until error rate or latency threshold is breached
- **Soak** — sustained load at fixed concurrency for hours

**Features (planned):**
- Warmup phase (N requests before recording)
- HDR histogram latency recording (P50/P90/P95/P99 per group and overall)
- Per-group statistics
- Report generation: JSON summary, full results JSON, text report, HTML dashboard
- Signal handling (Ctrl+C graceful shutdown)

**Status:** Scaffolded in v0.1.0 with types, config, and report structs. Runner stubs return empty reports. Implementation planned for v0.2.0.

### momus-fuzz

Payload mutation engine. Takes a valid JSON payload and produces mutated variants. The `Mutator` trait is the extension point:

```rust
pub trait Mutator: Send + Sync {
    fn name(&self) -> &'static str;
    fn mutate(&self, base: &serde_json::Value, seed: u64) -> serde_json::Value;
}
```

**Built-in mutators (v0.1.0):**
- **Boundary** — empty strings, very long strings, zero/negative/NaN numbers, extreme dates, null values
- **Encoding** — JSON injection, deeply nested objects, duplicate keys, unicode normalization attacks, null bytes
- **Type mismatch** — string where number expected, array where object expected, boolean where string expected
- **Cardinality** — remove required fields, duplicate array elements, add unexpected fields, empty arrays

All mutators use a deterministic PRNG (`SimpleRng`) so the same `(base, seed)` pair always produces the same mutation.

**Status:** Mutators are implemented and tested. The runner that sends mutations to a server and classifies responses is a stub — implementation planned for v0.2.0.

### momus-chaos

Chaos engineering engine. Injects infrastructure-level faults into a running system and verifies that the system self-heals, degrades gracefully, or fails safely.

**Experiment types (v0.1.0):**

| Category | Experiment | Description |
|----------|-----------|-------------|
| Network | `NetworkLatency` | Inject artificial delay into requests to a specific endpoint |
| Network | `ConnectionReset` | Simulate connection resets for a percentage of requests |
| Network | `PacketLoss` | Drop a percentage of requests |
| Service | `ServiceError` | Return a specific HTTP status code for a matching endpoint |
| Service | `ServiceDown` | Simulate a downstream service being unreachable |
| Resource | `CpuPressure` | Busy-loop on N cores |
| Resource | `MemoryPressure` | Allocate N MB of memory |
| State | `ClockSkew` | Simulate clock offset |

**Status:** Scaffolded in v0.1.0 with types, config, and report structs. Runner stubs return empty reports. Implementation planned for v0.2.0.

### momus-contract

Contract testing. Runs a test plan and validates each response against the API's declared schema (OpenAPI or GraphQL). Reports compliance percentage, missing fields, type mismatches, and undocumented fields.

**Status:** Scaffolded in v0.1.0. Implementation planned for v0.2.0.

### momus-guard

Security scanning. Inspects responses for common security issues:

- Missing or weak authentication headers
- CORS misconfiguration (permissive origins, credentials with wildcard)
- Information leakage (stack traces, SQL errors, path disclosure in error bodies)
- Exposed internal endpoints
- Missing security headers (HSTS, CSP, X-Content-Type-Options)

**Status:** Scaffolded in v0.1.0. Implementation planned for v0.2.0.

### momus-diff

Regression/diff testing. Runs the same test plan against two environments (e.g. staging vs production) and reports differences:

- Status code changes
- New or missing response fields
- Modified field values
- Header differences

**Status:** Scaffolded in v0.1.0. Implementation planned for v0.2.0.

### momus-cli

The CLI binary. Thin wrapper that dispatches to the appropriate crate:

```
momus run plan.json                    # momus-core: execute a test plan
momus validate plan.json               # momus-core: parse + validate a test plan
momus mock [--port 8091]               # momus-mock: start a mock server
momus bench plan.json [--concurrency N] # momus-bench: load test
momus fuzz plan.json [--iterations N]   # momus-fuzz: fuzz test
momus chaos plan.json                  # momus-chaos: chaos experiments
momus contract plan.json --spec spec.yaml # momus-contract: contract validation
momus guard plan.json                  # momus-guard: security scan
momus diff plan.json --baseline URL --target URL # momus-diff: regression diff

# Convert API descriptions into test plans
momus convert curl 'curl ...'          # momus-convert: curl command → plan
momus convert har traffic.har          # momus-convert: HAR → plan
momus convert openapi spec.yaml        # momus-convert: OpenAPI → plan (WIP)
momus convert postman collection.json  # momus-convert: Postman → plan (WIP)
momus convert graphql schema.graphql   # momus-convert: SDL → plan (WIP)
momus convert grpc proto/service.proto # momus-convert: protobuf → plan (WIP)
momus convert fhir package.tgz         # momus-convert: FHIR IG → plan (WIP)
```

## What Momus Is Not

### Not a fuzzer

The `momus-fuzz` crate generates mutated payloads, but it is not a coverage-guided fuzzer (AFL-style). It applies schema-aware mutations to valid payloads and classifies server responses. True coverage-guided fuzzing is a separate engineering problem with its own tooling (cargo-fuzz, libFuzzer). Momus-fuzz is a mutation testing tool that happens to share the HTTP transport with the rest of Momus.

### Not multi-protocol (yet)

gRPC, GraphQL, and Protobuf each require fundamentally different transport and serialization. A `TransportAdapter` trait is the right abstraction, but implementing it for each protocol is a crate's worth of work per protocol. Start with HTTP/REST and prove the architecture before expanding.

### Not a chaos platform

The `momus-chaos` crate defines experiment types and reports, but the actual fault injection (network latency, CPU pressure, etc.) requires platform-specific tooling (tc, stress-ng, iptables). Momus-chaos orchestrates experiments and validates system behavior — it does not replace dedicated chaos engineering platforms like Chaos Mesh or Gremlin.

## Future State: End-to-End Workflow

```
# 1. Generate a test plan from a curl command
momus convert curl 'curl -X POST https://api.example.com/users -d "{\"name\":\"test\"}"' > plan.json

# 2. Or from recorded browser traffic
momus convert har traffic.har > plan.json

# 3. Validate the plan
momus validate plan.json

# 4. Run the tests
momus run plan.json --base-url http://localhost:8080

# 5. Load test
momus bench plan.json --concurrency 50 --duration 60

# 6. Fuzz test
momus fuzz plan.json --iterations 10000

# 7. Chaos test
momus chaos plan.json

# 8. Contract validation
momus contract plan.json --spec openapi.yaml

# 9. Security scan
momus guard plan.json

# 10. Diff between environments
momus diff plan.json --baseline https://prod --target https://staging
```

Each step is a separate crate with a single responsibility. They compose via the `TestPlan` JSON format — the universal contract between frontends and engines.
