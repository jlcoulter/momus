package golden

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/jlcoulter/momus/internal/core/ast"
	"github.com/jlcoulter/momus/internal/core/coverage"
	"github.com/jlcoulter/momus/internal/core/runner"
	fhircoverage "github.com/jlcoulter/momus/internal/fhir/coverage"
	fhirgeneration "github.com/jlcoulter/momus/internal/fhir/generation"
	"github.com/jlcoulter/momus/internal/fhir/validate"
	"github.com/jlcoulter/momus/internal/mock"
)

// placeholderBase is the fixed base URL baked into generated plans before the
// mock's dynamic address is known. The snapshot is taken against it; execution
// rewrites it to the live mock URL.
const placeholderBase = "http://momus-golden.invalid/fhir"

// Result reports a single fixture's golden run.
type Result struct {
	Name       string
	Generated  int
	Passed     int
	Failed     int
	FailedReqs []string
}

// Run generates, snapshots, and executes a single fixture's plan against the
// semantic mock, asserting every case passes. It returns an error on failure.
func Run(ctx context.Context, name string, fx *Fixture) (*Result, error) {
	reg, err := BuildRegistry(fx)
	if err != nil {
		return nil, fmt.Errorf("golden %s: build registry: %w", name, err)
	}

	// 1. Derive the coverage plan. When the fixture declares a server-mode
	// CapabilityStatement, its search-parameter codes scope the search
	// obligations (including the universal _parameters parameter when
	// declared) exactly as the CLI scopes to a live server's capability
	// statement.
	capabilitySearchCodes := fhircoverage.SearchCodesFromCapabilityStatementUnion(fx.CapabilityStatements)
	coveragePlan, err := fhircoverage.DerivePlan(reg, coverage.DeriveOptions{
		CapabilitySearchCodes: capabilitySearchCodes,
	})
	if err != nil {
		return nil, fmt.Errorf("golden %s: derive: %w", name, err)
	}

	// 2. Generate the test AST with the placeholder base URL.
	opts := fhirgeneration.BuildOptions{
		BaseURL:  placeholderBase,
		Registry: reg,
	}
	plan, err := fhirgeneration.GenerateFromCoveragePlan(coveragePlan, opts)
	if err != nil {
		return nil, fmt.Errorf("golden %s: generate: %w", name, err)
	}

	// 3. Snapshot the plan (byte-identical check).
	if err := snapshot(name, plan); err != nil {
		return nil, err
	}

	// 4. Build the semantic mock (validator wired) and rewrite the plan's base
	// URL to the mock's live address.
	validator := validate.New(reg)
	ms := mock.New(200, "",
		mock.WithPlanAware(),
		mock.WithValidator(mockValidatorAdapter{inner: validator}),
		mock.WithLogger(false),
		mock.WithBasePath("/fhir"),
	)
	addr, err := ms.Start()
	if err != nil {
		return nil, fmt.Errorf("golden %s: start mock: %w", name, err)
	}
	defer ms.Close()

	// 5. Validate the fixture's sample resources (T21): every sample is a
	// conformant example for its claimed profile, so the oracle must report no
	// issues. A failing sample proves the validator and the fixture disagree.
	if err := validateSamples(ctx, validator, fx); err != nil {
		return nil, fmt.Errorf("golden %s: %w", name, err)
	}

	mockBase := "http://" + addr + "/fhir"
	rewriteBase(plan.Root, placeholderBase, mockBase)
	ms.SetPlan(plan.Root)

	// 6. Provision the seed dataset (search seeds + referenced resources) so
	// searches return matches and CRUD/operations have data. Without this, a
	// search-multiple-results case has nothing to match and fails vacuously.
	if err := provisionSeed(ctx, opts, coveragePlan, mockBase); err != nil {
		return nil, fmt.Errorf("golden %s: provision seed: %w", name, err)
	}
	// 7. Execute and assert 100% pass.
	report, err := runner.Execute(ctx, plan.Root, runner.ExecuteOptions{BaseURL: mockBase})
	if err != nil {
		return nil, fmt.Errorf("golden %s: execute: %w", name, err)
	}

	res := &Result{Name: name, Generated: len(report.Cases), Passed: report.Passed, Failed: report.Failed}
	for _, c := range report.Cases {
		if !c.Passed {
			res.FailedReqs = append(res.FailedReqs, c.RequirementID)
			fmt.Fprintf(os.Stderr, "  [%s] %s: %s\n", c.RequirementID, c.Description, c.Error)
		}
	}
	if report.Failed > 0 {
		return res, fmt.Errorf("golden %s: %d/%d cases failed: %s", name, report.Failed, report.Total, strings.Join(res.FailedReqs, ", "))
	}
	return res, nil
}

