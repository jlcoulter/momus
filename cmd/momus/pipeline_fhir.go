package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	testast "github.com/jlcoulter/momus/internal/core/ast"
	testcoverage "github.com/jlcoulter/momus/internal/core/coverage"
	coregeneration "github.com/jlcoulter/momus/internal/core/generation"
	fhircoverage "github.com/jlcoulter/momus/internal/fhir/coverage"
	fhirgeneration "github.com/jlcoulter/momus/internal/fhir/generation"
	"github.com/jlcoulter/momus/internal/fhir/model"
	fhirpackage "github.com/jlcoulter/momus/internal/fhir/package"
	provisioning "github.com/jlcoulter/momus/internal/fhir/provisioning"
	"github.com/jlcoulter/momus/internal/fhir/registry"
)

// This file holds the FHIR-specific stage functions of the test pipeline. They
// are the front-end that turns a FHIR package archive into a test plan, and
// the provisioning/coverage stages that are unique to FHIR. The generic
// execution and reporting stages shared across all server types live in
// pipeline.go.

// resolvePackageGraph resolves the root package's dependency graph and builds
// the scoped registry. It is the shared entry point for every command that
// consumes a package archive.
func resolvePackageGraph(cfg *config, rootPath string) (*fhirpackage.ResolvedGraph, *registry.Registry, error) {
	searchDir := cfg.depsDir
	if searchDir == "" {
		searchDir = filepath.Dir(rootPath)
	}
	cacheDir := cfg.downloadDir
	if cacheDir == "" {
		cacheDir = filepath.Join(searchDir, ".momus", "packages")
	}

	graph, err := fhirpackage.ResolveLocalPackageGraphWithOptions(rootPath, fhirpackage.ResolveOptions{
		DepsDir:        searchDir,
		DownloadDir:    cacheDir,
		ConflictPolicy: fhirpackage.ConflictPolicy(cfg.conflictPolicy),
	})
	if err != nil {
		return nil, nil, err
	}
	if err := writeDebugGraph(cfg.debug, graph); err != nil {
		return nil, nil, err
	}

	builder := fhirpackage.NewRegistryBuilder()
	reg, err := builder.BuildFromPackagesScoped(graph.Packages, graph.Root)
	if err != nil {
		return nil, nil, err
	}
	return graph, reg, nil
}

// deriveCoveragePlan derives the coverage plan from the registry, scoped to the
// given resource types, profiles, and search codes.
func deriveCoveragePlan(cfg *config, reg *registry.Registry, resourceTypes, profileURLs []string, searchCodes map[string][]string) (*testcoverage.CoveragePlan, error) {
	return fhircoverage.DerivePlan(reg, testcoverage.DeriveOptions{
		IncludeResourceTypes:         resourceTypes,
		IncludeProfileURLs:           profileURLs,
		ExcludePathPrefixes:          cfg.excludePathPrefixes,
		MustSupportOnly:              cfg.mustSupportOnly,
		IncludeOptional:              cfg.includeOptional,
		IncludeLowValuePaths:         cfg.includeLowValuePaths,
		Strength:                     cfg.interactionStrength,
		CapabilitySearchCodes:        searchCodes,
		IncludeUniversalSearchParams: cfg.includeUniversalSearch,
	})
}

