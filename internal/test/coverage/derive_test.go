package coverage

import (
	"testing"

	"github.com/jlcoulter/momus/internal/fhir/constraint"
	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
)

func TestDeriveMVPPlanPatientNameOneToMany(t *testing.T) {
	r := registry.New()
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/StructureDefinition/patient-profile",
		Type: "Patient",
		Elements: []model.ElementDefinition{
			{Path: "Patient", Min: 0, Max: "*"},
			{Path: "Patient.name", Min: 1, Max: "*"},
		},
	})
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/StructureDefinition/observation-profile",
		Type: "Observation",
		Elements: []model.ElementDefinition{
			{Path: "Observation", Min: 0, Max: "*"},
			{Path: "Observation.status", Min: 1, Max: "1"},
		},
	})

	plan, err := DeriveMVPPlan(r)
	if err != nil {
		t.Fatalf("DeriveMVPPlan returned error: %v", err)
	}
	if len(plan.Requirements) != 17 {
		t.Fatalf("got %d requirements, want 17", len(plan.Requirements))
	}

	if !hasVariant(plan, CoverageVariantValidMin) {
		t.Fatal("expected valid-min requirement")
	}
	if !hasVariant(plan, CoverageVariantMissingRequired) {
		t.Fatal("expected missing-required requirement")
	}
	if !hasVariant(plan, CoverageVariantMultipleValues) {
		t.Fatal("expected multiple-values requirement")
	}
}

func TestDeriveMVPPlanPatientNameOptionalSingle(t *testing.T) {
	r := registry.New()
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/StructureDefinition/patient-profile",
		Type: "Patient",
		Elements: []model.ElementDefinition{
			{Path: "Patient", Min: 0, Max: "*"},
			{Path: "Patient.name", Min: 0, Max: "1"},
		},
	})

	plan, err := DeriveMVPPlan(r)
	if err != nil {
		t.Fatalf("DeriveMVPPlan returned error: %v", err)
	}
	if len(plan.Requirements) != 6 {
		t.Fatalf("got %d requirements, want 6", len(plan.Requirements))
	}
	if plan.Summary.PrunedByReason[PruneReasonOptionalFiltered] == 0 {
		t.Fatal("expected optional-filtered prune reason")
	}
}

func TestDeriveMVPPlanDerivesWithoutPatientProfiles(t *testing.T) {
	r := registry.New()
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/StructureDefinition/observation-profile",
		Type: "Observation",
		Elements: []model.ElementDefinition{
			{Path: "Observation", Min: 0, Max: "*"},
			{Path: "Observation.status", Min: 1, Max: "1"},
		},
	})

	plan, err := DeriveMVPPlan(r)
	if err != nil {
		t.Fatalf("DeriveMVPPlan returned error: %v", err)
	}
	if len(plan.Requirements) != 8 {
		t.Fatalf("got %d requirements, want 8", len(plan.Requirements))
	}
}

func TestDerivePlanIncludeOptional(t *testing.T) {
	r := registry.New()
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/StructureDefinition/patient-profile",
		Type: "Patient",
		Elements: []model.ElementDefinition{
			{Path: "Patient", Min: 0, Max: "*"},
			{Path: "Patient.name", Min: 0, Max: "1"},
		},
	})

	plan, err := DerivePlan(r, DeriveOptions{IncludeOptional: true})
	if err != nil {
		t.Fatalf("DerivePlan returned error: %v", err)
	}
	if len(plan.Requirements) != 7 {
		t.Fatalf("got %d requirements, want 7", len(plan.Requirements))
	}
	if !hasVariant(plan, CoverageVariantValidMin) {
		t.Fatal("expected valid-min requirement")
	}
}

