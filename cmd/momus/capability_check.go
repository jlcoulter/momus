package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
	testcoverage "github.com/jlcoulter/momus/internal/test/coverage"
)

// capabilityEvidence summarises a verification of the seed dataset's resource
// types and profiles against a CapabilityStatement. It is the hard evidence
// that what we are about to provision is something the server (or the package,
// when the live server is unreachable) declares it supports.
type capabilityEvidence struct {
	Fetched             bool
	Source              string // "live" or "package"
	Error               error
	SupportedTypes      []string
	PlanTypes           []string
	UnsupportedTypes    []string
	SupportedProfiles   []string
	PlanProfiles        []string
	UnsupportedProfiles []string
}

// verifyPlanAgainstCapability cross-checks every resource type and meta.profile
// in the seed dataset against a CapabilityStatement. It prefers the live
// server's /metadata; when the server is unreachable (or no base URL is
// configured), it falls back to the first CapabilityStatement carried in the
// package registry, so capability evidence is still available without a live
// server.
func verifyPlanAgainstCapability(ctx context.Context, cfg *config, reg *registry.Registry, dataset *model.Dataset) capabilityEvidence {
	ev := capabilityEvidence{}
	var cs *model.CapabilityStatement
	// A local metadata file takes precedence over a live fetch: it lets the
	// caller supply server metadata from a saved CapabilityStatement when the
	// server is unreachable or not yet running.
	if cfg != nil && cfg.metadataFile != "" {
		loaded, err := loadMetadataFile(cfg.metadataFile)
		if err == nil {
			cs = loaded
			ev.Source = "file"
		} else {
			ev.Error = err
		}
	} else if cfg != nil && cfg.baseURL != "" {
		fetched, err := testcoverage.FetchCapabilityStatement(ctx, cfg.baseURL, testcoverage.CapabilityFetchOptions{
			BearerToken:   cfg.apiBearerToken,
			BasicUsername: cfg.apiBasicUsername,
			BasicPassword: cfg.apiBasicPassword,
			Tracer:        newDebugTracer(cfg.debug),
		})
		if err == nil {
			cs = fetched
			ev.Source = "live"
		} else {
			ev.Error = err
		}
	}
	if cs == nil {
		cs = packageCapabilityStatement(reg)
		if cs != nil {
			ev.Source = "package"
		}
	}
	if cs == nil {
		return ev
	}
	ev.Fetched = true
	ev.SupportedTypes = testcoverage.ResourceTypesFromCapabilityStatement(cs, false)
	ev.SupportedProfiles = testcoverage.SupportedProfileURLsFromCapabilityStatement(cs, false)

	supportedTypes := make(map[string]struct{}, len(ev.SupportedTypes))
	for _, t := range ev.SupportedTypes {
		supportedTypes[t] = struct{}{}
	}
	// Only flag types/profiles against the capability statement when it actually
	// declares them; a CapabilityStatement that lists neither supportedProfile nor
	// resource types cannot constrain the plan.
	profileSet := make(map[string]struct{})
	for _, p := range ev.SupportedProfiles {
		profileSet[p] = struct{}{}
	}

	seenType := make(map[string]struct{})
	seenProfile := make(map[string]struct{})
	if dataset != nil {
		for _, inst := range dataset.Resources {
			if inst == nil {
				continue
			}
			if inst.ResourceType != "" && len(supportedTypes) > 0 {
				if _, ok := seenType[inst.ResourceType]; !ok {
					seenType[inst.ResourceType] = struct{}{}
					ev.PlanTypes = append(ev.PlanTypes, inst.ResourceType)
					if _, ok := supportedTypes[inst.ResourceType]; !ok {
						ev.UnsupportedTypes = append(ev.UnsupportedTypes, inst.ResourceType)
					}
				}
			}
			if inst.Profile != "" && len(profileSet) > 0 {
				if _, ok := seenProfile[inst.Profile]; !ok {
					seenProfile[inst.Profile] = struct{}{}
					ev.PlanProfiles = append(ev.PlanProfiles, inst.Profile)
					if _, ok := profileSet[inst.Profile]; !ok {
						ev.UnsupportedProfiles = append(ev.UnsupportedProfiles, inst.Profile)
					}
				}
			}
		}
	}
	sort.Strings(ev.PlanTypes)
	sort.Strings(ev.UnsupportedTypes)
	sort.Strings(ev.PlanProfiles)
	sort.Strings(ev.UnsupportedProfiles)
	return ev
}

// loadMetadataFile reads a local CapabilityStatement JSON file and returns the
// parsed model. This bypasses the live /metadata fetch, letting the caller
// supply server metadata from a saved file when the server is unreachable or
// not yet running.
func loadMetadataFile(path string) (*model.CapabilityStatement, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read metadata file %s: %w", path, err)
	}
	return parseCapabilityStatement(data)
}

