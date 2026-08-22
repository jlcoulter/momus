package openapi

import (
	"fmt"

	"github.com/jlcoulter/momus/internal/core/constraint"
)

// DeriveConstraints normalises an OpenAPI document into constraint-model
// obligations: one api-operation constraint per HTTP operation and one
// api-parameter constraint per declared parameter. This proves the "FHIR/API"
// duality by feeding API contracts through the same constraint model the FHIR
// pipeline uses.
func DeriveConstraints(doc *Document) []constraint.Constraint {
	if doc == nil {
		return nil
	}
	out := make([]constraint.Constraint, 0)
	for _, op := range doc.Paths {
		if op == nil {
			continue
		}
		out = append(out, constraint.Constraint{
			ID:        fmt.Sprintf("api|%s|%s|operation", op.Method, op.Path),
			Kind:      constraint.KindAPIOperation,
			APIMethod: op.Method,
			APIPath:   op.Path,
		})
		for _, p := range op.Parameters {
			out = append(out, constraint.Constraint{
				ID:            fmt.Sprintf("api|%s|%s|param|%s", op.Method, op.Path, p.Name),
				Kind:          constraint.KindAPIParameter,
				APIMethod:     op.Method,
				APIPath:       op.Path,
				ParameterName: p.Name,
				ParameterIn:   p.In,
			})
		}
	}
	return out
}
