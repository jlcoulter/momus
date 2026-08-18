# Momus Feature Roadmap

This document lists, in priority order, where the project should go next. It
follows the implementation staging defined in `docs/architecture.md` and the
central principle: completeness is defined as contractual coverage
obligations satisfied, not exhaustive value permutation.

Each item states what it is, why it comes at that position, and which
architectural boundary it touches.

## 1. Constraint model

**Status: implemented.** `internal/fhir/constraint` now defines the
constraint model: a flat, `Kind`-discriminated `Constraint` type with a
stable `ID` (via `constraint.ID`), and `constraint.Derive`, which normalises
StructureDefinition elements, SearchParameters, and CapabilityStatements into
cardinality, datatype, terminology, invariant, reference, fixed, pattern,
search, and interaction constraints. Derivation is deterministic,
de-duplicated by ID, and sorted. Exposed via `momus coverage constraints`.

This is the bridge between the registry and coverage. It is not yet wired
into coverage derivation; consuming constraints to emit per-domain coverage
obligations is feature 2.

Why first: every downstream feature (all coverage domains, generation
variants, reporting) keys off constraints. Without this layer each new domain
will re-implement its own ad-hoc rule extraction, and the architecture's
"constraint to coverage obligations" stage cannot exist.

## 2. Coverage derivation across all domains

**Status: implemented.** `DerivePlan` is now constraint-driven: it derives the
constraint model via `constraint.Derive` and maps each element-derived
constraint to obligations across the cardinality, datatype, terminology,
invariant, and reference domains, plus required-slice structure obligations.
Each requirement carries its source `ConstraintID`, giving end-to-end
traceability (requirement -> constraint -> profile/path/variant).

Generated domains and variants:

- Datatype: valid, invalid lexical, wrong JSON type, null.
- Terminology: valid, invalid, absent.
- Structure: required slice present (derived directly from slice elements;
  the constraint model does not yet model slicing). Unknown-element and
  slice-missing cases await mutation generation (feature 4).
- Invariant: satisfies, violates.
- Reference: valid target, wrong target, dangling.

To keep coverage honest, the AST generator only emits tests for the positive
variants (and the existing cardinality cases). Negative variants are derived
into the plan and reported as **uncovered** until feature 4 implements
negative mutation generation — a valid payload must never be marked as
satisfying a negative obligation.

Why second: this turns the coverage plan from a cardinality demo into the
actual contractual obligation set the architecture promises, and it exercises
the constraint model end to end before generation complexity is added.

## 3. Move generation into `internal/test/generation`

**Status: generation logic currently lives in
`internal/test/ast/from_coverage.go`.**

Relocate the profile-driven body synthesis (value generation, pattern and
binding merging, constraint-aware population) into
`internal/test/generation`, leaving `internal/test/ast` with only node
definitions and encoding. Split along the documented files:

- `positive.go` — valid-data generation (most of what exists today).
- `negative.go` — single-constraint mutations of otherwise valid data.
- `boundary.go` — boundary values for min/max, string length, numeric ranges.

Why third: negative and boundary generation are the next features and they
cannot land cleanly on top of a 1700-line file in the wrong package. The move
is mechanical and preserves the architecture boundary before it ossifies.

## 4. Negative and boundary generation

**Status: `missing-required` exists only as a body-level field deletion.**

Generate true negative variants per requirement: each negative test violates
exactly one known constraint against otherwise valid data. Boundary tests
exercise the edges called out in the architecture (cardinality `1..1` ->
zero/two; string length `3..10` -> 2/3/4/9/10/11).

Why fourth: this is the highest-value testing capability — negative coverage
is what actually distinguishes conformance testing from smoke testing — and
it depends on both the constraint model (2) and the generation home (3).

## 5. Coverage evaluator parity

**Status: evaluator exists but only consumes pass/fail per requirement ID.**

Extend evaluation so every derived requirement is traceable to at least one
executed test, and report precise gaps per domain (the architecture's
"uncovered obligations" output). This includes per-requirement trace
information (profile, path, constraint, variant, expectation) flowing through
the AST `Assert` node into the report.

Why fifth: with more domains and variants being generated, the evaluator must
prove the plan was satisfied rather than trusting the generator. It is the
defensibility half of the system.

## 6. Resource `Generator` and `Dataset` implementation

