package search

import (
	"reflect"
	"testing"

	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
)

func TestModifiersForType(t *testing.T) {
	tests := []struct {
		name string
		typ  string
		want []string
	}{
		{name: "string", typ: "string", want: []string{"exact", "contains"}},
		{name: "token", typ: "token", want: []string{"text", "not", "in", "not-in", "above", "below", "identifier"}},
		{name: "reference", typ: "reference", want: []string{"type", "identifier"}},
		{name: "uri", typ: "uri", want: []string{"above", "below"}},
		{name: "quantity", typ: "quantity", want: []string{"not"}},
		{name: "date", typ: "date", want: nil},
		{name: "number", typ: "number", want: nil},
		{name: "boolean", typ: "boolean", want: nil},
		{name: "special", typ: "special", want: nil},
		{name: "composite", typ: "composite", want: nil},
		{name: "case-insensitive", typ: "TOKEN", want: []string{"text", "not", "in", "not-in", "above", "below", "identifier"}},
		{name: "unknown", typ: "bogus", want: nil},
		{name: "empty", typ: "", want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ModifiersForType(tt.typ)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ModifiersForType(%q) = %v, want %v", tt.typ, got, tt.want)
			}
		})
	}
}

func TestTypeModifiers(t *testing.T) {
	m := TypeModifiers()
	// Mutating the returned map must not affect the package global.
	m["string"] = append(m["string"], "custom")
	if len(m["string"]) != len(modifierMapping["string"])+1 {
		t.Fatalf("mutating TypeModifiers output leaked: got %v", m["string"])
	}
	if got := ModifiersForType("string"); len(got) != 2 {
		t.Fatalf("ModifiersForType affected by map mutation: %v", got)
	}
}

func TestUniversalParametersCompleteAndSorted(t *testing.T) {
	got := UniversalParameters()
	// The complete set must include both declared and implicit universals.
	byCode := make(map[string]model.SearchParameter, len(got))
	for _, sp := range got {
		byCode[sp.Code] = sp
	}
	for _, wantCode := range []string{
		"_id", "_lastUpdated", "_profile", "_security", "_source", "_tag",
		"_content", "_text", "_filter",
		"_include", "_revinclude", "_has", "_type", "_sort", "_count",
		"_summary", "_elements", "_contained", "_containedType", "_list", "_query",
	} {
		if _, ok := byCode[wantCode]; !ok {
			t.Fatalf("universal parameter %q missing from UniversalParameters", wantCode)
		}
	}
	// Sorted by code.
	for i := 1; i < len(got); i++ {
		if got[i-1].Code > got[i].Code {
			t.Fatalf("UniversalParameters not sorted: %q before %q", got[i-1].Code, got[i].Code)
		}
	}
	// Must not share backing arrays with the globals.
	got[0].Code = "mutated"
	if UniversalParameters()[0].Code == "mutated" {
		t.Fatal("UniversalParameters returned aliased data")
	}
}

func TestDeclaredAndImplicitUniversal(t *testing.T) {
	declared := DeclaredUniversal()
	implicit := ImplicitUniversal()
	if len(declared) == 0 || len(implicit) == 0 {
		t.Fatalf("expected non-empty declared (%d) and implicit (%d) sets", len(declared), len(implicit))
	}
	for _, sp := range declared {
		if sp.Code == "" {
			t.Fatal("declared universal has empty code")
		}
	}
	for _, sp := range implicit {
		if sp.Code == "" {
			t.Fatal("implicit universal has empty code")
		}
	}
	// _include must be implicit (no SearchParameter resource in R4 core).
	found := false
	for _, sp := range implicit {
		if sp.Code == "_include" {
			found = true
		}
	}
	if !found {
		t.Fatal("_include expected in implicit universal set")
	}
}

