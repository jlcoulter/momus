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
          ┌────────────────┼──────────────────────────┐
          │                │                          │
          ▼                ▼                          ▼
   ┌────────────┐   ┌────────────┐   ┌──────────────────────┐
   │  momus     │   │  momus-    │   │  momus-              │
   │  bench     │   │  fuzz      │   │  chaos               │
   └────────────┘   └────────────┘   └──────────────────────┘
          │                │                          │
          └────────────────┼──────────────────────────┘
                           │
          ┌────────────────┼──────────────────────────┐
          │                │                          │
          ▼                ▼                          ▼
   ┌────────────┐   ┌────────────┐   ┌──────────────────────┐
   │  momus-    │   │  momus-    │   │  momus-              │
   │  contract  │   │  guard     │   │  diff                │
   └────────────┘   └────────────┘   └──────────────────────┘
          │                │                          │
          └────────────────┼──────────────────────────┘
                           │
          ┌────────────────┼──────────────────────────┐
          │                │                          │
          ▼                ▼                          ▼
   ┌────────────┐   ┌────────────┐   ┌──────────────────────┐
   │  momus-    │   │  momus-    │   │  momus               │
   │  convert   │   │  mock      │   │  (umbrella)          │
   └────────────┘   └────────────┘   └──────────────────────┘
          │                │                          │
          └────────────────┼──────────────────────────┘
                           │
                           ▼
                    ┌──────────────┐
                    │  momus-core  │  Foundation: AST, engine, templates
                    └──────────────┘
