# Momus — Roadmap

---

## Legend

| Icon | Meaning |
|------|---------|
| 🔜 | Scaffold first (types/interface), logic later |
| 🧪 | Ship with a full test suite |
| 📋 | Planned, not started |

---

## P1 — Core FHIR conformance gaps (highest value)

These directly strengthen Momus's purpose: proving contractual coverage of a
FHIR server.

### P1.1 Mock server: real query-parameter search filtering
**Present:** Go's mock `Store` is `Put/Get/Delete/List`; a search returns every
stored resource regardless of query string.
**Gap:** search filters by `key=value` query
params, supports `_sort` (with `-` prefix for descending), and `_count`
pagination.
**Do:**
- Add query-parameter filtering to the mock search (`?name=X` returns only
  resources whose field matches; `?active=true` etc.).
- Support `_sort` and `_count`.
- Add search-seed matching so accept searches actually return the provisioned
  matching resource (today the mock returns the whole store).
**Why:** Without real search semantics the mock cannot validate
search-obligation coverage meaningfully — a search test "passes" trivially.

### P1.2 Broaden FHIR search test generation
**Present:** Go derives/emits 6 search variants: valid, no-results,
invalid-value, multiple-results, invalid-modifier, combination.
**Gap:** chained search, reverse-chaining
(`_has`), `_include` / `_revinclude`, near/proximity (`near`), and 11 result-param
tests (`_summary`, `_elements`, `_count`, `_sort`, `_has`, `_filter`, `_tag`,
`_profile`, `_security`, `_type`, `_language`), plus search *prefixes*
(`gt/lt/ge/le/ne/eq`) and modifiers (`:contains`, `:exact`, `:missing`, `:not`).
**Do (🔜):** add these as coverage domains/variants in
`internal/fhir/coverage` + generation. Start with prefixes/modifiers and
`_include` / `_revinclude`, which are the most common real-world gaps, then
chained / `_has`, then the result-param sweep.
**Why it matters:** these are contractual obligations a real server declares;
leaving them untested means coverage is overstated.

### P1.3 FHIR resource validation against a profile (`fhir validate`)
**Present:** Go has no command to validate a single JSON resource against a
StructureDefinition profile.
**Gap**
with `--base-url` to use the server's `/metadata` CapabilityStatement to pick the
server-enforced profile per type):
**Do (🧪):** a `momus validate` command that loads an IG package, resolves the
profile chain, and checks required elements (`min > 0`), required slices,
datatypes, and terminology bindings — the same logic `synthesizeBody` already
uses, inverted into a validator.
**Why:** useful standalone, and a building block for contract validation (see P3.3).

### P1.4 FHIR → OpenAPI generation
**Present:** Go loads OpenAPI and derives API constraints, but cannot emit one.
**Gap** the inverse
converter — turn a FHIR CapabilityStatement (+ StructureDefinition snapshots)
into a reusable OpenAPI 3.x document.
**Do (🔜):** `momus fhir openapi` emitting paths per resource type, schemas from
profile snapshots, search params as query params.
**Why it matters:** enables downstream OpenAPI tooling (mock generation, contract
checks) and lets an IG be published as a plain API spec.

### P1.5 Bulk data upload / delete (not just generate)
**Present:** Go has `coverage bulk` (generate NDJSON) and `coverage provision`
(upload a test-plan seed dataset).
**Gap:** upload a whole `{Type}.ndjson` data
directory to a write endpoint with configurable method (PUT/POST), auth, and
concurrency (default 8); and delete the uploaded resources back.
**Do (🔜):** add `--data-dir`, `--endpoint`, `--method`, `--concurrency` upload
and delete paths reusing `internal/fhir/provisioning`.

---

## P2 — Generic harness value (portable across FHIR + OpenAPI)

### P2.1 Data-driven testing (dataset / row templates)
**Gap:** run a request step
once per row of a CSV/JSON dataset, with `{row.<field>}` template substitution,
per-row results, and optional fail-fast.
**Do (🧪):** add a `dataset` field to `ast.Request`, a `{row.*}` resolver in
`internal/core/runner`, and per-row report aggregation.

### P2.2 Richer assertion grammar
**Present:** Go assertions are `status in [...]` plus `body.<path>`,
`header.<name>`, `variable.<name>` comparisons (`== != < <= > >=`).
**Gap:** `AllOf`/`AnyOf`/`Not` combinators, `Header`
(present/absent/equals/contains/regex), `BodyLength`, `ContentType`, `ValidJson`,
`JsonPath` with predicates (`exists`, `not_exists`, `eq`, `not_eq`, `cmp`,
`length`, `count`, `every`, `some`, `schema`), `Schema` (JSON Schema), and
`ResponseTime`.
**Do (🔜):** add a composable expression grammar on top of the existing
`Assertion` interface — start with combinators (`and`/`or`/`not`), `response-time <= N`,
`content-type`, `json-valid`, and a JSONPath selector for the body. JSON Schema
validation is the highest-value single addition.
- **Why:** the current grammar is expressive enough for generated FHIR asserts
  but not for hand-authored or API tests.

### P2.3 Template functions
**Present:** Go resolves `{{var}}` captured variables only.
**Gap:** `{base_url}`, `{steps.<name>.*}`, `{env.VAR}`, `{random.uuid}` /
`{random.int}` / `{random.string}`, and `{body.<path>}` (reference a prior
response field).
**Do (🔜):** add `{random.*}`, `{env.*}`, `{body.<path>}` to the runner template
resolver. `{body.<path>}` is high-value for API chaining.
- **Why:** enables self-contained plans without a capture step for every link.

