package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jlcoulter/momus/internal/core/ast"
)

func TestKarateCmdExportsFeatureFiles(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.json")
	if err := os.WriteFile(planPath, []byte(encodeTestPlanFixture(t)), 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}

	outDir := filepath.Join(dir, "out")
	cfg := &config{}
	cmd := newKarateCmd(cfg)
	cfg.KarateOutDir = outDir
	if err := cmd.RunE(cmd, []string{planPath}); err != nil {
		t.Fatalf("karate failed: %v", err)
	}

	featurePath := filepath.Join(outDir, "Patient.feature")
	content, err := os.ReadFile(featurePath)
	if err != nil {
		t.Fatalf("read feature: %v", err)
	}
	text := string(content)
	for _, want := range []string{
		"Feature: Patient conformance",
		"@requirement:search|Patient|name|search-valid",
		"Scenario: return results for a valid search",
		"Given url baseUrl",
		"param name = 'momus-search'",
		"Then assert responseStatus in [200, 201]",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("feature missing %q:\n%s", want, text)
		}
	}
}

func TestKarateCmdGeneratesConfig(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.json")
	if err := os.WriteFile(planPath, []byte(encodeTestPlanFixture(t)), 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}

	outDir := filepath.Join(dir, "out")
	cfg := &config{}
	cmd := newKarateCmd(cfg)
	cfg.KarateOutDir = outDir
	cfg.GenerateKarateCfg = true
	if err := cmd.RunE(cmd, []string{planPath}); err != nil {
		t.Fatalf("karate failed: %v", err)
	}

	cfgPath := filepath.Join(outDir, "karate-config.js")
	cfgContent, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Errorf("expected karate-config.js: %v", err)
	}
	for _, want := range []string{
		"momus.auth.bearerToken",
		"momus.auth.basicUsername",
		"momus.auth.basicPassword",
		"karate.configure('headers'",
	} {
		if !strings.Contains(string(cfgContent), want) {
			t.Errorf("karate-config.js missing %q", want)
		}
	}
}

func TestKarateCmdMissingFile(t *testing.T) {
	cfg := &config{}
	cmd := newKarateCmd(cfg)
	if err := cmd.RunE(cmd, []string{"/nonexistent/plan.json"}); err == nil {
		t.Fatal("expected error for missing plan, got nil")
	}
}

// encodeTestPlanFixture builds a minimal EncodePlan-format plan JSON fixture.
func encodeTestPlanFixture(t *testing.T) string {
	t.Helper()
	plan := &ast.Plan{
		Version: "v1",
		Root: &ast.Sequence{Steps: []ast.Node{
			&ast.Sequence{Steps: []ast.Node{
				&ast.Sequence{Steps: []ast.Node{
					&ast.Request{Method: "GET", URL: "http://host/fhir/Patient?name=momus-search", Headers: map[string]string{}},
					&ast.Assert{
						Description:   "return results for a valid search",
						Expression:    "status in [200,201]",
						RequirementID: "search|Patient|name|search-valid",
						Trace: &ast.Trace{
							ResourceType: "Patient",
							Domain:       "search",
							Variant:      "search-valid",
						},
					},
				}},
			}},
		}},
	}
	payload, err := ast.EncodePlan(plan)
	if err != nil {
		t.Fatalf("EncodePlan: %v", err)
	}
	return string(mustJSON(t, payload))
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return out
}
