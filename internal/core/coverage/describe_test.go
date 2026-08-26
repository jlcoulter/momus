package coverage

import "testing"

func TestDescribeCoverageRequirement(t *testing.T) {
	cases := []struct {
		name string
		req  CoverageRequirement
		want string
	}{
		{
			name: "cardinality valid-min",
			req:  CoverageRequirement{ResourceType: "Patient", ElementPath: "Patient.name", Domain: CoverageDomainCardinality, Variant: CoverageVariantValidMin, Min: 1},
			want: "Patient.name: accept a resource with the required element present (min=1)",
		},
		{
			name: "cardinality missing-required",
			req:  CoverageRequirement{ResourceType: "Patient", ElementPath: "Patient.name", Domain: CoverageDomainCardinality, Variant: CoverageVariantMissingRequired, Min: 1},
			want: "Patient.name: reject a resource missing the required element (min=1)",
		},
		{
			name: "datatype valid",
			req:  CoverageRequirement{ResourceType: "Patient", ElementPath: "Patient.gender", Datatype: "code", Domain: CoverageDomainDatatype, Variant: CoverageVariantDatatypeValid},
			want: "Patient.gender (code): accept a valid value",
		},
		{
			name: "terminology invalid",
			req:  CoverageRequirement{ResourceType: "Patient", ElementPath: "Patient.gender", Domain: CoverageDomainTerminology, Variant: CoverageVariantTerminologyInvalid},
			want: "Patient.gender: reject an invalid code",
		},
		{
			name: "search valid",
			req:  CoverageRequirement{ResourceType: "Patient", Domain: CoverageDomainSearch, Variant: CoverageVariantSearchValid, SearchCode: "name"},
			want: "Patient?name: return results for a valid search",
		},
		{
			name: "search combination",
			req:  CoverageRequirement{ResourceType: "Patient", Domain: CoverageDomainSearch, Variant: CoverageVariantSearchCombination, SearchCode: "name", SearchCodeB: "gender"},
			want: "Patient?name&gender: return results for a combined search",
		},
		{
			name: "operation read",
			req:  CoverageRequirement{ResourceType: "Patient", Domain: CoverageDomainOperation, Variant: CoverageVariantOperationRead},
			want: "Patient: read (GET) returns the resource",
		},
		{
			name: "operation custom",
			req:  CoverageRequirement{ResourceType: "Organization", Domain: CoverageDomainOperation, Variant: CoverageVariantOperationCustom, OperationName: "everything"},
			want: "Organization: $everything custom operation succeeds",
		},
		{
			name: "state crud",
			req:  CoverageRequirement{ResourceType: "Patient", Domain: CoverageDomainState, Variant: CoverageVariantStateCRUDSequence},
			want: "Patient: create-read-update-read-delete-read(404) sequence",
		},
		{
			name: "interaction pair",
			req:  CoverageRequirement{ResourceType: "Patient", ElementPath: "Patient.name ++ Patient.gender", Domain: CoverageDomainInteraction, Variant: CoverageVariantInteractionPair},
			want: "Patient.name ++ Patient.gender: accept both elements present together (pairwise)",
		},
		{
			name: "reference dangling",
			req:  CoverageRequirement{ResourceType: "Observation", ElementPath: "Observation.subject", Domain: CoverageDomainReference, Variant: CoverageVariantReferenceDangling},
			want: "Observation.subject: reject a dangling reference to a nonexistent resource",
		},
		{
			name: "invariant satisfies",
			req:  CoverageRequirement{ResourceType: "Observation", ElementPath: "Observation.value", Domain: CoverageDomainInvariant, Variant: CoverageVariantInvariantSatisfies},
			want: "Observation.value: accept a resource satisfying the invariant",
		},
		{
			name: "unknown variant fallback",
			req:  CoverageRequirement{ResourceType: "Patient", Variant: CoverageVariant("mystery-xyz")},
			want: "Patient: mystery-xyz obligation",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DescribeCoverageRequirement(tc.req)
			if got != tc.want {
				t.Fatalf("DescribeCoverageRequirement() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestHumanID(t *testing.T) {
	cases := []struct {
		name string
		req  CoverageRequirement
		want string
	}{
		{
			name: "cardinality",
			req:  CoverageRequirement{ResourceType: "Patient", ElementPath: "Patient.name", Domain: CoverageDomainCardinality, Variant: CoverageVariantValidMin},
			want: "Patient.name.cardinality.valid-min",
		},
		{
			name: "datatype",
			req:  CoverageRequirement{ResourceType: "Patient", ElementPath: "Patient.gender", Domain: CoverageDomainDatatype, Variant: CoverageVariantDatatypeValid},
			want: "Patient.gender.datatype.datatype-valid",
		},
		{
			name: "search",
			req:  CoverageRequirement{ResourceType: "Patient", Domain: CoverageDomainSearch, Variant: CoverageVariantSearchValid, SearchCode: "name"},
			want: "Patient.search.name.valid",
		},
		{
			name: "search combination",
			req:  CoverageRequirement{ResourceType: "Patient", Domain: CoverageDomainSearch, Variant: CoverageVariantSearchCombination, SearchCode: "name", SearchCodeB: "gender"},
			want: "Patient.search.name+gender.combination",
		},
		{
			name: "operation",
			req:  CoverageRequirement{ResourceType: "Patient", Domain: CoverageDomainOperation, Variant: CoverageVariantOperationRead},
			want: "Patient.operation.read",
		},
		{
			name: "state",
			req:  CoverageRequirement{ResourceType: "Patient", Domain: CoverageDomainState, Variant: CoverageVariantStateCRUDSequence},
			want: "Patient.state.crud-sequence",
		},
		{
			name: "interaction",
			req:  CoverageRequirement{ResourceType: "Patient", Domain: CoverageDomainInteraction, Variant: CoverageVariantInteractionPair},
			want: "Patient.interaction.pair",
		},
		{
			name: "terminology",
			req:  CoverageRequirement{ResourceType: "Patient", ElementPath: "Patient.gender", Domain: CoverageDomainTerminology, Variant: CoverageVariantTerminologyInvalid},
			want: "Patient.gender.terminology.terminology-invalid",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := HumanID(tc.req)
			if got != tc.want {
				t.Fatalf("HumanID() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestGlossaryCoversAllDomains(t *testing.T) {
	descs := DomainDescriptions()
	for _, d := range allDomains() {
		if _, ok := descs[d]; !ok {
			t.Errorf("DomainDescriptions() missing description for domain %q", d)
		}
	}
}

func TestGlossaryCoversAllVariants(t *testing.T) {
	descs := VariantDescriptions()
	for _, v := range allVariants() {
		if _, ok := descs[v]; !ok {
			t.Errorf("VariantDescriptions() missing description for variant %q", v)
		}
	}
}

func allDomains() []CoverageDomain {
	return []CoverageDomain{
		CoverageDomainCardinality,
		CoverageDomainDatatype,
		CoverageDomainTerminology,
		CoverageDomainStructure,
		CoverageDomainInvariant,
		CoverageDomainReference,
		CoverageDomainInteraction,
		CoverageDomainSearch,
		CoverageDomainOperation,
		CoverageDomainState,
	}
}

func allVariants() []CoverageVariant {
	return []CoverageVariant{
		CoverageVariantValidMin,
		CoverageVariantMissingRequired,
		CoverageVariantMultipleValues,
		CoverageVariantDatatypeValid,
		CoverageVariantDatatypeInvalidLexical,
		CoverageVariantDatatypeWrongJSONType,
		CoverageVariantDatatypeNull,
		CoverageVariantTerminologyValid,
		CoverageVariantTerminologyInvalid,
		CoverageVariantTerminologyAbsent,
		CoverageVariantStructureSlicePresent,
		CoverageVariantInvariantSatisfies,
		CoverageVariantInvariantViolates,
		CoverageVariantReferenceValid,
		CoverageVariantReferenceWrongTarget,
		CoverageVariantReferenceDangling,
		CoverageVariantInteractionPair,
		CoverageVariantSearchValid,
		CoverageVariantSearchNoResults,
		CoverageVariantSearchInvalidValue,
		CoverageVariantSearchMultipleResults,
		CoverageVariantSearchInvalidModifier,
		CoverageVariantSearchCombination,
		CoverageVariantOperationRead,
		CoverageVariantOperationUpdate,
		CoverageVariantOperationPatch,
		CoverageVariantOperationDelete,
		CoverageVariantOperationHistory,
		CoverageVariantOperationCustom,
		CoverageVariantStateCRUDSequence,
		CoverageVariantStateReadNonexistent,
		CoverageVariantStateDeleteNonexistent,
	}
}