**Status: `internal/fhir/resource` and `internal/fhir/provisioning` are
interface-only stubs; body synthesis is inline in AST generation.**

Implement the `Generator` interface so `DataRequirement` values produce a
`Dataset` of concrete resources with references, and implement a
`Provisioner` that writes datasets to the target server ahead of execution.
This separates data from execution as the architecture requires, and lets one
dataset serve multiple positive/negative/boundary plans.

Why sixth: it only pays off once generation (4) is real, and it unlocks
multi-plan reuse and pre-provisioned fixtures for stateful workflows.

## 7. Interaction and pairwise coverage

**Status: not started.**

Add interaction strength to the coverage plan (strength 1 default, 2 =
pairwise). Generate candidate tests and select a near-minimal satisfying set
(greedy set-cover is acceptable per the architecture). Record which
interaction requirements each test satisfies.

Why seventh: pairwise generation multiplies test count; it should only
arrive after the evaluator (5) can measure it and generation (3, 4) is in its
proper home. The architecture explicitly forbids Cartesian-product defaults.

## 8. Planner and true `TestPlan`

**Status: `internal/fhir/planner` is an interface-only stub; AST generation
currently doubles as the planner.**

Implement `Planner` to turn data requirements and datasets into a `TestPlan`,
choosing `Sequence` vs `Parallel` nodes based on dependency analysis rather
than purely topological levels. This is also where `DataRequirement` and
`Dataset` become first-class in the pipeline (they are currently bypassed).

Why eighth: it depends on the `Generator`/`Provisioner` work (6) and is the
integration point for execution-flow features.

## 9. Runner concurrency

**Status: `Parallel` nodes execute sequentially ("minimal runner behavior").**

Execute `Parallel` subtrees concurrently (e.g. with `golang.org/x/sync/errgroup`),
with per-branch variable scoping so captures do not race, and aggregate the
report deterministically.

Why ninth: concurrency before the planner can express intent (8) is risk
without reward. The registry is already documented as concurrency-safe, so
this change is contained to the runner.

## 10. Operation, state, and search coverage

**Status: not started.** Only create-style PUT flows exist.

- Operations: read, update, patch, delete, history, and custom operations,
  scoped by CapabilityStatement interactions.
- State/transition coverage: CRUD sequences and negative transitions (GET
  nonexistent, DELETE already deleted, etc.).
- Search coverage: valid, no-results, multiple-results, invalid value,
  invalid and unsupported modifiers, and pairwise parameter combinations
  derived from indexed `SearchParameter`s.

Why tenth: these domains extend coverage beyond resource validation into
behavioural conformance. They need the planner (8) to express multi-step
workflows and the runner (9) to execute them efficiently.

## 11. Coverage reporting surfaces

**Status: JSON report plus console summary exist; domain percentages print
only on gaps.**

Produce the architecture's illustrative report: per-domain percentages
always visible, overall contractual coverage, and explicit uncovered
obligation lists, in both machine (JSON) and human (console/HTML) forms.

Why eleventh: reporting is only as meaningful as the domains behind it; by
this point the report has real content to show.

## 12. Assertion expression engine

**Status: only `status in [200,201]` is supported.**

Grow the assertion grammar (or adopt JSONPath/FHIRPath evaluation) so
assertions can inspect response bodies, headers, and captured variables —
required to express expected outcomes for negative tests ("validation failure
on path X") rather than just status codes.

Why twelfth: negative and state coverage (4, 10) already function with
status assertions; richer expressions raise fidelity but are not blocking.
Listing it late keeps it honest, though small increments may land earlier as
needed by 4 and 10.

## 13. OpenAPI contract support

**Status: `internal/openapi` is a placeholder.**

Load OpenAPI documents into the same architectural roles: operation
contracts, parameter definitions, request/response schemas, and supported
interactions feeding the constraint model. This proves the architecture's
"FHIR/API" duality rather than leaving it aspirational.

Why thirteenth: valuable, but it should follow the FHIR pipeline reaching
feature-complete form so the API side reuses proven layers instead of
co-evolving with them.

## Explicit non-goals (for now)

- Exhaustive value permutation or Cartesian-product generation by default.
- A general-purpose FHIR SDK — `internal/fhir/model` stays a minimal,
  normalised subset.
- Reporting any coverage percentage without a defined domain and proof that
  its obligations were satisfied.
