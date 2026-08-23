package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"

	testast "github.com/jlcoulter/momus/internal/core/ast"
	testcoverage "github.com/jlcoulter/momus/internal/core/coverage"
	testrunner "github.com/jlcoulter/momus/internal/core/runner"
	"github.com/jlcoulter/momus/internal/core/tracing"
	testbulk "github.com/jlcoulter/momus/internal/fhir/bulk"
	fhircoverage "github.com/jlcoulter/momus/internal/fhir/coverage"
	testgeneration "github.com/jlcoulter/momus/internal/fhir/generation"
	"github.com/jlcoulter/momus/internal/fhir/model"
	fhirpackage "github.com/jlcoulter/momus/internal/fhir/package"
	provisioning "github.com/jlcoulter/momus/internal/fhir/provisioning"
	"github.com/spf13/cobra"
)

// resourceScopeForRun resolves the resource/profile/search scope for ast/run
// from the target server's CapabilityStatement, falling back to the
// caller-provided scope (or the loaded package) when the server is unreachable.
//
// The CapabilityStatement always defines the test plan: resource types,
// profiles, and search parameters are scoped to what the server declares. When
// the server is unreachable or no CapabilityStatement is available, the
// caller-provided scope (or the loaded package) is used as the source of truth.
func resourceScopeForRun(cmd *cobra.Command, cfg *config, tracer *tracing.Tracer) ([]string, []string, map[string][]string, map[string][]string, error) {
	metadataBaseURL := strings.TrimSpace(cfg.CapabilityBaseURL)
	if metadataBaseURL == "" {
		metadataBaseURL = cfg.BaseURL
	}

	var capabilityStatement *model.CapabilityStatement
	var fetchErr error
	if cfg.MetadataFile != "" {
		loaded, loadErr := loadMetadataFile(cfg.MetadataFile)
		if loadErr != nil {
			return nil, nil, nil, nil, loadErr
		}
		capabilityStatement = loaded
	} else if metadataBaseURL != "" {
		capabilityStatement, fetchErr = fhircoverage.FetchCapabilityStatement(cmd.Context(), metadataBaseURL, fhircoverage.CapabilityFetchOptions{
			BearerToken:   cfg.ApiBearerToken,
			BasicUsername: cfg.ApiBasicUsername,
			BasicPassword: cfg.ApiBasicPassword,
			Tracer:        tracer,
		})
	}

	// When the server was reachable but returned an error, surface it.
	if fetchErr != nil && !isServerUnavailable(fetchErr) {
		return nil, nil, nil, nil, fetchErr
	}

	// When the server was unreachable, warn and fall back.
	if fetchErr != nil {
		fmt.Fprintf(os.Stderr, "WARNING: target server unreachable (%v); falling back to package definitions as source of truth\n", fetchErr)
	}

	// When no CapabilityStatement is available, return the caller-provided scope.
	if capabilityStatement == nil {
		return cfg.IncludeResourceTypes, cfg.IncludeProfileURLs, nil, nil, nil
	}

	// The CapabilityStatement always defines the test plan: extract resource
	// types, profiles, and search codes from what the server declares.
	capabilityTypes := fhircoverage.ResourceTypesFromCapabilityStatement(capabilityStatement, true)
	capabilityProfiles := fhircoverage.SupportedProfileURLsFromCapabilityStatement(capabilityStatement, true)
	capabilityProfilesByResource := fhircoverage.SupportedProfileURLsByResourceFromCapabilityStatement(capabilityStatement, true)
	capabilitySearchCodes := fhircoverage.SearchCodesFromCapabilityStatement(capabilityStatement)
	if len(capabilityTypes) == 0 {
		// Some CapabilityStatements omit per-resource create interactions.
		// Fall back to server-declared resource/profile scope instead of unscoped derivation.
		capabilityTypes = fhircoverage.ResourceTypesFromCapabilityStatement(capabilityStatement, false)
		capabilityProfiles = fhircoverage.SupportedProfileURLsFromCapabilityStatement(capabilityStatement, false)
		capabilityProfilesByResource = fhircoverage.SupportedProfileURLsByResourceFromCapabilityStatement(capabilityStatement, false)
	}
	if len(capabilityProfiles) > 0 {
		types, err := intersectCaseInsensitive(cfg.IncludeResourceTypes, capabilityTypes)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		profiles, err := intersectCaseInsensitive(cfg.IncludeProfileURLs, capabilityProfiles)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		return types, profiles, capabilityProfilesByResource, capabilitySearchCodes, nil
	}
	if len(capabilityTypes) == 0 {
		return cfg.IncludeResourceTypes, cfg.IncludeProfileURLs, capabilityProfilesByResource, capabilitySearchCodes, nil
	}
	types, err := intersectCaseInsensitive(cfg.IncludeResourceTypes, capabilityTypes)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return types, cfg.IncludeProfileURLs, capabilityProfilesByResource, capabilitySearchCodes, nil
}

