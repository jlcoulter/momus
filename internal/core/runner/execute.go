package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jlcoulter/momus/internal/core/assertions"
	"github.com/jlcoulter/momus/internal/core/ast"
	"github.com/jlcoulter/momus/internal/core/tracing"
)

// ExecuteOptions configures AST execution.
type ExecuteOptions struct {
	BaseURL string
	// WriteBaseURL, when set, is used for write requests (PUT/PATCH/POST/DELETE)
	// instead of BaseURL, so resource creation can target a different endpoint
	// than read/search requests. When empty, write requests use BaseURL.
	WriteBaseURL  string
	HTTPClient    *http.Client
	BearerToken   string
	BasicUsername string
	BasicPassword string
	// WriteBasicUsername and WriteBasicPassword, when set, are used for HTTP
	// basic auth on write requests (PUT/PATCH/POST/DELETE) that target the write
	// base URL, overriding the general BasicUsername/BasicPassword for those
	// requests.
	WriteBasicUsername string
	WriteBasicPassword string
	IncludeDebug       bool
	// Tracer, when set, logs every HTTP request and response as it is made
	// (used for --debug request/response tracing).
	Tracer *tracing.Tracer
	// PreCreated lists resource keys ("Type/id") that already exist on the server
	// before execution, e.g. seed resources provisioned ahead of the run. They are
	// treated as created so setup-reference validation passes for test cases that
	// reference them.
	PreCreated map[string]struct{}
	// Progress, when set, is invoked after each test case completes with the
	// number of cases completed so far and the total number of cases. It is used
	// to render a progress bar in the CLI. total is the number of requirement
	// cases (setup cases are not counted toward it).
	Progress func(done, total int)
}

const maxDebugBodyBytes = 4096

// CaseDebug captures request/response context for troubleshooting failed assertions.
type CaseDebug struct {
	RequestMethod string `json:"requestMethod,omitempty"`
	RequestURL    string `json:"requestUrl,omitempty"`
	RequestBody   string `json:"requestBody,omitempty"`
	StatusCode    int    `json:"statusCode,omitempty"`
	ResponseBody  string `json:"responseBody,omitempty"`
}

// CaseResult is the outcome of an assertion-bound test case.
type CaseResult struct {
	RequirementID string `json:"requirementId"`
	Description   string `json:"description"`
	Expression    string `json:"expression"`
	Passed        bool   `json:"passed"`
	StatusCode    int    `json:"statusCode,omitempty"`
	Error         string `json:"error,omitempty"`
	// Trace is the coverage requirement this case is bound to, providing
	// end-to-end traceability from the executed test to its source constraint.
	Trace *ast.Trace `json:"requirement,omitempty"`
	// FailureFingerprint points to the aggregated failure signature for this case.
	FailureFingerprint string     `json:"failureFingerprint,omitempty"`
	Debug              *CaseDebug `json:"debug,omitempty"`
}

// FailureSignature summarizes a recurring validation failure shape.
type FailureSignature struct {
	Signature            string `json:"signature"`
	Count                int    `json:"count"`
	StatusCode           int    `json:"statusCode,omitempty"`
	Severity             string `json:"severity,omitempty"`
	Code                 string `json:"code,omitempty"`
	Expression           string `json:"expression,omitempty"`
	Location             string `json:"location,omitempty"`
	Diagnostics          string `json:"diagnostics,omitempty"`
	RootCauseCategory    string `json:"rootCauseCategory,omitempty"`
	Confidence           string `json:"confidence,omitempty"`
	TriageRole           string `json:"triageRole,omitempty"`
	ExampleRequirementID string `json:"exampleRequirementId,omitempty"`
	ExampleDescription   string `json:"exampleDescription,omitempty"`
}

// DiagnosticsSummary contains aggregated failure diagnostics for triaging runs.
type DiagnosticsSummary struct {
	OperationOutcomeFailures int                `json:"operationOutcomeFailures,omitempty"`
	TopSignatures            []FailureSignature `json:"topSignatures,omitempty"`
	LikelyAuthFailure        bool               `json:"likelyAuthFailure,omitempty"`
	Hint                     string             `json:"hint,omitempty"`
}

