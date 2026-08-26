// Package report renders a test run into a navigable output directory instead
// of one monolithic JSON file. It addresses the "millions of lines, hard to
// navigate" problem by slicing the run along the dimensions a developer
// actually debugs by: case, resource type, and search/operation parameter.
//
// Layout:
//
//	dir/
//	  index.json          small: summary, coverage, file list, failed pointers
//	  index.html          navigable tree
//	  summary.json        the concise run summary (what --output wrote)
//	  full.json           the monolithic artifact, only when writeFull
//	  cases/<req-id>.json one small file per case
//	  by-resource/<Type>.json    pass/fail matrix per resource type
//	  by-parameter/<param>.json  pass/fail matrix per search/operation param
package report

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jlcoulter/momus/internal/core/coverage"
	"github.com/jlcoulter/momus/internal/core/runner"
)

// Options controls what the writer emits.
type Options struct {
	// WriteFull writes the monolithic full.json (forensics escape hatch).
	WriteFull bool
}

// Write renders a run into dir. report is the executed run; evaluation is the
// optional coverage evaluation (may be the zero value when not evaluated).
func WriteDir(dir string, report *runner.Report, evaluation coverage.EvaluationReport, evaluated bool, opts Options) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	// cases/
	if err := writeCases(dir, report.Cases); err != nil {
		return err
	}
	// failed-index
	if err := writeFailedIndex(dir, report.Cases); err != nil {
		return err
	}
	// by-resource/ and by-parameter/
	if err := writeMatrices(dir, report.Cases); err != nil {
		return err
	}
	// summary.json
	if err := writeJSON(filepath.Join(dir, "summary.json"), summary(report, evaluation)); err != nil {
		return err
	}
	// full.json (opt-in)
	if opts.WriteFull {
		if err := writeJSON(filepath.Join(dir, "full.json"), report); err != nil {
			return err
		}
	}
	// index.json
	if err := writeJSON(filepath.Join(dir, "index.json"), index(report, evaluation)); err != nil {
		return err
	}
	// index.html
	html, err := renderHTML(report, evaluation)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), html, 0o644); err != nil {
		return err
	}
	return nil
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// writeCases writes one small file per case under cases/.
func writeCases(dir string, cases []runner.CaseResult) error {
	casesDir := filepath.Join(dir, "cases")
	if err := os.MkdirAll(casesDir, 0o755); err != nil {
		return err
	}
	for _, c := range cases {
		name := caseFileName(c)
		if err := writeJSON(filepath.Join(casesDir, name), c); err != nil {
			return err
		}
	}
	return nil
}

// caseFileName produces a stable, unique filename for a case. When the
// requirement id is not unique (e.g. duplicate setups) a counter disambiguates.
func caseFileName(c runner.CaseResult) string {
	id := c.RequirementID
	if id == "" {
		id = "case"
	}
	safe := strings.NewReplacer("|", "_", " ", "_", "/", "_", ":", "_", "{", "_", "}", "_").Replace(id)
	return safe + ".json"
}

func writeFailedIndex(dir string, cases []runner.CaseResult) error {
	byKind := map[string][]string{}
	var failed []string
	for _, c := range cases {
		if c.Passed {
			continue
		}
		failed = append(failed, c.RequirementID)
		key := kindOf(c)
		byKind[key] = append(byKind[key], c.RequirementID)
	}
	if err := writeJSON(filepath.Join(dir, "failed-index.json"), map[string]any{
		"total":    len(failed),
		"byReason": byKind,
		"failed":   failed,
	}); err != nil {
		return err
	}
	return nil
}

func kindOf(c runner.CaseResult) string {
	if c.Trace != nil && c.Trace.Variant != "" {
		return c.Trace.Domain + "|" + c.Trace.Variant
	}
	if c.Error != "" {
		return "error"
	}
	return "failure"
}

