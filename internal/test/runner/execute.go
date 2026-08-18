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
	"time"

	"github.com/jlcoulter/momus/internal/test/assertions"
	"github.com/jlcoulter/momus/internal/test/ast"
)

// ExecuteOptions configures AST execution.
type ExecuteOptions struct {
	BaseURL       string
	HTTPClient    *http.Client
	BearerToken   string
	BasicUsername string
	BasicPassword string
	IncludeDebug  bool
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
		ctx:           ctx,
		client:        client,
		baseURL:       strings.TrimRight(options.BaseURL, "/"),
		bearerToken:   options.BearerToken,
		basicUsername: options.BasicUsername,
		basicPassword: options.BasicPassword,
		includeDebug:  options.IncludeDebug,
		report:        &Report{Cases: make([]CaseResult, 0)},
		variables:     make(map[string]any),
		created:       make(map[string]struct{}),
		failuresBySig: make(map[string]*FailureSignature),
	}

	if err := exec.runNode(plan); err != nil {
		return nil, err
	}
	exec.report.Total = len(exec.report.Cases)
	exec.report.Diagnostics = exec.buildDiagnosticsSummary()
	return exec.report, nil
}

type executor struct {
	ctx           context.Context
	client        *http.Client
	baseURL       string
	bearerToken   string
	basicUsername string
	basicPassword string
	includeDebug  bool
	report        *Report
	lastResult    assertions.Result
	hasResult     bool
	lastErr       error
	lastDebug     *CaseDebug
	variables     map[string]any
	created       map[string]struct{}
	failuresBySig map[string]*FailureSignature
	ooFailures    int
}

var variablePattern = regexp.MustCompile(`\{\{([a-zA-Z0-9_.-]+)\}\}`)

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
		// Minimal runner behavior: execute in-order even for parallel nodes.
		for _, step := range n.Steps {
			if err := e.runNode(step); err != nil {
				return err
			}
		}
		return nil
	case *ast.Request:
		res, err := e.executeRequest(n)
		e.lastResult = res
		e.hasResult = err == nil
		e.lastErr = err
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

func (e *executor) executeRequest(reqNode *ast.Request) (assertions.Result, error) {
	url := reqNode.URL
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		if e.baseURL == "" {
			return assertions.Result{}, fmt.Errorf("request URL %q is relative and no base URL is configured", reqNode.URL)
		}
		url = e.baseURL + "/" + strings.TrimPrefix(reqNode.URL, "/")
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
		return assertions.Result{}, err
	}
	for k, v := range reqNode.Headers {
		req.Header.Set(k, v)
	}
	e.applyRequestAuth(req)

	resp, err := e.client.Do(req)
	if err != nil {
		return assertions.Result{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return assertions.Result{}, err
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

	return assertions.Result{StatusCode: resp.StatusCode, Body: body, Variables: variables}, nil
}

func isSetupRequest(url string) bool {
	return strings.Contains(url, "momus-setup-")
}

func (e *executor) evaluateAssert(assertNode *ast.Assert) {
	result := CaseResult{
		RequirementID: assertNode.RequirementID,
		Description:   assertNode.Description,
		Expression:    assertNode.Expression,
	}

	if e.lastErr != nil {
		result.Passed = false
		result.Error = e.lastErr.Error()
		result.Debug = e.copyDebugIfEnabled()
		e.report.Failed++
		e.report.Cases = append(e.report.Cases, result)
		return
	}
	if !e.hasResult {
		result.Passed = false
		result.Error = "no request result available for assertion"
		result.Debug = e.copyDebugIfEnabled()
		e.report.Failed++
		e.report.Cases = append(e.report.Cases, result)
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

func (e *executor) applyRequestAuth(req *http.Request) {
	if req == nil {
		return
	}
	if req.Header.Get("Authorization") != "" {
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

func warningOnlySuccess(expression string, result assertions.Result) bool {
	if strings.TrimSpace(expression) != "status in [200,201]" {
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
	summary := &DiagnosticsSummary{
		OperationOutcomeFailures: e.ooFailures,
		TopSignatures:            top,
	}
	if isLikelyAuthFailure(top, e.report.Failed) {
		summary.LikelyAuthFailure = true
		summary.Hint = "All failures look authentication-related. Provide API credentials with --api-basic-username/--api-basic-password or --api-bearer-token."
	}
	return summary
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
