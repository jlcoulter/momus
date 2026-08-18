package ast

import "fmt"

// EncodeNode converts an AST node into a JSON-friendly structure with explicit
// node type tags.
func EncodeNode(node Node) (map[string]any, error) {
	switch n := node.(type) {
	case *Sequence:
		steps := make([]any, 0, len(n.Steps))
		for _, step := range n.Steps {
			encoded, err := EncodeNode(step)
			if err != nil {
				return nil, err
			}
			steps = append(steps, encoded)
		}
		return map[string]any{
			"type":  "sequence",
			"steps": steps,
		}, nil
	case *Parallel:
		steps := make([]any, 0, len(n.Steps))
		for _, step := range n.Steps {
			encoded, err := EncodeNode(step)
			if err != nil {
				return nil, err
			}
			steps = append(steps, encoded)
		}
		return map[string]any{
			"type":  "parallel",
			"steps": steps,
		}, nil
	case *Request:
		return map[string]any{
			"type":    "request",
			"method":  n.Method,
			"url":     n.URL,
			"headers": n.Headers,
			"body":    n.Body,
		}, nil
	case *Capture:
		return map[string]any{
			"type": "capture",
			"name": n.Name,
			"path": n.Path,
		}, nil
	case *Assert:
		encoded := map[string]any{
			"type":          "assert",
			"description":   n.Description,
			"requirementId": n.RequirementID,
			"expression":    n.Expression,
		}
		if n.Trace != nil {
			encoded["requirement"] = encodeTrace(n.Trace)
		}
		return encoded, nil
	default:
		return nil, fmt.Errorf("unsupported AST node type %T", node)
	}
}

func encodeTrace(t *Trace) map[string]any {
	return map[string]any{
		"constraintId": t.ConstraintID,
		"profileUrl":   t.ProfileURL,
		"resourceType": t.ResourceType,
		"elementPath":  t.ElementPath,
		"domain":       t.Domain,
		"variant":      t.Variant,
		"expected":     t.Expected,
	}
}

// EncodePlan converts a Plan into a JSON-friendly shape.
func EncodePlan(plan *Plan) (map[string]any, error) {
	if plan == nil {
		return nil, fmt.Errorf("plan is nil")
	}
	root, err := EncodeNode(plan.Root)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"version": plan.Version,
		"root":    root,
	}, nil
}