// Report summarizes execution outcomes.
type Report struct {
	Total       int                 `json:"total"`
	Passed      int                 `json:"passed"`
	Failed      int                 `json:"failed"`
	Cases       []CaseResult        `json:"cases"`
	Diagnostics *DiagnosticsSummary `json:"diagnostics,omitempty"`
	// Triage, when present, rolls failed cases into broken-test vs server-defect
	// buckets to help triage large runs.
	Triage *TriageSummary `json:"triage,omitempty"`
}

// Execute runs the AST and returns a structured report.
func Execute(ctx context.Context, plan ast.Node, options ExecuteOptions) (*Report, error) {
	if plan == nil {
		return nil, fmt.Errorf("plan is required")
	}

	client := options.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	exec := &executor{
		ctx:                ctx,
		client:             client,
		baseURL:            strings.TrimRight(options.BaseURL, "/"),
		writeBaseURL:       strings.TrimRight(options.WriteBaseURL, "/"),
		bearerToken:        options.BearerToken,
		basicUsername:      options.BasicUsername,
		basicPassword:      options.BasicPassword,
		writeBasicUsername: options.WriteBasicUsername,
		writeBasicPassword: options.WriteBasicPassword,
		includeDebug:       options.IncludeDebug,
		tracer:             options.Tracer,
		progress:           options.Progress,
		progressTotal:      countRequirementAsserts(plan),
		report:             &Report{Cases: make([]CaseResult, 0)},
		variables:          make(map[string]any),
		created:            cloneStringSet(options.PreCreated),
		failuresBySig:      make(map[string]*FailureSignature),
	}

	err := exec.runNode(plan)
	exec.report.Total = len(exec.report.Cases)
	exec.report.Diagnostics = exec.buildDiagnosticsSummary()
	exec.report.Triage = buildTriageSummary(exec.report.Cases)
	return exec.report, err
}

type executor struct {
	ctx                context.Context
	client             *http.Client
	baseURL            string
	writeBaseURL       string
	bearerToken        string
	basicUsername      string
	basicPassword      string
	writeBasicUsername string
	writeBasicPassword string
	includeDebug       bool
	tracer             *tracing.Tracer
	progress           func(done, total int)
	progressTotal      int
	progressDone       int
	report             *Report
	lastResult         assertions.Result
	hasResult          bool
	lastErr            error
	lastDebug          *CaseDebug
	errorRecorded      bool
	variables          map[string]any
	created            map[string]struct{}
	failuresBySig      map[string]*FailureSignature
	ooFailures         int
}

var variablePattern = regexp.MustCompile(`\{\{([a-zA-Z0-9_.-]+)\}\}`)

// cloneStringSet returns a copy of a string set, or an empty set when src is nil.
func cloneStringSet(src map[string]struct{}) map[string]struct{} {
	out := make(map[string]struct{}, len(src))
	for k := range src {
		out[k] = struct{}{}
	}
	return out
}

// countRequirementAsserts returns the number of non-setup Assert nodes in the
// plan. This is the total used for progress reporting.
func countRequirementAsserts(node ast.Node) int {
	count := 0
	var walk func(ast.Node)
	walk = func(n ast.Node) {
		switch node := n.(type) {
		case *ast.Sequence:
			for _, step := range node.Steps {
				walk(step)
			}
		case *ast.Parallel:
			for _, step := range node.Steps {
				walk(step)
			}
		case *ast.Assert:
			if !strings.HasPrefix(node.RequirementID, "setup:") {
				count++
			}
		}
	}
	walk(node)
	return count
}

// notifyProgress invokes the progress callback (when set) with the number of
// requirement cases completed so far and the total. It is called after each
// case is recorded.
func (e *executor) notifyProgress() {
	if e.progress == nil {
		return
	}
	e.progressDone++
	e.progress(e.progressDone, e.progressTotal)
}

