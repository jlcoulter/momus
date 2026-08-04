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
| `ResponseTime` — max response time | ✅ | Wired in evaluator + runner |

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
| `schema` — match result against JSON Schema | ✅ | Wired via `jsonschema` crate |

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
| Schema validation | ✅ | Wired via `jsonschema` crate |
| ResponseTime measurement | ✅ | Wired in evaluator + runner |
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

### cURL Converter (✅ Complete)

| Feature | Status | Notes |
|---------|--------|-------|
| Parse `-X`/`--request` | ✅ | |
| Parse `-H`/`--header` | ✅ | |
| Parse `-d`/`--data`/`--data-raw`/`--data-binary` | ✅ | |
| Parse URL | ✅ | |
| Parse `--max-time` | ✅ | |
| Parse `-u`/`--user` (basic auth) | ✅ | Base64 encoded |
| Parse `-b`/`--cookie` | ✅ | |
| Generate `TestPlan` with assertions | ✅ | 13 tests |

### HAR Converter (✅ Complete)

| Feature | Status | Notes |
|---------|--------|-------|
| Parse HAR 1.2 JSON | ✅ | |
| Map entries to RequestSteps | ✅ | |
| Extract headers, method, URL, body | ✅ | |
| Generate status code assertions | ✅ | 6 tests |

### OpenAPI Converter (✅ Complete)

| Feature | Status | Notes |
|---------|--------|-------|
| Parse YAML/JSON OpenAPI 3.x | ✅ | |
| Walk paths and operations | ✅ | |
| Path/query/header parameter extraction | ✅ | |
| Example request body generation from schemas | ✅ | Object, array, string, number, boolean, allOf |
| Status/content-type assertions from responses | ✅ | |
| Schema-aware value generation | ✅ | Date, datetime, email, uri, uuid formats |
| Path parameter substitution | ✅ | `{id}` → `1` |

### Postman Converter (✅ Complete)

| Feature | Status | Notes |
|---------|--------|-------|
| Parse Collection v2.1 JSON | ✅ | |
| Recursive folder walking | ✅ | |
| Method, URL, headers, body extraction | ✅ | |
| Body modes: raw, urlencoded, formdata | ✅ | |
| Status code assertions from responses | ✅ | 7 tests |

### GraphQL Converter (✅ Complete)

| Feature | Status | Notes |
|---------|--------|-------|
| Parse SDL schema | ✅ | Regex-based |
| Extract Query fields | ✅ | |
| Extract Mutation fields | ✅ | |
| Generate query/mutation request bodies | ✅ | |
| Status 200 assertions | ✅ | 12 tests |

### gRPC Converter

| Feature | Status | Notes |
|---------|--------|-------|
| Parse protobuf definitions | 🔜 | Stub (v0.4.0) |
| RPC method → test case | 🔜 | Stub (v0.4.0) |
| Message types → example payload | 🔜 | Stub (v0.4.0) |

### FHIR Converter (✅ Complete — ported from fhir-autotest)

| Feature | Status | Notes |
|---------|--------|-------|
| Parse IG package (.tgz) | ✅ | Ported |
| Categorize resources by type | ✅ | Ported |
| Select CapabilityStatement | ✅ | Ported |
| Resolve parent profile chain | ✅ | Ported |
| Download missing profiles from registry | ✅ | Multi-source download |
| Extract dependencies (topological sort) | ✅ | Uses `momus_core::deps` |
| Generate resources from StructureDefinitions | ✅ | Ported (5-pass) |
| Required field population | ✅ | Ported |
| Required slice population | ✅ | Ported |
| Extension slice population | ✅ | Ported |
| MustSupport backbone population | ✅ | Ported |
| MustSupport optional field population | ✅ | Ported |
| Type-specific value generation | ✅ | Ported (HumanName, Address, etc.) |
| Value set resolution | ✅ | Ported (ValueSet/CodeSystem maps) |
| Generate test plan from CapabilityStatement | ✅ | Ported |
| CRUD test generation | ✅ | Ported |
| Search test generation (single, modifiers, prefixes) | ✅ | Ported |
| Near/proximity search tests | ✅ | Ported |
| Combinatorial search tests | ✅ | Ported |
| Chained search tests | ✅ | Ported |
| Include/revinclude tests | ✅ | Ported |
| Result param tests (_summary, _elements, _count, _sort, _has) | ✅ | Ported |
| Operation tests ($operation) | ✅ | Ported |
| Negative tests | ✅ | Ported |
| Conformance tests (mustSupport) | ✅ | Ported |
| Bulk data generation (NDJSON) | ✅ | Ported |
| Bulk data upload with wave ordering | ✅ | Ported |
| HCPD/AU-specific generation | ✅ | Ported |
| Locality/suburb generation | ✅ | Ported (65 Australian suburbs) |
| Profile validation against StructureDefinition | ✅ | Ported |
| Response assertion engine | ✅ | Ported |

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
| `run` subcommand | ✅ | Full test plan execution |
| `validate` subcommand | ✅ | Parse + validate test plan JSON |
| `mock` subcommand | ✅ | Axum-based mock server |
| `bench` subcommand | ✅ | Steady mode with latency histograms |
| `fuzz` subcommand | ✅ | 4 mutators with HTTP dispatch + leak detection |
| `chaos` subcommand | ✅ | 5 implemented experiments |
| `contract` subcommand | ✅ | OpenAPI/GraphQL response validation |
| `guard` subcommand | ✅ | 5 security check categories |
| `diff` subcommand | ✅ | Field-level JSON diff between environments |
| `convert` subcommand | ✅ | curl, HAR, FHIR, OpenAPI, Postman, GraphQL converters |
| `--base-url` flag | ✅ | |
| `--output` flag | ✅ | |
| `--verbose` flag | ✅ | |
| `--config` flag | ✅ | |
| Tracing/logging setup | ✅ | |

### Planned (v0.2.0+)

| Feature | Priority | Notes |
|---------|----------|-------|
| `--format` (json/html/text) for output | ✅ | Auto-detect from .html extension |
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
| momus-core unit tests | ✅ | 56 tests |
| momus-mock unit tests | ✅ | 19 tests |
| momus (umbrella) unit tests | ✅ | 5 tests |
| momus-fuzz unit tests | ✅ | 16 tests |
| momus-bench unit tests | ✅ | 5 tests |
| momus-chaos unit tests | ✅ | 10 tests |
| momus-contract unit tests | ✅ | 9 tests |
| momus-guard unit tests | ✅ | 2 tests |
| momus-diff unit tests | ✅ | 8 tests |
| momus-convert unit tests | ✅ | 146 tests |
| momus-cli integration tests | 📋 | 0 tests |

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