// isServerUnavailable reports whether a fetch error means the target server
// could not be reached, as opposed to a reachable server returning an
// application/protocol error.
func isServerUnavailable(err error) bool {
	if err == nil {
		return false
	}
	for _, target := range []error{
		syscall.ECONNREFUSED,
		syscall.ECONNRESET,
		syscall.ENETUNREACH,
		syscall.EHOSTUNREACH,
	} {
		if errors.Is(err, target) {
			return true
		}
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return false
}

func intersectCaseInsensitive(requested, available []string) ([]string, error) {
	if len(available) == 0 {
		return requested, nil
	}
	if len(requested) == 0 {
		return available, nil
	}
	requestedSet := make(map[string]string, len(requested))
	for _, value := range requested {
		key := strings.ToLower(strings.TrimSpace(value))
		if key == "" {
			continue
		}
		requestedSet[key] = value
	}
	intersected := make([]string, 0)
	for _, value := range available {
		key := strings.ToLower(strings.TrimSpace(value))
		if original, ok := requestedSet[key]; ok {
			intersected = append(intersected, original)
		}
	}
	if len(intersected) == 0 {
		return nil, fmt.Errorf("none of the requested resource types are supported by the server")
	}
	return intersected, nil
}

func marshalCoverageRunOutput(report *testrunner.Report, evaluation testcoverage.EvaluationReport, includeCases bool) ([]byte, error) {
	requirementCases, setupCases := countRequirementAndSetupCases(report.Cases)

	// Readable summary first so the report can be scanned without wading through
	// the (potentially very large) per-case data.
	payload := map[string]any{
		"summary": map[string]any{
			"totalCases":            report.Total,
			"passedCases":           report.Passed,
			"failedCases":           report.Failed,
			"requirementCases":      requirementCases,
			"setupCases":            setupCases,
			"totalRequirements":     evaluation.TotalRequirements,
			"coveredRequirements":   evaluation.CoveredRequirements,
			"uncoveredRequirements": evaluation.UncoveredRequirements,
			"coveragePercent":       evaluation.CoveragePercent,
		},
		"coverage":    evaluation,
		"triage":      report.Triage,
		"diagnostics": report.Diagnostics,
	}

	// Failures, compact, so every failed test is addressable by requirement id
	// without dumping all passing cases (which coverage.covered already records).
	failures := make([]map[string]any, 0)
	for _, c := range report.Cases {
		if c.Passed {
			continue
		}
		failures = append(failures, compactFailure(c))
	}
	payload["failures"] = failures

	if includeCases {
		payload["cases"] = report.Cases
	}
	return json.MarshalIndent(payload, "", "  ")
}

// compactFailure renders a failed case with the fields needed to look it up and
// decide whether it is a broken test or a server issue, omitting debug bloat.
func compactFailure(c testrunner.CaseResult) map[string]any {
	m := map[string]any{
		"requirementId": c.RequirementID,
		"description":   c.Description,
		"expression":    c.Expression,
		"statusCode":    c.StatusCode,
	}
	if c.Error != "" {
		m["error"] = c.Error
	}
	if c.FailureFingerprint != "" {
		m["failureFingerprint"] = c.FailureFingerprint
	}
	if c.Trace != nil {
		m["expected"] = c.Trace.Expected
		m["domain"] = c.Trace.Domain
		m["variant"] = c.Trace.Variant
		m["resourceType"] = c.Trace.ResourceType
		m["elementPath"] = c.Trace.ElementPath
	}
	if c.Debug != nil {
		m["requestUrl"] = c.Debug.RequestURL
		m["requestBody"] = c.Debug.RequestBody
		m["responseBody"] = c.Debug.ResponseBody
	}
	return m
}

// htmlItems converts executed runner cases into HTML report items carrying
// pass/fail status and, for drill-down, their assertion and request/response
// detail. Setup scaffolding cases (no trace) are excluded.
func htmlItems(cases []testrunner.CaseResult) []testcoverage.HTMLItem {
	out := make([]testcoverage.HTMLItem, 0, len(cases))
	for _, c := range cases {
		if c.Trace == nil {
			continue
		}
		item := testcoverage.HTMLItem{
			ID:         c.RequirementID,
			Domain:     c.Trace.Domain,
			Resource:   c.Trace.ResourceType,
			Variant:    c.Trace.Variant,
			Expression: c.Expression,
			Passed:     c.Passed,
			StatusCode: c.StatusCode,
		}
		if c.Debug != nil {
			item.RequestMethod = c.Debug.RequestMethod
			item.RequestURL = c.Debug.RequestURL
			item.RequestBody = c.Debug.RequestBody
			item.ResponseBody = c.Debug.ResponseBody
		}
		out = append(out, item)
	}
	return out
}

func countRequirementAndSetupCases(cases []testrunner.CaseResult) (requirement, setup int) {
	seenReq := make(map[string]struct{})
	seenSetup := make(map[string]struct{})
	for _, c := range cases {
		if strings.HasPrefix(c.RequirementID, "setup:") {
			if _, ok := seenSetup[c.RequirementID]; ok {
				continue
			}
			seenSetup[c.RequirementID] = struct{}{}
			setup++
			continue
		}
		// Count each obligation once even when its execution expands to multiple
		// cases (e.g. a CRUD sequence).
		if _, ok := seenReq[c.RequirementID]; ok {
			continue
		}
		seenReq[c.RequirementID] = struct{}{}
		requirement++
	}
	return requirement, setup
}

func printCoverageGapSummary(evaluation testcoverage.EvaluationReport) {
	for _, domain := range sortedDomainKeys(evaluation.ByDomain) {
		summary := evaluation.ByDomain[domain]
		if summary.Uncovered <= 0 {
			continue
		}
		fmt.Printf("  Domain %s: %d uncovered\n", domain, summary.Uncovered)
	}
	for _, resourceType := range sortedStringKeys(evaluation.ByResourceType) {
		summary := evaluation.ByResourceType[resourceType]
		if summary.Uncovered <= 0 {
			continue
		}
		fmt.Printf("  Resource %s: %d uncovered\n", resourceType, summary.Uncovered)
	}
	for _, variant := range sortedVariantKeys(evaluation.ByVariant) {
		summary := evaluation.ByVariant[variant]
		if summary.Uncovered <= 0 {
			continue
		}
		fmt.Printf("  Variant %s: %d uncovered\n", variant, summary.Uncovered)
	}
}

func sortedDomainKeys(m map[testcoverage.CoverageDomain]testcoverage.DomainCoverageSummary) []testcoverage.CoverageDomain {
	keys := make([]testcoverage.CoverageDomain, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i] < keys[j]
	})
	return keys
}