func (e *executor) runNode(node ast.Node) error {
	switch n := node.(type) {
	case *ast.Sequence:
		for _, step := range n.Steps {
			if err := e.runNode(step); err != nil {
				return err
			}
		}
		return nil
	case *ast.Parallel:
		return e.runParallel(n.Steps)
	case *ast.Request:
		res, err := e.executeRequest(n)
		e.lastResult = res
		e.hasResult = err == nil
		e.lastErr = err
		if err != nil {
			// Record the failure immediately so a standalone/trailing request
			// error is not silently swallowed. A following Assert observes the
			// error via lastErr and attributes this case to itself instead of
			// adding a duplicate.
			e.report.Failed++
			e.report.Cases = append(e.report.Cases, CaseResult{
				Passed: false,
				Error:  err.Error(),
				Debug:  e.copyDebugIfEnabled(),
			})
			e.errorRecorded = true
			e.notifyProgress()
		}
		return nil
	case *ast.Capture:
		e.capture(n)
		return nil
	case *ast.Assert:
		e.evaluateAssert(n)
		return nil
	default:
		return fmt.Errorf("unsupported AST node type %T", node)
	}
}

// runParallel executes each branch of a Parallel node concurrently. Each branch
// runs in its own executor with an isolated variable/created scope and its own
// report, so concurrent captures and request-state do not race. Results are
// merged back into the parent deterministically, in branch order.
func (e *executor) runParallel(steps []ast.Node) error {
	if len(steps) == 0 {
		return nil
	}
	children := make([]*executor, len(steps))
	for i := range steps {
		children[i] = e.child()
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	for i := range steps {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := children[i].runNode(steps[i])
			mu.Lock()
			if err != nil && firstErr == nil {
				firstErr = err
			}
			mu.Unlock()
		}(i)
	}
	wg.Wait()

	for _, child := range children {
		e.merge(child)
	}
	if firstErr != nil {
		// Record a structural error as a failed case instead of aborting, so
		// the results already merged from the other branches are preserved.
		e.report.Failed++
		e.report.Cases = append(e.report.Cases, CaseResult{
			Passed:      false,
			Description: "structural error",
			Error:       firstErr.Error(),
		})
	}
	return nil
}

// child returns an isolated execution context for a parallel branch: it shares
// the immutable configuration and context, snapshots the current variables and
// created resources, and starts with a fresh request state and report.
func (e *executor) child() *executor {
	variables := make(map[string]any, len(e.variables))
	for k, v := range e.variables {
		variables[k] = v
	}
	created := make(map[string]struct{}, len(e.created))
	for k := range e.created {
		created[k] = struct{}{}
	}
	return &executor{
		ctx:                e.ctx,
		client:             e.client,
		baseURL:            e.baseURL,
		writeBaseURL:       e.writeBaseURL,
		bearerToken:        e.bearerToken,
		basicUsername:      e.basicUsername,
		basicPassword:      e.basicPassword,
		writeBasicUsername: e.writeBasicUsername,
		writeBasicPassword: e.writeBasicPassword,
		includeDebug:       e.includeDebug,
		tracer:             e.tracer,
		report:             &Report{Cases: make([]CaseResult, 0)},
		variables:          variables,
		created:            created,
		failuresBySig:      make(map[string]*FailureSignature),
	}
}

// merge folds a parallel branch's results back into the parent deterministically
// (in branch order): cases/pass/fail counts, captured variables, created
// resources, and failure-signature diagnostics. The parent's request state is
// taken from the last branch, matching sequential ordering.
func (e *executor) merge(child *executor) {
	e.report.Cases = append(e.report.Cases, child.report.Cases...)
	e.report.Passed += child.report.Passed
	e.report.Failed += child.report.Failed

	for k, v := range child.variables {
		e.variables[k] = v
	}
	for k := range child.created {
		e.created[k] = struct{}{}
	}
	for k, sig := range child.failuresBySig {
		if existing, ok := e.failuresBySig[k]; ok {
			existing.Count += sig.Count
			if existing.ExampleRequirementID == "" {
				existing.ExampleRequirementID = sig.ExampleRequirementID
			}
			continue
		}
		e.failuresBySig[k] = sig
	}
	e.ooFailures += child.ooFailures

	// The last branch's request state wins, matching the previous in-order run.
	e.lastResult = child.lastResult
	e.hasResult = child.hasResult
	e.lastErr = child.lastErr
	e.lastDebug = child.lastDebug
}