// parseCapabilityStatement decodes raw JSON bytes into a model.CapabilityStatement.
// It validates the resourceType field.
func parseCapabilityStatement(data []byte) (*model.CapabilityStatement, error) {
	var raw struct {
		ResourceType string `json:"resourceType"`
		URL          string `json:"url"`
		Version      string `json:"version"`
		Name         string `json:"name"`
		Status       string `json:"status"`
		FhirVersion  string `json:"fhirVersion"`
		Rest         []struct {
			Mode     string `json:"mode"`
			Resource []struct {
				Type             string   `json:"type"`
				Profile          string   `json:"profile"`
				SupportedProfile []string `json:"supportedProfile"`
				Interaction      []struct {
					Code string `json:"code"`
				} `json:"interaction"`
				Operation []struct {
					Name       string `json:"name"`
					Definition string `json:"definition"`
				} `json:"operation"`
				SearchParam []struct {
					Name       string `json:"name"`
					Definition string `json:"definition"`
					Type       string `json:"type"`
				} `json:"searchParam"`
			} `json:"resource"`
		} `json:"rest"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("decode capability statement: %w", err)
	}
	if raw.ResourceType != "CapabilityStatement" {
		return nil, fmt.Errorf("expected CapabilityStatement, got %q", raw.ResourceType)
	}
	rest := make([]model.CapabilityStatementRest, 0, len(raw.Rest))
	for _, rb := range raw.Rest {
		resources := make([]model.CapabilityStatementRestResource, 0, len(rb.Resource))
		for _, res := range rb.Resource {
			interactions := make([]model.CapabilityStatementInteraction, 0, len(res.Interaction))
			for _, in := range res.Interaction {
				interactions = append(interactions, model.CapabilityStatementInteraction{Code: in.Code})
			}
			ops := make([]model.CapabilityStatementOperation, 0, len(res.Operation))
			for _, op := range res.Operation {
				ops = append(ops, model.CapabilityStatementOperation{Name: op.Name, Definition: op.Definition})
			}
			searchParams := make([]model.CapabilityStatementSearchParam, 0, len(res.SearchParam))
			for _, sp := range res.SearchParam {
				searchParams = append(searchParams, model.CapabilityStatementSearchParam{
					Name:       sp.Name,
					Definition: sp.Definition,
					Type:       sp.Type,
				})
			}
			resources = append(resources, model.CapabilityStatementRestResource{
				Type:             res.Type,
				Profile:          res.Profile,
				SupportedProfile: res.SupportedProfile,
				Interaction:      interactions,
				Operation:        ops,
				SearchParam:      searchParams,
			})
		}
		rest = append(rest, model.CapabilityStatementRest{Mode: rb.Mode, Resource: resources})
	}
	return &model.CapabilityStatement{
		URL:         raw.URL,
		Version:     raw.Version,
		Name:        raw.Name,
		Status:      raw.Status,
		FhirVersion: raw.FhirVersion,
		Rest:        rest,
	}, nil
}

// packageCapabilityStatement returns the first CapabilityStatement indexed in
// the registry (from the loaded packages), or nil.
func packageCapabilityStatement(reg *registry.Registry) *model.CapabilityStatement {
	if reg == nil {
		return nil
	}
	for _, cs := range reg.CapabilityStatements() {
		if cs != nil {
			return cs
		}
	}
	return nil
}

// reportCapabilityEvidence prints the capability cross-check result.
func reportCapabilityEvidence(ev capabilityEvidence, baseURL string) {
	if ev.Error != nil {
		fmt.Fprintf(os.Stderr, "WARNING: failed to load CapabilityStatement (%v); falling back to package CapabilityStatement\n", ev.Error)
	}
	if !ev.Fetched {
		fmt.Printf("Capability evidence: no CapabilityStatement available (live %s/metadata unreachable and package carries none); skipping type/profile verification\n", baseURL)
		return
	}
	if len(ev.UnsupportedTypes) == 0 && len(ev.UnsupportedProfiles) == 0 {
		fmt.Printf("Capability evidence (source %s): server/package declares %d resource types; plan sends %d types and %d profiles, all supported\n", ev.Source, len(ev.SupportedTypes), len(ev.PlanTypes), len(ev.PlanProfiles))
		return
	}
	if len(ev.UnsupportedTypes) > 0 {
		fmt.Printf("WARNING: plan sends resource types the CapabilityStatement does not support: %v\n", ev.UnsupportedTypes)
	}
	if len(ev.UnsupportedProfiles) > 0 {
		fmt.Printf("WARNING: plan sends profiles not in the CapabilityStatement's supportedProfile: %v\n", ev.UnsupportedProfiles)
	}
}