func TestDerivePlanScopeAndPruningOptions(t *testing.T) {
	r := registry.New()
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/StructureDefinition/patient-profile",
		Type: "Patient",
		Elements: []model.ElementDefinition{
			{Path: "Patient", Min: 0, Max: "*"},
			{Path: "Patient.identifier", Min: 0, Max: "*", MustSupport: true},
			{Path: "Patient.meta", Min: 0, Max: "1", MustSupport: true},
			{Path: "Patient.name", Min: 1, Max: "*"},
		},
	})
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/StructureDefinition/observation-profile",
		Type: "Observation",
		Elements: []model.ElementDefinition{
			{Path: "Observation", Min: 0, Max: "*"},
			{Path: "Observation.status", Min: 1, Max: "1", MustSupport: true},
		},
	})

	plan, err := DerivePlan(r, DeriveOptions{
		IncludeResourceTypes: []string{"Patient"},
		MustSupportOnly:      true,
		ExcludePathPrefixes:  []string{"Patient.meta"},
	})
	if err != nil {
		t.Fatalf("DerivePlan returned error: %v", err)
	}

	if len(plan.Requirements) != 8 {
		t.Fatalf("got %d requirements, want 8", len(plan.Requirements))
	}
	if plan.Summary.ByResourceType["Patient"] != 8 {
		t.Fatalf("got patient summary count %d, want 8", plan.Summary.ByResourceType["Patient"])
	}
	if plan.Summary.PrunedByReason[PruneReasonResourceFiltered] == 0 {
		t.Fatal("expected resource-filtered prune reason")
	}
	if plan.Summary.PrunedByReason[PruneReasonExcludedPathPrefix] == 0 {
		t.Fatal("expected excluded-path-prefix prune reason")
	}
}

func TestDerivePlanSkipsNonResourceStructureDefinitions(t *testing.T) {
	r := registry.New()
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/StructureDefinition/patient-profile",
		Type: "Patient",
		Kind: "resource",
		Elements: []model.ElementDefinition{
			{Path: "Patient", Min: 0, Max: "*"},
			{Path: "Patient.name", Min: 1, Max: "1"},
		},
	})
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://hl7.org/fhir/StructureDefinition/Extension",
		Type: "Extension",
		Kind: "complex-type",
		Elements: []model.ElementDefinition{
			{Path: "Extension", Min: 0, Max: "*"},
			{Path: "Extension.valueString", Min: 0, Max: "1"},
		},
	})

	plan, err := DerivePlan(r, DeriveOptions{IncludeOptional: true})
	if err != nil {
		t.Fatalf("DerivePlan returned error: %v", err)
	}

	if len(plan.Requirements) != 8 {
		t.Fatalf("got %d requirements, want 8", len(plan.Requirements))
	}
	for _, req := range plan.Requirements {
		if req.ResourceType != "Patient" {
			t.Fatalf("unexpected resource type %q in requirement %s", req.ResourceType, req.ID)
		}
	}
}

func TestDerivePlanIncludesDependencyTargetsFromElementMetadata(t *testing.T) {
	r := registry.New()
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/StructureDefinition/observation-profile",
		Type: "Observation",
		Elements: []model.ElementDefinition{
			{Path: "Observation", Min: 0, Max: "*"},
			{
				Path: "Observation.subject",
				Min:  1,
				Max:  "1",
				Types: []model.ElementType{
					{Code: "Reference", TargetProfile: []string{"http://hl7.org/fhir/StructureDefinition/Patient|4.0.1"}},
				},
			},
		},
	})

	plan, err := DerivePlan(r, DeriveOptions{IncludeOptional: true})
	if err != nil {
		t.Fatalf("DerivePlan returned error: %v", err)
	}

	var found bool
	for _, req := range plan.Requirements {
		if req.ElementPath != "Observation.subject" {
			continue
		}
		found = true
		if len(req.DependencyTargets) != 1 || req.DependencyTargets[0] != "Patient" {
			t.Fatalf("unexpected dependency targets for %s: %+v", req.ID, req.DependencyTargets)
		}
	}
	if !found {
		t.Fatal("expected requirement for Observation.subject")
	}
}

