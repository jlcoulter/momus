# Momus

Momus is a testing framework for **API and FHIR conformance testing**. It
derives contractual coverage obligations from FHIR packages and OpenAPI
documents, generates executable test plans, provisions seed data, runs the
tests against a live server, and reports which obligations were satisfied and
which remain uncovered.

The core principle: *completeness is defined as contractual coverage
obligations satisfied, not exhaustive value permutation.*

## Status

Momus implements the full v1 pipeline end to end. The constraint model,
coverage derivation across all domains, test generation (positive, negative,
boundary, interaction), the dependency-ordered provisioner, the concurrent
runner, coverage reporting, and OpenAPI contract support are all implemented
and exercised by tests. See [`docs/architecture.md`](docs/architecture.md) for
the layering and design decisions.

What is implemented:

- **FHIR package loading and resolution** — local `.tgz` loading, recursive
  dependency resolution, local-first resolution with remote download
  fallback, a download cache, floating-version resolution (`current` →
  concrete), and manifest parsing.
- **Normalised FHIR model** — `StructureDefinition`, `ValueSet`, `CodeSystem`,
  `CapabilityStatement`, and `SearchParameter` normalised into internal model
  types.
- **Constraint model** (`internal/fhir/constraintderive`) — a flat, `Kind`-typed
  set of contractual rules derived from the registry with stable identifiers
  (`cardinality`, `datatype`, `terminology`, `invariant`, `reference`, `fixed`,
  `pattern`, `search`, `interaction`, `api-operation`, `api-parameter`).
- **Coverage derivation** (`internal/fhir/coverage`) — constraint-driven
  obligations across the cardinality, datatype, terminology, invariant,
  reference, and required-slice structure domains, plus search, operation,
  state/CRUD, and (opt-in) pairwise interaction coverage. Every requirement is
  traceable to its source constraint.
- **Test generation** (`internal/fhir/generation` + `internal/core/generation`) —
  positive, negative, and boundary cases. The domain-agnostic core framework
  (`internal/core/generation`) orchestrates generation and delegates all
  payload/search synthesis through a `PayloadBuilder` interface; the FHIR
  adapter (`internal/fhir/generation`) implements it. Negative variants mutate
  a valid payload against exactly one constraint and assert rejection; boundary
  helpers emit edge values for string length, numeric, and cardinality ranges.
- **Interaction (pairwise) coverage** — at `--strength 2`, pairwise obligations
  between accept requirements on the same profile are derived and generation
  selects a near-minimal set by greedy set-cover, grouping compatible accepts
  into shared valid payloads.
- **Search, operation, and CRUD coverage** — valid / no-results /
  invalid-value / multiple-results / invalid-modifier searches, read / update /
  patch / delete / history, custom (`$`) operations, negative state
  transitions, and a full create-read-update-read-delete-read(404) sequence.
  Search obligations are scoped to the parameters the server's
  CapabilityStatement declares; pass `--include-universal-search` to also cover
  the default universal FHIR parameters (`_id`, `_count`, `_sort`, `_include`,
  `_summary`, `_filter`) for every resource type even when the server does not
  declare them.
- **A single registry-driven data pipeline** — one core synthesises both the
  seed `Dataset` and every test payload from the registry as the source of
  truth, so test data and provisioned data cannot drift apart.
- **Dependency-ordered provisioning** (`coverage provision`) — uploads the seed
  dataset ahead of execution, targets before dependents, with cyclic resources
  provisioned serially.
- **Bulk NDJSON generation** (`coverage bulk`) — a realistic corpus across
  resource types with references wired into a distributed, coherent web.
- **Concurrent runner** — executes AST requests/assertions with `Parallel`
  branches running concurrently, per-branch variable scoping, and
  deterministic result aggregation.
- **Coverage reporting** — JSON report (`--output`), console summary, and an
  HTML report with drill-down navigation (`--html`).
- **Assertion expression engine** — `status in [...]` plus `body.<path>`,
  `header.<name>`, and `variable.<name>` comparisons (`==`, `!=`, `<`, `<=`,
  `>`, `>=`).
- **OpenAPI contract support** — loads OpenAPI 3.x documents into operation
  contracts, parameters, and schemas, and derives API constraints through the
  shared constraint model (`momus api`).

