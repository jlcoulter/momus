package generation

import (
	"testing"

	fhir "github.com/jlcoulter/fhir-registry"
	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
)

// cyclicSeedRegistry builds a registry where Endpoint.managingOrganization is
// required (min 1) and Organization.endpoint is optional (min 0), mirroring the
// resolved AU profiles. The cycle must therefore be broken on the optional
// Organization.endpoint side.
func cyclicSeedRegistry(t *testing.T) *registry.Registry {
	t.Helper()
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{
		URL: "http://example.org/StructureDefinition/organization", Type: "Organization", Kind: "resource",
		Elements: []model.ElementDefinition{
			{Path: "Organization", Min: 0, Max: 1},
			{Path: "Organization.endpoint", Min: 0, Max: fhir.MaxUnbounded, Types: []model.ElementType{{Code: "Reference"}}},
		},
	})
	reg.AddStructureDefinition(&model.StructureDefinition{
		URL: "http://example.org/StructureDefinition/endpoint", Type: "Endpoint", Kind: "resource",
		Elements: []model.ElementDefinition{
			{Path: "Endpoint", Min: 0, Max: 1},
			{Path: "Endpoint.managingOrganization", Min: 1, Max: 1, Types: []model.ElementType{{Code: "Reference"}}},
		},
	})
	return reg
}

func cyclicSeedDataset() *model.Dataset {
	return &model.Dataset{
		Resources: map[string]*model.ResourceInstance{
			"momus-setup-organization": {
				LocalID:      "momus-setup-organization",
				ResourceType: "Organization",
				Resource: map[string]any{
					"resourceType": "Organization",
					"id":           "momus-setup-organization",
					"endpoint":     []any{map[string]any{"reference": "Endpoint/momus-setup-endpoint"}},
				},
			},
			"momus-setup-endpoint": {
				LocalID:      "momus-setup-endpoint",
				ResourceType: "Endpoint",
				Resource: map[string]any{
					"resourceType":         "Endpoint",
					"id":                   "momus-setup-endpoint",
					"managingOrganization": map[string]any{"reference": "Organization/momus-setup-organization"},
				},
			},
		},
	}
}

// TestBreakSetupReferenceCyclesDropsOptionalEdge verifies that the cycle is
// broken on the optional side (Organization.endpoint) and the required
// Endpoint.managingOrganization reference is preserved.
func TestBreakSetupReferenceCyclesDropsOptionalEdge(t *testing.T) {
	ds := cyclicSeedDataset()
	breakSetupReferenceCycles(ds, cyclicSeedRegistry(t))

	org := ds.Resources["momus-setup-organization"].Resource
	if _, ok := org["endpoint"]; ok {
		t.Fatalf("expected optional Organization.endpoint to be removed, still present: %v", org["endpoint"])
	}
	ep := ds.Resources["momus-setup-endpoint"].Resource
	if _, ok := ep["managingOrganization"]; !ok {
		t.Fatalf("expected required Endpoint.managingOrganization to be retained")
	}
}

// TestBreakSetupReferenceCyclesLeavesAcyclicUntouched verifies the pass does not
// alter a dataset whose references are already acyclic.
func TestBreakSetupReferenceCyclesLeavesAcyclicUntouched(t *testing.T) {
	ds := &model.Dataset{
		Resources: map[string]*model.ResourceInstance{
			"momus-setup-endpoint": {
				LocalID:      "momus-setup-endpoint",
				ResourceType: "Endpoint",
				Resource: map[string]any{
					"resourceType":         "Endpoint",
					"id":                   "momus-setup-endpoint",
					"managingOrganization": map[string]any{"reference": "Organization/momus-setup-organization"},
				},
			},
			"momus-setup-organization": {
				LocalID:      "momus-setup-organization",
				ResourceType: "Organization",
				Resource: map[string]any{
					"resourceType": "Organization",
					"id":           "momus-setup-organization",
				},
			},
		},
	}

	breakSetupReferenceCycles(ds, cyclicSeedRegistry(t))

	ep := ds.Resources["momus-setup-endpoint"].Resource
	if _, ok := ep["managingOrganization"]; !ok {
		t.Fatalf("acyclic reference should be retained")
	}
	org := ds.Resources["momus-setup-organization"].Resource
	if _, ok := org["endpoint"]; ok {
		t.Fatalf("organization without a cycle should be untouched")
	}
}
