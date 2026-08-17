# Momus Architecture

Momus is a testing framework/tool for API and FHIR conformance testing. This
document describes the production architecture that the implementation is
growing into. 

## Dependency direction

Dependencies flow downward. A layer may depend on the layers below it, never
on the layers above it.

```
FHIR Package (external .tgz / registry)
        |
        v
internal/fhir/package        load + resolve FHIR packages
        |
        v
internal/fhir/model          normalised FHIR domain model
        |
        v
internal/fhir/registry       canonical URL + resource-type index
        |
   +----+------------+
   |                 |
   v                 v
terminology      internal/fhir/resource   resource Generator
   |                 |
   +--------+--------+
            |
            v
internal/fhir/planner        TestPlan (AST)
            |
            v
internal/fhir/provisioning   Dataset -> FHIR server
            |
            v
internal/test/runner         executes the AST
            |
            v
internal/test/assertions     evaluates results
```

Key rules:

- The registry never depends on the generator.
- The generator never depends on the test runner.
- The test runner never sees raw FHIR package files.

## The FHIR Registry

The registry is a single, immutable index of FHIR knowledge, keyed by
canonical URL and resource type. It is the one place where Structure
Definitions, ValueSets, CodeSystems, SearchParameters, and Capability
Statements live.

Why it exists: every downstream layer (terminology, resource generation,
planning) needs to look up FHIR knowledge. Without a registry, each consumer
would parse and cache its own copy of StructureDefinitions, leading to
inconsistent and duplicated logic. The registry centralises indexing so that
`Generator` and `Planner` depend on a clean lookup surface rather than on
raw FHIR JSON.

The registry is built once (from loaded packages, via `RegistryBuilder`) and
treated as effectively immutable afterwards. It is safe for concurrent use.
This keeps concurrent generation and test planning free of locking headaches
and lets consumers cache resolved profiles safely.

## Element tree and path index

A resolved profile is represented twice:

1. **A tree** (`ElementNode` with nested `Children`) for structural
   traversal and resource generation — walking `Observation -> component ->
   code -> coding -> code` as nested nodes.
2. **A path index** (`ResolvedProfile.Elements`) keyed by canonical FHIR
   path for fast lookup — `Observation.component.code.coding.code`.

Both are intentional. Generation needs the structure; path-indexed lookup is
how constraints, search parameters, and assertions address elements. Keeping
canonical FHIR paths first-class is a core design decision. Sliced elements
are preserved on their parent's `Slices` map, ready for slicing resolution
later.

## DataRequirement vs Dataset

`DataRequirement` is **declarative**: it describes *what* data a test needs
(`Observation` with `component.code.coding.code == "1234-5"`), not the data
itself. It is the bridge between test planning and resource generation.

`Dataset` is **generated state**: concrete `ResourceInstance`s and the
references between them. A generator consumes a `DataRequirement` and
produces a `Dataset`. Separating them keeps requirements free of generated
JSON and keeps generation a pure function of a declarative input.

## Dataset vs TestPlan

`Dataset` represents *data*; `TestPlan` represents *execution*. A dataset is
independent of how it will be used by tests — the same dataset could feed
multiple plans. Keeping them separate means the planner can reason about
data requirements and execution steps independently, and the dataset can be
provisioned before any test runs.

## Sequence vs Parallel

`Sequence` and `Parallel` are explicit AST nodes because they express a
semantic guarantee:

- **Sequence**: later steps depend on earlier ones (e.g. create a resource,
  then fetch it, then assert).
- **Parallel**: steps are independent and may execute concurrently.

Making them first-class AST nodes lets the planner express intent and lets
the runner make safe concurrency decisions later.

## Package layout

- `cmd/momus` — CLI entry point.
- `internal/fhir/model` — normalised FHIR domain model (no I/O, no execution).
- `internal/fhir/package` — package loading and registry building.
- `internal/fhir/registry` — immutable FHIR knowledge index.
- `internal/fhir/terminology` — value set / code system expansion (future).
- `internal/fhir/resource` — resource generator interface.
- `internal/fhir/planner` — planner interface and `TestPlan`.
- `internal/fhir/provisioning` — provisioner interface.
- `internal/test/ast` — test plan AST.
- `internal/test/runner` — test execution (future).
- `internal/test/assertions` — assertion interface.
- `internal/openapi` — OpenAPI testing support (future).

## Design notes

- `internal/fhir/package` is named to match the architecture, but its Go
  package name is `fhirpackage` because `package` is a reserved word.
- `RegistryBuilder` lives in the `package` layer because it must import both
  the package types and the registry; putting it in `registry` would create
  an import cycle.
- Domain models (`model`, `ast`) carry no I/O or execution logic. Interfaces
  (`PackageLoader`, `Generator`, `Planner`, `Provisioner`, `Runner`,
  `Assertion`) are defined now, but their implementations are intentionally
  deferred.