func sortedVariantKeys(m map[testcoverage.CoverageVariant]testcoverage.DomainCoverageSummary) []testcoverage.CoverageVariant {
	keys := make([]testcoverage.CoverageVariant, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i] < keys[j]
	})
	return keys
}

func sortedStringKeys(m map[string]testcoverage.DomainCoverageSummary) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func writeOutputFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, data, 0o644)
}

// writeDebugOutput writes stage data to the debug output directory when debug
// debugOutputDir is the default directory where per-stage JSON artifacts are
// written when --debug is enabled.
var debugOutputDir = ".momus/output"

// newDebugTracer returns a request/response tracer writing to stderr when debug
// mode is enabled, or nil otherwise. It is used to surface every HTTP request a
// run makes (capability fetch, provisioning, and test execution).
func newDebugTracer(debug bool) *tracing.Tracer {
	if !debug {
		return nil
	}
	return tracing.New(os.Stderr)
}

// encodeTestPlan builds the on-disk test plan artifact from a generated AST.
// The seed dataset is embedded in the AST plan (astPlan.Dataset), so the plan
// is the single artifact that drives provisioning and execution.
func encodeTestPlan(astPlan *testast.Plan) ([]byte, error) {
	payload, err := testast.EncodePlan(astPlan)
	if err != nil {
		return nil, fmt.Errorf("encode test plan: %w", err)
	}
	return json.MarshalIndent(payload, "", "  ")
}

