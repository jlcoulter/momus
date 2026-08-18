package ast

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/jlcoulter/momus/internal/test/coverage"
)

// BuildOptions controls AST construction behavior.
type BuildOptions struct {
	BaseURL string
}

// GenerateFromCoveragePlan maps coverage requirements into a concrete AST.
func GenerateFromCoveragePlan(plan *coverage.CoveragePlan, options BuildOptions) (*Plan, error) {
	if plan == nil {
		return nil, errors.New("coverage plan is required")
	}

	depPlan, err := coverage.PlanDependencies(plan.Requirements)
	if err != nil {
		return nil, err
	}

	byResource := make(map[string][]coverage.CoverageRequirement)
	for _, req := range plan.Requirements {
		if req.ResourceType == "" {
			return nil, fmt.Errorf("coverage requirement %s missing resource type", req.ID)
		}
		byResource[req.ResourceType] = append(byResource[req.ResourceType], req)
	}
	for resourceType := range byResource {
		sort.Slice(byResource[resourceType], func(i, j int) bool {
			return byResource[resourceType][i].ID < byResource[resourceType][j].ID
		})
	}

	root := &Sequence{Steps: make([]Node, 0)}
	for _, level := range depPlan.Levels {
		resourceNodes := make([]Node, 0, len(level))
		for _, resourceType := range level {
			resourceSeq := &Sequence{Steps: make([]Node, 0)}
			deps := depPlan.Dependencies[resourceType]

			resourceSeq.Steps = append(resourceSeq.Steps,
				&Request{
					Method: "POST",
					URL:    joinURL(options.BaseURL, resourceType),
					Headers: map[string]string{
						"Content-Type": "application/fhir+json",
					},
					Body: buildSetupBody(resourceType, deps),
				},
				&Assert{
					Description:   "setup create seed resource",
					RequirementID: "setup:" + resourceType,
					Expression:    "status in [200,201]",
				},
				&Capture{Name: resourceType + ".id", Path: "id"},
			)

			for _, req := range byResource[resourceType] {
				caseSeq := &Sequence{Steps: []Node{
					&Request{
						Method: "POST",
						URL:    joinURL(options.BaseURL, req.ResourceType),
						Headers: map[string]string{
							"Content-Type":           "application/fhir+json",
							"X-Momus-Requirement-ID": req.ID,
						},
						Body: buildBodyTemplate(req, deps),
					},
					buildRequirementAssert(req),
				}}
				resourceSeq.Steps = append(resourceSeq.Steps, caseSeq)
			}

			resourceNodes = append(resourceNodes, resourceSeq)
		}

		if len(resourceNodes) == 1 {
			root.Steps = append(root.Steps, resourceNodes[0])
			continue
		}
		root.Steps = append(root.Steps, &Parallel{Steps: resourceNodes})
	}

	return &Plan{Version: "v1", Root: root}, nil
}

func joinURL(baseURL, resourceType string) string {
	if baseURL == "" {
		return "/" + strings.TrimPrefix(resourceType, "/")
	}
	return strings.TrimRight(baseURL, "/") + "/" + strings.TrimPrefix(resourceType, "/")
}

func buildBodyTemplate(req coverage.CoverageRequirement, deps []string) map[string]any {
	body := baseBodyTemplate(req.ResourceType, deps)
	body["_momus"] = map[string]any{
		"requirementId": req.ID,
		"profileUrl":    req.ProfileURL,
		"elementPath":   req.ElementPath,
		"variant":       string(req.Variant),
		"min":           req.Min,
		"max":           req.Max,
	}
	if req.Variant == coverage.CoverageVariantMissingRequired {
		delete(body, "subject")
		delete(body, "patient")
	}
	return body
}

func buildSetupBody(resourceType string, deps []string) map[string]any {
	return baseBodyTemplate(resourceType, deps)
}

func baseBodyTemplate(resourceType string, deps []string) map[string]any {
	body := map[string]any{
		"resourceType": resourceType,
	}

	switch resourceType {
	case "Patient":
		body["name"] = []map[string]any{{"family": "Momus", "given": []string{"Seed"}}}
		body["gender"] = "unknown"
	case "Observation", "DiagnosticReport":
		body["status"] = "final"
		body["code"] = map[string]any{"text": "momus-code"}
	case "MedicationRequest", "CarePlan", "Condition", "Procedure", "ServiceRequest", "Goal", "CareTeam", "Encounter", "AllergyIntolerance", "Immunization", "DocumentReference", "Appointment":
		body["status"] = "active"
	}

	attachDependencyReferences(body, resourceType, deps)
	return body
}

func attachDependencyReferences(body map[string]any, resourceType string, deps []string) {
	for _, dep := range deps {
		switch dep {
		case "Patient":
			ref := map[string]any{"reference": dep + "/{{" + dep + ".id}}"}
			switch resourceType {
			case "AllergyIntolerance", "Immunization":
				body["patient"] = ref
			case "Appointment":
				body["participant"] = []map[string]any{{"actor": ref, "status": "accepted"}}
			default:
				body["subject"] = ref
			}
		case "Encounter":
			body["encounter"] = map[string]any{"reference": dep + "/{{" + dep + ".id}}"}
		case "Observation":
			body["result"] = []map[string]any{{"reference": dep + "/{{" + dep + ".id}}"}}
		}
	}
}

func buildRequirementAssert(req coverage.CoverageRequirement) *Assert {
	switch req.Variant {
	case coverage.CoverageVariantMissingRequired:
		return &Assert{
			Description:   "server rejects missing required element",
			RequirementID: req.ID,
			Expression:    "status in [400,422]",
		}
	default:
		return &Assert{
			Description:   "server accepts generated payload",
			RequirementID: req.ID,
			Expression:    "status in [200,201]",
		}
	}
}