func (e *executor) executeRequest(reqNode *ast.Request) (assertions.Result, error) {
	url := reqNode.URL
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		base := e.baseURLForMethod(reqNode.Method)
		if base == "" {
			return assertions.Result{}, fmt.Errorf("request URL %q is relative and no base URL is configured", reqNode.URL)
		}
		url = base + "/" + strings.TrimPrefix(reqNode.URL, "/")
	}

	var bodyReader io.Reader
	var requestBodyBytes []byte
	var resolvedBody any
	if reqNode.Body != nil {
		resolvedBody = resolveTemplates(reqNode.Body, e.variables)
		if isSetupRequest(reqNode.URL) {
			if err := validateSetupReferenceResolvable(resolvedBody, e.created); err != nil {
				return assertions.Result{}, err
			}
		}
		body, err := json.Marshal(resolvedBody)
		if err != nil {
			return assertions.Result{}, fmt.Errorf("marshal request body: %w", err)
		}
		requestBodyBytes = body
		bodyReader = bytes.NewReader(body)
	}

	e.lastDebug = &CaseDebug{
		RequestMethod: reqNode.Method,
		RequestURL:    url,
		RequestBody:   truncateDebugBody(requestBodyBytes),
	}

	req, err := http.NewRequestWithContext(e.ctx, reqNode.Method, url, bodyReader)
	if err != nil {
		return assertions.Result{}, fmt.Errorf("%s %s: create request: %w", reqNode.Method, url, err)
	}
	for k, v := range reqNode.Headers {
		req.Header.Set(k, v)
	}
	e.applyRequestAuth(req, reqNode.Method)

	var reqSeq int
	if e.tracer != nil {
		reqSeq = e.tracer.LogRequest(req, requestBodyBytes)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return assertions.Result{}, fmt.Errorf("%s %s: %w", reqNode.Method, url, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return assertions.Result{StatusCode: resp.StatusCode, Headers: resp.Header}, fmt.Errorf("%s %s: read response body: %w", reqNode.Method, url, err)
	}

	if e.tracer != nil {
		e.tracer.LogResponse(req, reqSeq, resp.StatusCode, resp.Header, body)
	}

	if e.lastDebug != nil {
		e.lastDebug.StatusCode = resp.StatusCode
		e.lastDebug.ResponseBody = truncateDebugBody(body)
	}

	variables := map[string]any{}
	if bodyMap, ok := resolvedBody.(map[string]any); ok {
		if resourceType, ok := bodyMap["resourceType"].(string); ok && resourceType != "" {
			id := extractResourceID(body)
			if id == "" {
				id, _ = bodyMap["id"].(string)
			}
			if id != "" {
				variables[resourceType+".id"] = id
				e.variables[resourceType+".id"] = id
				if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
					e.created[resourceType+"/"+id] = struct{}{}
				}
			}
		}
	}

	return assertions.Result{StatusCode: resp.StatusCode, Body: body, Headers: resp.Header, Variables: variables}, nil
}

// baseURLForMethod returns the base URL to use for a request of the given
// method: write methods (PUT/PATCH/POST/DELETE) use the write base URL when
// configured, while read/search (GET) requests use the read base URL.
func (e *executor) baseURLForMethod(method string) string {
	if !ast.IsWriteMethod(method) {
		return e.baseURL
	}
	if e.writeBaseURL != "" {
		return e.writeBaseURL
	}
	return e.baseURL
}

func isSetupRequest(url string) bool {
	return strings.Contains(url, "momus-setup-")
}