// decodeTestPlan reads a test plan artifact and returns its AST and seed dataset.
func decodeTestPlan(raw []byte) (*testast.Plan, *model.Dataset, error) {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, nil, fmt.Errorf("parse test plan: %w", err)
	}
	plan, err := testast.DecodePlan(payload)
	if err != nil {
		return nil, nil, fmt.Errorf("decode test plan: %w", err)
	}
	return plan, testgeneration.FromCoreDataset(plan.Dataset), nil
}

// datasetResourceKeys returns the "Type/id" keys of every resource in a dataset,
// used to mark seed resources as already-created so the runner's setup-reference
// validation passes for test cases that reference them.
func datasetResourceKeys(ds *model.Dataset) map[string]struct{} {
	keys := make(map[string]struct{})
	if ds == nil {
		return keys
	}
	for _, inst := range ds.Resources {
		if inst == nil || inst.ResourceType == "" || inst.LocalID == "" {
			continue
		}
		keys[inst.ResourceType+"/"+inst.LocalID] = struct{}{}
	}
	return keys
}

// writeDebugOutput writes stage data to the debug output directory when debug
// mode is enabled. It is a no-op otherwise. stage is the file name within the
// debug output directory, e.g. "coverage-plan.json".
func writeDebugOutput(debug bool, stage string, data []byte) error {
	if !debug {
		return nil
	}
	path := filepath.Join(debugOutputDir, stage)
	if err := writeOutputFile(path, data); err != nil {
		return fmt.Errorf("write debug output %s: %w", path, err)
	}
	fmt.Fprintf(os.Stderr, "debug: wrote %s\n", path)
	return nil
}

// writeDebugBulk writes NDJSON bulk instances to the debug output directory
// when debug mode is enabled. It is a no-op otherwise.
func writeDebugBulk(debug bool, instances []*model.ResourceInstance) error {
	if !debug {
		return nil
	}
	path := filepath.Join(debugOutputDir, "bulk.ndjson")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create debug bulk dir: %w", err)
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create debug bulk file %s: %w", path, err)
	}
	defer f.Close()
	w := testbulk.NewWriter(f)
	if err := w.WriteInstances(instances); err != nil {
		return fmt.Errorf("write debug bulk output %s: %w", path, err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("flush debug bulk output %s: %w", path, err)
	}
	fmt.Fprintf(os.Stderr, "debug: wrote %s\n", path)
	return nil
}

// writeDebugProvisionFailures writes provisioning failures (rejected payloads
// and full server responses) to the debug output directory when debug mode is
// enabled. It is a no-op otherwise.
func writeDebugProvisionFailures(debug bool, failures []provisioning.Failure) error {
	if !debug || len(failures) == 0 {
		return nil
	}
	out, err := json.MarshalIndent(failures, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal provision failures: %w", err)
	}
	return writeDebugOutput(debug, "provision-failures.json", append(out, '\n'))
}

// writeDebugGraph writes the resolved dependency graph to the debug output
// directory when debug mode is enabled. It is a no-op otherwise.
func writeDebugGraph(debug bool, graph *fhirpackage.ResolvedGraph) error {
	if !debug {
		return nil
	}
	out, err := json.MarshalIndent(graph, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal resolved graph: %w", err)
	}
	return writeDebugOutput(debug, "resolved-graph.json", append(out, '\n'))
}

// parsePerTypeCounts parses repeatable Type=Count overrides into a map. Values
// that cannot be parsed as counts are ignored.
func parsePerTypeCounts(entries []string) map[string]int {
	out := make(map[string]int)
	for _, entry := range entries {
		idx := strings.LastIndex(entry, "=")
		if idx <= 0 || idx == len(entry)-1 {
			continue
		}
		typ := strings.TrimSpace(entry[:idx])
		count, err := strconv.Atoi(strings.TrimSpace(entry[idx+1:]))
		if err != nil || typ == "" || count < 0 {
			continue
		}
		out[typ] = count
	}
	return out
}
