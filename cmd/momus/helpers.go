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

	testbulk "github.com/jlcoulter/momus/internal/fhir/bulk"
	"github.com/jlcoulter/momus/internal/fhir/model"
	fhirpackage "github.com/jlcoulter/momus/internal/fhir/package"
	testast "github.com/jlcoulter/momus/internal/test/ast"
	testcoverage "github.com/jlcoulter/momus/internal/test/coverage"
	testrunner "github.com/jlcoulter/momus/internal/test/runner"
	"github.com/spf13/cobra"
)

// resourceScopeForRun resolves the resource/profile scope for ast/run from the
// target server's CapabilityStatement, falling back to the caller-provided
// scope (or the loaded package) when the server is unreachable.
func resourceScopeForRun(cmd *cobra.Command, cfg *config) ([]string, []string, map[string][]string, error) {
	if !cfg.scopeToCapability {
		return cfg.includeResourceTypes, cfg.includeProfileURLs, nil, nil
	}
	metadataBaseURL := strings.TrimSpace(cfg.capabilityBaseURL)
	if metadataBaseURL == "" {
		metadataBaseURL = cfg.baseURL
	}
	capabilityStatement, err := testcoverage.FetchCapabilityStatement(cmd.Context(), metadataBaseURL, testcoverage.CapabilityFetchOptions{
		BearerToken:   cfg.apiBearerToken,
		BasicUsername: cfg.apiBasicUsername,
		BasicPassword: cfg.apiBasicPassword,
	})
	if err != nil {
		// When the target server is not reachable, fall back to the loaded
		// package as the source of truth rather than failing the run. Other
		// errors (a reachable server returning an auth or protocol error) are
		// surfaced so misconfiguration is not silently ignored.
		if isServerUnavailable(err) {
			fmt.Fprintf(os.Stderr, "WARNING: target server unreachable (%v); falling back to package definitions as source of truth\n", err)
			return cfg.includeResourceTypes, cfg.includeProfileURLs, nil, nil
		}
		return nil, nil, nil, err
	}
	capabilityTypes := testcoverage.ResourceTypesFromCapabilityStatement(capabilityStatement, true)
	capabilityProfiles := testcoverage.SupportedProfileURLsFromCapabilityStatement(capabilityStatement, true)
	capabilityProfilesByResource := testcoverage.SupportedProfileURLsByResourceFromCapabilityStatement(capabilityStatement, true)
	if len(capabilityTypes) == 0 {
		// Some CapabilityStatements omit per-resource create interactions.
		// Fall back to server-declared resource/profile scope instead of unscoped derivation.
		capabilityTypes = testcoverage.ResourceTypesFromCapabilityStatement(capabilityStatement, false)
		capabilityProfiles = testcoverage.SupportedProfileURLsFromCapabilityStatement(capabilityStatement, false)
		capabilityProfilesByResource = testcoverage.SupportedProfileURLsByResourceFromCapabilityStatement(capabilityStatement, false)
	}
	if len(capabilityProfiles) > 0 {
		return intersectCaseInsensitive(cfg.includeResourceTypes, capabilityTypes), intersectCaseInsensitive(cfg.includeProfileURLs, capabilityProfiles), capabilityProfilesByResource, nil
	}
	if len(capabilityTypes) == 0 {
		return cfg.includeResourceTypes, cfg.includeProfileURLs, capabilityProfilesByResource, nil
	}
	return intersectCaseInsensitive(cfg.includeResourceTypes, capabilityTypes), cfg.includeProfileURLs, capabilityProfilesByResource, nil
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

func intersectCaseInsensitive(requested, available []string) []string {
	if len(available) == 0 {
		return requested
	}
	if len(requested) == 0 {
		return available
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
	return intersected
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

// dependencyLevelCount returns the number of dependency levels in a plan root
// built by the planner (a Sequence of Parallel levels).
func dependencyLevelCount(root testast.Node) int {
	if seq, ok := root.(*testast.Sequence); ok {
		return len(seq.Steps)
	}
	return 0
}

// debugOutputDir is the default directory where per-stage JSON artifacts are
// written when --debug is enabled.
var debugOutputDir = ".momus/output"

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
		if err != nil || typ == "" {
			continue
		}
		out[typ] = count
	}
	return out
}
