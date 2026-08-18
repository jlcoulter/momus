package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
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
	RequirementID string     `json:"requirementId"`
	Description   string     `json:"description"`
	Expression    string     `json:"expression"`
	Passed        bool       `json:"passed"`
	StatusCode    int        `json:"statusCode,omitempty"`
	Error         string     `json:"error,omitempty"`
	Debug         *CaseDebug `json:"debug,omitempty"`
}

// Report summarizes execution outcomes.
type Report struct {
	Total  int          `json:"total"`
	Passed int          `json:"passed"`
	Failed int          `json:"failed"`
	Cases  []CaseResult `json:"cases"`
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
	}

	if err := exec.runNode(plan); err != nil {
		return nil, err
	}
	exec.report.Total = len(exec.report.Cases)
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
	if reqNode.Body != nil {
		resolvedBody := resolveTemplates(reqNode.Body, e.variables)
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
	if reqNode.Body != nil {
		if bodyMap, ok := reqNode.Body.(map[string]any); ok {
			if resourceType, ok := bodyMap["resourceType"].(string); ok && resourceType != "" {
				id := extractResourceID(body)
				if id == "" {
					id, _ = bodyMap["id"].(string)
				}
				if id != "" {
					variables[resourceType+".id"] = id
					e.variables[resourceType+".id"] = id
				}
			}
		}
	}

	return assertions.Result{StatusCode: resp.StatusCode, Body: body, Variables: variables}, nil
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
		result.Passed = false
		result.Error = err.Error()
		result.Debug = e.copyDebugIfEnabled()
		e.report.Failed++
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
