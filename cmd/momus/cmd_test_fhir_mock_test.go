package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// buildMinimalFHIRPackage writes a minimal FHIR package archive (a Patient
// StructureDefinition with a required name element) to a temp file and returns
// its path. It is enough to drive the full "test fhir --mock" pipeline through
// the refactored generation path (core framework + FHIR PayloadBuilder).
func buildMinimalFHIRPackage(t *testing.T) string {
	t.Helper()
	files := map[string]any{
		"package/package.json": map[string]any{
			"name":    "example.fhir.pkg",
			"version": "1.0.0",
		},
		"package/StructureDefinition-patient.json": map[string]any{
			"resourceType":   "StructureDefinition",
			"url":            "http://example.org/StructureDefinition/patient",
			"version":        "1.0.0",
			"name":           "PatientProfile",
			"type":           "Patient",
			"baseDefinition": "http://hl7.org/fhir/StructureDefinition/Patient",
			"kind":           "resource",
			"derivation":     "constraint",
			"snapshot": map[string]any{
				"element": []map[string]any{
					{"id": "Patient", "path": "Patient", "min": 0, "max": "*"},
					{"id": "Patient.name", "path": "Patient.name", "min": 1, "max": "*", "type": []map[string]any{{"code": "HumanName"}}},
				},
			},
		},
	}

	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "package.tgz")
	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	defer f.Close()

	gzw := gzip.NewWriter(f)
	tw := tar.NewWriter(gzw)
	for name, content := range files {
		raw, err := json.Marshal(content)
		if err != nil {
			t.Fatalf("marshal %s: %v", name, err)
		}
		hdr := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(raw))}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("write header %s: %v", name, err)
		}
		if _, err := tw.Write(raw); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return archivePath
}

// TestTestFhirCmdMockEndToEnd drives the full "test fhir --mock" pipeline from a
// minimal synthetic package. It exercises the refactored generation path end to
// end: resolve -> derive -> buildTestPlan (core framework + FHIR PayloadBuilder)
// -> provision -> execute against the plan-aware mock -> report. It asserts the
// pipeline completes and produces a report with at least one passed case.
func TestTestFhirCmdMockEndToEnd(t *testing.T) {
	pkgPath := buildMinimalFHIRPackage(t)

	outPath := filepath.Join(t.TempDir(), "results.json")
	cfg := &config{}
	cmd := newTestFhirCmd(cfg)
	// Set mock/exhaustive AFTER newTestFhirCmd registers flags, because flag
	// registration resets the bound field to the flag's default (false).
	cfg.Mock = true
	cfg.Exhaustive = true
	cfg.OutputPath = outPath
	cmd.SetContext(context.Background())

	if err := cmd.RunE(cmd, []string{pkgPath}); err != nil {
		t.Fatalf("test fhir --mock pipeline failed: %v", err)
	}

	raw, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read results: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("results not valid JSON: %v", err)
	}
	summary, ok := payload["summary"].(map[string]any)
	if !ok {
		t.Fatalf("results missing summary: %v", payload)
	}
	passed, _ := summary["passedCases"].(float64)
	if passed < 1 {
		t.Fatalf("expected at least 1 passed case, got %v (summary=%v)", passed, summary)
	}
	if failed, _ := summary["failedCases"].(float64); failed != 0 {
		t.Fatalf("expected 0 failed cases, got %v", failed)
	}
}
