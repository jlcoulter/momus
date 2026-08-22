package fhircoverage

import (
	"testing"

	"github.com/jlcoulter/momus/internal/core/coverage"
	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
)

func TestDerivePlanAddsCustomOperationObligation(t *testing.T) {
	r := registry.New()
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/StructureDefinition/org-profile",
		Type: "Organization",
		Elements: []model.ElementDefinition{
			{Path: "Organization", Min: 0, Max: "*"},
			{Path: "Organization.name", Min: 1, Max: "1"},
		},
	})
	r.AddCapabilityStatement(&model.CapabilityStatement{
		URL: "http://example.org/CapabilityStatement/server",
		Rest: []model.CapabilityStatementRest{{Mode: "server", Resource: []model.CapabilityStatementRestResource{
			{Type: "Organization", Operation: []model.CapabilityStatementOperation{{Name: "$everything", Definition: "http://example.org/OperationDefinition/Organization-everything"}}},
		}}},
	})

	plan, err := DerivePlan(r, coverage.DeriveOptions{})
	if err != nil {
		t.Fatalf("DerivePlan returned error: %v", err)
	}
	var found bool
	for _, req := range plan.Requirements {
		if req.Variant == coverage.CoverageVariantOperationCustom {
			found = true
			if req.OperationName != "everything" {
				t.Fatalf("operation name = %q, want everything", req.OperationName)
			}
		}
	}
	if !found {
		t.Fatal("expected operation-custom obligation")
	}
}

func TestDerivePlanAddsOperationAndStateObligations(t *testing.T) {
	r := registry.New()
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/StructureDefinition/org-profile",
		Type: "Organization",
		Elements: []model.ElementDefinition{
			{Path: "Organization", Min: 0, Max: "*"},
			{Path: "Organization.name", Min: 1, Max: "1"},
		},
	})

	plan, err := DerivePlan(r, coverage.DeriveOptions{})
	if err != nil {
		t.Fatalf("DerivePlan returned error: %v", err)
	}

	var ops, states int
	for _, req := range plan.Requirements {
		if req.Domain == coverage.CoverageDomainOperation {
			ops++
		}
		if req.Domain == coverage.CoverageDomainState {
			states++
		}
	}
	if ops != 5 {
		t.Fatalf("got %d operation obligations, want 5 (read/update/patch/delete/history)", ops)
	}
	if states != 3 {
		t.Fatalf("got %d state obligations, want 3 (crud-sequence/read/delete nonexistent)", states)
	}

	for _, req := range plan.Requirements {
		if req.Domain != coverage.CoverageDomainOperation && req.Domain != coverage.CoverageDomainState {
			continue
		}
		if req.ResourceType != "Organization" {
			t.Fatalf("operation/state obligation resource type = %q, want Organization", req.ResourceType)
		}
		if req.ProfileURL != "http://example.org/StructureDefinition/org-profile" {
			t.Fatalf("operation/state obligation profile = %q, want org-profile", req.ProfileURL)
		}
	}
}