```

All crates depend directly on `momus-core` — it is a flat star topology. The diagram above groups crates by functional area for readability, but there are no intermediate layers. Every crate composes on top of the core AST and engine.

## Crate Map

```
momus/                          # workspace root
├── crates/
│   ├── momus/                  # Umbrella crate: re-exports, builder, prelude
│   ├── momus-core/             # AST types, assertion evaluation, plan runner, template resolution
│   │   ├── ast/                #   TestPlan, Step, RequestStep, Assertion, Method, etc.
│   │   ├── engine/             #   Runner, evaluator, templates, script execution
│   │   ├── config.rs           #   TOML config loading (per-crate sections)
│   │   ├── deps.rs             #   Generic dependency resolver (topological sort via petgraph)
│   │   ├── leak.rs             #   Memory leak detection helper
│   │   └── transport.rs        #   TransportAdapter trait + HttpAdapter implementation
│   ├── momus-mock/             # Configurable mock HTTP server
│   │   ├── lib.rs              #   MockServer, MockResponse, MockHandler
│   │   └── store.rs            #   Stateful CRUD store with search/filter/sort/pagination
│   ├── momus-convert/          # Convert API descriptions into test plans
│   │   ├── lib.rs              #   Dispatcher: routes by format name
│   │   ├── curl.rs             #   cURL command → TestPlan
│   │   ├── har.rs              #   HAR file → TestPlan
│   │   ├── openapi.rs          #   OpenAPI 3.x → TestPlan
│   │   ├── postman.rs          #   Postman Collection → TestPlan
│   │   ├── graphql.rs          #   GraphQL SDL → TestPlan
│   │   ├── grpc.rs             #   gRPC proto → TestPlan (stub)
│   │   └── fhir/               #   FHIR IG → TestPlan (ported from fhir-autotest)
│   │       ├── mod.rs          #     Orchestrator: parse → resolve → generate → plan
│   │       ├── profile.rs     #     StructureDefinition model
│   │       ├── capability.rs  #     CapabilityStatement model
│   │       ├── search_param.rs #     SearchParameter model
│   │       ├── operation.rs   #     OperationDefinition model
│   │       ├── package.rs     #     IG package parser (.tgz)
│   │       ├── profile_resolver.rs # Parent profile chain resolution + download
│   │       ├── resource_gen.rs #     5-pass resource generation from StructureDefinitions
│   │       ├── planner.rs     #     Test plan generation from CapabilityStatement
│   │       ├── assertions.rs  #     FHIR-specific response assertions
│   │       ├── validator.rs   #     Profile validation against StructureDefinition
│   │       ├── bulk_data.rs   #     Bulk data generation (NDJSON)
│   │       ├── bulk_loader.rs #     Bulk data upload with wave ordering
│   │       ├── hcpd.rs        #     HCPD/AU-specific generation + locality data
│   │       ├── test_model.rs  #     Test model types
│   │       ├── value_resolver.rs #  Field value extraction for test URLs
│   │       ├── valuesets.rs   #     Value set resolution maps
│   │       └── test_helpers.rs #    Test IG package builder for integration tests
│   ├── momus-bench/            # Load testing: steady, max-throughput, soak modes
│   │   ├── lib.rs              #   Public API
│   │   ├── config.rs           #   BenchConfig, BenchMode
│   │   ├── modes.rs            #   Steady/MaxThroughput/Soak mode implementations
│   │   ├── runner.rs           #   Concurrent worker pool, HDR histograms
│   │   └── report.rs           #   BenchReport with Display
│   ├── momus-fuzz/             # Payload mutation: boundary, encoding, type mismatch, cardinality
│   │   ├── lib.rs              #   Public API, Mutator trait
│   │   ├── config.rs           #   FuzzConfig
│   │   ├── mutators.rs         #   Built-in mutators + SimpleRng
│   │   ├── runner.rs           #   run_fuzz() — HTTP dispatch + response classification
│   │   └── report.rs           #   FuzzReport with Display
│   ├── momus-chaos/            # Chaos engineering: network, service, resource, state faults
│   │   ├── lib.rs              #   Public API
│   │   ├── config.rs           #   ChaosConfig
│   │   ├── experiments.rs      #   ChaosExperiment enum (8 types), 5 implemented
│   │   ├── runner.rs           #   Experiment execution + health checks
│   │   └── report.rs           #   ChaosReport with Display
│   ├── momus-contract/         # Contract testing: validate responses against OpenAPI/GraphQL specs
│   │   ├── lib.rs              #   Public API
│   │   ├── config.rs           #   ContractConfig
│   │   ├── spec.rs             #   OpenAPI/GraphQL spec parsing
│   │   ├── runner.rs           #   Response validation against spec
│   │   └── report.rs           #   ContractReport, ContractViolation
│   ├── momus-guard/            # Security scanning: auth, CORS, info leaks, exposed endpoints
│   │   ├── lib.rs              #   Public API
│   │   ├── config.rs           #   GuardConfig
│   │   ├── runner.rs           #   5 check categories
│   │   └── report.rs           #   GuardReport, GuardIssue
│   ├── momus-diff/             # Regression/diff testing: compare responses between environments
│   │   ├── lib.rs              #   Public API
│   │   ├── config.rs           #   DiffConfig
│   │   ├── runner.rs           #   Parallel execution + field-level JSON diff
│   │   └── report.rs           #   DiffReport, DiffEntry
│   └── momus-cli/              # CLI binary: run, validate, mock, bench, fuzz, chaos, contract, guard, diff, convert
│       └── main.rs             #   Clap-derived CLI with all subcommands
└── examples/
    ├── health-check.json
    └── crud-sequence.json
```

---

## momus (umbrella)

**Status:** ✅ v0.4.0 — Complete

The top-level `momus` crate re-exports all sub-crates and provides convenience APIs:

- **`prelude`** — re-exports common types (`TestPlan`, `Step`, `Assertion`, `Method`, `RunReport`, `runner`)
- **`builder`** — programmatic plan construction (`TestPlanBuilder`, `RequestStepBuilder`, `SequenceStepBuilder`)
- **`load_plan` / `parse_plan` / `validate_plan`** — convenience functions

Users who want the full toolkit depend on `momus`. Users who only need the AST depend on `momus-core`.

---

## momus-core

**Status:** ✅ v0.4.0 — Complete

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
├── Schema(Value)                   — JSON Schema validation (wired via `jsonschema` crate)
└── ResponseTime(u64)               — Max response time in ms (wired in evaluator + runner)
```

### Engine (`momus_core::engine`)