func TestDerivePlanPrunesOptionalReferenceDependencies(t *testing.T) {
	r := registry.New()
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/StructureDefinition/location-profile",
		Type: "Location",
		Elements: []model.ElementDefinition{
			{Path: "Location", Min: 0, Max: "*"},
			{Path: "Location.name", Min: 1, Max: "1", Types: []model.ElementType{{Code: "string"}}},
			{Path: "Location.managingOrganization", Min: 0, Max: "1", Types: []model.ElementType{{Code: "Reference", TargetProfile: []string{"http://hl7.org/fhir/StructureDefinition/Organization|4.0.1"}}}},
		},
	})
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:      "http://hl7.org/fhir/StructureDefinition/Organization",
		Type:     "Organization",
		Elements: []model.ElementDefinition{{Path: "Organization", Min: 0, Max: "*"}},
	})

	plan, err := DerivePlan(r, DeriveOptions{IncludeOptional: false})
	if err != nil {
		t.Fatalf("DerivePlan returned error: %v", err)
	}

	var found bool
	for _, req := range plan.Requirements {
		if req.ResourceType != "Location" || req.ElementPath != "Location.name" || req.Variant != CoverageVariantValidMin {
			continue
		}
		found = true
		if len(req.DependencyTargets) != 0 {
			t.Fatalf("unexpected dependency targets for %s: %+v", req.ID, req.DependencyTargets)
		}
	}
	if !found {
		t.Fatal("expected Location.name requirement")
	}
}

func TestDeriveMVPPlanFailsWithoutStructureDefinitions(t *testing.T) {
	r := registry.New()
	if _, err := DeriveMVPPlan(r); err == nil {
		t.Fatal("expected error when no structure definitions exist")
	}
}

func hasVariant(plan *CoveragePlan, variant CoverageVariant) bool {
	for _, req := range plan.Requirements {
		if req.Variant == variant {
			return true
		}
	}
	return false
}

func TestDerivePlanEmitMultiDomainObligations(t *testing.T) {
	r := registry.New()
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/StructureDefinition/observation",
		Type: "Observation",
		Elements: []model.ElementDefinition{
			{Path: "Observation", Min: 0, Max: "*"},
			{Path: "Observation.value", Min: 1, Max: "1", Types: []model.ElementType{{Code: "string"}}},
			{
				Path:  "Observation.status",
				Min:   1,
				Max:   "1",
				Types: []model.ElementType{{Code: "code"}},
				Binding: &model.Binding{
					Strength: "required",
					ValueSet: "http://hl7.org/fhir/ValueSet/observation-status",
				},
				Constraints: []model.ElementConstraint{{Key: "obs-1", Severity: "error", Expression: "status.exists()"}},
			},
			{Path: "Observation.subject", Min: 1, Max: "1", Types: []model.ElementType{{Code: "Reference", TargetProfile: []string{"http://hl7.org/fhir/StructureDefinition/Patient|4.0.1"}}}},
		},
	})

	plan, err := DerivePlan(r, DeriveOptions{})
	if err != nil {
		t.Fatalf("DerivePlan returned error: %v", err)
	}

	for _, v := range []CoverageVariant{
		CoverageVariantDatatypeValid,
		CoverageVariantDatatypeInvalidLexical,
		CoverageVariantDatatypeWrongJSONType,
		CoverageVariantDatatypeNull,
	} {
		if !hasVariant(plan, v) {
			t.Fatalf("expected datatype variant %s", v)
		}
	}
	for _, v := range []CoverageVariant{
		CoverageVariantTerminologyValid,
		CoverageVariantTerminologyInvalid,
		CoverageVariantTerminologyAbsent,
	} {
		if !hasVariant(plan, v) {
			t.Fatalf("expected terminology variant %s", v)
		}
	}
	if !hasVariant(plan, CoverageVariantInvariantSatisfies) || !hasVariant(plan, CoverageVariantInvariantViolates) {
		t.Fatal("expected invariant satisfies and violates variants")
	}
	for _, v := range []CoverageVariant{
		CoverageVariantReferenceValid,
		CoverageVariantReferenceWrongTarget,
		CoverageVariantReferenceDangling,
	} {
		if !hasVariant(plan, v) {
			t.Fatalf("expected reference variant %s", v)
		}
	}
	if plan.Summary.ByDomain[CoverageDomainDatatype] == 0 || plan.Summary.ByDomain[CoverageDomainTerminology] == 0 || plan.Summary.ByDomain[CoverageDomainInvariant] == 0 || plan.Summary.ByDomain[CoverageDomainReference] == 0 {
		t.Fatalf("missing domain summaries: %+v", plan.Summary.ByDomain)
	}
}