## Layout

The codebase follows a narrow-core-wide-composition layout: a domain-agnostic
core (`internal/core`) holds the generic engine, and the FHIR domain
(`internal/fhir`) implements the domain-specific adapters that plug into it.
The core never imports FHIR; FHIR depends on core, never the reverse.

```
cmd/momus/          CLI entry point
internal/core/      domain-agnostic engine: test AST, coverage model/planner/
                    evaluator/report, generation framework, runner, assertions,
                    tracing, constraint model
internal/fhir/      FHIR domain: model, constraint derivation, coverage
                    derivation, generation adapter (PayloadBuilder), package
                    loading, registry, terminology, provisioning, bulk,
                    profile validator (validate), FHIRPath engine (fhirpath),
                    golden self-conformance runner, navigable report writer
internal/openapi/   OpenAPI document loading and API constraint derivation
docs/               architecture and feature documentation
pkg/                reserved public API (intentionally empty)
```

## Build & test

Requires Go 1.26+.

```sh
go build ./...
go vet ./...
go test ./...
```

Run the CLI:

```sh
go run ./cmd/momus --help
```

## CLI

Momus uses a Cobra-based CLI with command groups: `package`, `coverage`,
`api`, `mock`, `test`, `validate`, and `conformance`.

### `package` — FHIR package operations

Load a local FHIR package archive (`.tgz`):

```sh
go run ./cmd/momus package load package.tgz
```

Resolve a package and its transitive dependencies:

```sh
go run ./cmd/momus package resolve package.tgz
```

Resolver behaviour:

- Searches the local dependency directory first
- Downloads missing package archives from FHIR package registries
- Stores downloaded archives in `.momus/packages` by default
- Resolves floating dependency versions such as `current` using registry metadata
- Uses `root-wins` as the default conflict policy (`--conflict-policy strict`
  is available for auditing)

Override the search/download directories:

```sh
go run ./cmd/momus package resolve package.tgz --deps-dir . --download-dir ./.momus/packages
```

### `coverage` — coverage planning operations

Derive the constraint model (the normalised set of contractual rules):

```sh
go run ./cmd/momus coverage constraints package.tgz --output ./constraints.json
```

Derive a coverage plan from resolved package constraints:

```sh
go run ./cmd/momus coverage derive package.tgz --output ./coverage-plan.json
```

Derivation defaults are intentionally practical:

- Includes required elements (`min > 0`) and `mustSupport` elements
- Excludes optional non-`mustSupport` elements unless `--include-optional` is set
- Excludes low-value infrastructure paths (such as `meta`, `text`, `language`)
  unless `--include-low-value-paths` is set

Scope derivation to specific contracts:

```sh
go run ./cmd/momus coverage derive package.tgz \
  --include-resource Observation \
  --include-profile-url http://hl7.org/fhir/StructureDefinition/Observation \
  --exclude-path-prefix Observation.meta
```

Generate a test plan (seed dataset + test AST). `coverage ast` and
`coverage plan` produce the same artifact; provisioning and execution both
consume the test plan, not the package:

```sh
go run ./cmd/momus coverage ast package.tgz \
  --base-url http://localhost:8080/fhir \
  --output ./test-plan.json
```

Provision the seed dataset up front (stage J). `coverage provision` consumes
the test plan and uploads the seed resources it carries to the target server
before any tests run:

```sh
go run ./cmd/momus coverage provision ./test-plan.json --base-url http://localhost:8080/fhir
```

Execute a generated test plan and output a JSON result report. `coverage run`
consumes the test plan; pass `--coverage-plan` to evaluate contractual
coverage against the plan produced by `coverage derive`:

```sh
go run ./cmd/momus coverage run ./test-plan.json \
  --coverage-plan ./coverage-plan.json \
  --base-url http://localhost:8080/fhir \
  --output ./test-results.json \
  --html ./coverage.html
```

The `--html` flag writes an HTML coverage report with drill-down navigation:
overall contractual coverage, per-domain percentages, and per-domain /
per-resource / per-variant lists where every executed item shows a pass/fail
badge and expands to its assertion, request URL/body, and response body.

#### Navigable output directory