- **`runner::execute_plan`** — walks the step tree, resolves `{base_url}`, `{steps.<name>.*}`, `{env.VAR}`, and `{random.*}` templates, dispatches HTTP requests via reqwest, evaluates assertions, collects results into `RunReport`
- **`evaluator::evaluate_assertions`** — evaluates the assertion tree against a response. Includes a simple JSONPath resolver (supports `$.key`, `$.key.nested`, `$.key[*]`, `$.key[0]`)
- **`templates::resolve_url`, `resolve_body`, `resolve_headers`** — template substitution for `{base_url}`, `{steps.<name>.*}`, `{env.VAR}`, and `{random.*}`
- **`script::execute_script`** — executes `ScriptStep` nodes using the rhai scripting engine (7 tests)

### Key Design Decisions

- **`TransportAdapter` trait** — defined in `momus_core::transport` with `TransportRequest`, `TransportResponse`, and the `TransportAdapter` trait. `HttpAdapter` implements it using reqwest. This decouples the engine from any specific protocol, enabling future gRPC/GraphQL transport implementations.
- **Template substitution** (`{steps.<name>.*}`) replaces DAG-based dependency resolution. It's simpler, more flexible, and handles the same cases.
- **The `Schema` assertion variant** is wired via the `jsonschema` crate for JSON Schema validation.
- **The `ResponseTime` assertion variant** is wired in the evaluator and runner — it measures and asserts response duration.
- **Script steps** are implemented using the rhai scripting engine with access to step response context.

### Gaps to Fill (v0.5.0+)

| Gap | Priority | Description |
|-----|----------|-------------|
| Full JSONPath | Medium | Replace simple JSONPath with `jsonpath-rust` or similar |
| Script step improvements | Low | Add more built-in functions, sandboxing, timeout |
| Report HTML output | Low | Enhance `RunReport::to_html()` with richer formatting |

---

## momus-mock

**Status:** ✅ v0.4.0 — Complete

A configurable mock HTTP server for testing.

### Features

- Route-based response matching (`"GET /path"` → canned JSON response)
- Custom handler functions for dynamic responses
- Request recording for verification
- Graceful shutdown
- Stateful CRUD store with search/filter/sort/pagination (ported from fhir-autotest)

### API

```rust
let mut server = MockServer::new();
server.when("GET /health", MockResponse::json(json!({"status": "ok"})).with_status(200));
server.start(8091).await?;
let requests = server.recorded_requests();
server.stop();
```

### Gaps to Fill (v0.5.0+)

| Gap | Priority | Description |
|-----|----------|-------------|
| Latency simulation | Medium | Configurable response delays per route |
| Fault injection | Medium | Return errors, timeouts, connection resets per route |
| TLS support | Low | HTTPS mock server |
| WebSocket support | Low | WebSocket mock endpoints |

---

## momus-convert

**Status:** ✅ v0.4.0 — Complete

Converts API descriptions into `TestPlan` JSON. Each format is a feature-gated module.

### Converter Interface

```rust
pub fn convert(input: &str) -> Result<TestPlan>;
```

All converters share this interface. The dispatcher in `lib.rs` routes by format name.

### Format Status

| Module | Format | Status | Notes |
|--------|--------|--------|-------|
| `curl` | cURL command string | ✅ Complete | 13 tests |
| `har` | HAR (HTTP Archive) file | ✅ Complete | 6 tests |
| `openapi` | OpenAPI 3.x YAML/JSON | ✅ Complete | Schema-aware value generation |
| `postman` | Postman Collection v2.1 | ✅ Complete | 7 tests |
| `graphql` | GraphQL SDL / introspection | ✅ Complete | 12 tests |
| `grpc` | gRPC proto file | 🔜 Stub | Requires protobuf compilation tooling |
| `fhir` | FHIR IG package | ✅ Complete | Ported from fhir-autotest |

### cURL Converter

Parses cURL command strings into `TestPlan`:

- `-X`/`--request` → HTTP method
- `-H`/`--header` → request headers
- `-d`/`--data`/`--data-raw`/`--data-binary` → request body
- URL → request URL
- `--max-time` → timeout
- `-u`/`--user` → basic auth header
- `-b`/`--cookie` → cookie header

### HAR Converter

Reads HAR 1.2 format, extracts each request/response pair, and produces a `RequestStep` with a `Status` assertion matching the recorded status code. The generated plan is a starting point — users add `JsonPath`, `Schema`, and `ResponseTime` assertions on top.

