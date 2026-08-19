package planner

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/test/ast"
)

// DefaultPlanner turns data requirements into a TestPlan. It generates a
// Dataset via the Generator and lays out an executable AST that provisions the
// dataset in dependency order (targets before dependents), choosing Parallel
// for independent resources and Sequence for dependent levels.
type DefaultPlanner struct {
	generator Generator
}

// NewDefaultPlanner returns a DefaultPlanner backed by the given generator.
func NewDefaultPlanner(generator Generator) *DefaultPlanner {
	return &DefaultPlanner{generator: generator}
}

// Plan generates the data required by input.Requirements and returns a
// TestPlan that provisions and verifies it. The generated Dataset is attached
// so a caller can provision it (ahead of execution) and so one Dataset can back
// multiple plans.
func (p *DefaultPlanner) Plan(ctx context.Context, input Input) (*TestPlan, error) {
	if p.generator == nil {
		return nil, fmt.Errorf("planner requires a generator")
	}
	ds, err := p.generateDataset(ctx, input.Requirements)
	if err != nil {
		return nil, err
	}
	writeBase := input.WriteBaseURL
	if writeBase == "" {
		writeBase = input.BaseURL
	}
	return &TestPlan{
		Root:    buildProvisionPlan(writeBase, ds),
		Dataset: ds,
	}, nil
}

// generateDataset merges the Dataset produced for each requirement into one.
func (p *DefaultPlanner) generateDataset(ctx context.Context, requirements []model.DataRequirement) (*model.Dataset, error) {
	ds := &model.Dataset{
		Resources:     make(map[string]*model.ResourceInstance),
		Relationships: make([]model.Reference, 0),
	}
	for _, req := range requirements {
		part, err := p.generator.Generate(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("generate requirement %s: %w", req.ID, err)
		}
		if part == nil {
			continue
		}
		for id, inst := range part.Resources {
			if _, exists := ds.Resources[id]; !exists {
				ds.Resources[id] = inst
			}
		}
		ds.Relationships = append(ds.Relationships, part.Relationships...)
	}
	return ds, nil
}

// buildProvisionPlan lays out an executable AST that provisions every resource
// in ds in dependency order: independent resources at each dependency level run
// in Parallel, levels run in Sequence (targets first).
func buildProvisionPlan(baseURL string, ds *model.Dataset) ast.Node {
	root := &ast.Sequence{Steps: []ast.Node{}}
	for _, level := range dependencyLevels(ds) {
		parallel := &ast.Parallel{Steps: []ast.Node{}}
		for _, id := range level {
			inst := ds.Resources[id]
			if inst == nil {
				continue
			}
			parallel.Steps = append(parallel.Steps, provisionNode(baseURL, inst))
		}
		if len(parallel.Steps) > 0 {
			root.Steps = append(root.Steps, parallel)
		}
	}
	return root
}

// provisionNode builds a PUT request (with body) plus an assert that the
// resource was created.
func provisionNode(baseURL string, inst *model.ResourceInstance) ast.Node {
	url := instanceURL(baseURL, inst.ResourceType, inst.LocalID)
	return &ast.Sequence{Steps: []ast.Node{
		&ast.Request{
			Method:  "PUT",
			URL:     url,
			Headers: map[string]string{"Content-Type": "application/fhir+json"},
			Body:    inst.Resource,
		},
		&ast.Assert{
			Description:   "provision " + inst.ResourceType + "/" + inst.LocalID,
			RequirementID: "plan:" + inst.LocalID,
			Expression:    "status in [200,201]",
		},
	}}
}

func instanceURL(baseURL, resourceType, id string) string {
	base := strings.TrimRight(baseURL, "/")
	if base == "" {
		return "/" + resourceType + "/" + id
	}
	return base + "/" + resourceType + "/" + id
}

// dependencyLevels returns dataset resource ids grouped into topological levels
// using the recorded relationships: a resource belongs to the level after its
// most dependent target, so targets are always provisioned before dependents.
// Resources with no relationships land in level 0 and are mutually independent.
func dependencyLevels(ds *model.Dataset) [][]string {
	ids := make([]string, 0, len(ds.Resources))
	for id := range ds.Resources {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	levelOf := make(map[string]int, len(ids))
	for _, id := range ids {
		levelOf[id] = 0
	}
	for _, rel := range ds.Relationships {
		if _, ok := levelOf[rel.SourceID]; !ok {
			continue
		}
		if _, ok := levelOf[rel.TargetID]; !ok {
			continue
		}
		if targetLevel := levelOf[rel.TargetID]; levelOf[rel.SourceID] <= targetLevel {
			levelOf[rel.SourceID] = targetLevel + 1
		}
	}

	levels := make([][]string, 0)
	// Deterministic: emit levels in order, resources sorted within each level.
	maxLevel := 0
	for _, l := range levelOf {
		if l > maxLevel {
			maxLevel = l
		}
	}
	for l := 0; l <= maxLevel; l++ {
		bucket := make([]string, 0)
		for _, id := range ids {
			if levelOf[id] == l {
				bucket = append(bucket, id)
			}
		}
		if len(bucket) > 0 {
			levels = append(levels, bucket)
		}
	}
	return levels
}
