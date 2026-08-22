# Momus Core Extraction — Implementation Plan

> **For Hermes:** Execute this plan phase-by-phase. Verify `go build ./... && go test ./...` is green after EVERY phase before moving on.

**Goal:** Introduce a domain-agnostic `internal/core` (the "momus core") into the Go project, make the test-plan AST the single artifact that drives provisioning and execution (embed the seed dataset in the AST), and split the generation layer so generic scaffolding lives in core while FHIR payload synthesis lives on the FHIR side — mirroring the Rust project's narrow-core-wide-composition.

**Architecture:** `internal/core` holds the generic engine (AST, assertions, runner, tracing, constraint model, coverage model/evaluator/planner/report, generation scaffolding + `PayloadBuilder` interface). `internal/fhir/*` and `internal/openapi` become converters/adapters that depend on core. The test-plan AST (`ast.Plan`) gains a `Dataset` field so provisioning and execution are driven by the plan alone.

**Tech Stack:** Go 1.26, stdlib only (no new deps).

---

## Current state (verified)

Dependency graph (prod code):

```
cmd/momus → fhir/{bulk,constraint,model,package,provisioning,registry}, mock, openapi,
            test/{ast,coverage,generation,runner}, tracing
fhir/bulk        → fhir/{model,registry}
fhir/constraint  → fhir/{model,registry}          (constraint.go is generic; derive.go is FHIR-coupled)
fhir/package     → fhir/{model,registry}
fhir/provisioning→ fhir/model, tracing
fhir/registry    → fhir/model
mock             → test/ast
openapi          → fhir/constraint, test/ast
test/coverage    → fhir/{constraint,model,registry}, tracing   (model/evaluator/planner/report/html generic; derive/capability_scope FHIR-coupled)
test/generation  → fhir/{model,registry}, test/{ast,coverage} (interaction/operations generic; positive/negative/search/search_seed/dependencies FHIR-coupled)
test/runner      → test/{assertions,ast}, tracing              (generic)
test/assertions  → (generic)
test/ast         → (generic)
tracing          → (generic)
```

Key facts:
- `internal/test/ast`, `assertions`, `runner`, `tracing` are already generic (no FHIR imports).
- `internal/fhir/constraint/constraint.go` is generic (types only); `derive.go` imports model+registry.
- `internal/test/coverage/{model,evaluator,planner,report,html}.go` are generic; `derive.go` + `capability_scope.go` are FHIR-coupled.
- `internal/test/generation/{interaction,operations}.go` are generic; `{positive,negative,search,search_seed,dependencies}.go` are FHIR-coupled.
- The on-disk plan file (`cmd/momus/helpers.go:testPlanFile`) already carries `Dataset` + `Root` as siblings. We move the dataset INTO the AST `Plan` struct.
- `internal/mock` reads the plan file to derive reject routes (uses `ast.DecodeNode`).

---

## Target layout

```
internal/core/
  ast/          (moved from internal/test/ast)      + add Dataset field to Plan
  assertions/   (moved from internal/test/assertions)
  runner/       (moved from internal/test/runner)
  tracing/      (moved from internal/tracing)
  constraint/   (moved from internal/fhir/constraint/constraint.go — generic types only)
  coverage/     (moved from internal/test/coverage/{model,evaluator,planner,report,html}.go)
  generation/   (moved from internal/test/generation/{interaction,operations}.go + generic scaffolding
                 from positive.go + PayloadBuilder interface + BuildOptions)

internal/fhir/
  model/        (unchanged)
  registry/     (unchanged)
  package/      (unchanged)
  provisioning/ (unchanged)
  bulk/         (unchanged)
  constraintderive/  (moved from internal/fhir/constraint/derive.go — FHIR→constraint derivation)
  coverage/     (moved from internal/test/coverage/{derive,capability_scope}.go — FHIR→coverage derivation)
  generation/   (moved from internal/test/generation/{positive,negative,search,search_seed,dependencies}.go
                 + BuildSetupDataset + PayloadBuilder impl)

internal/openapi/  (unchanged location; imports now point at core/constraint + core/ast)
internal/mock/     (unchanged location; imports now point at core/ast)
cmd/momus/         (import rewrites)
```

---

## Phase 1 — Create core packages by moving generic code (pure moves + import rewrites)

Goal: `internal/core/{ast,assertions,runner,tracing,constraint,coverage}` exist and compile. No behavior change. Tests stay green.

