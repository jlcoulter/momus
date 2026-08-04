# Momus Design

## Philosophy

Momus is a **generic API test harness** built around a composable assertion AST. It is not a fuzzer, not a schema validator, not a performance benchmarker, and not a security scanner. It is a test runner that happens to be good at those things when pointed at the right input.

The core insight from the FHIR project was: *the test structure (setup → request → assert → teardown) is universal; only the test generators are domain-specific.* Momus extracts that universal engine and lets domain frontends (OpenAPI, FHIR, GraphQL) compile into its AST.

## Architecture

```
┌──────────────────────────────────────────────────┐
│  Domain Frontends (external / future)             │
│  OpenAPI → AST  │  FHIR → AST  │  Postman → AST  │
└──────────────────────┬───────────────────────────┘
                       │ JSON plan
                       ▼
┌──────────────────────────────────────────────────┐
│  Momus Core                                       │
│                                                    │
│  ┌──────────┐  ┌──────────┐  ┌────────────────┐ │
│  │  AST     │  │  Engine  │  │  Mock Server    │ │
│  │  (plan,  │→ │  (eval,  │  │  (axum,         │ │
│  │  assert) │  │  runner) │  │  record/replay)  │ │
│  └──────────┘  └──────────┘  └────────────────┘ │
└──────────────────────┬───────────────────────────┘
                       │ HTTP
                       ▼
              ┌────────────────┐
              │  Target API(s)  │
              └────────────────┘
```

The boundary is clean: frontends produce `TestPlan` JSON, Momus executes it. No frontend code lives in the core.

## What Momus Is Not (And Why)

### Not a fuzzer

Fuzzing is a distinct engineering problem with its own literature (coverage-guided, grammar-based, AFL-style). Bolting SQLi/XSS mutation onto a test runner produces shallow results and conflates concerns. Momus can *execute* fuzz-generated test cases, but generating them belongs in a separate tool that feeds Momus plans.

### Not multi-protocol (yet)

gRPC, GraphQL, and Protobuf each require fundamentally different transport and serialization. A `TransportAdapter` trait is the right abstraction, but implementing it for each protocol is a crate's worth of work per protocol. Start with HTTP/REST and prove the architecture before expanding.

## Benchmark Engine (momus-bench)

The benchmark crate from the FHIR project demonstrates a pattern that belongs in Momus: take a `TestPlan`, spawn N concurrent workers, execute random tests from the plan under load, and record latency distributions. The core is already protocol-agnostic — it only needs a list of `(group_name, test_case)` tuples and an HTTP client.

A `momus-bench` crate would provide:

| Feature | Description |
|---------|-------------|
| **Steady mode** | Fixed concurrency for a fixed duration |
| **Max-throughput mode** | Ramp concurrency upward until error rate or latency threshold is breached |
| **Soak mode** | Sustained load at fixed concurrency for hours |
| **Warmup** | N requests before recording to warm caches |
| **HDR histograms** | P50/P90/P95/P99 latency per group and overall |
| **Reports** | JSON summary, full results, text report, HTML dashboard |
| **Signal handling** | Graceful shutdown on Ctrl+C |

The benchmark engine is a separate concern from the assertion runner. The assertion runner answers "does this API work correctly?" The benchmark engine answers "how does this API perform under load?" They share the `TestPlan` format and the HTTP client, but the execution model is fundamentally different — sequential with state passing vs concurrent stateless fire-and-forget.

This would live as `crates/momus-bench/` in the workspace, depending on `momus` for the AST types and the HTTP transport.

## Core Data Structures

### TestPlan

The top-level input. Everything is a `Step`:

```rust
pub struct TestPlan {
    pub name: String,
    pub base_url: String,
    pub default_headers: HashMap<String, String>,
    pub steps: Vec<Step>,
    pub setup: Vec<Step>,
    pub teardown: Vec<Step>,
}
```

### Step

A tagged union with four active variants and one placeholder:

| Variant | Purpose |
|---------|---------|
| `Request` | Single HTTP call with assertions |
| `Sequence` | Ordered sub-steps with state passing |
| `Parallel` | Concurrent sub-steps |
| `Script` | Reserved for inline logic (not yet implemented) |
| `Noop` | Disabled / placeholder |

`Sequence` is the key composability primitive. Steps within a sequence share a `RunContext` that accumulates saved responses. Later steps reference earlier ones via `{steps.<name>.*}` templates.

