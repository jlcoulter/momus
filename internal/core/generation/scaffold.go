package generation

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/jlcoulter/momus/internal/core/ast"
	"github.com/jlcoulter/momus/internal/core/coverage"
)

// BuildOptions controls AST construction behavior.
type BuildOptions struct {
	BaseURL string
	// WriteBaseURL, when set, is used for write requests (PUT/PATCH/POST/DELETE)
	// instead of BaseURL, so resource creation can target a different endpoint
	// than read/search requests. When empty, write requests use BaseURL.
	WriteBaseURL string
	// Builder synthesizes payloads and search values for generated test cases.
	// It is the domain adapter (e.g. FHIR) that the generic framework calls.
	Builder PayloadBuilder
	// PreferredProfileURLsByResource, when non-empty, orders the profile URLs
	// used to synthesize a resource type's payloads.
	PreferredProfileURLsByResource map[string][]string
	// Strength is the interaction strength used when generating. When unset (or
	// < 2) it falls back to the coverage plan's own Strength, and finally to
	// strength 1 (one test per requirement). Strength >= 2 groups compatible
	// obligations into shared payloads selected by greedy set-cover.
	Strength int
	// Exhaustive populates optional (Min == 0) elements in addition to required
	// ones, with randomised presence, so generated payloads are fuller and more
	// realistic. When false, only required and contract-driven elements are
	// populated.
	Exhaustive bool
	// CapabilityResourceTypes, when non-nil, restricts the seed dataset (and the
	// transitive reference closure) to resource types the target server's
	// CapabilityStatement declares. When nil/empty, all registry types are
	// allowed.
	CapabilityResourceTypes map[string]struct{}
	// CapabilityProfiles, when non-nil, restricts the seed dataset to resource
	// profiles the target server's CapabilityStatement declares. When
	// nil/empty, all profiles are allowed.
	CapabilityProfiles map[string]struct{}
	// Progress, when non-nil, is invoked after each requirement is processed
	// during generation, with the number of requirements completed so far and
	// the total number of requirements. It is used to render a live progress
	// bar in the CLI.
	Progress func(done, total int)
}

// GenerateFromCoveragePlan maps coverage requirements into a concrete AST.
func GenerateFromCoveragePlan(plan *coverage.CoveragePlan, options BuildOptions) (*ast.Plan, error) {
	if plan == nil {
		return nil, errors.New("coverage plan is required")
	}
	if options.Builder == nil {
		return nil, errors.New("payload builder is required")
	}

	depPlan, err := options.Builder.DependencyPlan(plan, options.CapabilityResourceTypes)
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

	root := &ast.Sequence{Steps: make([]ast.Node, 0)}
	// Progress reporting: count requirement-bound cases as they are generated.
	doneReqs := 0
	totalReqs := len(plan.Requirements)
	progress := func() {
		if options.Progress == nil {
			return
		}
		doneReqs++
		options.Progress(doneReqs, totalReqs)
	}
	for _, level := range depPlan.Levels {
		resourceNodes := make([]ast.Node, 0, len(level))
		for _, resourceType := range level {
			// Skip types present only as seed dependencies (reachable via references
			// but with no coverage obligations of their own): they are provisioned by
			// the seed dataset but have no test cases to emit.
			if len(byResource[resourceType]) == 0 {
				continue
			}
			resourceSeq := &ast.Sequence{Steps: make([]ast.Node, 0)}
			deps := depPlan.Dependencies[resourceType]

			// Provisioning is a separate stage (BuildSetupDataset + provisioner): the
			// generated AST contains only test cases, which run against data already
			// provisioned on the server. Test cases that need seed data reference it by
			// its deterministic setup id (e.g. operations target "momus-setup-<Type>").
			for _, caseSeq := range options.Builder.BuildResourceCases(byResource[resourceType], plan, options, deps, progress) {
				resourceSeq.Steps = append(resourceSeq.Steps, caseSeq)
			}

			resourceNodes = append(resourceNodes, resourceSeq)
		}

		if len(resourceNodes) == 0 {
			continue
		}
		if len(resourceNodes) == 1 {
			root.Steps = append(root.Steps, resourceNodes[0])
			continue
		}
		root.Steps = append(root.Steps, &ast.Parallel{Steps: resourceNodes})
	}

	return &ast.Plan{Version: "v1", Root: root}, nil
}