### OpenAPI Converter

Parses OpenAPI 3.x YAML/JSON specs and generates test plans:

- Each path + operation → `RequestStep`
- Request body schemas → example body generation
- Response schemas → `Schema` assertions
- Parameters → URL/header/query construction
- Security schemes → auth header setup

### Postman Converter

Parses Postman Collection v2.1 JSON:

- Each request in the collection → `RequestStep`
- Variables → template substitution
- Tests/scripts → assertion mapping (best-effort)
- Auth → header setup

### GraphQL Converter

Parses GraphQL SDL or introspection result:

- Each query/mutation/subscription → `RequestStep`
- Input types → example variable generation
- Response types → `JsonPath` assertions
- Schema validation → `Schema` assertions

### gRPC Converter

**Status:** 🔜 Stub (v0.5.0 target)

Parses protobuf definitions:

- Each RPC method → test case
- Message types → example payload generation
- Requires protobuf compilation tooling

### FHIR Converter

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

The orchestrator pipeline (`momus-convert/src/fhir/mod.rs`) is intentionally generic — it follows the same parse → resolve → generate → plan pattern as the original fhir-autotest but is simplified (125 lines vs 400). The momus-core generic runner handles plan execution; FHIR-specific setup/teardown, bulk upload, and profile validation are handled by the FHIR converter modules.

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

**Status:** ✅ v0.4.0 — Steady mode complete, MaxThroughput and Soak are stubs

Load testing engine. Takes a `TestPlan` and runs it under load. The execution model is fundamentally different from the assertion runner — concurrent stateless fire-and-forget rather than sequential stateful execution.

### Modes

| Mode | Description | Configuration | Status |
|------|-------------|---------------|--------|
| **Steady** | Fixed concurrency for a fixed duration | `--concurrency N --duration S` | ✅ Implemented |
| **Max-throughput** | Ramp concurrency upward until error rate or latency threshold breached | `--min-concurrency N --max-concurrency N --error-threshold P` | 🔜 Stub |
| **Soak** | Sustained load at fixed concurrency for hours | `--concurrency N --duration H` | 🔜 Stub |

### Features (implemented)

- Steady mode with concurrent worker pool
- HDR histogram latency recording (P50/P90/P95/P99 per group and overall)
- Per-group statistics
- Report generation: JSON summary, full results JSON, text report, HTML dashboard
- Signal handling (Ctrl+C graceful shutdown)
- Request timeout configuration

### Gaps to Fill (v0.5.0+)

| Gap | Priority | Description |
|-----|----------|-------------|
| Max-throughput mode | High | Binary search for max concurrency within error/latency bounds |
| Soak mode | Medium | Steady load with periodic health checks |
| Warmup phase | Medium | N requests before recording |

---

## momus-fuzz

**Status:** ✅ v0.4.0 — Complete

Payload mutation engine. Takes a valid JSON payload and produces mutated variants.

### Mutator Trait

```rust
pub trait Mutator: Send + Sync {
    fn name(&self) -> &'static str;
    fn mutate(&self, base: &serde_json::Value, seed: u64) -> serde_json::Value;
}
```

### Built-in Mutators

| Mutator | Description | Examples |
|---------|-------------|---------|
| **Boundary** | Edge case values | Empty strings, very long strings, zero/negative/NaN numbers, extreme dates, null values |
| **Encoding** | Injection attacks | JSON injection, deeply nested objects, duplicate keys, unicode normalization attacks, null bytes |
| **Type mismatch** | Wrong types | String where number expected, array where object expected, boolean where string expected |
| **Cardinality** | Structure changes | Remove required fields, duplicate array elements, add unexpected fields, empty arrays |

All mutators use a deterministic PRNG (`SimpleRng`) so the same `(base, seed)` pair always produces the same mutation.

### Features (implemented)

- `run_fuzz()` — iterate mutations, send to server, classify responses
- Response classification: pass (2xx), fail (4xx/5xx), crash (connection error), timeout
- Report generation: mutation count, pass/fail/crash breakdown, interesting mutations

### Gaps to Fill (v0.5.0+)

