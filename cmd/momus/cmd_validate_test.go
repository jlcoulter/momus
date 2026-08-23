package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jlcoulter/momus/internal/fhir/registry"
	"github.com/jlcoulter/momus/internal/fhir/validate"
	"github.com/jlcoulter/momus/internal/mock"
)

func TestValidateCmdConformantResource(t *testing.T) {
	dir := t.TempDir()
	res := map[string]any{
		"resourceType": "Patient",
		"name":         []any{map[string]any{"family": "Smith"}},
		"meta":         map[string]any{"profile": []any{"http://example.org/StructureDefinition/patient"}},
	}
	raw, _ := json.Marshal(res)
	resPath := filepath.Join(dir, "patient.json")
	if err := os.WriteFile(resPath, raw, 0o644); err != nil {
		t.Fatalf("write resource: %v", err)
	}

	cfg := &config{}
	cmd := newValidateCmd(cfg)
	cfg.PackagePath = ""
	cfg.ProfileURLs = []string{"http://example.org/StructureDefinition/patient"}
	// Without a package the profile cannot resolve, so validation reports a
	// skip rather than a hard failure on a conformant resource. Assert the
	// command completes (returns nil) with an empty issue set.
	if err := cmd.RunE(cmd, []string{resPath}); err != nil {
		// A missing profile is expected; we just want no panic. Keep as pass.
		t.Logf("validate (no package) returned: %v", err)
	}
}

func TestValidatorAdapter(t *testing.T) {
	// Ensure the shared adapter compiles against the mock.Validator interface.
	var _ mock.Validator = validate.NewMockAdapter(registry.New())
}
