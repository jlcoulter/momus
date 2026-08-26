package fhircoverage

import (
	"testing"

	"github.com/jlcoulter/momus/internal/core/coverage"
	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
)

// filterTestRegistry builds a registry that derives obligations across several
// domains: element constraints (cardinality/datatype), search, operation, and
// state.
func filterTestRegistry() *registry.Registry {
	r := registry.New()
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/StructureDefinition/patient-profile",
		Type: "Patient",
		Elements: []model.ElementDefinition{
			{Path: "Patient", Min: 0, Max: "*"},
			{Path: "Patient.name", Min: 1, Max: "*", Types: []model.ElementType{{Code: "HumanName"}}},
		},
	})
	r.AddSearchParameter(&model.SearchParameter{
		Code:       "name",
		Base:       []string{"Patient"},
		Type:       "string",
		Expression: "Patient.name",
	})
	return r
}

func TestDerivePlanIncludeDomains(t *testing.T) {
	r := filterTestRegistry()
	plan, err := DerivePlan(r, coverage.DeriveOptions{
		IncludeDomains: []coverage.CoverageDomain{coverage.CoverageDomainSearch},
	})
	if err != nil {
		t.Fatalf("DerivePlan returned error: %v", err)
	}
	if len(plan.Requirements) == 0 {
		t.Fatal("expected at least one search requirement")
	}
	for _, req := range plan.Requirements {
		if req.Domain != coverage.CoverageDomainSearch {
			t.Fatalf("requirement %s has domain %q, want search", req.ID, req.Domain)
		}
	}
	// Summary must reflect the filtered set.
	if plan.Summary.TotalRequirements != len(plan.Requirements) {
		t.Fatalf("summary total %d != requirements %d", plan.Summary.TotalRequirements, len(plan.Requirements))
	}
	if plan.Summary.ByDomain[coverage.CoverageDomainSearch] != len(plan.Requirements) {
		t.Fatalf("summary search count %d != requirements %d", plan.Summary.ByDomain[coverage.CoverageDomainSearch], len(plan.Requirements))
	}
}

func TestDerivePlanExcludeVariants(t *testing.T) {
	r := filterTestRegistry()
	plan, err := DerivePlan(r, coverage.DeriveOptions{
		ExcludeVariants: []coverage.CoverageVariant{
			coverage.CoverageVariantOperationDelete,
			coverage.CoverageVariantStateCRUDSequence,
		},
	})
	if err != nil {
		t.Fatalf("DerivePlan returned error: %v", err)
	}
	for _, req := range plan.Requirements {
		if req.Variant == coverage.CoverageVariantOperationDelete {
			t.Fatalf("requirement %s has excluded variant operation-delete", req.ID)
		}
		if req.Variant == coverage.CoverageVariantStateCRUDSequence {
			t.Fatalf("requirement %s has excluded variant state-crud-sequence", req.ID)
		}
	}
	// The plan should still contain other operation/state obligations.
	if !hasVariant(plan, coverage.CoverageVariantOperationRead) {
		t.Fatal("expected operation-read to remain after variant exclusion")
	}
	if !hasVariant(plan, coverage.CoverageVariantStateReadNonexistent) {
		t.Fatal("expected state-read-nonexistent to remain after variant exclusion")
	}
}

func TestDerivePlanNoFilterIsNoop(t *testing.T) {
	r := filterTestRegistry()
	unfiltered, err := DerivePlan(r, coverage.DeriveOptions{})
	if err != nil {
		t.Fatalf("DerivePlan returned error: %v", err)
	}
	filtered, err := DerivePlan(r, coverage.DeriveOptions{
		IncludeDomains:  nil,
		ExcludeVariants: nil,
	})
	if err != nil {
		t.Fatalf("DerivePlan returned error: %v", err)
	}
	if len(unfiltered.Requirements) != len(filtered.Requirements) {
		t.Fatalf("no-op filter changed requirement count: %d -> %d", len(unfiltered.Requirements), len(filtered.Requirements))
	}
}