| Gap | Priority | Description |
|-----|----------|-------------|
| Unicode normalization mutator | Medium | |
| Format string injection mutator | Medium | |
| Path traversal mutator | Medium | |
| SSRF attempt mutator | Low | |
| SQL injection mutator | Low | |
| XSS injection mutator | Low | |
| Schema-aware mutation (from OpenAPI) | Medium | Use type info for smarter mutations |
| Mutation coverage tracking | Low | |

---

## momus-chaos

**Status:** ✅ v0.4.0 — 5 of 8 experiments implemented

Chaos engineering engine. Injects infrastructure-level faults into a running system and verifies that the system self-heals, degrades gracefully, or fails safely.

### Experiment Types

| Category | Experiment | Description | Implementation | Status |
|----------|-----------|-------------|----------------|--------|
| Network | `NetworkLatency` | Inject artificial delay into requests to a specific endpoint | Proxy-based (mitmproxy/hyper) | ✅ |
| Network | `ConnectionReset` | Simulate connection resets for a percentage of requests | TCP-level (iptables) | 🔜 |
| Network | `PacketLoss` | Drop a percentage of requests | Network-level (tc netem) | 🔜 |
| Service | `ServiceError` | Return a specific HTTP status code for a matching endpoint | Proxy-based | ✅ |
| Service | `ServiceDown` | Simulate a downstream service being unreachable | DNS/network-level | ✅ |
| Resource | `CpuPressure` | Busy-loop on N cores | Process-level (stress-ng) | ✅ |
| Resource | `MemoryPressure` | Allocate N MB of memory | Process-level | ✅ |
| State | `ClockSkew` | Simulate clock offset | System-level (faketime) | 🔜 |

### Gaps to Fill (v0.5.0+)

| Gap | Priority | Description |
|-----|----------|-------------|
| ConnectionReset experiment | Medium | Requires iptables |
| PacketLoss experiment | Medium | Requires tc netem |
| ClockSkew experiment | Low | Requires faketime |
| Health check before/during/after | Medium | |
| Healing time measurement | Medium | |
| Steady-state hypothesis validation | Medium | |

### Design Notes

Momus-chaos defines experiment types and reports, but the actual fault injection requires platform-specific tooling (tc, stress-ng, iptables). Momus-chaos orchestrates experiments and validates system behavior — it does not replace dedicated chaos engineering platforms like Chaos Mesh or Gremlin.

---

## momus-contract

**Status:** ✅ v0.4.0 — Complete (validation is functional but can be deepened)

Contract testing. Runs a test plan and validates each response against the API's declared schema (OpenAPI or GraphQL). Reports compliance percentage, missing fields, type mismatches, and undocumented fields.

### Features (implemented)

- OpenAPI 3.x response validation
- GraphQL response validation against schema
- Response field coverage analysis
- Undocumented field detection
- Type mismatch detection
- Compliance percentage reporting

### Gaps to Fill (v0.5.0+)

| Gap | Priority | Description |
|-----|----------|-------------|
| Deeper contract validation | Medium | More thorough schema coverage, nested object validation |
| FHIR profile validation | High | Port from fhir-autotest's `validate_against_profile()` |
| Resource type matching | High | |
| Required element presence (min > 0) | High | |
| Fixed/pattern value matching | High | |
| MustSupport field presence (best-effort) | Medium | |

---

## momus-guard

**Status:** ✅ v0.4.0 — Complete

Security scanning. Inspects responses for common security issues.

### Checks (implemented)

| Check | Description | Detection |
|-------|-------------|-----------|
| Auth headers | Missing or weak authentication | Check for Authorization header presence and scheme |
| CORS | CORS misconfiguration | Check `Access-Control-Allow-Origin` for permissive values |
| Info leaks | Information leakage in error bodies | Scan for stack traces, SQL errors, path disclosure |
| Exposed endpoints | Internal endpoints exposed | Check for common internal paths |
| Security headers | Missing security headers | Check for HSTS, CSP, X-Content-Type-Options, X-Frame-Options |

### Gaps to Fill (v0.5.0+)

| Gap | Priority | Description |
|-----|----------|-------------|
| Rate limiting detection | Low | |
| JWT analysis | Low | |