// buildTestPlan builds the test plan (seed dataset + test AST) from a coverage
// plan, restricting the seed dataset to the given capability resource types and
// profiles when non-empty. The seed dataset is embedded in the returned AST
// plan, so the plan is the single artifact that drives provisioning and
// execution.
func buildTestPlan(cfg *config, reg *registry.Registry, coveragePlan *testcoverage.CoveragePlan, preferredProfilesByResource map[string][]string, capabilityResourceTypes, capabilityProfiles []string) (*testast.Plan, error) {
	// Render a live progress bar to stderr during generation (only when stderr
	// is a terminal). It is cleared before the next stage prints.
	bar := newProgressBar(40)
	fhirOpts := fhirgeneration.BuildOptions{
		BaseURL:                        cfg.baseURL,
		WriteBaseURL:                   cfg.writeBaseURL,
		Registry:                       reg,
		PreferredProfileURLsByResource: preferredProfilesByResource,
		Strength:                       cfg.interactionStrength,
		Exhaustive:                     cfg.exhaustive,
		Progress:                       bar.render,
	}
	if len(capabilityResourceTypes) > 0 {
		fhirOpts.CapabilityResourceTypes = make(map[string]struct{}, len(capabilityResourceTypes))
		for _, t := range capabilityResourceTypes {
			fhirOpts.CapabilityResourceTypes[t] = struct{}{}
		}
	}
	if len(capabilityProfiles) > 0 {
		fhirOpts.CapabilityProfiles = make(map[string]struct{}, len(capabilityProfiles))
		for _, p := range capabilityProfiles {
			fhirOpts.CapabilityProfiles[p] = struct{}{}
		}
	}

	coreOpts := coregeneration.BuildOptions{
		BaseURL:                        cfg.baseURL,
		WriteBaseURL:                   cfg.writeBaseURL,
		Builder:                        fhirgeneration.NewBuilder(reg, cfg.exhaustive),
		PreferredProfileURLsByResource: preferredProfilesByResource,
		Strength:                       cfg.interactionStrength,
		Exhaustive:                     cfg.exhaustive,
		CapabilityResourceTypes:        fhirOpts.CapabilityResourceTypes,
		CapabilityProfiles:             fhirOpts.CapabilityProfiles,
		Progress:                       bar.render,
	}

	astPlan, err := coregeneration.GenerateFromCoveragePlan(coveragePlan, coreOpts)
	bar.finish()
	if err != nil {
		return nil, err
	}
	setupDataset, err := fhirgeneration.BuildSetupDataset(coveragePlan, fhirOpts)
	if err != nil {
		return nil, err
	}
	astPlan.Dataset = fhirgeneration.ToCoreDataset(setupDataset)
	return astPlan, nil
}

// provisionDataset uploads the seed dataset to the target server. It is a
// no-op (with a notice) when the dataset carries no seed resources.
func provisionDataset(cfg *config, ctx context.Context, dataset *model.Dataset) error {
	if cfg.baseURL == "" {
		return fmt.Errorf("base URL is required; provide --base-url")
	}
	writeBase := cfg.writeBaseURL
	if writeBase == "" {
		writeBase = cfg.baseURL
	}
	writeBasicUser := cfg.writeBasicUsername
	if writeBasicUser == "" {
		writeBasicUser = cfg.apiBasicUsername
	}
	writeBasicPass := cfg.writeBasicPassword
	if writeBasicPass == "" {
		writeBasicPass = cfg.apiBasicPassword
	}

	if dataset == nil || len(dataset.Resources) == 0 {
		fmt.Printf("Provisioning skipped: test plan carries no seed resources\n")
		return nil
	}

	provisioner := provisioning.New(writeBase, &provisioning.Options{
		BearerToken:   cfg.apiBearerToken,
		BasicUsername: writeBasicUser,
		BasicPassword: writeBasicPass,
		Tracer:        newDebugTracer(cfg.debug),
	})
	fmt.Printf("Provisioning phase: uploading %d seed resources to %s\n", len(dataset.Resources), writeBase)
	seed := provisioner.ProvisionAll(ctx, dataset)
	if !seed.Complete() {
		fmt.Printf("WARNING: dataset seeding incomplete — %d of %d resources uploaded. Data seeding is essential to achieve full coverage success. Fix the failing resources and re-run.\n", seed.Provisioned, seed.Provisioned+seed.Failed)
		for _, failure := range seed.Failures {
			fmt.Printf("  - %s\n", failure.Describe())
		}
		if !cfg.debug {
			fmt.Printf("Run with --debug to write the rejected payloads and full server responses to %s for inspection.\n", debugOutputDir)
		}
		if err := writeDebugProvisionFailures(cfg.debug, seed.Failures); err != nil {
			return err
		}
		// Incomplete provisioning is a warning, not a failure: the run can still
		// proceed. Failures are reported above so the operator can fix and re-run.
		return nil
	}
	fmt.Printf("Provisioning complete: %d resources uploaded\n", seed.Provisioned)
	return nil
}

// loadCoveragePlanFromFile reads a coverage plan JSON file. It returns nil when
// path is empty.
func loadCoveragePlanFromFile(path string) (*testcoverage.CoveragePlan, error) {
	if path == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read coverage plan %s: %w", path, err)
	}
	var plan testcoverage.CoveragePlan
	if err := json.Unmarshal(raw, &plan); err != nil {
		return nil, fmt.Errorf("parse coverage plan %s: %w", path, err)
	}
	return &plan, nil
}
