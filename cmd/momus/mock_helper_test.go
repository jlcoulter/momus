package main

import (
	"testing"
)

func TestTestFhirCmdMockFlags(t *testing.T) {
	cfg := &config{}
	cmd := newTestFhirCmd(cfg)
	for _, flag := range []string{"mock", "mock-port"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Fatalf("test fhir command missing --%s flag", flag)
		}
	}
}

func TestTestOpenapiCmdMockFlags(t *testing.T) {
	cfg := &config{}
	cmd := newTestOpenapiCmd(cfg)
	for _, flag := range []string{"mock", "mock-port"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Fatalf("test openapi command missing --%s flag", flag)
		}
	}
}

func TestStartMock(t *testing.T) {
	cfg := &config{mock: true}
	s, baseURL, err := startMock(cfg, "/fhir")
	if err != nil {
		t.Fatalf("startMock: %v", err)
	}
	defer s.Close()

	if baseURL == "" {
		t.Fatal("expected a non-empty base URL")
	}
	// The base URL should include the /fhir base path.
	if len(baseURL) < len("http://")+len("/fhir") {
		t.Fatalf("base URL too short: %q", baseURL)
	}
}
