package main

import (
	"context"
	"strings"
	"testing"
)

func TestTestOpenapiCmdRegistered(t *testing.T) {
	cfg := &config{}
	root := newRootCmd(cfg)
	testCmd, _, err := root.Find([]string{"test"})
	if err != nil {
		t.Fatalf("test command not found: %v", err)
	}
	openapiCmd, _, err := testCmd.Find([]string{"openapi"})
	if err != nil {
		t.Fatalf("test openapi subcommand not found: %v", err)
	}
	if openapiCmd.Use != "openapi <path-to-openapi.json>" {
		t.Fatalf("test openapi Use = %q, want %q", openapiCmd.Use, "openapi <path-to-openapi.json>")
	}
	for _, flag := range []string{"base-url", "output", "html", "include-cases"} {
		if openapiCmd.Flags().Lookup(flag) == nil {
			t.Fatalf("test openapi command missing --%s flag", flag)
		}
	}
}

func TestTestOpenapiCmdRequiresBaseURL(t *testing.T) {
	cfg := &config{}
	cmd := newTestOpenapiCmd(cfg)
	cmd.SetContext(context.Background())
	// The base URL guard runs before any document loading, so any path
	// exercises the error branch.
	err := cmd.RunE(cmd, []string{"does-not-exist.json"})
	if err == nil {
		t.Fatal("expected an error when --base-url is not set, got nil")
	}
	if !strings.Contains(err.Error(), "base URL is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}
