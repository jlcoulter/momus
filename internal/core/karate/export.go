package karate

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/jlcoulter/momus/internal/core/ast"
)

// Options configures Karate export. Reserved for future use; currently empty.
type Options struct{}

// FeatureFile represents a single Karate .feature file (one per resource type).
type FeatureFile struct {
	// Name is the resource type, used for the Feature header and the filename.
	Name string
	// Background holds seed-data provisioning steps shared by every scenario.
	Background []Step
	// Scenarios are the test cases for this resource type.
	Scenarios []Scenario
}

// Scenario is a single Karate Scenario (one per AST test case).
type Scenario struct {
	Name  string
	Tags  []string
	Steps []Step
}

// Step is a single Karate step. When DocString is non-empty it is emitted as a
// triple-quoted block immediately following the step line.
type Step struct {
	Keyword   string
	Text      string
	DocString string
}

// queryParam is a single URL query parameter decomposed from a request URL.
type queryParam struct {
	name  string
	value string
}

// caseData is an AST test case (a sequence of request/assert/capture leaves)
// grouped by its resource type, preserving global ordering.
type caseData struct {
	steps        []ast.Node
	resourceType string
	order        int
}

// Export converts a test-plan AST into Karate feature files, one per resource
// type. Read requests target the baseUrl variable and write requests
// (PUT/PATCH/POST/DELETE) target writeBaseUrl; both are configured in
// karate-config.js. opts is reserved for future use.
func Export(plan *ast.Plan, opts Options) ([]FeatureFile, error) {
	if plan == nil || plan.Root == nil {
		return nil, fmt.Errorf("plan is nil or empty")
	}
	_ = opts

	byResource := make(map[string][]*caseData)
	order := 0
	collectCases(plan.Root, &order, func(c *caseData) {
		rt := c.resourceType
		if rt == "" {
			rt = "Resource"
		}
		byResource[rt] = append(byResource[rt], c)
	})

	resourceTypes := make([]string, 0, len(byResource))
	for rt := range byResource {
		resourceTypes = append(resourceTypes, rt)
	}
	sort.Strings(resourceTypes)

	files := make([]FeatureFile, 0, len(resourceTypes))
	for _, rt := range resourceTypes {
		cases := byResource[rt]
		sort.SliceStable(cases, func(i, j int) bool {
			return cases[i].order < cases[j].order
		})
		scenarios, err := buildScenarios(cases)
		if err != nil {
			return nil, fmt.Errorf("resource %s: %w", rt, err)
		}
		files = append(files, FeatureFile{
			Name:       rt,
			Background: buildBackground(plan, rt),
			Scenarios:  scenarios,
		})
	}
	return files, nil
}

// collectCases walks the AST and emits every test case (a sequence whose direct
// children are request/assert/capture leaves) via emit, with a global order.
func collectCases(node ast.Node, order *int, emit func(*caseData)) {
	switch n := node.(type) {
	case *ast.Sequence:
		if isCase(n) {
			c := &caseData{steps: n.Steps, order: *order}
			*order++
			c.resourceType = resourceTypeFor(n.Steps)
			emit(c)
			return
		}
		for _, s := range n.Steps {
			collectCases(s, order, emit)
		}
	case *ast.Parallel:
		for _, s := range n.Steps {
			collectCases(s, order, emit)
		}
	}
}

// isCase reports whether a sequence's direct children are all leaf nodes.
func isCase(n *ast.Sequence) bool {
	if len(n.Steps) == 0 {
		return false
	}
	for _, s := range n.Steps {
		switch s.(type) {
		case *ast.Request, *ast.Assert, *ast.Capture:
		default:
			return false
		}
	}
	return true
}

// resourceTypeFor derives a case's resource type from its first assertion trace.
func resourceTypeFor(steps []ast.Node) string {
	for _, s := range steps {
		if a, ok := s.(*ast.Assert); ok && a.Trace != nil && a.Trace.ResourceType != "" {
			return a.Trace.ResourceType
		}
	}
	return ""
}

// buildScenarios converts cases into Scenarios, propagating expression errors.
func buildScenarios(cases []*caseData) ([]Scenario, error) {
	scenarios := make([]Scenario, 0, len(cases))
	for _, c := range cases {
		steps, err := buildSteps(c)
		if err != nil {
			return nil, err
		}
		scenarios = append(scenarios, Scenario{
			Name:  scenarioName(c),
			Tags:  scenarioTags(c),
			Steps: steps,
		})
	}
	return scenarios, nil
}

// buildSteps converts a case's leaf nodes into Karate steps.
func buildSteps(c *caseData) ([]Step, error) {
	var steps []Step
	for _, node := range c.steps {
		switch n := node.(type) {
		case *ast.Request:
			steps = append(steps, requestSteps(n, c.resourceType)...)
		case *ast.Assert:
			step, err := assertStep(n)
			if err != nil {
				return nil, err
			}
			steps = append(steps, step)
		case *ast.Capture:
			steps = append(steps, captureStep(n))
		}
	}
	return steps, nil
}