func TestDerivePlanAnchorsRequirementToConstraintID(t *testing.T) {
	r := registry.New()
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/StructureDefinition/observation",
		Type: "Observation",
		Elements: []model.ElementDefinition{
			{Path: "Observation", Min: 0, Max: "*"},
			{Path: "Observation.value", Min: 1, Max: "1", Types: []model.ElementType{{Code: "string"}}},
		},
	})

	plan, err := DerivePlan(r, DeriveOptions{})
	if err != nil {
		t.Fatalf("DerivePlan returned error: %v", err)
	}

	wantID := constraint.ID("http://example.org/StructureDefinition/observation", "Observation.value", string(constraint.KindDatatype), "string")
	for _, req := range plan.Requirements {
		if req.ElementPath != "Observation.value" || req.Domain != CoverageDomainDatatype {
			continue
		}
		if req.ConstraintID != wantID {
			t.Fatalf("requirement %s anchored to constraint %q, want %q", req.ID, req.ConstraintID, wantID)
		}
	}
}

func TestDerivePlanRequiredSliceStructureObligation(t *testing.T) {
	r := registry.New()
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/StructureDefinition/patient",
		Type: "Patient",
		Elements: []model.ElementDefinition{
			{Path: "Patient", Min: 0, Max: "*"},
			{Path: "Patient.contact", Min: 0, Max: "*"},
			{Path: "Patient.contact", Min: 1, Max: "1", SliceName: "primary"},
		},
	})

	plan, err := DerivePlan(r, DeriveOptions{})
	if err != nil {
		t.Fatalf("DerivePlan returned error: %v", err)
	}

	if !hasVariant(plan, CoverageVariantStructureSlicePresent) {
		t.Fatal("expected structure-slice-present obligation")
	}
	for _, req := range plan.Requirements {
		if req.Domain == CoverageDomainStructure {
			if req.ConstraintID != constraint.ID("http://example.org/StructureDefinition/patient", "Patient.contact", "structure") {
				t.Fatalf("unexpected structure constraint id %q", req.ConstraintID)
			}
		}
	}
}

// TestDerivePlanScopedToRootPackage verifies that when a registry is scoped to
// a root package, derivation produces obligations only for the root package's
// StructureDefinitions, while dependency (parent) definitions remain indexed
// for resolution but are not test subjects.
func TestDerivePlanScopedToRootPackage(t *testing.T) {
	r := registry.New()
	// Root package profile.
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/StructureDefinition/root-patient",
		Type: "Patient",
		Elements: []model.ElementDefinition{
			{Path: "Patient", Min: 0, Max: "*"},
			{Path: "Patient.name", Min: 1, Max: "*", Types: []model.ElementType{{Code: "HumanName"}}},
		},
	})
	// Parent/core package profile that must NOT be a test subject.
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://hl7.org/fhir/StructureDefinition/Observation",
		Type: "Observation",
		Elements: []model.ElementDefinition{
			{Path: "Observation", Min: 0, Max: "*"},
			{Path: "Observation.status", Min: 1, Max: "1", Types: []model.ElementType{{Code: "code"}}},
		},
	})

	r.SetScope([]string{"http://example.org/StructureDefinition/root-patient"})

	plan, err := DerivePlan(r, DeriveOptions{})
	if err != nil {
		t.Fatalf("DerivePlan returned error: %v", err)
	}

	if len(plan.Requirements) == 0 {
		t.Fatal("expected requirements for the scoped root profile")
	}
	for _, req := range plan.Requirements {
		if req.ProfileURL != "http://example.org/StructureDefinition/root-patient" {
			t.Fatalf("requirement %s scoped to out-of-scope profile %q", req.ID, req.ProfileURL)
		}
		if req.ResourceType == "Observation" {
			t.Fatalf("requirement %s derived from parent package profile", req.ID)
		}
	}
}