### Assertion

A composable tree, not a flat struct:

```
Assertion
├── AllOf(Vec<Assertion>)   // logical AND
├── AnyOf(Vec<Assertion>)   // logical OR
├── Not(Box<Assertion>)     // logical NOT
├── Status(u16)
├── StatusIn(Vec<u16>)
├── Header { name, predicate }
├── BodyLength(BodyLengthPredicate)
├── ContentType(String)
├── ValidJson
├── JsonPath { path, predicate }
└── Schema { schema }       // stub — delegates to jsonschema crate
```

The tree structure lets frontends build complex assertions from simple parts without a DSL. A FHIR frontend might produce `AllOf([Status(200), JsonPath("$.resourceType", Eq("Bundle")), ...])` — no new assertion types needed.

### Template Resolution

Three template forms, resolved at runtime:

| Template | Resolves To |
|----------|-------------|
| `{base_url}` | The plan's `base_url` field |
| `{steps.<name>.id}` | The `id` field of a saved step response |
| `{steps.<name>.<path>}` | Any JSON field from a saved step response |

This is the mechanism for parameter chaining. A `POST` that returns `{"id": "abc-123"}` saved as `"create_user"` makes `{steps.create_user.id}` available to subsequent steps. No DAG, no topological sort — just template substitution in a flat context map.

## Extension Points

### 1. Domain Frontends (external crates)

A frontend is anything that produces a `TestPlan`. The contract is JSON serialization. Examples:

- **OpenAPI frontend**: walks paths, generates CRUD sequences, extracts response schemas for `JsonPath` assertions
- **FHIR frontend**: the existing `fhir-autotest` project, refactored to emit Momus plans
- **Postman frontend**: converts Postman collections to Momus sequences

Each frontend lives in its own crate with a dependency on `momus` for the AST types.

### 2. Transport Adapters (future)

```rust
#[async_trait]
pub trait TransportAdapter: Send + Sync {
    async fn execute(&self, req: TestRequest) -> Result<TestResponse, TransportError>;
}
```

The built-in `HttpTransport` uses `reqwest`. A `GrpcTransport` would use `tonic` + `prost-reflect`. A `GraphQLTransport` would wrap queries in POST requests. The plan's `base_url` and step `method`/`url` fields are transport-agnostic — the adapter interprets them.

### 3. Assertion Plugins (future)

Custom assertion nodes can be added by implementing evaluation on the `Assertion` enum. The `Schema` variant is the canonical example — it's a placeholder that will delegate to the `jsonschema` crate when implemented.

## What's Next (Rough Priority)

1. **JSON Schema assertion** — implement `Assertion::Schema` using the `jsonschema` crate
2. **Script steps** — embed a minimal scripting runtime (Rhai) for custom logic between requests
3. **Config file format** — TOML frontend that compiles to the AST (simpler than raw JSON for humans)
4. **OpenAPI frontend** — first domain frontend, as a separate crate
5. **Transport adapter trait** — formalize the abstraction, keep HTTP as the only implementation initially

## Design Decisions That Want Revisiting

- **`soft_fail` on RequestStep** — currently unused in the runner. The intent was "log failure but continue the sequence", but `SequenceStep.continue_on_failure` already covers this at the group level. One of these should go.
- **`Script` step** — the variant exists but nothing handles it. Either implement Rhai integration or remove the variant until ready.
- **Assertion serialization** — the `#[serde(tag = "type")]` on `Step` works but the `Assertion` enum uses `#[serde(rename_all = "snake_case")]` without a tag, which means JSON consumers must guess the variant from the field names. Adding a tag would make machine-generated plans more robust.

## Comparison to the OmniTest Proposal

The OmniTest design document (an earlier exploration) proposed a significantly more ambitious system: multi-protocol transport, fuzzing engines, DAG dependency resolvers, performance benchmarking, and JUnit/HTML exporters — all in a single crate. That design conflated too many concerns and would have produced a system that does many things poorly rather than one thing well.

Momus takes the opposite approach: **narrow core, wide composition**. The core does one thing (execute test plans) and does it well. Everything else is a frontend, adapter, or plugin that lives outside the core crate. This is the Unix philosophy applied to API testing — each tool does one thing, and they compose via pipes (in this case, JSON plans and trait objects).
