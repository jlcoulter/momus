package generation

import (
	"github.com/jlcoulter/momus/internal/core/ast"
	"github.com/jlcoulter/momus/internal/core/coverage"
	coregen "github.com/jlcoulter/momus/internal/core/generation"
)

// GenerateFromCoveragePlan maps coverage requirements into a concrete AST using
// the FHIR payload builder. It is the FHIR-side entry point: it adapts the
// FHIR BuildOptions (which carries the registry) into the generic core options
// (which carries a PayloadBuilder).
func GenerateFromCoveragePlan(plan *coverage.CoveragePlan, options BuildOptions) (*ast.Plan, error) {
	coreOpts := coregen.BuildOptions{
		BaseURL:                        options.BaseURL,
		WriteBaseURL:                   options.WriteBaseURL,
		Builder:                        NewBuilder(options.Registry, options.Exhaustive),
		PreferredProfileURLsByResource: options.PreferredProfileURLsByResource,
		Strength:                       options.Strength,
		Exhaustive:                     options.Exhaustive,
		CapabilityResourceTypes:        options.CapabilityResourceTypes,
		CapabilityProfiles:             options.CapabilityProfiles,
		Progress:                       options.Progress,
	}
	return coregen.GenerateFromCoveragePlan(plan, coreOpts)
}