Every run also writes a **navigable output directory** to `.momus/output` by
default, so a large run is sliced into small, easy-to-navigate files instead of
one monolithic JSON:

```text
.momus/output/
  index.json        summary, coverage %, failed-case pointers
  index.html        navigable tree
  summary.json      the concise run summary
  full.json         the monolithic artifact (only with --include-cases)
  cases/<req-id>.json      one small file per case
  by-resource/<Type>.json      pass/fail matrix per resource type
  by-parameter/<param>.json    pass/fail matrix per search/operation param
  failed-index.json  failed cases grouped by reason
```

- `--output-dir path` relocates it; `--output-dir -` disables it.
- `--output file.json` still writes the single-file JSON for CI tooling.
- Inspect one case in full depth (request, response, assertion, trace) without
  touching the rest of the report:

```sh
go run ./cmd/momus coverage explain 'search|Patient|name|search-valid'
```

Generate realistic bulk data as NDJSON (the FHIR Bulk Data `$export` format):

```sh
go run ./cmd/momus coverage bulk package.tgz --output ./data.ndjson
```

Key options:

- `--count N` — resources to generate per type (default `25`)
- `--per-type Type=Count` — per-type counts, overriding `--count` (repeatable)
- `--include-resource Type` — only generate these types (repeatable); referenced
  target types are pulled in automatically so all references resolve
- `--exhaustive` — populate optional elements with realistic values (default `true`)
- `--output path` — write NDJSON to a file

#### Write endpoints and credentials

If resource creation (write) requests must target a different endpoint than
read/search requests, pass `--write-base-url` (defaults to `--base-url`). This
is set at AST generation and provisioning time; `coverage run` inherits the
baked-in URLs:

```sh
go run ./cmd/momus coverage ast package.tgz \
  --base-url http://read.example/fhir \
  --write-base-url http://write.example/fhir \
  --output ./test-plan.json
go run ./cmd/momus coverage provision ./test-plan.json \
  --base-url http://read.example/fhir \
  --write-base-url http://write.example/fhir
go run ./cmd/momus coverage run ./test-plan.json \
  --coverage-plan ./coverage-plan.json --output ./test-results.json
```

Write requests (PUT/PATCH/POST/DELETE) go to `--write-base-url`; GET
read/search requests go to `--base-url`. The same flag is available on
`coverage ast`, `coverage provision`, `coverage plan`, `api ast`, and `api run`.

If the write endpoint requires different credentials than the read endpoint,
pass `--write-basic-username` and `--write-basic-password` to `coverage
provision` (and `coverage run`). These apply to write requests targeting
`--write-base-url` and default to `--api-basic-username` /
`--api-basic-password` when unset:

```sh
go run ./cmd/momus coverage provision ./test-plan.json \
  --base-url http://read.example/fhir \
  --write-base-url http://write.example/fhir \
  --api-basic-username read-user --api-basic-password read-pass \
  --write-basic-username write-user --write-basic-password write-pass
```

When the server rejects a seeded resource, `coverage provision` prints each
rejected resource with the server's reason parsed from the OperationOutcome
response. Run with `--debug` to also write the rejected payloads and full
server responses to `.momus/output/provision-failures.json` for inspection.

### `api` — OpenAPI contract operations

Derive the constraint model from an OpenAPI contract:

```sh
go run ./cmd/momus api constraints ./openapi.json --output ./api-constraints.json
```

Generate a test AST from an OpenAPI document:

```sh
go run ./cmd/momus api ast ./openapi.json --base-url http://localhost:8080 --output ./api-ast.json
```

Generate and execute tests against a live API:

```sh
go run ./cmd/momus api run ./openapi.json --base-url http://localhost:8080
```

### `validate` — profile conformance

Validate a FHIR JSON resource against a profile, printing one line per
violation. The profile is resolved from the loaded package(s):

```sh
go run ./cmd/momus validate ./resource.json \
  --package package.tgz \
  --profile http://hl7.org/fhir/StructureDefinition/Patient
```

Without `--profile`, Momus uses the resource's own `meta.profile` claims. Exit
code is non-zero when the resource fails validation.

### `mock` — a local FHIR server for development

