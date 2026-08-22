package main

import (
	"testing"
)

func TestMockCmdRegistered(t *testing.T) {
	cfg := &config{}
	root := newRootCmd(cfg)
	mockCmd, _, err := root.Find([]string{"mock"})
	if err != nil {
		t.Fatalf("mock command not found: %v", err)
	}
	if mockCmd.Use != "mock" {
		t.Fatalf("mock command Use = %q, want %q", mockCmd.Use, "mock")
	}
	if mockCmd.Flags().Lookup("status") == nil {
		t.Fatal("mock command missing --status flag")
	}
	if mockCmd.Flags().Lookup("body") == nil {
		t.Fatal("mock command missing --body flag")
	}
}
