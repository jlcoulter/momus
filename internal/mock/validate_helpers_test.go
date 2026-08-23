package mock

import "testing"

func TestProfileAndResource(t *testing.T) {
	// Invalid JSON.
	if _, _, ok := profileAndResource([]byte("not-json")); ok {
		t.Fatal("invalid JSON should have no profile")
	}
	// No meta.
	url, res, ok := profileAndResource([]byte(`{"resourceType":"Patient"}`))
	if ok || url != "" || res == nil {
		t.Fatalf("no meta = %q, %v, %v", url, res != nil, ok)
	}
	// Meta without profile.
	if _, _, ok := profileAndResource([]byte(`{"meta":{}}`)); ok {
		t.Fatal("empty meta should have no profile")
	}
	// Empty profile array.
	if _, _, ok := profileAndResource([]byte(`{"meta":{"profile":[]}}`)); ok {
		t.Fatal("empty profile array should have no profile")
	}
	// Non-string profile entry.
	if _, _, ok := profileAndResource([]byte(`{"meta":{"profile":[42]}}`)); ok {
		t.Fatal("non-string profile should have no profile")
	}
	// Valid profile.
	validURL, validRes, validOK := profileAndResource([]byte(`{"meta":{"profile":["http://example.org/StructureDefinition/patient"]}}`))
	if !validOK || validURL != "http://example.org/StructureDefinition/patient" || validRes == nil {
		t.Fatalf("valid = %q, %v, %v", validURL, validRes != nil, validOK)
	}
}
