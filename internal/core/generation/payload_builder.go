// Package generation contains the domain-agnostic test-generation framework.
// It maps coverage requirements into a concrete test-plan AST. Payload and
// search-value synthesis is delegated to a PayloadBuilder implemented by a
// domain adapter (e.g. FHIR), so the framework never sees domain types.
package generation

import (
	"github.com/jlcoulter/momus/internal/core/ast"
	"github.com/jlcoulter/momus/internal/core/coverage"
)

// PayloadBuilder is implemented by a domain adapter (e.g. FHIR) to synthesize
// request payloads and search values for generated test cases. The generic
// generation framework calls these; it never sees domain types.
type PayloadBuilder interface {
	// DependencyPlan computes the resource dependency order for execution,
	// expanding the coverage-derived dependencies with every resource type a
	// profile's Reference elements target. capabilityResourceTypes, when
	// non-nil, restricts the plan to server-declared types.
	DependencyPlan(plan *coverage.CoveragePlan, capabilityResourceTypes map[string]struct{}) (*coverage.DependencyPlan, error)

	// BuildResourceCases turns a resource type's obligations into a list of
	// test case nodes. At strength 1 it emits one test per requirement; at
	// strength >= 2 it groups compatible accept obligations into a single
	// shared-payload test. progress, when non-nil, is invoked after each
	// requirement is processed.
	BuildResourceCases(reqs []coverage.CoverageRequirement, plan *coverage.CoveragePlan, options BuildOptions, deps []string, progress func()) []ast.Node

	// BuildBody returns a test payload for a requirement and whether a negative
	// mutation was applied (false when the target element is absent, in which
	// case the caller should skip a reject test).
	BuildBody(req coverage.CoverageRequirement, id string, profileURLs []string,
		primaryProfileURL string, deps []string, exhaustive bool) (map[string]any, bool)

	// SearchParamType returns the domain type of a search parameter (e.g.
	// "token", "reference", "date"), lowercased, or "" when it cannot be
	// resolved.
	SearchParamType(req coverage.CoverageRequirement, code string) string

	// SearchAcceptValue returns a value for an accept search obligation that
	// both makes the query meaningful and validates on the provisioned seed.
	SearchAcceptValue(req coverage.CoverageRequirement, code string) string

	// SearchInvalidValue returns a search value for the invalid-value
	// obligation and whether a conformant server is expected to reject it.
	SearchInvalidValue(req coverage.CoverageRequirement, code string) (string, bool)
}