func (e *executor) evaluateAssert(assertNode *ast.Assert) {
	result := CaseResult{
		RequirementID: assertNode.RequirementID,
		Description:   assertNode.Description,
		Expression:    assertNode.Expression,
		Trace:         assertNode.Trace,
	}

	if e.lastErr != nil {
		if e.errorRecorded {
			// The request error was already recorded as a failed case by the
			// Request node. Attribute it to this assertion by filling in the
			// assertion metadata, then clear the flag so a subsequent assertion
			// records its own case.
			if n := len(e.report.Cases); n > 0 {
				last := &e.report.Cases[n-1]
				last.RequirementID = assertNode.RequirementID
				last.Description = assertNode.Description
				last.Expression = assertNode.Expression
				last.Trace = assertNode.Trace
				last.StatusCode = e.lastResult.StatusCode
			}
			e.errorRecorded = false
			return
		}
		result.Passed = false
		result.StatusCode = e.lastResult.StatusCode
		result.Error = e.lastErr.Error()
		result.Debug = e.copyDebugIfEnabled()
		e.report.Failed++
		e.report.Cases = append(e.report.Cases, result)
		e.notifyProgress()
		return
	}
	if !e.hasResult {
		result.Passed = false
		result.Error = "no request result available for assertion"
		result.Debug = e.copyDebugIfEnabled()
		e.report.Failed++
		e.report.Cases = append(e.report.Cases, result)
		e.notifyProgress()
		return
	}

	assertion, err := assertions.ParseExpression(assertNode.Expression)
	if err != nil {
		result.Passed = false
		result.StatusCode = e.lastResult.StatusCode
		result.Error = err.Error()
		result.Debug = e.copyDebugIfEnabled()
		e.report.Failed++
		e.report.Cases = append(e.report.Cases, result)
		e.notifyProgress()
		return
	}

	result.StatusCode = e.lastResult.StatusCode
	if err := assertion.Evaluate(e.ctx, e.lastResult); err != nil {
		if warningOnlySuccess(assertNode.Expression, e.lastResult) {
			result.Passed = true
			result.Debug = e.copyDebugIfEnabled()
			e.report.Passed++
		} else {
			result.Passed = false
			result.Error = err.Error()
			e.recordFailureSignature(&result, assertNode, e.lastResult.StatusCode, e.lastResult.Body)
			result.Debug = e.copyDebugIfEnabled()
			e.report.Failed++
		}
	} else {
		result.Passed = true
		result.Debug = e.copyDebugIfEnabled()
		e.report.Passed++
	}
	e.report.Cases = append(e.report.Cases, result)
	e.notifyProgress()
}

func (e *executor) copyDebugIfEnabled() *CaseDebug {
	if !e.includeDebug || e.lastDebug == nil {
		return nil
	}
	copy := *e.lastDebug
	return &copy
}

func (e *executor) capture(captureNode *ast.Capture) {
	if captureNode == nil || captureNode.Name == "" {
		return
	}
	if e.lastErr != nil || !e.hasResult {
		return
	}
	var val string
	switch captureNode.Path {
	case "id":
		val = extractResourceID(e.lastResult.Body)
	}
	if val != "" {
		e.variables[captureNode.Name] = val
	}
}

func extractResourceID(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	id, _ := payload["id"].(string)
	return id
}

func resolveTemplates(v any, vars map[string]any) any {
	switch typed := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for k, child := range typed {
			out[k] = resolveTemplates(child, vars)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, child := range typed {
			out[i] = resolveTemplates(child, vars)
		}
		return out
	case []map[string]any:
		out := make([]any, len(typed))
		for i := range typed {
			out[i] = resolveTemplates(typed[i], vars)
		}
		return out
	case string:
		return replaceTemplateString(typed, vars)
	default:
		return v
	}
}

func replaceTemplateString(in string, vars map[string]any) string {
	return variablePattern.ReplaceAllStringFunc(in, func(token string) string {
		matches := variablePattern.FindStringSubmatch(token)
		if len(matches) != 2 {
			return token
		}
		if value, ok := vars[matches[1]]; ok {
			return fmt.Sprintf("%v", value)
		}
		return token
	})
}