---

## momus-diff

**Status:** ✅ v0.4.0 — Complete

Regression/diff testing. Runs the same test plan against two environments (e.g. staging vs production) and reports differences.

### Features (implemented)

- Status code comparison
- Response body diff (field-level, recursive JSON)
- Header comparison
- New/missing field detection
- Value change detection
- Structured JSON diff with path tracking

### Gaps to Fill (v0.5.0+)

| Gap | Priority | Description |
|-----|----------|-------------|
| Severity classification | Medium | |
| HTML report output | Low | |

---

## momus-cli

**Status:** ✅ v0.4.0 — Complete (all subcommands wired)

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

### Gaps to Fill (v0.5.0+)

| Gap | Priority | Description |
|-----|----------|-------------|
| CLI integration tests | Medium | 0 integration tests currently |
| `--timeout` global flag | Medium | |
| `--dry-run` for run subcommand | Medium | |
| Tab completion (shell completions) | Low | |
| `init` subcommand (scaffold config) | Low | |
| `docs` subcommand (open docs) | Low | |

---

## FHIR Autotest Porting Status

The following table maps every module from fhir-autotest to its destination in momus, reflecting the actual porting status as of v0.4.0:

| fhir-autotest Module | Lines | Destination | Status |
|---------------------|-------|-------------|--------|
| `config/models.rs` | 490 | `momus-core/src/config.rs` (generic `RunConfig`) | ✅ Ported (simplified) |
| `model/profile.rs` | 176 | `momus-convert/src/fhir/profile.rs` | ✅ Ported |
| `model/capability.rs` | 101 | `momus-convert/src/fhir/capability.rs` | ✅ Ported |
| `model/search_param.rs` | 38 | `momus-convert/src/fhir/search_param.rs` | ✅ Ported |
| `model/operation.rs` | 56 | `momus-convert/src/fhir/operation.rs` | ✅ Ported |
| `parse/package.rs` | 215 | `momus-convert/src/fhir/package.rs` | ✅ Ported |
| `parse/profile_resolver.rs` | 1025 | `momus-convert/src/fhir/profile_resolver.rs` | ✅ Ported |
| `generate/model.rs` | 335 | `momus-convert/src/fhir/test_model.rs` | ✅ Ported |
| `generate/planner.rs` | 1298 | `momus-convert/src/fhir/planner.rs` | ✅ Ported |
| `generate/conformance.rs` | 902 | `momus-convert/src/fhir/planner.rs` (merged) | ✅ Ported |
| `generate/dependency_resolver.rs` | 376 | `momus-core/src/deps.rs` (generic) | ✅ Ported |
| `generate/value_resolver.rs` | 312 | `momus-convert/src/fhir/value_resolver.rs` | ✅ Ported |
| `generate/resource_generator/` | 2561 | `momus-convert/src/fhir/resource_gen.rs` | ✅ Ported (simplified — 507 lines vs 2561) |
| `generate/bulk_data.rs` | 2972 | `momus-convert/src/fhir/bulk_data.rs` | ✅ Ported |
| `generate/hcpd.rs` | 482 | `momus-convert/src/fhir/hcpd.rs` | ✅ Ported |
| `generate/locality.rs` | ~100 | `momus-convert/src/fhir/hcpd.rs` (merged) | ✅ Ported |
| `runner/orchestrator.rs` | 1008 | `momus-convert/src/fhir/mod.rs` (generic orchestrator) | ✅ Ported (simplified — 125 lines) |
| `runner/executor.rs` | 752 | `momus-core` (generic runner) | ✅ Replaced by momus-core generic runner |
| `runner/response_assertions.rs` | 570 | `momus-convert/src/fhir/assertions.rs` | ✅ Ported |
| `runner/validator.rs` | 316 | `momus-convert/src/fhir/validator.rs` | ✅ Ported |
| `runner/bulk_loader.rs` | 1788 | `momus-convert/src/fhir/bulk_loader.rs` | ✅ Ported |
| `mock_server.rs` | 806 | `momus-mock/src/store.rs` | ✅ Ported |
| `main.rs` | 142 | `momus-cli` (already wired) | ✅ Done |
| `lib.rs` (orchestration) | 400 | `momus-convert/src/fhir/mod.rs` | ✅ Ported (simplified — 125 lines) |
| `test_helpers.rs` | 195 | `momus-convert/src/fhir/test_helpers.rs` | ✅ Ported |

