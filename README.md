# Momus

Momus is a testing framework/tool for API and FHIR conformance testing.

This repository is a fresh, production-oriented implementation of Momus.
The current state is an **early MVP**: key end-to-end vertical slices are
implemented for package loading, dependency resolution, the constraint
model, coverage derivation, AST generation, and minimal test
execution/reporting.

## Status

The following capabilities are planned but **not yet fully implemented**:

- Full profile-resolution pipeline (inheritance, slicing, differential/snapshot merge)
- Profile resolution and inheritance, element trees, cardinality, slicing,
  and extensions
- Terminology expansion and validation against required bindings
- Rich DataRequirement planning, dataset generation, and resource generation
- Robust dataset provisioning lifecycle (seed, isolation, teardown)
- OpenAPI-based API testing
- Advanced test planning, execution (true parallelism), and assertion engines

What exists today implements the first executable slices of this architecture.
See
[`docs/architecture.md`](docs/architecture.md) for the layering and design
decisions.

Currently implemented:

- Local FHIR package `.tgz` loading via CLI
- Recursive package dependency resolution via CLI
- Local-first dependency resolution with remote package download fallback
- Download cache for resolved dependency archives
- Floating dependency version resolution such as `current` -> concrete package version
- Package manifest parsing (`name`, `version`, `dependencies`)
- Normalisation of core FHIR resources into internal model types:
  `StructureDefinition`, `ValueSet`, `CodeSystem`, `CapabilityStatement`,
  and `SearchParameter`
- Constraint model (`internal/fhir/constraint`): normalised, `Kind`-typed
  contractual rules derived from the registry, with stable identifiers
  (`cardinality`, `datatype`, `terminology`, `invariant`, `reference`,
  `fixed`, `pattern`, `search`, `interaction`)
- Constraint-driven coverage derivation across the cardinality, datatype,
  terminology, invariant, and reference domains (plus required-slice
  structure obligations), each requirement traceable to its source
  constraint
- AST generation for positive, negative, and boundary cases: negative
  variants mutate a valid payload against exactly one constraint and assert
  rejection; boundary helpers emit edge values for string length, numeric,
  and cardinality ranges
- End-to-end requirement traceability: each generated assertion and each
  executed test result carries the source constraint (id, profile, path,
  domain, variant, expected outcome), and coverage reports list both the
  covered and uncovered requirements
- Registry-backed `DataRequirement` -> `Dataset` generator and a
  dependency-ordered server `Provisioner`, separating data generation from
  test execution
- Generic dependency DAG planning for resource execution ordering
- AST generation from coverage requirements with setup/capture scaffolding
- Minimal assertion parser/evaluator (`status in [..]`)
- Minimal runner that executes AST requests/assertions and emits JSON test reports

## Layout

```
cmd/momus/          CLI entry point (minimal; --help / --version)
internal/fhir/      FHIR model, constraint model, package loading, registry,
                    terminology, resource generation, planner, provisioning
internal/test/      test AST, assertions, generation (positive/negative/boundary), runner
internal/openapi/   OpenAPI testing support (future)
docs/               architecture documentation
pkg/                reserved public API (intentionally empty)
```

## Build & test

Requires Go 1.26+.

```sh
go build ./...
go vet ./...
go test ./...
```

Run the CLI entry point:

```sh
go run ./cmd/momus --help
```

## CLI

Momus uses a Cobra-based CLI.

Show top-level help:

```sh
go run ./cmd/momus --help
```

Load a local FHIR package archive (`.tgz`):

```sh
go run ./cmd/momus package load package.tgz
```

Example output:

```text
Loaded package au.gov.digitalhealth.fhir.hcpd@26.0.0 with 7 dependencies and 55 resources
```

Resolve a package and its transitive dependencies:

```sh
go run ./cmd/momus package resolve package.tgz
```

Example output:

```text
Resolved 10 packages from . using download dir .momus/packages with 9067 total resources
- hl7.fhir.r4.core@4.0.1 (deps=0, resources=4441)
- hl7.terminology.r4@7.3.0 (deps=2, resources=3470)
- hl7.fhir.uv.extensions.r4@5.3.0 (deps=2, resources=884)
- hl7.fhir.au.base@6.0.0 (deps=3, resources=151)
- hl7.fhir.uv.smart-app-launch@2.0.0 (deps=1, resources=0)
- hl7.fhir.uv.ipa@1.1.0 (deps=4, resources=14)
- hl7.fhir.au.core@2.0.0 (deps=6, resources=30)
- hl7.fhir.au.pd@2.0.1 (deps=1, resources=14)
- hl7.fhir.uv.bulkdata@3.0.0 (deps=3, resources=8)
- au.gov.digitalhealth.fhir.hcpd@26.0.0 (deps=7, resources=55)
```