func TestChains(t *testing.T) {
	r := registry.New()
	r.AddSearchParameter(&model.SearchParameter{
		Code:   "organization",
		Base:   []string{"Patient"},
		Type:   "reference",
		Target: []string{"Organization"},
	})
	r.AddSearchParameter(&model.SearchParameter{
		Code: "name",
		Base: []string{"Organization"},
		Type: "string",
	})
	r.AddSearchParameter(&model.SearchParameter{
		Code: "identifier",
		Base: []string{"Organization"},
		Type: "token",
	})
	r.AddSearchParameter(&model.SearchParameter{
		Code:   "general-practitioner",
		Base:   []string{"Patient"},
		Type:   "reference",
		Target: []string{"Practitioner", "Organization"},
	})
	// Non-reference param must not produce chains.
	r.AddSearchParameter(&model.SearchParameter{
		Code: "name",
		Base: []string{"Patient"},
		Type: "string",
	})

	got := Chains(r, "Patient")
	want := []ChainParam{
		{Path: "general-practitioner.identifier", TargetType: "Organization", TargetCode: "identifier", Depth: 1},
		{Path: "general-practitioner.name", TargetType: "Organization", TargetCode: "name", Depth: 1},
		{Path: "organization.identifier", TargetType: "Organization", TargetCode: "identifier", Depth: 1},
		{Path: "organization.name", TargetType: "Organization", TargetCode: "name", Depth: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Chains mismatch:\n got: %+v\nwant: %+v", got, want)
	}
}

func TestChainsEmptyForUnknownType(t *testing.T) {
	r := registry.New()
	if got := Chains(r, "Patient"); len(got) != 0 {
		t.Fatalf("expected no chains, got %+v", got)
	}
	if got := Chains(nil, "Patient"); len(got) != 0 {
		t.Fatalf("expected no chains for nil registry, got %+v", got)
	}
}

func TestChainsWithDepthMultiLevel(t *testing.T) {
	r := registry.New()
	r.AddSearchParameter(&model.SearchParameter{
		Code:   "organization",
		Base:   []string{"Patient"},
		Type:   "reference",
		Target: []string{"Organization"},
	})
	r.AddSearchParameter(&model.SearchParameter{
		Code:   "part-of",
		Base:   []string{"Organization"},
		Type:   "reference",
		Target: []string{"Organization"},
	})
	r.AddSearchParameter(&model.SearchParameter{
		Code: "name",
		Base: []string{"Organization"},
		Type: "string",
	})
	r.AddSearchParameter(&model.SearchParameter{
		Code: "city",
		Base: []string{"Organization"},
		Type: "string",
	})
	r.AddSearchParameter(&model.SearchParameter{
		Code:   "general-practitioner",
		Base:   []string{"Patient"},
		Type:   "reference",
		Target: []string{"Organization", "Practitioner"},
	})
	// Practitioner has a reference param that itself chains to Organization.
	r.AddSearchParameter(&model.SearchParameter{
		Code:   "organization",
		Base:   []string{"Practitioner"},
		Type:   "reference",
		Target: []string{"Organization"},
	})
	r.AddSearchParameter(&model.SearchParameter{
		Code: "name",
		Base: []string{"Practitioner"},
		Type: "string",
	})

	// Depth 1: only one-level chains, all Depth 1.
	got1 := ChainsWithDepth(r, "Patient", 1)
	if len(got1) == 0 {
		t.Fatal("expected depth-1 chains")
	}
	for _, c := range got1 {
		if c.Depth != 1 {
			t.Fatalf("depth-1 chain %q has Depth %d", c.Path, c.Depth)
		}
	}
	hasChain := func(chains []ChainParam, path, targetType, targetCode string, depth int) bool {
		for _, c := range chains {
			if c.Path == path && c.TargetType == targetType && c.TargetCode == targetCode && c.Depth == depth {
				return true
			}
		}
		return false
	}
	if !hasChain(got1, "organization.name", "Organization", "name", 1) {
		t.Fatalf("expected one-level chain organization.name in %+v", got1)
	}
	if !hasChain(got1, "general-practitioner.name", "Organization", "name", 1) {
		t.Fatalf("expected one-level chain general-practitioner.name in %+v", got1)
	}

	// Depth 2: adds multi-level chains through one intermediate reference
	// param, e.g. "general-practitioner.organization.name" (2 ref hops).
	got2 := ChainsWithDepth(r, "Patient", 2)
	if !hasChain(got2, "general-practitioner.organization.name", "Organization", "name", 2) {
		t.Fatalf("expected depth-2 chain general-practitioner.organization.name in %+v", got2)
	}
	// Depth-1 chains are preserved at higher depths.
	if !hasChain(got2, "organization.name", "Organization", "name", 1) {
		t.Fatalf("expected chain organization.name in %+v", got2)
	}

	// Depth 3 also recurses through two intermediate hops, but depth-2 chains
	// remain present.
	got3 := ChainsWithDepth(r, "Patient", 3)
	if !hasChain(got3, "general-practitioner.organization.name", "Organization", "name", 2) {
		t.Fatalf("expected chain general-practitioner.organization.name at depth 3 in %+v", got3)
	}
	if !hasChain(got3, "organization.part-of.name", "Organization", "name", 2) {
		t.Fatalf("expected chain organization.part-of.name in %+v", got3)
	}
}