// provisionSeed generates and uploads the seed dataset for a coverage plan to
// the mock server, using the same BuildSetupDataset the coverage provision
// command uses.
func provisionSeed(ctx context.Context, options fhirgeneration.BuildOptions, coveragePlan *coverage.CoveragePlan, mockBase string) error {
	options.BaseURL = mockBase
	ds, err := fhirgeneration.BuildSetupDataset(coveragePlan, options)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	for localID, inst := range ds.Resources {
		body, err := json.Marshal(inst.Resource)
		if err != nil {
			return fmt.Errorf("marshal seed %s: %w", localID, err)
		}
		url := mockBase + "/" + inst.ResourceType + "/" + localID
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/fhir+json")
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		resp.Body.Close()
	}
	return nil
}

// packageRoot is the absolute path to the directory containing this package
// (internal/fhir/golden). The repo testdata/golden dir is two levels up.
var packageRoot = func() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Dir(file)
}()

// goldenDir is the canonical repo testdata/golden directory, resolved from the
// package location so it is independent of the process working directory.
var goldenDir = filepath.Join(packageRoot, "..", "..", "..", "testdata", "golden")

// snapshot marshals a plan deterministically and compares it to the committed
// .plan.json file, writing it if absent and failing on a diff.
func snapshot(name string, plan *ast.Plan) error {
	if err := os.MkdirAll(goldenDir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(goldenDir, name+".plan.json")
	got := marshalDeterministic(plan)

	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "golden %s: wrote snapshot %s\n", name, path)
		return nil
	}
	want, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if string(got) != string(want) {
		return fmt.Errorf("golden %s: plan snapshot mismatch (%s); a generation change altered the plan", name, path)
	}
	return nil
}

// marshalDeterministic marshals a plan with sorted keys for a stable snapshot.
func marshalDeterministic(plan *ast.Plan) []byte {
	b, _ := json.MarshalIndent(plan, "", "  ")
	// json.MarshalIndent on structs is already deterministic for this model.
	return append(b, '\n')
}

// rewriteBase replaces every occurrence of oldBase with newBase in request URLs
// across the AST.
func rewriteBase(node ast.Node, old, new string) {
	switch n := node.(type) {
	case *ast.Sequence:
		for _, s := range n.Steps {
			rewriteBase(s, old, new)
		}
	case *ast.Parallel:
		for _, s := range n.Steps {
			rewriteBase(s, old, new)
		}
	case *ast.Request:
		n.URL = strings.ReplaceAll(n.URL, old, new)
	case *ast.Capture:
	}
}

// mockValidatorAdapter adapts a *validate.ProfileValidator to the mock.Validator
// interface (whose Issue type is local to the mock package).
type mockValidatorAdapter struct {
	inner *validate.ProfileValidator
}

func (a mockValidatorAdapter) Validate(ctx context.Context, profileURL string, resource map[string]any) ([]mock.Issue, error) {
	issues, err := a.inner.Validate(ctx, profileURL, resource)
	if err != nil {
		return nil, err
	}
	out := make([]mock.Issue, 0, len(issues))
	for _, iss := range issues {
		out = append(out, mock.Issue{Path: iss.Path, Kind: iss.Kind, Message: iss.Message, Value: iss.Value})
	}
	return out, nil
}

// sortedKeys returns a sorted slice of a map's keys (helper for any future
// deterministic rendering).
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
