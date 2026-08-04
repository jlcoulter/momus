# Momus Feature List

This document catalogs every feature across all momus crates, organized by implementation status. Use this as a roadmap and checklist for development.

---

## Legend

| Icon | Meaning |
|------|---------|
| ✅ | Implemented and tested |
| 🔜 | Scaffolded (types exist, logic is stub) |
| 📋 | Planned (not yet started) |
| 🧪 | Needs more tests |

---

## momus-core — Foundation

### AST Types (✅ Complete)

| Feature | Status | Notes |
|---------|--------|-------|
| `TestPlan` with name, base_url, headers, steps, setup, teardown | ✅ | |
| `Step` enum (Request, Sequence, Parallel, Script, Noop) | ✅ | Script is placeholder |
| `RequestStep` with method, url, headers, body, assertions, save_as, soft_fail | ✅ | |
| `SequenceStep` with name, steps, continue_on_failure | ✅ | |
| `Method` enum (GET, POST, PUT, DELETE, PATCH, HEAD, OPTIONS) | ✅ | |
| `RunReport` with total/passed/failed/results | ✅ | |
| `TestResult` with test_name, passed, status_code, errors | ✅ | |
| `TestGroupResult` for grouped results | ✅ | |

### Assertion AST (✅ Complete)

| Feature | Status | Notes |
|---------|--------|-------|
| `AllOf` — AND composition | ✅ | |
| `AnyOf` — OR composition | ✅ | |
| `Not` — negation | ✅ | |
| `Status(u16)` — exact status code | ✅ | |
| `StatusIn(Vec<u16>)` — status in set | ✅ | |
| `Header` — header present/absent/equals/contains/regex | ✅ | |
| `BodyLength` — body size constraints | ✅ | |
| `ContentType` — Content-Type match | ✅ | |
| `ValidJson` — valid JSON check | ✅ | |
| `JsonPath` — JSONPath query with predicate | ✅ | Simple resolver |
| `Schema` — JSON Schema validation | ✅ | Wired via `jsonschema` crate |
| `ResponseTime` — max response time | 🔜 | No-op |

### JSONPath Predicates (✅ Complete)

| Feature | Status | Notes |
|---------|--------|-------|
| `exists` | ✅ | |
| `not_exists` | ✅ | |
| `eq` | ✅ | |
| `not_eq` | ✅ | |
| `cmp` (gt/lt/ge/le) | ✅ | |
| `length` (eq/min/max/range) | ✅ | |
| `count` (eq/min/max/range) | ✅ | |
| `every` — all results satisfy sub-predicate | ✅ | |
| `some` — at least one satisfies sub-predicate | ✅ | |
| `schema` — match result against JSON Schema | 🔜 | No-op |

### Engine (✅ Complete)

| Feature | Status | Notes |
|---------|--------|-------|
| `execute_plan()` — walk step tree, dispatch HTTP, evaluate assertions | ✅ | |
| `execute_steps()` — handle sequences, parallel, setup/teardown | ✅ | |
| `evaluate_assertions()` — evaluate assertion tree against response | ✅ | |
| Template resolution: `{base_url}` | ✅ | |
| Template resolution: `{steps.<name>.*}` | ✅ | |
| Template resolution in URLs, headers, bodies | ✅ | |
| JSONPath resolver (simple) | ✅ | Supports `$.key`, `$.key.nested`, `$.key[*]`, `$.key[0]` |
| Schema validation | 🔜 | No-op |
| ResponseTime measurement | 🔜 | No-op |
| Script step execution | 📋 | Not implemented |

### Gaps (v0.2.0+)

| Feature | Priority | Notes |
|---------|----------|-------|
| Full JSONPath support (jsonpath-rust) | Medium | Current resolver is simple subset |
| JSON Schema validation (jsonschema crate) | High | Needed for contract testing |
| ResponseTime assertion | Medium | |
| Script execution (rhai/wasm) | Low | |
| Generic config system (TOML) | High | Port from fhir-autotest |
| Generic dependency resolver (topological sort) | Medium | Port from fhir-autotest |
| Report JSON file output | Medium | Port from fhir-autotest |
| `{env.VAR}` template syntax | Medium | Environment variable substitution |
| `{random.uuid}` / `{random.int}` template functions | Low | Random value generation in templates |
| `{body.<path>}` template syntax | Medium | Reference response body fields |
| Soft-fail step support | ✅ | Already in AST, needs testing |
| Parallel step execution | ✅ | Already in AST, needs testing |

