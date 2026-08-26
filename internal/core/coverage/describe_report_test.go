package coverage

import (
	"strings"
	"testing"
)

func TestDescribePlan(t *testing.T) {
	plan := &CoveragePlan{
		Strength: 2,
		Requirements: []CoverageRequirement{
			{ID: "http://example.org/StructureDefinition/patient|Patient.name|valid-min", HumanID: "Patient.name.cardinality.valid-min", ResourceType: "Patient", ElementPath: "Patient.name", Domain: CoverageDomainCardinality, Variant: CoverageVariantValidMin, Min: 1, Description: "Patient.name: accept a resource with the required element present (min=1)"},
			{ID: "search|Patient|name|search-valid", HumanID: "Patient.search.name.valid", ResourceType: "Patient", Domain: CoverageDomainSearch, Variant: CoverageVariantSearchValid, SearchCode: "name", Description: "Patient?name: return results for a valid search"},
			{ID: "operation|Patient|operation-read", HumanID: "Patient.operation.read", ResourceType: "Patient", Domain: CoverageDomainOperation, Variant: CoverageVariantOperationRead, Description: "Patient: read (GET) returns the resource"},
		},
	}

	out := DescribePlan(plan)

	for _, want := range []string{
		"# Coverage Plan",
		"**Total obligations**: 3",
		"**Interaction strength**: 2",
		"## cardinality",
		"## search",
		"## operation",
		"Patient.name: accept a resource with the required element present (min=1)",
		"Patient?name: return results for a valid search",
		"Patient: read (GET) returns the resource",
		"## Glossary",
		"### Domains",
		"### Variants",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("DescribePlan() missing %q in output:\n%s", want, out)
		}
	}
}

func TestDescribePlanEmpty(t *testing.T) {
	out := DescribePlan(&CoveragePlan{})
	if !strings.Contains(out, "No obligations in this plan") {
		t.Fatalf("expected empty-plan message, got:\n%s", out)
	}
	if !strings.Contains(DescribePlan(nil), "No obligations in this plan") {
		t.Fatalf("nil plan should render empty message")
	}
}

func TestDescribeAPIOperation(t *testing.T) {
	if got := DescribeAPIOperation("get", "/fhir/Patient"); got != "GET /fhir/Patient: respond correctly" {
		t.Fatalf("DescribeAPIOperation() = %q", got)
	}
	if got := DescribeAPIParameter("get", "/fhir/Patient", "query", "name"); got != "GET /fhir/Patient: query parameter name is handled" {
		t.Fatalf("DescribeAPIParameter() = %q", got)
	}
}