// RequirementCount returns the number of requirement-bound Assertions in a
// generated plan, excluding setup scaffolding.
func RequirementCount(plan *ast.Plan) int {
	if plan == nil || plan.Root == nil {
		return 0
	}
	count := 0
	seen := make(map[string]struct{})
	var walk func(ast.Node)
	walk = func(node ast.Node) {
		switch n := node.(type) {
		case *ast.Sequence:
			for _, step := range n.Steps {
				walk(step)
			}
		case *ast.Parallel:
			for _, step := range n.Steps {
				walk(step)
			}
		case *ast.Assert:
			if strings.HasPrefix(n.RequirementID, "setup:") {
				return
			}
			// Count each obligation once even when its execution expands to
			// multiple asserts (e.g. a CRUD sequence).
			if _, ok := seen[n.RequirementID]; ok {
				return
			}
			seen[n.RequirementID] = struct{}{}
			count++
		}
	}
	walk(plan.Root)
	return count
}

func JoinURL(baseURL, resourceType string) string {
	if baseURL == "" {
		return "/" + strings.TrimPrefix(resourceType, "/")
	}
	return strings.TrimRight(baseURL, "/") + "/" + strings.TrimPrefix(resourceType, "/")
}

func JoinInstanceURL(baseURL, resourceType, id string) string {
	return JoinURL(baseURL, resourceType) + "/" + strings.TrimPrefix(id, "/")
}

// baseURLForMethod returns the base URL to use for a request of the given
// method: write methods (PUT/PATCH/POST/DELETE) use the write base URL when
// configured, while read/search (GET) requests use the read base URL.
func BaseURLForMethod(options BuildOptions, method string) string {
	if !ast.IsWriteMethod(method) {
		return options.BaseURL
	}
	if options.WriteBaseURL != "" {
		return options.WriteBaseURL
	}
	return options.BaseURL
}

func FirstProfileURL(profileURLs []string) string {
	for _, profileURL := range profileURLs {
		profileURL = strings.TrimSpace(profileURL)
		if profileURL != "" {
			return profileURL
		}
	}
	return ""
}

func OrderedProfilesForResource(resourceType, requestedProfileURL string, preferredByResource map[string][]string) []string {
	profiles := make([]string, 0)
	seen := make(map[string]struct{})
	appendProfile := func(profileURL string) {
		profileURL = strings.TrimSpace(profileURL)
		if profileURL == "" {
			return
		}
		if _, ok := seen[profileURL]; ok {
			return
		}
		seen[profileURL] = struct{}{}
		profiles = append(profiles, profileURL)
	}

	if len(preferredByResource) > 0 {
		for _, key := range []string{strings.TrimSpace(resourceType), strings.ToLower(strings.TrimSpace(resourceType))} {
			for _, profileURL := range preferredByResource[key] {
				appendProfile(profileURL)
			}
		}
	}
	appendProfile(requestedProfileURL)
	return profiles
}

func RequirementResourceID(req coverage.CoverageRequirement) string {
	resourceType := strings.TrimSpace(req.ResourceType)
	if resourceType == "" {
		resourceType = "resource"
	}
	variant := strings.TrimSpace(string(req.Variant))
	if variant == "" {
		variant = "case"
	}
	return SanitizeFHIRID("momus-" + resourceType + "-" + variant + "-" + strconv.Itoa(StableChecksum(req.ID)))
}

func SetupResourceID(resourceType string) string {
	return SanitizeFHIRID("momus-setup-" + resourceType)
}

func SanitizeFHIRID(value string) string {
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

func UniqueProfileURLs(reqs []coverage.CoverageRequirement) []string {
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

func Max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func BuildMeta(profileURLs []string) map[string]any {
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

func StableChecksum(value string) int {
	sum := 0
	for _, r := range value {
		sum = (sum*31 + int(r)) % 1000000
	}
	if sum < 0 {
		return -sum
	}
	return sum
}