---

## momus-mock — Mock HTTP Server

### Implemented (✅)

| Feature | Status | Notes |
|---------|--------|-------|
| Route-based response matching | ✅ | `"METHOD /path"` → `MockResponse` |
| Custom handler functions | ✅ | `MockHandler` type alias |
| Request recording | ✅ | `recorded_requests()` |
| Graceful shutdown | ✅ | `stop()` |
| JSON response helper | ✅ | `MockResponse::json()` |
| Status code configuration | ✅ | `with_status()` |
| Random port binding | ✅ | Port 0 = random |

### Planned (v0.2.0+)

| Feature | Priority | Notes |
|---------|----------|-------|
| Stateful CRUD store | High | Port from fhir-autotest mock_server.rs |
| Search/filter support | High | Query parameter filtering |
| Sorting support | Medium | `_sort` parameter |
| Pagination support | Medium | `_count`, `_offset` |
| Latency simulation | Medium | Per-route delay configuration |
| Fault injection | Medium | Error responses, timeouts, resets |
| TLS support | Low | HTTPS |
| WebSocket support | Low | |
| Request matching by header/body | Medium | Beyond just method+path |
| Response templates | Medium | `{request.path}`, `{request.body.*}` |
| OpenAPI-driven mock generation | Low | Auto-generate from spec |

---

## momus-convert — API Description Converters

### cURL Converter

| Feature | Status | Notes |
|---------|--------|-------|
| Parse `-X`/`--request` | 🔜 | Stub |
| Parse `-H`/`--header` | 🔜 | Stub |
| Parse `-d`/`--data`/`--data-raw`/`--data-binary` | 🔜 | Stub |
| Parse URL | 🔜 | Stub |
| Parse `--max-time` | 🔜 | Stub |
| Parse `-u`/`--user` (basic auth) | 🔜 | Stub |
| Parse `-b`/`--cookie` | 🔜 | Stub |
| Generate `TestPlan` with assertions | 🔜 | Stub |

### HAR Converter

| Feature | Status | Notes |
|---------|--------|-------|
| Parse HAR 1.2 format | 🔜 | Stub |
| Extract request/response pairs | 🔜 | Stub |
| Generate `Status` assertions from recorded status | 🔜 | Stub |
| Generate `Header` assertions | 🔜 | Stub |
| Generate `JsonPath` assertions | 🔜 | Stub |

### OpenAPI Converter

| Feature | Status | Notes |
|---------|--------|-------|
| Parse OpenAPI 3.x YAML/JSON | 🔜 | Stub |
| Path + operation → RequestStep | 🔜 | Stub |
| Request body schema → example generation | 🔜 | Stub |
| Response schema → Schema assertion | 🔜 | Stub |
| Parameters → URL/header/query construction | 🔜 | Stub |
| Security schemes → auth header setup | 🔜 | Stub |

### Postman Converter

| Feature | Status | Notes |
|---------|--------|-------|
| Parse Collection v2.1 JSON | 🔜 | Stub |
| Request → RequestStep | 🔜 | Stub |
| Variables → template substitution | 🔜 | Stub |
| Auth → header setup | 🔜 | Stub |

### GraphQL Converter

| Feature | Status | Notes |
|---------|--------|-------|
| Parse SDL schema | 🔜 | Stub (v0.3.0) |
| Parse introspection result | 🔜 | Stub (v0.3.0) |
| Query/mutation → RequestStep | 🔜 | Stub (v0.3.0) |
| Input types → example variables | 🔜 | Stub (v0.3.0) |
| Response types → JsonPath assertions | 🔜 | Stub (v0.3.0) |

### gRPC Converter

