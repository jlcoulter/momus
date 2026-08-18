# Momus

Momus is a testing framework/tool for API and FHIR conformance testing.

This repository is a fresh, production-oriented implementation of Momus. The
current state is an **architecture scaffold**: the package layout, domain
models, and interfaces are established, but none of the core functionality
is implemented yet.

## Status

The following capabilities are planned but **not yet implemented**:

- A normalised FHIR Registry of profiles, value sets, code systems, and
  search parameters
- Profile resolution and inheritance, element trees, cardinality, slicing,
  and extensions
- Terminology expansion
- DataRequirement planning, dataset generation, and resource generation
- Dataset provisioning to a FHIR server
- OpenAPI-based API testing
- Test planning, execution (sequential and parallel), and assertions

What exists today is the architecture that these will grow into. See
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

## Layout

```
cmd/momus/          CLI entry point (minimal; --help / --version)
internal/fhir/      FHIR model, package loading, registry, terminology,
                    resource generation, planner, provisioning
internal/test/      test AST, assertions, runner
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

## License

See [LICENSE](LICENSE).