Resolver behaviour:

- Searches the local dependency directory first
- Downloads missing package archives from FHIR package registries
- Stores downloaded archives in `.momus/packages` by default
- Resolves floating dependency versions such as `current` using registry metadata
- Uses `root-wins` as the default conflict policy for version conflicts

Override dependency search and download directories:

```sh
go run ./cmd/momus package resolve package.tgz --deps-dir . --download-dir ./.momus/packages
```

Enable debug logging:

```sh
go run ./cmd/momus --debug package resolve package.tgz
```

Advanced resolver option:

```sh
go run ./cmd/momus package resolve package.tgz --conflict-policy strict
```

`strict` is primarily useful for auditing package graph consistency. The default
`root-wins` mode is the normal operational mode.

Derive an MVP coverage plan from resolved package constraints:

```sh
go run ./cmd/momus coverage derive package.tgz
```

Derivation is constraint-driven and now spans multiple domains: cardinality,
datatype, terminology, invariant, reference, and required-slice structure.
Each obligation is traceable to its source constraint via `constraintId`.

Derivation defaults are intentionally practical:

- Includes required elements (`min > 0`)
- Includes `mustSupport` elements
- Excludes optional non-`mustSupport` elements unless `--include-optional` is set
- Excludes low-value infrastructure paths (such as `meta`, `text`, `language`) unless `--include-low-value-paths` is set

Example output:

```text
Derived 3 coverage requirements from 10 resolved packages
- cardinality: 3
Resource types: 1, variants: 3
Pruned elements:
- optional-filtered: 42
```

The command also prints the JSON coverage plan to stdout by default.

Write the plan to a file:

```sh
go run ./cmd/momus coverage derive package.tgz --output ./coverage-plan.json
```

Scope derivation to specific contracts:

```sh
go run ./cmd/momus coverage derive package.tgz \
  --include-resource Observation \
  --include-profile-url http://hl7.org/fhir/StructureDefinition/Observation \
  --exclude-path-prefix Observation.meta
```

Derive the constraint model (the normalised set of contractual rules
underlying coverage):

```sh
go run ./cmd/momus coverage constraints package.tgz
```

This walks every StructureDefinition, SearchParameter, and
CapabilityStatement in the resolved graph and emits cardinality, datatype,
terminology, invariant, reference, fixed, pattern, search, and interaction
constraints as JSON, each with a stable identifier.

Write the constraints to a file:

```sh
go run ./cmd/momus coverage constraints package.tgz --output ./constraints.json
```

Generate a test AST from derived coverage requirements:

```sh
go run ./cmd/momus coverage ast package.tgz --output ./test-ast.json
```

Optionally include a base URL for request nodes:

```sh
go run ./cmd/momus coverage ast package.tgz \
  --base-url http://localhost:8080/fhir \
  --include-resource Observation \
  --output ./test-ast.json
```

Execute generated tests with the minimal runner and output a JSON result report:

```sh
go run ./cmd/momus coverage run package.tgz \
  --base-url http://localhost:8080/fhir \
  --output ./test-results.json
```

Example summary output:

```text
Executed 42 cases: 40 passed, 2 failed
Test report written to ./test-results.json
```

### Coverage Pipeline (MVP)

The current executable MVP pipeline is:

1. Resolve package graph (`package resolve`)
2. Derive scoped coverage requirements (`coverage derive`)
3. Build resource dependency DAG (internal planner)
4. Generate dependency-aware AST (`coverage ast`)
5. Execute AST and emit result report (`coverage run`)

The constraint model (`coverage constraints`) is the normalised intermediate
representation between the registry and coverage: contractual rules are
derived once and can later anchor per-domain coverage obligations and test
traceability.

Dependency-chain behavior in the current AST/runner implementation:

- Resources are ordered by DAG levels (topological order)
- Setup nodes create seed resources before requirement cases
- Capture nodes extract resource IDs from setup responses
- Later request payloads can reference captured IDs via templates such as
  `Patient/{{Patient.id}}`

## License

See [LICENSE](LICENSE).