| Feature | Status | Notes |
|---------|--------|-------|
| Parse protobuf definitions | 🔜 | Stub (v0.4.0) |
| RPC method → test case | 🔜 | Stub (v0.4.0) |
| Message types → example payload | 🔜 | Stub (v0.4.0) |

### FHIR Converter (v0.2.0 target — port from fhir-autotest)

| Feature | Status | Notes |
|---------|--------|-------|
| Parse IG package (.tgz) | ✅ | Ported |
| Categorize resources by type | ✅ | Ported |
| Select CapabilityStatement | ✅ | Ported |
| Resolve parent profile chain | 🔜 | Port (v0.3.0) |
| Download missing profiles from registry | 🔜 | Port (v0.3.0) |
| Extract dependencies (topological sort) | ✅ | Uses `momus_core::deps` |
| Generate resources from StructureDefinitions | ✅ | Ported (5-pass) |
| Required field population | ✅ | Ported |
| Required slice population | ✅ | Ported |
| Extension slice population | ✅ | Ported |
| MustSupport backbone population | ✅ | Ported |
| MustSupport optional field population | ✅ | Ported |
| Type-specific value generation | ✅ | Ported (HumanName, Address, etc.) |
| Value set resolution | 🔜 | Port (v0.3.0) |
| Generate test plan from CapabilityStatement | ✅ | Ported |
| CRUD test generation | ✅ | Ported |
| Search test generation (single, modifiers, prefixes) | ✅ | Ported |
| Near/proximity search tests | 🔜 | Port (v0.3.0) |
| Combinatorial search tests | ✅ | Ported |
| Chained search tests | 🔜 | Port (v0.3.0) |
| Include/revinclude tests | ✅ | Ported |
| Result param tests (_summary, _elements, _count, _sort, _has) | ✅ | Ported |
| Operation tests ($operation) | ✅ | Ported |
| Negative tests | ✅ | Ported |
| Conformance tests (mustSupport) | 🔜 | Port (v0.3.0) |
| Bulk data generation (NDJSON) | 🔜 | Port (v0.3.0) |
| HCPD/AU-specific generation | 🔜 | Port (v0.3.0) |
| Locality/suburb generation | 🔜 | Port (v0.3.0) |

---

## momus-bench — Load Testing

### Implemented (✅)

| Feature | Status | Notes |
|---------|--------|-------|
| `BenchConfig` with concurrency, duration, mode | ✅ | |
| `BenchMode` enum (Steady, MaxThroughput, Soak) | ✅ | |
| `BenchReport` with Display | ✅ | |

### Planned (v0.2.0)

| Feature | Priority | Notes |
|---------|----------|-------|
| `run_steady()` — fixed concurrency, fixed duration | High | |
| `run_max_throughput()` — ramp until error/latency threshold | High | |
| `run_soak()` — sustained load for hours | Medium | |
| Warmup phase | Medium | N requests before recording |
| HDR histogram latency recording | High | P50/P90/P95/P99 |
| Per-group statistics | Medium | |
| JSON summary output | High | |
| Full results JSON | Medium | |
| Text report | Medium | |
| HTML dashboard | Low | |
| Signal handling (Ctrl+C) | Medium | Graceful shutdown |
| Concurrent worker pool | High | |
| Request timeout configuration | Medium | |

---

## momus-fuzz — Payload Mutation

### Implemented (✅)

| Feature | Status | Notes |
|---------|--------|-------|
| `Mutator` trait | ✅ | |
| `BoundaryMutator` | ✅ | 10 tests |
| `EncodingMutator` | ✅ | 10 tests |
| `TypeMismatchMutator` | ✅ | 10 tests |
| `CardinalityMutator` | ✅ | 10 tests |
| `all_mutators()` | ✅ | |
| `mutator_by_name()` | ✅ | |
| `SimpleRng` (deterministic PRNG) | ✅ | |
| `FuzzConfig` | ✅ | |
| `FuzzReport` with Display | ✅ | |

### Planned (v0.2.0)