Start a mock FHIR server that holds resources in memory and serves real FHIR
semantics (PUT/POST store, GET retrieves, DELETE removes, search returns a
Bundle):

```sh
go run ./cmd/momus mock --port 8080
```

With `--semantic --package package.tgz`, the mock also enforces profile
conformance: a non-conformant PUT/POST is rejected with `422` + an
OperationOutcome naming each issue, so positive and negative test cases are
meaningful:

```sh
go run ./cmd/momus mock --port 8080 --semantic --package package.tgz
```

### `conformance self-test` — the golden-matrix oracle

Run Momus's own generated tests against the semantic mock for every reference
fixture, proving conformance logic works end to end with no network:

```sh
go run ./cmd/momus conformance self-test
```

Each fixture derives a coverage plan, generates the test AST, snapshots it
byte-identically, provisions seed data, and asserts 100% pass. This is the
developer's daily oracle: add a feature, add a fixture, and every parameter's
positive/negative/edge cases are exercised and proven. Exits non-zero on any
failure.

Reference fixtures under `testdata/golden/` cover the breadth of what the
application supports, with one fixture per search-parameter family:

- `patient` — string (`name`) and token (`gender`) search; HumanName
- `observation-slice` — token search with slice constraints
- `observation-invariant` — token search with invariant regex
- `search-operations` — string/token/`_id` at interaction strength 2 (pairwise
  search-parameter combinations)
- `patient-date` — `date` search
- `observation-value` — `quantity` and `reference` search with `Quantity`
- `location-near` — `special` (`near`) geographic search with coordinates
- `observation-composite` — `composite` search (`part1$part2`)
- `patient-search` — union search expressions, `Identifier`/`ContactPoint`
  token seeds, choice `deceased[x]` date search, and `dateTime` partial-prefix
  matching
- `practitioner-role` — `date` search on a pure `Period` element (seeded via
  its `start` member)

Each fixture asserts the positive, negative, and edge cases for its search
parameters (valid, no-results, invalid-value, invalid-modifier,
multiple-results) plus the datatypes its elements use.

## Coverage pipeline

The executable pipeline, with each command owning one stage:

1. Resolve the package graph (`package resolve`)
2. Derive scoped coverage requirements (`coverage derive`)
3. Build the resource dependency DAG (internal planner)
4. Generate a test plan: seed dataset + test AST (`coverage ast` / `coverage plan`)
5. Provision the seed dataset from the test plan (`coverage provision`)
6. Execute the test plan, evaluate coverage, and emit a report (`coverage run`)

The constraint model (`coverage constraints`) is the normalised intermediate
representation between the registry and coverage: contractual rules are
derived once and anchor per-domain coverage obligations and test traceability.

Dependency-chain behaviour in the AST/runner:

- Resources are ordered by DAG levels (topological order)
- The seed dataset is a transitive closure: every type a test references is
  seeded and provisioned before its dependents, so references resolve
- Search-accept obligations carry the data they need to return results: a
  matching resource (two for multiple-results) is added to the seed dataset
  whenever the search parameter can be matched with a valid value
- Seed resources are provisioned up front by `coverage provision` from the
  dataset carried in the test plan, before any test case executes
- Test cases reference seed resources by their deterministic setup id
  (`momus-setup-<Type>`); `coverage run` marks the plan's seed resources as
  already provisioned
- Capture nodes extract resource IDs from responses; later request payloads
  can reference captured IDs via templates such as `Patient/{{Patient.id}}`

## Release process

Releases are automated with [release-please](https://github.com/googleapis/release-please)
and [git-cliff](https://git-cliff.org):

- On every push to `master`, **release-please** analyses conventional commits,
  bumps the version (`feat` → minor, `fix` → patch, `BREAKING CHANGE` → major),
  and opens a release PR.
- When the release PR is merged, release-please creates the version tag and a
  GitHub Release.
- A tag-triggered CI job then generates the changelog with **git-cliff**,
  builds the `momus` binary for Linux, macOS, and Windows (amd64/arm64,
  injecting the version via `-ldflags`), and attaches the binaries and changelog
  to the release.

To release, merge the release PR that release-please opens — the tag, changelog,
and binaries are produced automatically.

## License

See [LICENSE](LICENSE).
