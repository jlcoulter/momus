package main

import (
	"fmt"

	"github.com/jlcoulter/momus/internal/mock"
	"github.com/jlcoulter/momus/internal/test/ast"
)

// startMock starts a plan-aware mock server for the "test" command. It returns
// the running server and its base URL. The caller must call SetPlan once the
// test plan is generated (the plan's base URL depends on the mock's address),
// and Close when done. basePath is the path the mock serves under (e.g. "/fhir"
// for FHIR, "" for OpenAPI).
func startMock(cfg *config, basePath string) (*mock.Server, string, error) {
	opts := []mock.Option{mock.WithPort(cfg.mockPort), mock.WithBasePath(basePath), mock.WithPlanAware(), mock.WithLogger(false)}
	s := mock.New(200, "", opts...)
	addr, err := s.Start()
	if err != nil {
		return nil, "", fmt.Errorf("start mock server: %w", err)
	}
	baseURL := "http://" + addr + basePath
	fmt.Printf("Mock server listening on %s\n", baseURL)
	return s, baseURL, nil
}

// setMockPlan feeds the generated test plan's reject routes into the mock.
func setMockPlan(s *mock.Server, plan *ast.Plan) {
	if s == nil || plan == nil {
		return
	}
	s.SetPlan(plan.Root)
}