| Feature | Priority | Notes |
|---------|----------|-------|
| `run_fuzz()` — send mutations to server | High | |
| Response classification (pass/fail/crash/timeout) | High | |
| Interesting mutation detection | Medium | |
| Report generation | High | |
| Unicode normalization mutator | Medium | |
| Format string injection mutator | Medium | |
| Path traversal mutator | Medium | |
| SSRF attempt mutator | Low | |
| SQL injection mutator | Low | |
| XSS injection mutator | Low | |
| Schema-aware mutation (from OpenAPI) | Medium | Use type info for smarter mutations |
| Mutation coverage tracking | Low | |

---

## momus-chaos — Chaos Engineering

### Implemented (✅)

| Feature | Status | Notes |
|---------|--------|-------|
| `ChaosConfig` | ✅ | |
| `ChaosExperiment` enum (8 types) | ✅ | |
| `ChaosReport` with Display | ✅ | |
| Not-healed display | ✅ | |
| NetworkLatency experiment | ✅ | Proxy-based delay injection |
| ServiceError experiment | ✅ | Endpoint status monitoring |
| ServiceDown experiment | ✅ | Unreachability detection |
| CpuPressure experiment | ✅ | Busy-loop thread saturation |
| MemoryPressure experiment | ✅ | Vec allocation and hold |

### Planned (v0.3.0)

| Feature | Priority | Notes |
|---------|----------|-------|
| ConnectionReset experiment | Medium | Requires iptables |
| PacketLoss experiment | Medium | Requires tc netem |
| ClockSkew experiment | Low | Requires faketime |
| Health check before/during/after | Medium | |
| Healing time measurement | Medium | |
| Steady-state hypothesis validation | Medium | |

---

## momus-contract — Contract Testing

### Implemented (✅)

| Feature | Status | Notes |
|---------|--------|-------|
| `ContractConfig` with spec_path, strict mode | ✅ | |
| `ContractReport` with Display | ✅ | |
| `ContractViolation` with Display | ✅ | |

### Planned (v0.2.0)

| Feature | Priority | Notes |
|---------|----------|-------|
| OpenAPI 3.x response validation | High | |
| GraphQL response validation | High | |
| Response field coverage analysis | Medium | |
| Undocumented field detection | Medium | |
| Type mismatch detection | High | |
| Compliance percentage reporting | Medium | |
| FHIR profile validation (port from fhir-autotest) | High | |
| Resource type matching | High | |
| Required element presence (min > 0) | High | |
| Fixed/pattern value matching | High | |
| MustSupport field presence (best-effort) | Medium | |

---

## momus-guard — Security Scanning

### Implemented (✅)

| Feature | Status | Notes |
|---------|--------|-------|
| `GuardConfig` with check flags | ✅ | |
| `GuardReport` with Display | ✅ | |
| `GuardIssue` with Display | ✅ | |
| Auth header presence check | ✅ | Detects unauthenticated data responses |
| CORS misconfiguration check | ✅ | OPTIONS preflight with malicious origin |
| Info leak detection (stack traces, SQL errors) | ✅ | 12 leak patterns scanned |
| Exposed endpoint discovery | ✅ | 20 common paths checked |
| Security headers check (HSTS, CSP, X-Content-Type-Options, X-Frame-Options) | ✅ | 4 header checks per endpoint |

### Planned (v0.3.0)

| Feature | Priority | Notes |
|---------|----------|-------|
| Rate limiting detection | Low | |
| JWT analysis | Low | |

---

## momus-diff — Regression/Diff Testing

### Implemented (✅)

| Feature | Status | Notes |
|---------|--------|-------|
| `DiffConfig` with baseline/target URLs | ✅ | |
| `DiffReport` with Display | ✅ | |
| `DiffEntry` with Display | ✅ | |
| Parallel execution against two endpoints | ✅ | Concurrent requests to baseline + target |
| Status code comparison | ✅ | |
| Response body diff (field-level) | ✅ | Recursive JSON object/array diff |
| Header comparison | ✅ | |
| New field detection | ✅ | |
| Missing field detection | ✅ | |
| Value change detection | ✅ | |
| Structured JSON diff (not line-level) | ✅ | Recursive with path tracking |

