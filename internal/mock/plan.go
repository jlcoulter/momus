package mock

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/jlcoulter/momus/internal/test/ast"
)

// planFile is the on-disk test plan artifact: a version plus the executable
// test AST root. The mock reads it to learn which requests the plan expects to
// be rejected (negative tests), so it can return the matching 4xx status.
type planFile struct {
	Version string         `json:"version"`
	Root    map[string]any `json:"root"`
}

// rejectRoute records a request the plan expects to be rejected, keyed by
// "METHOD path", with the 4xx status the following assertion allows.
type rejectRoute struct {
	status int
}

// planRoutes holds the reject routes derived from a test plan.
type planRoutes struct {
	rejects map[string]rejectRoute
}

// loadPlanRoutes reads a test plan file and walks its AST, pairing each Request
// with the Assert that follows it. When an assertion allows a 4xx rejection
// (400/412/422), the preceding request is recorded as a reject route so the
// mock returns that status instead of accepting the payload.
func loadPlanRoutes(path string) (*planRoutes, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read test plan %s: %w", path, err)
	}
	var pf planFile
	if err := json.Unmarshal(raw, &pf); err != nil {
		return nil, fmt.Errorf("parse test plan %s: %w", path, err)
	}
	if pf.Root == nil {
		return nil, fmt.Errorf("test plan %s has no root", path)
	}
	root, err := ast.DecodeNode(pf.Root)
	if err != nil {
		return nil, fmt.Errorf("decode test plan %s: %w", path, err)
	}
	return buildPlanRoutes(root), nil
}

// buildPlanRoutes derives the reject routes from an in-memory test AST root.
func buildPlanRoutes(root ast.Node) *planRoutes {
	routes := &planRoutes{rejects: make(map[string]rejectRoute)}
	walkPlan(root, routes)
	return routes
}

// walkPlan traverses the AST, pairing each Request with the Assert that follows
// it in the same sequence.
func walkPlan(node ast.Node, routes *planRoutes) {
	switch n := node.(type) {
	case *ast.Sequence:
		for i, step := range n.Steps {
			if req, ok := step.(*ast.Request); ok {
				// Look ahead for the assertion that follows this request.
				if i+1 < len(n.Steps) {
					if assert, ok := n.Steps[i+1].(*ast.Assert); ok {
						recordReject(req, assert, routes)
					}
				}
			}
			walkPlan(step, routes)
		}
	case *ast.Parallel:
		for _, step := range n.Steps {
			walkPlan(step, routes)
		}
	}
}

// recordReject records a request as a reject route when its assertion allows a
// 4xx rejection status. The route is keyed by "METHOD path?query" (the full
// request URI, host-stripped) so distinct search queries match distinctly.
func recordReject(req *ast.Request, assert *ast.Assert, routes *planRoutes) {
	status, ok := rejectStatus(assert.Expression)
	if !ok {
		return
	}
	key := req.Method + " " + requestURI(req.URL)
	routes.rejects[key] = rejectRoute{status: status}
}

// requestURI returns the path and query portion of a request URL, stripping any
// scheme and host. It handles both absolute URLs
// ("http://host/Patient?name=x") and relative paths ("/Patient?name=x").
func requestURI(url string) string {
	rest := url
	if idx := strings.Index(url, "://"); idx >= 0 {
		rest = url[idx+3:]
		if slash := strings.IndexByte(rest, '/'); slash >= 0 {
			rest = rest[slash:]
		} else {
			return "/"
		}
	}
	if !strings.HasPrefix(rest, "/") {
		rest = "/" + rest
	}
	return rest
}

// rejectStatus returns the 4xx status an assertion allows, and whether it is a
// rejection. It parses "status in [400,412,422]" and returns the first 4xx code
// in the list.
func rejectStatus(expression string) (int, bool) {
	expr := strings.TrimSpace(expression)
	if !strings.HasPrefix(expr, "status in [") || !strings.HasSuffix(expr, "]") {
		return 0, false
	}
	list := strings.TrimSuffix(strings.TrimPrefix(expr, "status in ["), "]")
	for _, token := range strings.Split(list, ",") {
		code, err := strconv.Atoi(strings.TrimSpace(token))
		if err != nil {
			continue
		}
		if code >= 400 && code < 500 {
			return code, true
		}
	}
	return 0, false
}