### Task 1.1: Move `internal/test/ast` → `internal/core/ast`
- `git mv internal/test/ast internal/core/ast`
- Update package doc comment: "Package ast defines the abstract syntax tree for test plans." → "Package ast defines the core test-plan AST."
- Rewrite all importers: `internal/mock/*`, `internal/openapi/*`, `internal/test/generation/*`, `internal/test/runner/*`, `cmd/momus/*` → `github.com/jlcoulter/momus/internal/core/ast`.
- Verify: `go build ./... && go test ./internal/core/ast/...`

### Task 1.2: Move `internal/test/assertions` → `internal/core/assertions`
- `git mv internal/test/assertions internal/core/assertions`
- Rewrite importers: `internal/test/runner/execute.go`, `internal/test/runner/triage_test.go`.
- Verify: `go build ./... && go test ./internal/core/assertions/...`

### Task 1.3: Move `internal/test/runner` → `internal/core/runner`
- `git mv internal/test/runner internal/core/runner`
- Rewrite importers: `cmd/momus/{cmd_api,helpers,main_test,pipeline}.go`.
- Verify: `go build ./... && go test ./internal/core/runner/...`

### Task 1.4: Move `internal/tracing` → `internal/core/tracing`
- `git mv internal/tracing internal/core/tracing`
- Rewrite importers: `cmd/momus/helpers.go`, `internal/fhir/provisioning/provisioner_impl.go`, `internal/test/coverage/capability_scope.go`, `internal/test/runner/execute.go`.
- Verify: `go build ./... && go test ./internal/core/tracing/...`

### Task 1.5: Move constraint model types → `internal/core/constraint`
- `git mv internal/fhir/constraint/constraint.go internal/core/constraint/constraint.go`
- `git mv internal/fhir/constraint/constraint_test.go internal/core/constraint/constraint_test.go` (if exists)
- Update package doc: "Package constraint defines the constraint model..." (drop "from the FHIR registry" phrasing).
- Rewrite importers of `internal/fhir/constraint` that only use the types: `internal/openapi/constraints.go`, `internal/openapi/load_test.go`, `internal/test/coverage/derive.go`, `internal/test/coverage/derive_test.go`, `cmd/momus/{cmd_api,cmd_constraints}.go`.
- NOTE: `internal/fhir/constraint/derive.go` stays in fhir for now (Phase 4 moves it). It will import `internal/core/constraint` for the types.
- Verify: `go build ./... && go test ./internal/core/constraint/...`

### Task 1.6: Move generic coverage files → `internal/core/coverage`
- `git mv internal/test/coverage/{model,evaluator,planner,report,html}.go internal/core/coverage/`
- Move their test files: `model_test.go`, `evaluator_test.go`, `planner_test.go`, `html_test.go` (and any generic ones).
- Rewrite importers: `internal/test/generation/*`, `cmd/momus/*`, `internal/test/coverage/derive.go` (stays in fhir, Phase 4).
- Verify: `go build ./... && go test ./internal/core/coverage/...`

### Task 1.7: Interim — keep `internal/test/coverage` and `internal/test/generation` compiling
- After Phase 1, `internal/test/coverage` still holds `derive.go` + `capability_scope.go` (FHIR-coupled) and `internal/test/generation` still holds all generation. These now import `internal/core/*`.
- Verify: `go build ./... && go test ./...` — FULL suite green.

**Phase 1 exit gate:** `go build ./... && go vet ./... && go test ./...` all green. Commit: `refactor: extract generic engine into internal/core`.

---

## Phase 2 — Embed the seed dataset in the AST ("AST drives everything")

Goal: `ast.Plan` carries its seed dataset, so provisioning and execution are driven by the plan alone.

### Task 2.1: Add `Dataset` to `ast.Plan`
- In `internal/core/ast/ast.go`, add a `Dataset` field. To keep `ast` free of FHIR types, define a minimal generic dataset in core:
  ```go
  // Dataset is the seed data a test plan provisions ahead of execution.
  // It is intentionally generic (opaque resource bodies) so the AST does not
  // depend on any domain model.
  type Dataset struct {
      Resources     map[string]*ResourceInstance `json:"resources"`
      Relationships []Reference                  `json:"relationships,omitempty"`
  }
  type ResourceInstance struct {
      LocalID      string         `json:"localId"`
      ResourceType string         `json:"resourceType"`
      Profile      string         `json:"profile,omitempty"`
      Resource     map[string]any `json:"resource"`
      ServerID     string         `json:"serverId,omitempty"`
      Version      string         `json:"version,omitempty"`
  }
  type Reference struct {
      SourceID string `json:"sourceId"`
      Path     string `json:"path"`
      TargetID string `json:"targetId"`
  }
  ```
  Add `Dataset *Dataset \`json:"dataset,omitempty"\`` to `Plan`.
