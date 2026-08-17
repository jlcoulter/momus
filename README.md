# Momus

Momus is a testing framework/tool for API and FHIR conformance testing.

This repository is a fresh, production-oriented implementation of Momus. The
current state is an **architecture scaffold**: the package layout, domain
models, and interfaces are established, but none of the core functionality
is implemented yet.

## Status

The following capabilities are planned but **not yet implemented**:

- FHIR Implementation Guide (IG) loading and package/dependency resolution
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

## License

See [LICENSE](LICENSE).
