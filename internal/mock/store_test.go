package mock

import "testing"

func TestStoreSearchQuantityMatchesParts(t *testing.T) {
	s := NewStore()
	s.Put("Observation", "o1", []byte(`{"resourceType":"Observation","id":"o1","status":"final","valueQuantity":{"value":180.5,"unit":"cm","system":"http://unitsofmeasure.org","code":"cm"}}`))

	// Quantity search "180.5|http://unitsofmeasure.org|cm" should match via its
	// | -separated parts.
	got, err := s.Search("Observation", map[string]string{"valueQuantity": "180.5|http://unitsofmeasure.org|cm"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 match for valueQuantity=180.5|http://unitsofmeasure.org|cm, got %d", len(got))
	}

	// A quantity whose parts all differ from the resource must not match.
	got, err = s.Search("Observation", map[string]string{"valueQuantity": "99|http://example.org|in"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 matches for a mismatched quantity code, got %d", len(got))
	}
}

func TestStoreSearchFallsBackToNestedField(t *testing.T) {
	s := NewStore()
	s.Put("Patient", "p1", []byte(`{"resourceType":"Patient","id":"p1","birthDate":"1990-05-15","name":[{"family":"Doe"}]}`))

	// The query param code "birthdate" differs from the JSON field "birthDate";
	// a whole-resource fallback must still match.
	got, err := s.Search("Patient", map[string]string{"birthdate": "1990-05-15"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 match for birthdate=1990-05-15, got %d", len(got))
	}
}

func TestStoreSearchNear(t *testing.T) {
	s := NewStore()
	s.Put("Location", "l1", []byte(`{"resourceType":"Location","id":"l1","position":{"latitude":-33.8688,"longitude":151.2093}}`))

	got, err := s.Search("Location", map[string]string{"near": "-33.8688|151.2093"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 match for near, got %d", len(got))
	}

	// A far-away coordinate should not match.
	got, err = s.Search("Location", map[string]string{"near": "90.0|0.0"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 matches for a distant near, got %d", len(got))
	}
}

func TestStoreSearchComposite(t *testing.T) {
	s := NewStore()
	s.Put("Observation", "o1", []byte(`{"resourceType":"Observation","id":"o1","status":"final","code":{"coding":[{"code":"glucose"}]},"valueQuantity":{"value":5.4}}`))

	got, err := s.Search("Observation", map[string]string{"code-value-quantity": "glucose$5.4"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 match for composite, got %d", len(got))
	}

	// A composite where only one part matches must not match.
	got, err = s.Search("Observation", map[string]string{"code-value-quantity": "glucose$99"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 matches for partial composite, got %d", len(got))
	}
}
