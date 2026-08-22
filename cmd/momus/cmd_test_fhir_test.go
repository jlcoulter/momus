package main

import (
	"context"
	"strings"
	"testing"
)

func TestTestCmdRegistered(t *testing.T) {
	cfg := &config{}
	root := newRootCmd(cfg)
	testCmd, _, err := root.Find([]string{"test"})
	if err != nil {
		t.Fatalf("test command not found: %v", err)
	}
	if testCmd.Use != "test" {
		t.Fatalf("test command Use = %q, want %q", testCmd.Use, "test")
	}
	// The test group must expose a "fhir" subcommand.
	fhirCmd, _, err := testCmd.Find([]string{"fhir"})
	if err != nil {
		t.Fatalf("test fhir subcommand not found: %v", err)
	}
	if fhirCmd.Use != "fhir <path-to-package.tgz>" {
		t.Fatalf("test fhir Use = %q, want %q", fhirCmd.Use, "fhir <path-to-package.tgz>")
	}
	for _, flag := range []string{"base-url", "output", "html", "fail-on-uncovered", "include-resource", "strength"} {
		if fhirCmd.Flags().Lookup(flag) == nil {
			t.Fatalf("test fhir command missing --%s flag", flag)
		}
	}
}

func TestTestFhirCmdRequiresBaseURL(t *testing.T) {
	cfg := &config{}
	cmd := newTestFhirCmd(cfg)
	cmd.SetContext(context.Background())
	// The base URL guard runs before any package resolution, so any path
	// exercises the error branch.
	err := cmd.RunE(cmd, []string{"does-not-exist.tgz"})
	if err == nil {
		t.Fatal("expected an error when --base-url is not set, got nil")
	}
	if !strings.Contains(err.Error(), "base URL is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}