- Update `EncodePlan`/`DecodeNode` to serialize/deserialize the dataset (add a `DecodePlan` that reads `version`, `root`, `dataset`).

### Task 2.2: Make `internal/fhir/model.Dataset` the FHIR adapter
- Keep `internal/fhir/model.Dataset` as-is (bulk + provisioning use it). Add a conversion helper in `internal/fhir/generation` (Phase 3) or a small adapter: `func ToCoreDataset(ds *model.Dataset) *coreast.Dataset`.
- `internal/fhir/provisioning` continues to consume `*model.Dataset` (FHIR-side). The cmd layer converts core→fhir dataset before provisioning, OR provisioning is updated to accept the core dataset. **Decision:** keep provisioning on `*model.Dataset`; cmd converts.

### Task 2.3: Update plan-file encode/decode in `cmd/momus/helpers.go`
- `testPlanFile` currently has `Dataset` + `Root` siblings. Change to embed the dataset inside the AST plan: `encodeTestPlan` sets `astPlan.Dataset = coreDataset` and marshals via `ast.EncodePlan`; `decodeTestPlan` returns `(*ast.Plan, error)` (dataset now inside plan).
- Update `cmd_run.go`, `cmd_ast.go`, `cmd_plan.go`, `cmd_test_fhir.go` call sites: `setupDataset` now comes from `astPlan.Dataset` (converted to `*model.Dataset` for provisioning).

### Task 2.4: Update `buildTestPlan` (pipeline_fhir.go)
- `buildTestPlan` currently returns `(*ast.Plan, *model.Dataset, error)`. Change to set `astPlan.Dataset = coreDataset` and return `(*ast.Plan, error)`.
- `provisionDataset` reads `astPlan.Dataset` (converted to `*model.Dataset`).

**Phase 2 exit gate:** `go build ./... && go test ./...` green. `coverage ast` → `coverage provision` → `coverage run` round-trips the dataset through the plan file. Commit: `feat(core): embed seed dataset in test-plan AST`.

---

## Phase 3 — Split the generation layer (1A full split)

Goal: generic scaffolding in `internal/core/generation`; FHIR payload synthesis in `internal/fhir/generation`.

### Task 3.1: Define `PayloadBuilder` interface in `internal/core/generation`
```go
// PayloadBuilder is implemented by a domain adapter (e.g. FHIR) to synthesize
// request payloads and search values for generated test cases. The generic
// generation framework calls these; it never sees domain types.
type PayloadBuilder interface {
    // BuildBody returns a test payload for a requirement and whether a negative
    // mutation was applied (false when the target element is absent).
    BuildBody(req coverage.CoverageRequirement, id string, profileURLs []string,
        primaryProfileURL string, deps []string, exhaustive bool) (map[string]any, bool)
    // BuildSetupBody returns a seed resource body.
    BuildSetupBody(resourceType, id string, profileURLs []string,
        primaryProfileURL string, deps []string, exhaustive bool) map[string]any
    // SearchValue returns the value to use for a search parameter in a query.
    SearchValue(req coverage.CoverageRequirement, code string,
        variant coverage.CoverageVariant) string
    // SearchParamType returns the domain type of a search parameter.
    SearchParamType(req coverage.CoverageRequirement, code string) string
}
```

### Task 3.2: Move generic scaffolding to `internal/core/generation`
- Move `internal/test/generation/{interaction,operations}.go` → `internal/core/generation/`.
- From `positive.go`, move the generic helpers to core/generation: `BuildOptions`, `GenerateFromCoveragePlan`, `RequirementCount`, `joinURL`, `joinInstanceURL`, `baseURLForMethod`, `buildResourceCases`, `buildSingleRequirementCase`, `buildRequirementAssert`, `firstProfileURL`, `orderedProfilesForResource`, `requirementResourceID`, `setupResourceID`, `sanitizeFHIRID`, `uniqueProfileURLs`, `max`, `cloneValue`, `lastPathSegment`, `ensureDistinctContent`, `requiresDistinctContent`, `effectiveStrength`, `selectInteractionCandidates`, `greedySetCover`, `buildCandidateCase`, `buildInteractionAssert`, `buildCRUDCase`, `buildOperationCase`, `operationAssert`, `operationSpec`, `operationUpdateBody`, `patchProperty`, `crudResourceID`, `isStandaloneOperation`.
- `GenerateFromCoveragePlan` and `buildResourceCases` now take a `PayloadBuilder` instead of `*registry.Registry` + `BuildOptions.Registry`.
- `BuildOptions` drops `Registry *registry.Registry`; add `Builder PayloadBuilder`.

