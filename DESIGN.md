# Momus Design

## Origin

Momus is the generalization of [fhir-autotest](https://github.com/jlcoulter/fhir-autotest) — a tool that parses FHIR Implementation Guide packages and automatically generates conformance test suites from CapabilityStatements, StructureDefinitions, and SearchParameters. The core pipeline was:

```
IG Package → Parse → Resolve Profiles → Generate Resources → Generate Tests → Execute → Validate → Report
```

That pipeline is not FHIR-specific. Every API testing workflow follows the same shape: understand the schema, generate valid input, derive test cases from the spec, execute them, assert the responses, and report the results. Momus extracts the universal engine and defines a contract (the AST) that domain-specific frontends compile into.

## Architecture Overview

Momus follows **narrow-core-wide-composition**: `momus-core` is the foundation, and every other crate depends on it. The dependency direction is always toward `momus-core`.

```
                    ┌──────────────┐
                    │  momus-cli   │  CLI binary
                    └──────┬───────┘
                           │
          ┌────────────────┼────────────────┐
          │                │                │
          ▼                ▼                ▼
   ┌────────────┐   ┌────────────┐   ┌────────────┐
   │  momus     │   │  momus-    │   │  momus-    │
   │  bench     │   │  fuzz      │   │  chaos     │
   └────────────┘   └────────────┘   └────────────┘
          │                │                │
          └────────────────┼────────────────┘
                           │
          ┌────────────────┼────────────────┐
          │                │                │
          ▼                ▼                ▼
   ┌────────────┐   ┌────────────┐   ┌────────────┐
   │  momus-    │   │  momus-    │   │  momus-    │
   │  contract  │   │  guard     │   │  diff      │
   └────────────┘   └────────────┘   └────────────┘
          │                │                │
          └────────────────┼────────────────┘
                           │
          ┌────────────────┼────────────────┐
          │                │                │
          ▼                ▼                ▼
   ┌────────────┐   ┌────────────┐   ┌────────────┐
   │  momus-    │   │  momus-    │   │  momus     │
   │  convert   │   │  mock      │   │ (umbrella) │
   └────────────┘   └────────────┘   └────────────┘
          │                │                │
          └────────────────┼────────────────┘
                           │
                           ▼
                    ┌──────────────┐
                    │  momus-core  │  Foundation: AST, engine, templates
                    └──────────────┘
```

## Crate Map

```
momus/                          # workspace root
├── crates/
│   ├── momus/                  # Umbrella crate: re-exports, builder, prelude
│   ├── momus-core/             # AST types, assertion evaluation, plan runner, template resolution
│   ├── momus-mock/             # Configurable mock HTTP server
│   ├── momus-convert/          # Convert API descriptions into test plans
│   │   ├── curl.rs             #   cURL command → TestPlan
│   │   ├── har.rs              #   HAR file → TestPlan
│   │   ├── openapi.rs          #   OpenAPI 3.x → TestPlan
│   │   ├── postman.rs          #   Postman Collection → TestPlan
│   │   ├── graphql.rs          #   GraphQL SDL → TestPlan
│   │   ├── grpc.rs             #   gRPC proto → TestPlan
│   │   └── fhir.rs             #   FHIR IG → TestPlan
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

---

## momus (umbrella)

**Status:** ✅ v0.1.0 — Complete

The top-level `momus` crate re-exports all sub-crates and provides convenience APIs:

- **`prelude`** — re-exports common types (`TestPlan`, `Step`, `Assertion`, `Method`, `RunReport`, `runner`)
- **`builder`** — programmatic plan construction (`TestPlanBuilder`, `RequestStepBuilder`, `SequenceStepBuilder`)
- **`load_plan` / `parse_plan` / `validate_plan`** — convenience functions

Users who want the full toolkit depend on `momus`. Users who only need the AST depend on `momus-core`.

---

## momus-core

**Status:** ✅ v0.1.0 — Core complete, some assertion variants are no-ops

The foundation. No CLI, no config file parsing, no protocol-specific logic. Just the AST and the engine that executes it.

### AST Types (`momus_core::ast`)

```
TestPlan
├── name: String
├── base_url: Option<String>
├── default_headers: HashMap<String, String>
├── steps: Vec<Step>
├── setup: Option<Vec<Step>>
├── teardown: Option<Vec<Step>>
└── output: Option<String>

Step (enum)
├── Request(RequestStep)
├── Sequence(SequenceStep)
├── Parallel(Vec<Step>)
├── Script(ScriptStep)
└── Noop(String)

RequestStep
├── name: String
├── method: Method
├── url: String
├── headers: HashMap<String, String>
├── body: Option<Value>
├── assertions: Vec<Assertion>
├── save_as: Option<String>
└── soft_fail: bool

SequenceStep
├── name: String
├── steps: Vec<Step>
└── continue_on_failure: bool

ScriptStep
├── name: String
├── language: String
└── source: String
```

### Assertion AST (`momus_core::ast::assertion`)

```
Assertion (enum)
├── AllOf(Vec<Assertion>)           — AND: all sub-assertions must pass
├── AnyOf(Vec<Assertion>)           — OR: at least one must pass
├── Not(Box<Assertion>)             — NOT: sub-assertion must fail
├── Status(u16)                     — Exact HTTP status code
├── StatusIn(Vec<u16>)              — Status in a set
├── Header(HeaderPredicate)         — Header present/absent/equals/contains/regex
├── BodyLength(BodyLengthPredicate) — Body size constraints
├── ContentType(String)             — Content-Type header match
├── ValidJson                       — Response must be valid JSON
├── JsonPath(JsonPredicate)         — JSONPath query with predicate
├── Schema(Value)                   — JSON Schema validation (WIP — no-op)
└── ResponseTime(u64)               — Max response time in ms (WIP — no-op)
```

### Engine (`momus_core::engine`)

- **`runner::execute_plan`** — walks the step tree, resolves `{base_url}` and `{steps.<name>.*}` templates, dispatches HTTP requests via reqwest, evaluates assertions, collects results into `RunReport`
- **`evaluator::evaluate_assertions`** — evaluates the assertion tree against a response. Includes a simple JSONPath resolver (supports `$.key`, `$.key.nested`, `$.key[*]`, `$.key[0]`)
- **`templates::resolve_url`, `resolve_body`, `resolve_headers`** — template substitution for `{base_url}` and `{steps.<name>.*}`

### Key Design Decisions

- **No transport trait yet** — HTTP is the only protocol. A `TransportAdapter` trait is the right abstraction but implementing it for gRPC/GraphQL is a crate's worth of work per protocol.
- **Template substitution** (`{steps.<name>.*}`) replaces DAG-based dependency resolution. It's simpler, more flexible, and handles the same cases.
- **The `Schema` assertion variant** exists but delegates to nothing yet. Wiring in the `jsonschema` crate is the next implementation task.
- **The `ResponseTime` assertion variant** exists but delegates to nothing yet. It will assert that a response completed within a maximum duration.
- **Script steps** are a placeholder — not implemented.

### Gaps to Fill (v0.2.0+)

| Gap | Priority | Description |
|-----|----------|-------------|
| Schema assertion | High | Wire in `jsonschema` crate for JSON Schema validation |
| ResponseTime assertion | Medium | Measure and assert response duration |
| Script steps | Low | Implement script execution (rhai/wasm) |
| Full JSONPath | Medium | Replace simple JSONPath with `jsonpath-rust` or similar |
| Config system | High | Add generic TOML config parsing (port from fhir-autotest) |
| Dependency resolver | Medium | Add generic topological sort for resource creation order |
| Report output | Medium | Add JSON file output for RunReport (port from fhir-autotest) |

---

## momus-mock

**Status:** ✅ v0.1.0 — Complete

A configurable mock HTTP server for testing.

### Features

- Route-based response matching (`"GET /path"` → canned JSON response)
- Custom handler functions for dynamic responses
- Request recording for verification
- Graceful shutdown

### API

```rust
let mut server = MockServer::new();
server.when("GET /health", MockResponse::json(json!({"status": "ok"})).with_status(200));
server.start(8091).await?;
let requests = server.recorded_requests();
server.stop();
```

### Gaps to Fill (v0.2.0+)

| Gap | Priority | Description |
|-----|----------|-------------|
| Stateful CRUD store | High | In-memory resource store with CRUD operations (port from fhir-autotest mock_server.rs) |
| Search/filter support | High | Query parameter filtering, sorting, pagination |
| Latency simulation | Medium | Configurable response delays per route |
| Fault injection | Medium | Return errors, timeouts, connection resets per route |
| TLS support | Low | HTTPS mock server |
| WebSocket support | Low | WebSocket mock endpoints |

---

## momus-convert

**Status:** 🔜 v0.1.0 — Scaffolded (all converters are stubs)

Converts API descriptions into `TestPlan` JSON. Each format is a feature-gated module.

### Converter Interface

```rust
pub fn convert(input: &str) -> Result<TestPlan>;
```

All converters share this interface. The dispatcher in `lib.rs` routes by format name.

### Format Status

| Module | Format | Status | Target |
|--------|--------|--------|--------|
| `curl` | cURL command string | 🔜 Stub | v0.2.0 |
| `har` | HAR (HTTP Archive) file | 🔜 Stub | v0.2.0 |
| `openapi` | OpenAPI 3.x YAML/JSON | 🔜 Stub | v0.2.0 |
| `postman` | Postman Collection v2.1 | 🔜 Stub | v0.2.0 |
| `graphql` | GraphQL SDL / introspection | 🔜 Stub | v0.3.0 |
| `grpc` | gRPC proto file | 🔜 Stub | v0.4.0 |
| `fhir` | FHIR IG package | 🔜 Stub | v0.2.0 |

### cURL Converter (v0.2.0 target)

Parses cURL command strings into `TestPlan`:

- `-X`/`--request` → HTTP method
- `-H`/`--header` → request headers
- `-d`/`--data`/`--data-raw`/`--data-binary` → request body
- URL → request URL
- `--max-time` → timeout
- `-u`/`--user` → basic auth header
- `-b`/`--cookie` → cookie header

### HAR Converter (v0.2.0 target)

Reads HAR 1.2 format, extracts each request/response pair, and produces a `RequestStep` with a `Status` assertion matching the recorded status code. The generated plan is a starting point — users add `JsonPath`, `Schema`, and `ResponseTime` assertions on top.

### OpenAPI Converter (v0.2.0 target)

Parses OpenAPI 3.x YAML/JSON specs and generates test plans:

- Each path + operation → `RequestStep`
- Request body schemas → example body generation
- Response schemas → `Schema` assertions
- Parameters → URL/header/query construction
- Security schemes → auth header setup

### Postman Converter (v0.2.0 target)

Parses Postman Collection v2.1 JSON:

- Each request in the collection → `RequestStep`
- Variables → template substitution
- Tests/scripts → assertion mapping (best-effort)
- Auth → header setup

### GraphQL Converter (v0.3.0 target)

Parses GraphQL SDL or introspection result:

- Each query/mutation/subscription → `RequestStep`
- Input types → example variable generation
- Response types → `JsonPath` assertions
- Schema validation → `Schema` assertions

### gRPC Converter (v0.4.0 target)

Parses protobuf definitions:

- Each RPC method → test case
- Message types → example payload generation
- Requires protobuf compilation tooling

### FHIR Converter (v0.2.0 target)

**This is the most complex converter.** It ports the full fhir-autotest pipeline into a generic framework. The FHIR converter takes an IG package (.tgz) and produces a `TestPlan` with generated resources and comprehensive test cases.

#### Pipeline

```
IG Package (.tgz)
    │
    ├── parse_package() → IgPackage
    │   ├── CapabilityStatements
    │   ├── StructureDefinitions (profiles)
    │   ├── SearchParameters
    │   ├── OperationDefinitions
    │   └── raw_resources (ValueSets, CodeSystems, etc.)
    │
    ├── select_capability_statement() → CapabilityStatement
    │
    ├── resolve_parent_chain() — download missing parent profiles
    │   from FHIR package registry, merge snapshots
    │
    ├── extract_dependencies() → resolve_creation_order()
    │   (topological sort via petgraph)
    │
    ├── generate_resource_with_value_sets() per profile
    │   (5-pass resource generation from StructureDefinitions)
    │
    ├── generate_test_plan() → TestPlan
    │   ├── CRUD tests (read, vread, create, update, delete, patch, history)
    │   ├── Search tests (single param, modifiers, prefixes, near, combo, chained)
    │   ├── Include tests (_include, _revinclude)
    │   ├── Result param tests (_summary, _elements, _count, _sort, _has)
    │   ├── Operation tests ($operation)
    │   ├── Negative tests (undeclared interactions/params)
    │   └── Conformance tests (mustSupport field presence)
    │
    └── Output: TestPlan JSON + generated resources
```

#### FHIR Model Types (ported from fhir-autotest)

```rust
// Profile model
struct StructureDefinition { url, name, base_type, kind, snapshot, differential, ... }
struct ElementDefinition { id, path, min, max, type_, fixed_*, pattern_*, must_support, ... }
struct ElementDefinitionType { code, target_profile, profile, ... }

// CapabilityStatement model
struct CapabilityStatement { url, name, rest: Vec<Rest>, ... }
struct Rest { mode, resource: Vec<RestResource>, interaction, search_param, operation, ... }
struct RestResource { type_, interaction: Vec<Interaction>, search_param: Vec<SearchParam>, ... }

// SearchParameter model
struct SearchParameter { url, name, base: Vec<String>, type_, expression, ... }

// OperationDefinition model
struct OperationDefinition { url, name, kind, system, type_, instance, parameter, ... }
```

#### Resource Generator (5-pass)

1. **Required fields** — populate elements with `min > 0`
2. **Required slices** — populate sliced elements with discriminators
3. **Extension slices** — populate extension slices defined by the profile
4. **MustSupport backbones** — populate `mustSupport=true` BackboneElements with `min=0`
5. **MustSupport optional fields** — populate `mustSupport=true` non-backbone fields

Type-specific value generation for: HumanName, Address, Identifier, CodeableConcept, Coding, ContactPoint, Period, Quantity, etc.

Value set resolution: builds maps from ValueSet → system, CodeSystem → first code.

#### Test Plan Generator

Generates test cases from CapabilityStatement for each resource type:

| Category | Test Types | Assertions |
|----------|-----------|------------|
| CRUD | read, vread, create, update, delete, patch, history-instance, history-type | Status code, profile validation |
| Search | Single param, modifiers, prefixes, near, combo, chained | Bundle type=searchset, entry count |
| Include | _include, _revinclude | Included resource types present |
| Result Params | _summary, _elements, _count, _sort, _has | Absent fields, max entries, sort order |
| Operations | $operation | Response key presence, resourceType allow-list |
| Negative | Undeclared interactions, undeclared params | OperationOutcome severity |
| Conformance | mustSupport field presence | Required fields present in Bundle entries |

#### FHIR-Specific Assertions

The FHIR converter produces `TestPlan` JSON that uses the standard momus assertion AST. FHIR-specific response validation (Bundle structure, OperationOutcome, mustSupport) is encoded as `JsonPath` assertions and `Status` assertions. The `momus-contract` crate can additionally validate responses against FHIR profiles.

---

## momus-bench

**Status:** 🔜 v0.1.0 — Scaffolded (types + config + report, runner is stub)

Load testing engine. Takes a `TestPlan` and runs it under load. The execution model is fundamentally different from the assertion runner — concurrent stateless fire-and-forget rather than sequential stateful execution.

### Modes

| Mode | Description | Configuration |
|------|-------------|---------------|
| **Steady** | Fixed concurrency for a fixed duration | `--concurrency N --duration S` |
| **Max-throughput** | Ramp concurrency upward until error rate or latency threshold breached | `--min-concurrency N --max-concurrency N --error-threshold P` |
| **Soak** | Sustained load at fixed concurrency for hours | `--concurrency N --duration H` |

### Features (planned)

- Warmup phase (N requests before recording)
- HDR histogram latency recording (P50/P90/P95/P99 per group and overall)
- Per-group statistics
- Report generation: JSON summary, full results JSON, text report, HTML dashboard
- Signal handling (Ctrl+C graceful shutdown)

### Implementation Plan (v0.2.0)

1. Implement `run_steady()` — spawn N concurrent workers, each iterating the plan steps, collect latency histograms
2. Implement `run_max_throughput()` — binary search for max concurrency within error/latency bounds
3. Implement `run_soak()` — steady load with periodic health checks
4. Add HDR histogram dependency (or implement a simple histogram)
5. Add report output (JSON + text)

---

## momus-fuzz

**Status:** 🔜 v0.1.0 — Mutators implemented and tested, runner is stub

Payload mutation engine. Takes a valid JSON payload and produces mutated variants.

### Mutator Trait

```rust
pub trait Mutator: Send + Sync {
    fn name(&self) -> &'static str;
    fn mutate(&self, base: &serde_json::Value, seed: u64) -> serde_json::Value;
}
```

### Built-in Mutators (v0.1.0)

| Mutator | Description | Examples |
|---------|-------------|---------|
| **Boundary** | Edge case values | Empty strings, very long strings, zero/negative/NaN numbers, extreme dates, null values |
| **Encoding** | Injection attacks | JSON injection, deeply nested objects, duplicate keys, unicode normalization attacks, null bytes |
| **Type mismatch** | Wrong types | String where number expected, array where object expected, boolean where string expected |
| **Cardinality** | Structure changes | Remove required fields, duplicate array elements, add unexpected fields, empty arrays |

All mutators use a deterministic PRNG (`SimpleRng`) so the same `(base, seed)` pair always produces the same mutation.

### Implementation Plan (v0.2.0)

1. Implement `run_fuzz()` — iterate mutations, send to server, classify responses
2. Response classification: pass (2xx), fail (4xx/5xx), crash (connection error), timeout
3. Report generation: mutation count, pass/fail/crash breakdown, interesting mutations
4. Add more mutators: unicode normalization, format string injection, path traversal, SSRF attempts

---

## momus-chaos

**Status:** 🔜 v0.1.0 — Scaffolded (types + config + report, experiments are stubs)

Chaos engineering engine. Injects infrastructure-level faults into a running system and verifies that the system self-heals, degrades gracefully, or fails safely.

### Experiment Types

| Category | Experiment | Description | Implementation |
|----------|-----------|-------------|----------------|
| Network | `NetworkLatency` | Inject artificial delay into requests to a specific endpoint | Proxy-based (mitmproxy/hyper) |
| Network | `ConnectionReset` | Simulate connection resets for a percentage of requests | TCP-level (iptables) |
| Network | `PacketLoss` | Drop a percentage of requests | Network-level (tc netem) |
| Service | `ServiceError` | Return a specific HTTP status code for a matching endpoint | Proxy-based |
| Service | `ServiceDown` | Simulate a downstream service being unreachable | DNS/network-level |
| Resource | `CpuPressure` | Busy-loop on N cores | Process-level (stress-ng) |
| Resource | `MemoryPressure` | Allocate N MB of memory | Process-level |
| State | `ClockSkew` | Simulate clock offset | System-level (faketime) |

### Implementation Plan (v0.2.0)

1. Implement proxy-based experiments (NetworkLatency, ServiceError, ServiceDown) using a forward proxy
2. Implement resource experiments (CpuPressure, MemoryPressure) using subprocess management
3. Implement health check verification before/during/after experiments
4. Report generation: experiment results, system behavior, healing time

### Design Notes

Momus-chaos defines experiment types and reports, but the actual fault injection requires platform-specific tooling (tc, stress-ng, iptables). Momus-chaos orchestrates experiments and validates system behavior — it does not replace dedicated chaos engineering platforms like Chaos Mesh or Gremlin.

---

## momus-contract

**Status:** 🔜 v0.1.0 — Scaffolded (types + config + report, runner is stub)

Contract testing. Runs a test plan and validates each response against the API's declared schema (OpenAPI or GraphQL). Reports compliance percentage, missing fields, type mismatches, and undocumented fields.

### Features (planned)

- OpenAPI 3.x response validation
- GraphQL response validation against schema
- Response field coverage analysis
- Undocumented field detection
- Type mismatch detection
- Compliance percentage reporting

### Implementation Plan (v0.2.0)

1. Implement OpenAPI response validation using `openapiv3` crate
2. Implement GraphQL response validation
3. Wire into the momus-core runner as a post-execution validation pass
4. Report generation: per-endpoint compliance, field coverage, violations

### FHIR Profile Validation

The contract crate is also the natural home for FHIR profile validation (ported from fhir-autotest's `validate_against_profile()`). When a test plan is generated by the FHIR converter, the contract crate can validate responses against StructureDefinition profiles:

- Resource type matching
- Required element presence (min > 0)
- Fixed/pattern value matching
- MustSupport field presence (best-effort)

---

## momus-guard

**Status:** 🔜 v0.1.0 — Scaffolded (types + config + report, runner is stub)

Security scanning. Inspects responses for common security issues.

### Checks (planned)

| Check | Description | Detection |
|-------|-------------|-----------|
| Auth headers | Missing or weak authentication | Check for Authorization header presence and scheme |
| CORS | CORS misconfiguration | Check `Access-Control-Allow-Origin` for permissive values |
| Info leaks | Information leakage in error bodies | Scan for stack traces, SQL errors, path disclosure |
| Exposed endpoints | Internal endpoints exposed | Check for common internal paths |
| Security headers | Missing security headers | Check for HSTS, CSP, X-Content-Type-Options, X-Frame-Options |

### Implementation Plan (v0.2.0)

1. Implement header checks (auth, CORS, security headers)
2. Implement response body scanning for info leaks
3. Implement endpoint discovery (common paths, swagger docs, etc.)
4. Report generation: issues found, severity, remediation

---

## momus-diff

**Status:** 🔜 v0.1.0 — Scaffolded (types + config + report, runner is stub)

Regression/diff testing. Runs the same test plan against two environments (e.g. staging vs production) and reports differences.

### Features (planned)

- Status code comparison
- Response body diff (field-level)
- Header comparison
- New/missing field detection
- Value change detection

### Implementation Plan (v0.2.0)

1. Implement parallel execution against two endpoints
2. Implement response comparison (status, headers, body)
3. Implement structured JSON diff (field-level, not line-level)
4. Report generation: changes found, severity classification

---

## momus-cli

**Status:** ✅ v0.1.0 — Complete (all subcommands wired, some return empty reports)

The CLI binary. Thin wrapper that dispatches to the appropriate crate.

### Commands

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
momus convert openapi spec.yaml        # momus-convert: OpenAPI → plan
momus convert postman collection.json   # momus-convert: Postman → plan
momus convert graphql schema.graphql   # momus-convert: SDL → plan
momus convert grpc proto/service.proto # momus-convert: protobuf → plan
momus convert fhir package.tgz         # momus-convert: FHIR IG → plan
```

### CLI Flags (shared)

| Flag | Applies to | Description |
|------|-----------|-------------|
| `--base-url` | run, bench, fuzz, chaos, guard | Override base URL |
| `--output` | run, bench, fuzz, chaos, contract, guard, diff | Output directory |
| `--config` | All | Config file path |
| `--verbose` | All | Verbose logging |

---

## FHIR Autotest Porting Plan

The following table maps every module from fhir-autotest to its destination in momus:

| fhir-autotest Module | Lines | Destination | Status |
|---------------------|-------|-------------|--------|
| `config/models.rs` | 490 | `momus-core` (generic config) + `momus-convert/fhir.rs` (FHIR-specific) | 🔜 Port |
| `model/profile.rs` | 180 | `momus-convert/fhir.rs` | 🔜 Port |
| `model/capability.rs` | ~100 | `momus-convert/fhir.rs` | 🔜 Port |
| `model/search_param.rs` | ~50 | `momus-convert/fhir.rs` | 🔜 Port |
| `model/operation.rs` | ~50 | `momus-convert/fhir.rs` | 🔜 Port |
| `parse/package.rs` | ~200 | `momus-convert/fhir.rs` | 🔜 Port |
| `parse/profile_resolver.rs` | ~200 | `momus-convert/fhir.rs` | 🔜 Port |
| `generate/model.rs` | 373 | `momus-convert/fhir.rs` (FHIR test model) | 🔜 Port |
| `generate/planner.rs` | 2383 | `momus-convert/fhir.rs` | 🔜 Port |
| `generate/conformance.rs` | 902 | `momus-convert/fhir.rs` | 🔜 Port |
| `generate/dependency_resolver.rs` | ~150 | `momus-core` (generic) + `momus-convert/fhir.rs` (FHIR-specific) | 🔜 Port |
| `generate/value_resolver.rs` | ~200 | `momus-convert/fhir.rs` | 🔜 Port |
| `generate/resource_generator/` | 2561 | `momus-convert/fhir.rs` | 🔜 Port |
| `generate/bulk_data.rs` | 2972 | `momus-convert/fhir.rs` | 🔜 Port |
| `generate/hcpd.rs` | ~200 | `momus-convert/fhir.rs` | 🔜 Port |
| `generate/locality.rs` | ~100 | `momus-convert/fhir.rs` | 🔜 Port |
| `runner/orchestrator.rs` | 1008 | `momus-convert/fhir.rs` (FHIR orchestrator) | 🔜 Port |
| `runner/executor.rs` | 752 | `momus-core` (generic executor) | 🔜 Port |
| `runner/response_assertions.rs` | 1042 | `momus-core` (generic assertions) + `momus-convert/fhir.rs` (FHIR-specific) | 🔜 Port |
| `runner/validator.rs` | 570 | `momus-contract` (profile validation) | 🔜 Port |
| `runner/bulk_loader.rs` | 1788 | `momus-convert/fhir.rs` | 🔜 Port |
| `mock_server.rs` | 806 | `momus-mock` (stateful CRUD store) | 🔜 Port |
| `main.rs` | ~200 | `momus-cli` (already wired) | ✅ Done |
| `lib.rs` | 400 | `momus-convert/fhir.rs` (orchestration functions) | 🔜 Port |

### Porting Strategy

1. **Phase 1 (v0.2.0):** Port the core FHIR types, package parser, and test plan generator into `momus-convert/fhir.rs`. This makes `momus convert fhir package.tgz` produce a valid `TestPlan` JSON.

2. **Phase 2 (v0.2.0):** Port the resource generator, dependency resolver, and value resolver. This enables `momus run` with FHIR-generated plans.

3. **Phase 3 (v0.2.0):** Port the orchestrator, executor, and response assertions into the generic momus-core runner. Add FHIR-specific assertions as `JsonPath` predicates.

4. **Phase 4 (v0.2.0):** Port the mock server's stateful CRUD store into `momus-mock`. Port the profile validator into `momus-contract`.

5. **Phase 5 (v0.3.0):** Port bulk data generation, HCPD-specific generation, and locality data.

---

## What Momus Is Not

### Not a fuzzer

The `momus-fuzz` crate generates mutated payloads, but it is not a coverage-guided fuzzer (AFL-style). It applies schema-aware mutations to valid payloads and classifies server responses. True coverage-guided fuzzing is a separate engineering problem with its own tooling (cargo-fuzz, libFuzzer). Momus-fuzz is a mutation testing tool that happens to share the HTTP transport with the rest of Momus.

### Not multi-protocol (yet)

gRPC, GraphQL, and Protobuf each require fundamentally different transport and serialization. A `TransportAdapter` trait is the right abstraction, but implementing it for each protocol is a crate's worth of work per protocol. Start with HTTP/REST and prove the architecture before expanding.

### Not a chaos platform

The `momus-chaos` crate defines experiment types and reports, but the actual fault injection (network latency, CPU pressure, etc.) requires platform-specific tooling (tc, stress-ng, iptables). Momus-chaos orchestrates experiments and validates system behavior — it does not replace dedicated chaos engineering platforms like Chaos Mesh or Gremlin.

---

## Future State: End-to-End Workflow

```bash
# 1. Generate a test plan from a curl command
momus convert curl 'curl -X POST https://api.example.com/users -d "{\"name\":\"test\"}"' > plan.json

# 2. Or from recorded browser traffic
momus convert har traffic.har > plan.json

# 3. Or from a FHIR IG package
momus convert fhir package.tgz > plan.json

# 4. Validate the plan
momus validate plan.json

# 5. Run the tests
momus run plan.json --base-url http://localhost:8080

# 6. Load test
momus bench plan.json --concurrency 50 --duration 60

# 7. Fuzz test
momus fuzz plan.json --iterations 10000

# 8. Chaos test
momus chaos plan.json

# 9. Contract validation
momus contract plan.json --spec openapi.yaml

# 10. Security scan
momus guard plan.json

# 11. Diff between environments
momus diff plan.json --baseline https://prod --target https://staging
```

Each step is a separate crate with a single responsibility. They compose via the `TestPlan` JSON format — the universal contract between frontends and engines.
