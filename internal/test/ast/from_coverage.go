package ast

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"

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
			resourceProfiles := uniqueProfileURLs(byResource[resourceType])

			resourceSeq.Steps = append(resourceSeq.Steps,
				&Request{
					Method: "PUT",
					URL:    joinInstanceURL(options.BaseURL, resourceType, setupResourceID(resourceType)),
					Headers: map[string]string{
						"Content-Type": "application/fhir+json",
					},
					Body: buildSetupBody(resourceType, setupResourceID(resourceType), resourceProfiles, deps),
				},
				&Assert{
					Description:   "setup create seed resource",
					RequirementID: "setup:" + resourceType,
					Expression:    "status in [200,201]",
				},
				&Capture{Name: resourceType + ".id", Path: "id"},
			)

			for _, req := range byResource[resourceType] {
				requestID := requirementResourceID(req)
				caseSeq := &Sequence{Steps: []Node{
					&Request{
						Method: "PUT",
						URL:    joinInstanceURL(options.BaseURL, req.ResourceType, requestID),
						Headers: map[string]string{
							"Content-Type":           "application/fhir+json",
							"X-Momus-Requirement-ID": req.ID,
						},
						Body: buildBodyTemplate(req, requestID, deps),
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

func joinInstanceURL(baseURL, resourceType, id string) string {
	return joinURL(baseURL, resourceType) + "/" + strings.TrimPrefix(id, "/")
}

func buildBodyTemplate(req coverage.CoverageRequirement, id string, deps []string) map[string]any {
	body := baseBodyTemplate(req.ResourceType, id, []string{req.ProfileURL}, deps)
	if req.Variant == coverage.CoverageVariantMissingRequired {
		delete(body, "subject")
		delete(body, "patient")
	}
	return body
}

func buildSetupBody(resourceType, id string, profileURLs, deps []string) map[string]any {
	return baseBodyTemplate(resourceType, id, profileURLs, deps)
}

func baseBodyTemplate(resourceType, id string, profileURLs, deps []string) map[string]any {
	body := map[string]any{
		"resourceType": resourceType,
		"id":           id,
	}
	if meta := buildMeta(profileURLs); meta != nil {
		body["meta"] = meta
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

func buildMeta(profileURLs []string) map[string]any {
	profiles := make([]any, 0, len(profileURLs))
	seen := make(map[string]struct{}, len(profileURLs))
	for _, profileURL := range profileURLs {
		profileURL = strings.TrimSpace(profileURL)
		if profileURL == "" {
			continue
		}
		if _, ok := seen[profileURL]; ok {
			continue
		}
		seen[profileURL] = struct{}{}
		profiles = append(profiles, profileURL)
	}
	if len(profiles) == 0 {
		return nil
	}
	return map[string]any{"profile": profiles}
}

func uniqueProfileURLs(reqs []coverage.CoverageRequirement) []string {
	profiles := make([]string, 0, len(reqs))
	seen := make(map[string]struct{}, len(reqs))
	for _, req := range reqs {
		profileURL := strings.TrimSpace(req.ProfileURL)
		if profileURL == "" {
			continue
		}
		if _, ok := seen[profileURL]; ok {
			continue
		}
		seen[profileURL] = struct{}{}
		profiles = append(profiles, profileURL)
	}
	return profiles
}

func setupResourceID(resourceType string) string {
	return sanitizeFHIRID("momus-setup-" + resourceType)
}

func requirementResourceID(req coverage.CoverageRequirement) string {
	resourceType := strings.TrimSpace(req.ResourceType)
	if resourceType == "" {
		resourceType = "resource"
	}
	variant := strings.TrimSpace(string(req.Variant))
	if variant == "" {
		variant = "case"
	}
	return sanitizeFHIRID("momus-" + resourceType + "-" + variant + "-" + strconv.Itoa(stableChecksum(req.ID)))
}

func sanitizeFHIRID(value string) string {
	if value == "" {
		return "momus-id"
	}
	var b strings.Builder
	prevHyphen := false
	for _, r := range value {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
			prevHyphen = false
		case r == '-' || r == '.':
			if !prevHyphen {
				b.WriteRune(r)
				prevHyphen = true
			}
		default:
			if !prevHyphen {
				b.WriteRune('-')
				prevHyphen = true
			}
		}
	}
	out := strings.Trim(b.String(), "-.")
	if out == "" {
		return "momus-id"
	}
	if len(out) > 64 {
		return out[:64]
	}
	return out
}

func stableChecksum(value string) int {
	sum := 0
	for _, r := range value {
		sum = (sum*31 + int(r)) % 1000000
	}
	if sum < 0 {
		return -sum
	}
	return sum
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