### Task 3.3: Create `internal/fhir/generation` with FHIR synthesis
- Move `internal/test/generation/{positive,negative,search,search_seed,dependencies}.go` → `internal/fhir/generation/`.
- Implement `PayloadBuilder` (a `fhirBuilder` struct wrapping `*registry.Registry` + `BuildOptions`-equivalent config).
- Keep `BuildSetupDataset` here (returns `*model.Dataset`), plus `ToCoreDataset` adapter.
- `buildDependencyPlan` stays here (it needs registry for reference targets).

### Task 3.4: Rewire `cmd/momus/pipeline_fhir.go` + `cmd_test_fhir.go`
- `buildTestPlan` constructs a `fhirBuilder`, calls `coregeneration.GenerateFromCoveragePlan(plan, opts)` to get the AST, calls `fhirgeneration.BuildSetupDataset` to get the dataset, sets `astPlan.Dataset`, returns the plan.

**Phase 3 exit gate:** `go build ./... && go test ./...` green. `internal/test/generation` no longer exists. Commit: `refactor(core): split generation into core scaffolding + fhir synthesis`.

---

## Phase 4 — Move FHIR→constraint and FHIR→coverage derivation to the FHIR side

### Task 4.1: Move `internal/fhir/constraint/derive.go` → `internal/fhir/constraintderive/`
- `git mv internal/fhir/constraint/derive.go internal/fhir/constraintderive/derive.go`
- Package name `constraintderive`. Imports `internal/core/constraint` for types.
- Update importers: `internal/test/coverage/derive.go` (→ fhir/coverage, Phase 4.2), `cmd/momus/cmd_constraints.go`.

### Task 4.2: Move `internal/test/coverage/{derive,capability_scope}.go` → `internal/fhir/coverage/`
- `git mv internal/test/coverage/{derive,capability_scope}.go internal/fhir/coverage/`
- Package name `coverage` (or `fhircoverage`). Imports `internal/core/coverage` for model/evaluator/planner + `internal/core/constraint` + `internal/fhir/constraintderive`.
- Update importers: `cmd/momus/*`, `internal/test/generation/*` (now `internal/fhir/generation/*`).

**Phase 4 exit gate:** `go build ./... && go test ./...` green. `internal/fhir/constraint` and `internal/test/coverage` no longer exist. Commit: `refactor(core): move fhir derivation to fhir side`.

---

## Phase 5 — Cleanup, docs, verification

### Task 5.1: Update docs
- `docs/architecture.md`: document `internal/core` as the domain-agnostic engine, `internal/fhir/*` + `internal/openapi` as adapters, and the AST-driven pipeline (dataset embedded in plan).
- `README.md` layout section: reflect new package tree.

### Task 5.2: Full verification
- `go build ./... && go vet ./... && go test ./...`
- `gofmt -l .` (must be empty)
- Run `coverage ast` + `coverage provision` + `coverage run` against `--mock` to confirm the end-to-end pipeline still works with the embedded dataset.
- Confirm no remaining imports of `internal/test/` or `internal/fhir/constraint` (old paths).

### Task 5.3: Commit
- `refactor(core): complete momus core extraction — AST-driven pipeline`

---

## Risks / tradeoffs

- **Largest risk is Phase 3** (generation split). The generic scaffolding and FHIR synthesis are deeply interleaved (interaction.go/operations.go call helpers in positive.go). Mitigation: move helpers in the same commit as the scaffolding, keep `PayloadBuilder` interface minimal, verify after each task.
- **Dataset embedding changes the plan-file JSON shape.** `coverage ast` output gains a `dataset` field inside the plan root. Old plan files without it still decode (dataset nil). `mock` reads the plan via `ast.DecodeNode` — must handle the new shape.
- **`internal/fhir/model.Dataset` vs `internal/core/ast.Dataset` duplication.** Kept intentionally: core stays domain-free; fhir keeps its typed dataset for bulk/provisioning. A small adapter converts between them.
- **No behavior change** to test generation logic — this is a structural refactor. The 1721-case / 24k-pairwise outputs must be byte-identical for the same input.

## Open questions
- None blocking. (Package naming for fhir-side coverage/constraintderive is a style choice; `fhircoverage`/`constraintderive` are the defaults.)
