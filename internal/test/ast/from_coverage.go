package ast

import (
	"errors"
	"fmt"
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

	root := &Sequence{Steps: make([]Node, 0, len(plan.Requirements))}
	for _, req := range plan.Requirements {
		if req.ResourceType == "" {
			return nil, fmt.Errorf("coverage requirement %s missing resource type", req.ID)
		}

		caseSeq := &Sequence{Steps: []Node{
			&Request{
				Method: "POST",
				URL:    joinURL(options.BaseURL, req.ResourceType),
				Headers: map[string]string{
					"Content-Type":           "application/fhir+json",
					"X-Momus-Requirement-ID": req.ID,
				},
				Body: buildBodyTemplate(req),
			},
			buildRequirementAssert(req),
		}}

		root.Steps = append(root.Steps, caseSeq)
	}

	return &Plan{Version: "v1", Root: root}, nil
}

func joinURL(baseURL, resourceType string) string {
	if baseURL == "" {
		return "/" + strings.TrimPrefix(resourceType, "/")
	}
	return strings.TrimRight(baseURL, "/") + "/" + strings.TrimPrefix(resourceType, "/")
}

func buildBodyTemplate(req coverage.CoverageRequirement) map[string]any {
	return map[string]any{
		"resourceType": req.ResourceType,
		"_momus": map[string]any{
			"requirementId": req.ID,
			"profileUrl":    req.ProfileURL,
			"elementPath":   req.ElementPath,
			"variant":       string(req.Variant),
			"min":           req.Min,
			"max":           req.Max,
		},
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