### Remaining Gaps

1. **Resource generator simplification** — The momus port (`resource_gen.rs`, 507 lines) is significantly simpler than the fhir-autotest original (2561 lines across 4 sub-modules). Missing features:
   - Identifier profile constraints (`find_identifier_system`, `find_identifier_type`, `apply_identifier_profile_constraints`)
   - Slice handling (`populate_required_slices`, `populate_extension_slices`, `apply_slices_for_path`)
   - MustSupport backbones and optional fields at depth 2+
   - Nested required fields at depth 2+
   - Base spec repeatability rules
   - HumanName slice support
   - Complex extension handling with sub-extensions

2. **FHIR-specific config** — fhir-autotest's `TestConfig` (490 lines) with `ServerConfig`, `RepositoryConfig`, `OverrideConfig`, `DataGenerationConfig` is not ported. Momus-core has a simpler generic `RunConfig` (147 lines).

3. **Deeper contract validation** — The `momus-contract` crate validates responses against OpenAPI/GraphQL specs, but FHIR profile validation (resource type matching, required element presence, fixed/pattern value matching, MustSupport field presence) is not yet integrated.

### Porting Strategy

1. **Phase 1 (v0.2.0) — DONE:** Core FHIR types, package parser, test plan generator, resource generator, profile resolver, assertions, validator, mock server CRUD store.

2. **Phase 2 (v0.3.0) — DONE:** Bulk data generation, HCPD/AU-specific generation, bulk loader, locality data, value_resolver, test_helpers.

3. **Phase 3 (v0.4.0) — DONE:** All remaining fhir-autotest modules ported. The orchestrator pipeline is intentionally generic — momus-core handles plan execution, FHIR converter handles FHIR-specific logic.

4. **Phase 4 (v0.5.0+):** Enhance resource generator with slice handling, identifier constraints, and mustSupport support. Port FHIR-specific config types. Integrate FHIR profile validation into momus-contract.

---

## What Momus Is Not

### Not a fuzzer

The `momus-fuzz` crate generates mutated payloads, but it is not a coverage-guided fuzzer (AFL-style). It applies schema-aware mutations to valid payloads and classifies server responses. True coverage-guided fuzzing is a separate engineering problem with its own tooling (cargo-fuzz, libFuzzer). Momus-fuzz is a mutation testing tool that happens to share the HTTP transport with the rest of Momus.

### Not multi-protocol (yet)

gRPC, GraphQL, and Protobuf each require fundamentally different transport and serialization. A `TransportAdapter` trait is defined in `momus_core::transport` and implemented by `HttpAdapter` (using reqwest). Implementing it for each additional protocol is a crate's worth of work per protocol. Start with HTTP/REST and prove the architecture before expanding.

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

## Version Roadmap

| Version | Focus | Key Deliverables | Status |
|---------|-------|-----------------|--------|
| v0.1.0 | Foundation | Core AST, engine, mock server, CLI, fuzz mutators, scaffolded crates | ✅ Released |
| v0.2.0 | FHIR Port | FHIR converter (core types, package parser, resource gen, planner, assertions, validator), mock server CRUD store, config system, dependency resolver | ✅ Released |
| v0.3.0 | Converters + Advanced Features | OpenAPI/Postman/GraphQL converters, bulk data, HCPD/AU generation, bench/fuzz/chaos/contract/guard/diff runners, HTML reports | ✅ Released |
| v0.4.0 | Completion | All converters complete (except gRPC stub), all runners implemented, FHIR value_resolver + test_helpers ported, TransportAdapter trait, script execution (rhai) | ✅ Current |
| v0.5.0 | Depth + Polish | MaxThroughput/Soak bench modes, remaining chaos experiments, deeper contract validation, more fuzz mutators, CLI integration tests, resource generator enhancements | 🔜 Next |
| v1.0.0 | Stable Release | API stability, documentation complete, CI/CD, crates.io | 📋 Planned |
