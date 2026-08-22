package mock

import (
	"bytes"
	"context"
	"net/http"
	"testing"
)

// stubValidator rejects a payload whose name is absent, mimicking the real
// validator's cardinality check.
type stubValidator struct{}

func (stubValidator) Validate(ctx context.Context, profileURL string, resource map[string]any) ([]Issue, error) {
	if _, ok := resource["name"]; !ok {
		return []Issue{{Path: "Patient.name", Kind: "cardinality", Message: "required element Patient.name is missing"}}, nil
	}
	return nil, nil
}

func TestSemanticValidationRejectsNonConformantPUT(t *testing.T) {
	s := New(http.StatusOK, "", WithPlanAware(), WithValidator(stubValidator{}))
	addr, err := s.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Close()

	base := "http://" + addr
	// Payload missing required name, carrying a meta.profile.
	body := `{"resourceType":"Patient","id":"p1","meta":{"profile":["http://example.org/StructureDefinition/patient"]}}`
	req, _ := http.NewRequest(http.MethodPut, base+"/Patient/p1", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/fhir+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("PUT status = %d, want 422", resp.StatusCode)
	}
}

func TestSemanticValidationAcceptsConformantPUT(t *testing.T) {
	s := New(http.StatusOK, "", WithPlanAware(), WithValidator(stubValidator{}))
	addr, err := s.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Close()

	base := "http://" + addr
	body := `{"resourceType":"Patient","id":"p1","name":"Jane","meta":{"profile":["http://example.org/StructureDefinition/patient"]}}`
	req, _ := http.NewRequest(http.MethodPut, base+"/Patient/p1", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/fhir+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("PUT status = %d, want 201", resp.StatusCode)
	}
}

func TestSemanticValidationWithoutProfileSkipped(t *testing.T) {
	s := New(http.StatusOK, "", WithPlanAware(), WithValidator(stubValidator{}))
	addr, err := s.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Close()

	base := "http://" + addr
	// No meta.profile -> validation is skipped, payload stored (201).
	body := `{"resourceType":"Patient","id":"p1"}`
	req, _ := http.NewRequest(http.MethodPut, base+"/Patient/p1", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/fhir+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("PUT status = %d, want 201", resp.StatusCode)
	}
}
