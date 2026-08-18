package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jlcoulter/momus/internal/test/assertions"
	"github.com/jlcoulter/momus/internal/test/ast"
)

// ExecuteOptions configures AST execution.
type ExecuteOptions struct {
	BaseURL    string
	HTTPClient *http.Client
}

// CaseResult is the outcome of an assertion-bound test case.
type CaseResult struct {
	RequirementID string `json:"requirementId"`
	Description   string `json:"description"`
	Expression    string `json:"expression"`
	Passed        bool   `json:"passed"`
	StatusCode    int    `json:"statusCode,omitempty"`
	Error         string `json:"error,omitempty"`
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
		ctx:     ctx,
		client:  client,
		baseURL: strings.TrimRight(options.BaseURL, "/"),
		report:  &Report{Cases: make([]CaseResult, 0)},
	}

	if err := exec.runNode(plan); err != nil {
		return nil, err
	}
	exec.report.Total = len(exec.report.Cases)
	return exec.report, nil
}

type executor struct {
	ctx        context.Context
	client     *http.Client
	baseURL    string
	report     *Report
	lastResult assertions.Result
	hasResult  bool
	lastErr    error
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
	if reqNode.Body != nil {
		body, err := json.Marshal(reqNode.Body)
		if err != nil {
			return assertions.Result{}, fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(e.ctx, reqNode.Method, url, bodyReader)
	if err != nil {
		return assertions.Result{}, err
	}
	for k, v := range reqNode.Headers {
		req.Header.Set(k, v)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return assertions.Result{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return assertions.Result{}, err
	}

	return assertions.Result{StatusCode: resp.StatusCode, Body: body, Variables: map[string]any{}}, nil
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
		e.report.Failed++
		e.report.Cases = append(e.report.Cases, result)
		return
	}
	if !e.hasResult {
		result.Passed = false
		result.Error = "no request result available for assertion"
		e.report.Failed++
		e.report.Cases = append(e.report.Cases, result)
		return
	}

	assertion, err := assertions.ParseExpression(assertNode.Expression)
	if err != nil {
		result.Passed = false
		result.StatusCode = e.lastResult.StatusCode
		result.Error = err.Error()
		e.report.Failed++
		e.report.Cases = append(e.report.Cases, result)
		return
	}

	result.StatusCode = e.lastResult.StatusCode
	if err := assertion.Evaluate(e.ctx, e.lastResult); err != nil {
		result.Passed = false
		result.Error = err.Error()
		e.report.Failed++
	} else {
		result.Passed = true
		e.report.Passed++
	}
	e.report.Cases = append(e.report.Cases, result)
}