func (e *executor) applyRequestAuth(req *http.Request, method string) {
	if req == nil {
		return
	}
	if req.Header.Get("Authorization") != "" {
		return
	}
	// Write requests that target the write base URL use the write-specific basic
	// auth credentials when provided, overriding the general credentials.
	if ast.IsWriteMethod(method) && e.writeBaseURL != "" && (e.writeBasicUsername != "" || e.writeBasicPassword != "") {
		req.SetBasicAuth(e.writeBasicUsername, e.writeBasicPassword)
		return
	}
	if e.bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+e.bearerToken)
		return
	}
	if e.basicUsername != "" || e.basicPassword != "" {
		req.SetBasicAuth(e.basicUsername, e.basicPassword)
	}
}

func truncateDebugBody(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	if len(body) <= maxDebugBodyBytes {
		return string(body)
	}
	return fmt.Sprintf("%s... [truncated %d bytes]", string(body[:maxDebugBodyBytes]), len(body)-maxDebugBodyBytes)
}

func validateSetupReferenceResolvable(value any, created map[string]struct{}) error {
	switch typed := value.(type) {
	case map[string]any:
		if ref, ok := typed["reference"].(string); ok {
			if err := validateSetupReference(ref, created); err != nil {
				return err
			}
		}
		for _, child := range typed {
			if err := validateSetupReferenceResolvable(child, created); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range typed {
			if err := validateSetupReferenceResolvable(child, created); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateSetupReference(reference string, created map[string]struct{}) error {
	reference = strings.TrimSpace(reference)
	if reference == "" || strings.Contains(reference, "{{") {
		return nil
	}
	parts := strings.SplitN(reference, "/", 2)
	if len(parts) != 2 {
		return nil
	}
	resourceType := strings.TrimSpace(parts[0])
	resourceID := strings.TrimSpace(parts[1])
	if resourceType == "" || !strings.HasPrefix(resourceID, "momus-setup-") {
		return nil
	}
	key := resourceType + "/" + resourceID
	if _, ok := created[key]; ok {
		return nil
	}
	return fmt.Errorf("unresolved setup reference %q: %s has not been created yet; enforce setup dependency ordering", reference, key)
}

// successStatusExpression is the assertion expression emitted for a successful
// 2xx response by the OpenAPI generator. warningOnlySuccess recognizes it (and
// the legacy two-code form) so a warning-only 412 is treated as a pass.
const successStatusExpression = "status in [200,201,202,203,204]"

// legacySuccessStatusExpression is the two-code success expression used by the
// coverage-driven generators.
const legacySuccessStatusExpression = "status in [200,201]"

func warningOnlySuccess(expression string, result assertions.Result) bool {
	expr := strings.TrimSpace(expression)
	if expr != successStatusExpression && expr != legacySuccessStatusExpression {
		return false
	}
	if result.StatusCode != http.StatusPreconditionFailed {
		return false
	}
	return operationOutcomeHasOnlyWarnings(result.Body)
}

func operationOutcomeHasOnlyWarnings(body []byte) bool {
	if len(body) == 0 {
		return false
	}
	var payload struct {
		ResourceType string `json:"resourceType"`
		Issue        []struct {
			Severity string `json:"severity"`
		} `json:"issue"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return false
	}
	if payload.ResourceType != "OperationOutcome" || len(payload.Issue) == 0 {
		return false
	}
	for _, issue := range payload.Issue {
		severity := strings.ToLower(strings.TrimSpace(issue.Severity))
		if severity != "warning" && severity != "information" {
			return false
		}
	}
	return true
}

func (e *executor) recordFailureSignature(result *CaseResult, assertNode *ast.Assert, statusCode int, body []byte) {
	issue, ok := extractPrimaryOperationOutcomeIssue(body)
	if !ok {
		return
	}
	if shouldIgnoreFailureSignatureIssue(issue) {
		return
	}
	e.ooFailures++
	signature := buildFailureSignature(statusCode, issue)
	result.FailureFingerprint = signature
	existing, found := e.failuresBySig[signature]
	if !found {
		existing = &FailureSignature{
			Signature:            signature,
			Count:                0,
			StatusCode:           statusCode,
			Severity:             issue.Severity,
			Code:                 issue.Code,
			Expression:           issue.Expression,
			Location:             issue.Location,
			Diagnostics:          issue.Diagnostics,
			ExampleRequirementID: assertNode.RequirementID,
			ExampleDescription:   assertNode.Description,
		}
		e.failuresBySig[signature] = existing
	}
	existing.Count++
}

func (e *executor) buildDiagnosticsSummary() *DiagnosticsSummary {
	if e.ooFailures == 0 || len(e.failuresBySig) == 0 {
		return nil
	}
	top := make([]FailureSignature, 0, len(e.failuresBySig))
	for _, sig := range e.failuresBySig {
		top = append(top, *sig)
	}
	sort.Slice(top, func(i, j int) bool {
		if top[i].Count == top[j].Count {
			return top[i].Signature < top[j].Signature
		}
		return top[i].Count > top[j].Count
	})
	const maxTop = 10
	if len(top) > maxTop {
		top = top[:maxTop]
	}
	failedSetupResources := collectFailedSetupResources(e.report.Cases)
	likelyAuth := isLikelyAuthFailure(top, e.report.Failed)
	for idx := range top {
		category, confidence, role := classifyFailureSignature(top[idx], failedSetupResources, likelyAuth)
		top[idx].RootCauseCategory = category
		top[idx].Confidence = confidence
		top[idx].TriageRole = role
	}
	summary := &DiagnosticsSummary{
		OperationOutcomeFailures: e.ooFailures,
		TopSignatures:            top,
	}
	if likelyAuth {
		summary.LikelyAuthFailure = true
		summary.Hint = "All failures look authentication-related. Provide API credentials with --api-basic-username/--api-basic-password or --api-bearer-token."
	}
	return summary
}

var missingResourcePattern = regexp.MustCompile(`(?i)resource\s+([A-Za-z][A-Za-z0-9]*)/([^\s,]+)\s+not\s+found`)

func collectFailedSetupResources(cases []CaseResult) map[string]struct{} {
	if len(cases) == 0 {
		return nil
	}
	failed := make(map[string]struct{})
	for _, c := range cases {
		if c.Passed {
			continue
		}
		resourceType, ok := setupResourceTypeFromRequirementID(c.RequirementID)
		if !ok {
			continue
		}
		key := strings.ToLower(resourceType) + "/momus-setup-" + strings.ToLower(resourceType)
		failed[key] = struct{}{}
	}
	if len(failed) == 0 {
		return nil
	}
	return failed
}

func setupResourceTypeFromRequirementID(requirementID string) (string, bool) {
	if !strings.HasPrefix(requirementID, "setup:") {
		return "", false
	}
	resourceType := strings.TrimSpace(strings.TrimPrefix(requirementID, "setup:"))
	if resourceType == "" {
		return "", false
	}
	return resourceType, true
}

func classifyFailureSignature(sig FailureSignature, failedSetupResources map[string]struct{}, likelyAuth bool) (category, confidence, role string) {
	combined := strings.ToLower(strings.TrimSpace(sig.Signature + " " + sig.Diagnostics))
	if likelyAuth || sig.StatusCode == http.StatusUnauthorized || sig.StatusCode == http.StatusForbidden {
		return "authentication", "high", "root"
	}
	if strings.Contains(combined, "unresolved setup reference") {
		return "setup-dependency-ordering", "high", "root"
	}
	if missingKey, ok := extractMissingResourceKey(combined); ok {
		if _, failed := failedSetupResources[missingKey]; failed {
			return "missing-dependent-resource", "high", "dependent"
		}
		return "missing-dependent-resource", "medium", "root"
	}
	if sig.StatusCode == http.StatusPreconditionFailed || strings.Contains(combined, "constraint failed") || strings.Contains(combined, "minimum required") || strings.Contains(combined, "fixed to") {
		return "profile-validation", "high", "root"
	}
	if sig.StatusCode == http.StatusMethodNotAllowed || sig.StatusCode == http.StatusNotImplemented {
		return "server-capability", "high", "root"
	}
	if sig.StatusCode >= 500 {
		return "server-error", "medium", "root"
	}
	return "unknown", "low", "unknown"
}

func extractMissingResourceKey(text string) (string, bool) {
	matches := missingResourcePattern.FindStringSubmatch(text)
	if len(matches) != 3 {
		return "", false
	}
	resourceType := strings.TrimSpace(matches[1])
	resourceID := strings.TrimSpace(matches[2])
	if resourceType == "" || resourceID == "" {
		return "", false
	}
	return strings.ToLower(resourceType) + "/" + strings.ToLower(resourceID), true
}

func isLikelyAuthFailure(top []FailureSignature, failedCount int) bool {
	if failedCount <= 0 || len(top) == 0 {
		return false
	}
	lead := top[0]
	if lead.Count != failedCount {
		return false
	}
	if lead.StatusCode != http.StatusUnauthorized && lead.StatusCode != http.StatusForbidden {
		return false
	}
	corpus := strings.ToLower(strings.Join([]string{
		lead.Signature,
		lead.Diagnostics,
		lead.Code,
		lead.Severity,
	}, " "))
	for _, token := range []string{"auth", "authentication", "authorization", "basic", "bearer", "token", "forbidden", "unauthorized"} {
		if strings.Contains(corpus, token) {
			return true
		}
	}
	return false
}

type operationOutcomeIssue struct {
	Severity    string
	Code        string
	Expression  string
	Location    string
	Diagnostics string
}

func extractPrimaryOperationOutcomeIssue(body []byte) (operationOutcomeIssue, bool) {
	if len(body) == 0 {
		return operationOutcomeIssue{}, false
	}
	var payload struct {
		ResourceType string `json:"resourceType"`
		Issue        []struct {
			Severity    string   `json:"severity"`
			Code        string   `json:"code"`
			Diagnostics string   `json:"diagnostics"`
			Expression  []string `json:"expression"`
			Location    []string `json:"location"`
			Details     struct {
				Text string `json:"text"`
			} `json:"details"`
		} `json:"issue"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return operationOutcomeIssue{}, false
	}
	if payload.ResourceType != "OperationOutcome" || len(payload.Issue) == 0 {
		return operationOutcomeIssue{}, false
	}
	selected := payload.Issue[0]
	for _, issue := range payload.Issue {
		severity := strings.ToLower(strings.TrimSpace(issue.Severity))
		if severity != "warning" && severity != "information" {
			selected = issue
			break
		}
	}
	issue := operationOutcomeIssue{
		Severity:    normalizeToken(selected.Severity),
		Code:        normalizeToken(selected.Code),
		Expression:  firstNonEmpty(selected.Expression),
		Location:    firstNonEmpty(selected.Location),
		Diagnostics: normalizeDiagnostic(selected.Diagnostics, selected.Details.Text),
	}
	return issue, true
}

func shouldIgnoreFailureSignatureIssue(issue operationOutcomeIssue) bool {
	severity := strings.ToLower(strings.TrimSpace(issue.Severity))
	return severity == "warning" || severity == "information"
}

func buildFailureSignature(statusCode int, issue operationOutcomeIssue) string {
	path := issue.Expression
	if path == "" {
		path = issue.Location
	}
	path = normalizeToken(path)
	parts := []string{fmt.Sprintf("status=%d", statusCode)}
	if issue.Code != "" {
		parts = append(parts, "code="+issue.Code)
	}
	if issue.Severity != "" {
		parts = append(parts, "severity="+issue.Severity)
	}
	if path != "" {
		parts = append(parts, "path="+truncateText(path, 80))
	}
	if issue.Diagnostics != "" {
		parts = append(parts, "diag="+truncateText(issue.Diagnostics, 120))
	}
	return strings.Join(parts, " | ")
}

func normalizeDiagnostic(diagnostics, detailsText string) string {
	value := strings.TrimSpace(diagnostics)
	if value == "" {
		value = strings.TrimSpace(detailsText)
	}
	if value == "" {
		return ""
	}
	return normalizeToken(value)
}

func firstNonEmpty(values []string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func normalizeToken(value string) string {
	if value == "" {
		return ""
	}
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func truncateText(value string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max]) + "..."
}