### Planned (v0.3.0)

| Feature | Priority | Notes |
|---------|----------|-------|
| Severity classification | Medium | |
| HTML report output | Low | |

---

## momus-cli — CLI Binary

### Implemented (✅)

| Feature | Status | Notes |
|---------|--------|-------|
| `run` subcommand | ✅ | |
| `validate` subcommand | ✅ | |
| `mock` subcommand | ✅ | |
| `bench` subcommand | ✅ | Returns empty report |
| `fuzz` subcommand | ✅ | Returns empty report |
| `chaos` subcommand | ✅ | Returns empty report |
| `contract` subcommand | ✅ | Returns empty report |
| `guard` subcommand | ✅ | Returns empty report |
| `diff` subcommand | ✅ | Returns empty report |
| `convert` subcommand | ✅ | Dispatches to stubs |
| `--base-url` flag | ✅ | |
| `--output` flag | ✅ | |
| `--verbose` flag | ✅ | |
| `--config` flag | ✅ | |
| Tracing/logging setup | ✅ | |

### Planned (v0.2.0+)

| Feature | Priority | Notes |
|---------|----------|-------|
| `--format` (json/text) for output | Medium | |
| `--timeout` global flag | Medium | |
| `--dry-run` for run subcommand | Medium | |
| Tab completion (shell completions) | Low | |
| `init` subcommand (scaffold config) | Low | |
| `docs` subcommand (open docs) | Low | |

---

## Cross-Cutting Concerns

### Testing

| Area | Status | Notes |
|------|--------|-------|
| momus-core unit tests | ✅ | 7 tests |
| momus-mock unit tests | ✅ | 4 tests |
| momus (umbrella) unit tests | ✅ | 5 tests |
| momus-fuzz unit tests | ✅ | 10 tests |
| momus-bench unit tests | 🧪 | 1 test (report display) |
| momus-chaos unit tests | 🧪 | 2 tests |
| momus-contract unit tests | 🧪 | 1 test |
| momus-guard unit tests | 🧪 | 2 tests |
| momus-diff unit tests | 🧪 | 1 test |
| momus-convert unit tests | 📋 | 0 tests |
| momus-cli integration tests | 📋 | 0 tests |
| FHIR converter integration tests | 📋 | Port from fhir-autotest (8 tests) |

### Documentation

| Area | Status | Notes |
|------|--------|-------|
| DESIGN.md | ✅ | Updated with full architecture |
| AGENTS.md | ✅ | Updated with crate details |
| README.md | ✅ | Quick start, examples, CLI reference |
| FEATURES.md | ✅ | This file |
| Crate-level docs (lib.rs) | 🧪 | Some crates have minimal docs |
| API docs (docs.rs) | 📋 | Need doc comments on all public items |
| Examples directory | ✅ | health-check.json, crud-sequence.json |
| CLI help text | ✅ | Clap derive |

### Build & CI

| Area | Status | Notes |
|------|--------|-------|
| Workspace builds | ✅ | `cargo build` passes |
| All tests pass | ✅ | `cargo test --all-targets` passes |
| Clippy clean | ✅ | `cargo clippy -- -D warnings` passes |
| Format check | ✅ | `cargo fmt --check` passes |
| CI workflow | 📋 | Not yet set up |
| Crates.io publishing | 📋 | Not yet published |
| Docker image | 📋 | Not yet set up |

---

## Version Roadmap

| Version | Focus | Key Deliverables |
|---------|-------|-----------------|
| v0.1.0 | Foundation | Core AST, engine, mock server, CLI, fuzz mutators, scaffolded crates |
| v0.2.0 | FHIR Port + Runner Completion | FHIR converter, bench/fuzz/chaos runners, contract/guard/diff runners, config system |
| v0.3.0 | Converters + Advanced Features | OpenAPI/Postman converters, bulk data, GraphQL converter, HTML reports |
| v0.4.0 | Protocol Expansion | gRPC converter, TransportAdapter trait, WebSocket support |
| v1.0.0 | Stable Release | API stability, documentation complete, CI/CD, crates.io |