### P2.4 Plan-level setup / teardown and soft-fail
**Present:** Go `ast.Plan` has `Dataset` + `Root`; requests have no soft-fail;
no global setup/teardown step list.
**Gap `TestPlan{ setup, teardown }`, `RequestStep.soft_fail`,
`SequenceStep.continue_on_failure`):**
**Do (🧪):** add `setup`/`teardown` step lists to `ast.Plan`, a `soft-fail`
flag on `ast.Request` (failure recorded but not failing the case), and
`continue-on-failure` on `ast.Sequence`.
- **Why it matters:** setup/teardown and soft-fail are table-stakes for a general
  test harness and give better triage (a soft-fail step surfaces an issue without
  aborting a run).

### P2.5 Hand-authored / dry-run / validate / plan-summary / init
**Gap:** `validate` (parse + validate plan JSON without running),
`run --dry-run` (resolve and print requests, send none), `plan` (human-readable
plan summary), `init` (scaffold a skeleton plan/config), shell `completions`.
**Do (🔜):** add these to the Cobra CLI. Cheap and high developer value:
- `momus validate <plan>` — parse + validate the JSON.
- `momus run <plan> --dry-run` — print resolved requests, no HTTP.
- `momus plan <plan>` — render a readable summary of the AST.
- `momus completions` — Cobra has built-in shell completion.
- `momus init` — scaffold a minimal plan.

### P2.6 Config file (TOML/YAML) with env profiles
**Gap:** a `--config momus.toml` with per-command sections,
`[global]` cross-cutting settings, `[env.<name>]` profiles, env-var
interpolation, `deny_unknown_fields`.
**Do (🔜):** load an optional `momus.toml` / `momus.yaml` and merge CLI flags over
it. Useful for teams standardising a base URL, auth, headers, and coverage
defaults.

### P2.7 JSON Schema validation (general API responses)
**Gap:** Go has no JSON Schema validator; wires `jsonschema`.
**Do (🧪):** add a `jsonschema` Go dependency, expose it as an assertion
(`body.schema(file)` or a `schema` expression) and as a `contract`/`validate`
backend.
- **Why:** the single most reusable assertion for OpenAPI-backed API tests.

---

## P3 — Expanding Momos into a general API harness

### P3.1 Load / performance testing (`bench`)
`BenchConfig{ concurrency, duration, mode }` with **Steady**, **MaxThroughput**
(ramp until error/latency threshold), **Soak** (hours, health checks), warmup,
HDR-histogram latency (P50/P90/P95/P99), JSON + HTML reports.
- **Do (🔜):** `momus bench <plan> --concurrency N --duration S`, reusing the
  Go concurrent runner; start with Steady mode + percentiles + JSON report.

### P3.2 Payload mutation fuzzing (`fuzz`)
`Mutator` trait + concrete mutators (Boundary, Encoding, TypeMismatch,
Cardinality), deterministic PRNG, then `run_fuzz()` HTTP dispatch.
- **Do (🧪):** `momus fuzz <plan> --iterations N` that mutates request bodies
  and classifies responses (pass/fail/crash/timeout). The negative-mutation logic
  in `internal/fhir/generation` is a natural seed.

### P3.3 Contract validation (`contract`)
validate responses against an OpenAPI / GraphQL spec; planned FHIR profile
validation (profile matching, required-element presence, fixed/pattern,
mustSupport) — the FHIR half overlaps P1.3.
- **Do (🔜):** `momus contract <plan> --spec openapi.yaml` validating response
  status + body against schemas; reuse the OpenAPI model already in
  `internal/openapi`.

### P3.4 Security scanning (`guard`)
auth-header presence (including 200-with-error-body auth bypass), CORS
misconfiguration (wildcard / wildcard+credentials / reflected origin), info-leak
detection (stack traces, SQL errors, 30+ patterns), exposed-endpoint discovery
(22 common paths), security-headers check (HSTS, CSP, X-Content-Type-Options,
X-Frame-Options).
- **Do (🧪):** `momus guard <plan>` — all five categories are self-contained
  HTTP probes; high signal, low risk.

### P3.5 Environment diffing (`diff`)
run the same plan against baseline + target concurrently, compare status /
headers / body (recursive JSON object/array diff with path tracking), detect
new/missing/changed fields.
- **Do (🧪):** `momus diff <plan> --baseline URL --target URL` — a structured
  JSON diff (not line-level). Great for regression checking between environments.

### P3.6 Converters (`convert`)
`convert` for curl, HAR, OpenAPI, Postman, GraphQL, gRPC, FHIR.
- **Do (🔜):** start with `curl` and `har` (highest practical value for turning
  recorded traffic into plans), then OpenAPI, then Postman. FHIR conversion is
  already core to Go (`coverage ast`).
- **Why:** makes Momos a drop-in for teams migrating from cURL/Postman/HAR.

---

## Cross-cutting / engineering

- **JSON Schema validator dependency** — gate P2.7 / 3.3.
- **Global `--timeout` flag** — exposes a per-request timeout; Go has none
  (all requests rely on the runner default). Add `--timeout` mapping to a client
  timeout and a `ResponseTime` assertion.
- **Deterministic generation seed** — `fhir generate --seed N`; Go
  generation is already deterministic, but expose a seed flag for reproducible
  bulk/`synthesize` output across runs.
- **Snapshot/contract test hardening** — the moved `internal/fhir/generation`
  tests are green; add a snapshot test for a generated plan's byte-identical
  output (guard against regressions as the AST evolves).
