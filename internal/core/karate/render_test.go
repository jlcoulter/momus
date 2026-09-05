package karate

import (
	"strings"
	"testing"
)

func TestRenderSimpleScenario(t *testing.T) {
	f := FeatureFile{
		Name: "Patient",
		Scenarios: []Scenario{
			{
				Name: "search name valid",
				Tags: []string{"@requirement:search|Patient|name|search-valid", "@domain:search", "@variant:search-valid"},
				Steps: []Step{
					{Keyword: "Given", Text: "url baseUrl"},
					{Keyword: "And", Text: "path 'Patient'"},
					{Keyword: "And", Text: "param name = 'momus-search'"},
					{Keyword: "When", Text: "method GET"},
					{Keyword: "Then", Text: "assert responseStatus in [200, 201]"},
				},
			},
		},
	}
	got := Render(f)
	want := `@domain:search
@requirement:search|Patient|name|search-valid
@variant:search-valid
Feature: Patient conformance

  @requirement:search|Patient|name|search-valid
  @domain:search
  @variant:search-valid
  Scenario: search name valid
    Given url baseUrl
    And path 'Patient'
    And param name = 'momus-search'
    When method GET
    Then assert responseStatus in [200, 201]
`
	if got != want {
		t.Errorf("Render mismatch.\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestRenderScenarioWithBody(t *testing.T) {
	f := FeatureFile{
		Name: "Patient",
		Scenarios: []Scenario{
			{
				Name: "create patient",
				Tags: []string{"@requirement:operation|Patient|operation-create"},
				Steps: []Step{
					{Keyword: "Given", Text: "url writeBaseUrl"},
					{Keyword: "And", Text: "path 'Patient/abc'"},
					{Keyword: "And", Text: "request", DocString: `{"id": "abc", "resourceType": "Patient"}`},
					{Keyword: "And", Text: "header Content-Type = 'application/fhir+json'"},
					{Keyword: "When", Text: "method PUT"},
					{Keyword: "Then", Text: "assert responseStatus in [200, 201]"},
				},
			},
		},
	}
	got := Render(f)
	if !strings.Contains(got, "      \"\"\"\n      {\"id\": \"abc\", \"resourceType\": \"Patient\"}\n      \"\"\"") {
		t.Errorf("expected doc string body, got:\n%s", got)
	}
	if !strings.Contains(got, "And header Content-Type = 'application/fhir+json'") {
		t.Errorf("expected content-type header step, got:\n%s", got)
	}
}

func TestRenderBackground(t *testing.T) {
	f := FeatureFile{
		Name: "Patient",
		Background: []Step{
			{Keyword: "Given", Text: "url writeBaseUrl"},
			{Keyword: "And", Text: "path 'Patient/momus-setup-Patient'"},
			{Keyword: "And", Text: "request", DocString: `{"resourceType": "Patient"}`},
			{Keyword: "When", Text: "method PUT"},
			{Keyword: "Then", Text: "assert responseStatus == 200 || responseStatus == 201"},
		},
		Scenarios: []Scenario{
			{
				Name: "search",
				Tags: []string{"@requirement:search|Patient|name|search-valid"},
				Steps: []Step{
					{Keyword: "Given", Text: "url baseUrl"},
					{Keyword: "When", Text: "method GET"},
					{Keyword: "Then", Text: "assert responseStatus in [200, 201]"},
				},
			},
		},
	}
	got := Render(f)
	if !strings.Contains(got, "  Background:") {
		t.Errorf("expected Background section, got:\n%s", got)
	}
	if !strings.Contains(got, "assert responseStatus == 200 || responseStatus == 201") {
		t.Errorf("expected background status assertion, got:\n%s", got)
	}
}

func TestRenderAllFilenames(t *testing.T) {
	files := []FeatureFile{
		{Name: "Patient"},
		{Name: "Observation"},
	}
	got := RenderAll(files)
	if len(got) != 2 {
		t.Fatalf("expected 2 files, got %d", len(got))
	}
	if _, ok := got["Patient.feature"]; !ok {
		t.Errorf("missing Patient.feature in %v", got)
	}
	if _, ok := got["Observation.feature"]; !ok {
		t.Errorf("missing Observation.feature in %v", got)
	}
}