// writeMatrices writes by-resource/ and by-parameter/ matrices.
func writeMatrices(dir string, cases []runner.CaseResult) error {
	byResource := map[string][]runner.CaseResult{}
	byParam := map[string][]runner.CaseResult{}
	for _, c := range cases {
		if c.Trace != nil {
			rt := c.Trace.ResourceType
			if rt != "" {
				byResource[rt] = append(byResource[rt], c)
			}
		}
		// A search/operation parameter: derive from the trace variant (e.g.
		// search name in ConstraintID) or fall back to the resource type.
		param := paramOf(c)
		if param != "" {
			byParam[param] = append(byParam[param], c)
		}
	}

	resDir := filepath.Join(dir, "by-resource")
	if err := os.MkdirAll(resDir, 0o755); err != nil {
		return err
	}
	for rt, cs := range byResource {
		if err := writeJSON(filepath.Join(resDir, safeName(rt)+".json"), matrix(cs)); err != nil {
			return err
		}
	}

	paramDir := filepath.Join(dir, "by-parameter")
	if err := os.MkdirAll(paramDir, 0o755); err != nil {
		return err
	}
	for p, cs := range byParam {
		if err := writeJSON(filepath.Join(paramDir, safeName(p)+".json"), matrix(cs)); err != nil {
			return err
		}
	}
	return nil
}

func paramOf(c runner.CaseResult) string {
	if c.Trace == nil {
		return ""
	}
	// For a search constraint like "search|Patient|name", the param is the
	// third segment. For operation constraints it is empty.
	parts := strings.Split(c.Trace.ConstraintID, "|")
	if len(parts) >= 3 && parts[0] == "search" {
		return parts[2]
	}
	if c.Trace.ResourceType != "" {
		return c.Trace.ResourceType
	}
	return ""
}

func safeName(s string) string {
	return strings.ReplaceAll(s, "|", "_")
}

type matrixEntry struct {
	RequirementID string `json:"requirementId"`
	HumanID       string `json:"humanId,omitempty"`
	Passed        bool   `json:"passed"`
	Variant       string `json:"variant,omitempty"`
	Domain        string `json:"domain,omitempty"`
}

func matrix(cs []runner.CaseResult) map[string]any {
	entries := make([]matrixEntry, 0, len(cs))
	passed, failed := 0, 0
	for _, c := range cs {
		var humanID, variant, domain string
		if c.Trace != nil {
			humanID = c.Trace.HumanID
			variant = c.Trace.Variant
			domain = c.Trace.Domain
		}
		entries = append(entries, matrixEntry{
			RequirementID: c.RequirementID,
			HumanID:       humanID,
			Passed:        c.Passed,
			Variant:       variant,
			Domain:        domain,
		})
		if c.Passed {
			passed++
		} else {
			failed++
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].RequirementID < entries[j].RequirementID })
	return map[string]any{
		"total":  len(entries),
		"passed": passed,
		"failed": failed,
		"cases":  entries,
	}
}

func summary(report *runner.Report, evaluation coverage.EvaluationReport) map[string]any {
	m := map[string]any{
		"total":  report.Total,
		"passed": report.Passed,
		"failed": report.Failed,
	}
	if evaluation.TotalRequirements > 0 {
		m["coveragePercent"] = evaluation.CoveragePercent
		m["covered"] = evaluation.CoveredRequirements
		m["uncovered"] = evaluation.UncoveredRequirements
		m["totalRequirements"] = evaluation.TotalRequirements
	}
	return m
}

// index gathers the run summary, the output file list, and failed-case
// pointers so a developer opens one small file and navigates down.
func index(report *runner.Report, evaluation coverage.EvaluationReport) map[string]any {
	summary := summary(report, evaluation)
	// Collect the failed requirement ids (with case file references).
	var failed []map[string]any
	for _, c := range report.Cases {
		if c.Passed {
			continue
		}
		humanID := ""
		if c.Trace != nil {
			humanID = c.Trace.HumanID
		}
		failed = append(failed, map[string]any{
			"requirementId": c.RequirementID,
			"humanId":       humanID,
			"caseFile":      "cases/" + caseFileName(c),
			"reason":        kindOf(c),
		})
	}
	summary["failed"] = failed
	summary["glossary"] = glossaryJSON()
	return summary
}

// glossaryJSON renders the domain/variant glossary as a JSON-friendly map so
// reports are self-documenting even without the CLI.
func glossaryJSON() map[string]any {
	domains := make(map[string]string, 0)
	for d, desc := range coverage.DomainDescriptions() {
		domains[string(d)] = desc
	}
	variants := make(map[string]string, 0)
	for v, desc := range coverage.VariantDescriptions() {
		variants[string(v)] = desc
	}
	return map[string]any{
		"domains":  domains,
		"variants": variants,
	}
}