// requestSteps converts a Request node into url/path/param/request/method steps.
func requestSteps(r *ast.Request, resourceType string) []Step {
	baseVar := "baseUrl"
	if ast.IsWriteMethod(r.Method) {
		baseVar = "writeBaseUrl"
	}

	rel, params := splitURLAtResource(r.URL, resourceType)
	steps := []Step{
		{Keyword: "Given", Text: "url " + baseVar},
		{Keyword: "And", Text: "path '" + rel + "'"},
	}
	for _, p := range params {
		steps = append(steps, Step{Keyword: "And", Text: "param " + p.name + " = '" + p.value + "'"})
	}
	// Request body (when present) plus Content-Type header. A nil or empty map
	// body is treated as absent.
	if hasBody(r.Body) {
		bodyJSON, err := json.MarshalIndent(r.Body, "", "  ")
		if err == nil {
			steps = append(steps, Step{Keyword: "And", Text: "request", DocString: string(bodyJSON)})
		}
	}
	if ct, ok := r.Headers["Content-Type"]; ok && ct != "" {
		steps = append(steps, Step{Keyword: "And", Text: "header Content-Type = '" + ct + "'"})
	}
	steps = append(steps, Step{Keyword: "When", Text: "method " + r.Method})
	return steps
}

// assertStep converts an Assert node into a Karate Then step.
func assertStep(a *ast.Assert) (Step, error) {
	translated, err := TranslateExpression(a.Expression)
	if err != nil {
		return Step{}, fmt.Errorf("translate assertion %q: %w", a.Expression, err)
	}
	return Step{Keyword: "Then", Text: translated}, nil
}

// captureStep converts a Capture node into a Karate def step.
func captureStep(c *ast.Capture) Step {
	return Step{Keyword: "And", Text: "def " + sanitizeVariable(c.Name) + " = response." + c.Path}
}

// scenarioName derives a scenario name from the first assertion's description.
func scenarioName(c *caseData) string {
	for _, n := range c.steps {
		if a, ok := n.(*ast.Assert); ok {
			if a.Description != "" {
				return a.Description
			}
			if a.RequirementID != "" {
				return a.RequirementID
			}
		}
	}
	return "unnamed scenario"
}

// scenarioTags derives @requirement/@domain/@variant tags from the case's traces.
func scenarioTags(c *caseData) []string {
	var tags []string
	seen := make(map[string]bool)
	add := func(key string) {
		if key == "" || seen[key] {
			return
		}
		seen[key] = true
		tags = append(tags, key)
	}
	for _, n := range c.steps {
		a, ok := n.(*ast.Assert)
		if !ok || a.Trace == nil {
			continue
		}
		if a.RequirementID != "" {
			add("@requirement:" + a.RequirementID)
		}
		if a.Trace.Domain != "" {
			add("@domain:" + a.Trace.Domain)
		}
		if a.Trace.Variant != "" {
			add("@variant:" + a.Trace.Variant)
		}
	}
	sort.Strings(tags)
	return tags
}

// buildBackground provisions the plan's seed resources of a resource type as a
// Background block so the data sits near the tests that reference it.
func buildBackground(plan *ast.Plan, resourceType string) []Step {
	if plan.Dataset == nil {
		return nil
	}
	var keys []string
	seen := make(map[string]bool)
	for key, inst := range plan.Dataset.Resources {
		if inst == nil || inst.ResourceType != resourceType {
			continue
		}
		id := inst.LocalID
		if id == "" {
			id = key
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return nil
	}
	sort.Strings(keys)

	steps := []Step{{Keyword: "Given", Text: "url writeBaseUrl"}}
	for _, key := range keys {
		inst := plan.Dataset.Resources[key]
		steps = append(steps, Step{Keyword: "And", Text: "path '" + inst.ResourceType + "/" + inst.LocalID + "'"})
		if inst.Resource != nil {
			if bodyJSON, err := json.MarshalIndent(inst.Resource, "", "  "); err == nil {
				steps = append(steps, Step{Keyword: "And", Text: "request", DocString: string(bodyJSON)})
			}
		}
		steps = append(steps,
			Step{Keyword: "When", Text: "method PUT"},
			Step{Keyword: "Then", Text: "assert responseStatus == 200 || responseStatus == 201"},
		)
	}
	return steps
}

// hasBody reports whether a request body is present and non-empty.
func hasBody(body any) bool {
	if body == nil {
		return false
	}
	if m, ok := body.(map[string]any); ok && len(m) == 0 {
		return false
	}
	return true
}

// splitURLAtResource decomposes a fully-resolved request URL into the path
// relative to the plan's base (everything from the resource-type segment
// onward) plus any query parameters. Splitting at the resource-type boundary
// is robust: it does not depend on base-URL detection. When resourceType is
// empty the whole path (minus query) is used.
func splitURLAtResource(fullURL, resourceType string) (rel string, params []queryParam) {
	u := fullURL
	if qIdx := strings.IndexByte(u, '?'); qIdx >= 0 {
		for _, pair := range strings.Split(u[qIdx+1:], "&") {
			if pair == "" {
				continue
			}
			if eq := strings.IndexByte(pair, '='); eq < 0 {
				params = append(params, queryParam{name: pair})
			} else {
				params = append(params, queryParam{name: pair[:eq], value: pair[eq+1:]})
			}
		}
		u = u[:qIdx]
	}
	u = strings.Trim(u, "/")

	if resourceType != "" {
		// The resource-type segment is the first path element matching the case's
		// resource type (e.g. ".../fhir/Patient/123" -> "Patient/123").
		segments := strings.Split(u, "/")
		for i, seg := range segments {
			if seg == resourceType {
				return strings.Join(segments[i:], "/"), params
			}
		}
	}
	return u, params
}
