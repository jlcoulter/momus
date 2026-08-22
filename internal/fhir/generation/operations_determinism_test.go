package generation

import (
	"testing"

	"github.com/jlcoulter/momus/internal/core/coverage"
	coregen "github.com/jlcoulter/momus/internal/core/generation"
	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
)

// TestStateDeleteNonexistentPortableAssertion verifies that a DELETE on a
// nonexistent resource accepts the portable status set (200/204/404) rather than
// strictly 404, so the test is not flaky across conformant servers.
func TestStateDeleteNonexistentPortableAssertion(t *testing.T) {
	req := coverage.CoverageRequirement{
		ID: "st-2", ResourceType: "Organization", Domain: coverage.CoverageDomainState,
		Variant: coverage.CoverageVariantStateDeleteNonexistent,
	}
	_, _, expression, expected := operationSpec(req, coregen.BuildOptions{})
	if expression != "status in [200,204,404]" {
		t.Fatalf("delete-nonexistent expression = %q, want portable status in [200,204,404]", expression)
	}
	if expected != "accept" {
		t.Fatalf("delete-nonexistent expected = %q, want accept (portable outcome)", expected)
	}
}

// TestPatchPropertyDerivesFromProfile verifies that a PATCH test derives the
// patched property from the resource's profile rather than hard-coding `status`,
// so resources without a `status` element are not rejected.
func TestPatchPropertyDerivesFromProfile(t *testing.T) {
	r := registry.New()
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/StructureDefinition/patient",
		Type: "Patient",
		Elements: []model.ElementDefinition{
			{Path: "Patient", Min: 0, Max: "*"},
			{Path: "Patient.active", Min: 0, Max: "1", Types: []model.ElementType{{Code: "boolean"}}},
			{Path: "Patient.name", Min: 0, Max: "*", Types: []model.ElementType{{Code: "HumanName"}}},
		},
	})
	req := coverage.CoverageRequirement{
		ID: "patch-1", ResourceType: "Patient", ProfileURL: "http://example.org/StructureDefinition/patient",
		Domain: coverage.CoverageDomainOperation, Variant: coverage.CoverageVariantOperationPatch,
	}
	prop, value := patchProperty(req, coregen.BuildOptions{Builder: NewBuilder(r, false)})
	if prop == "status" {
		t.Fatalf("patchProperty returned hard-coded %q, want a property derived from the profile", prop)
	}
	if prop == "" {
		t.Fatalf("patchProperty returned empty property")
	}
	if value == nil {
		t.Fatalf("patchProperty returned nil value for %q", prop)
	}
}
