package validate

import (
	"github.com/jlcoulter/momus/internal/fhir/model"
)

// isMemberOf reports whether a value's code(s) are members of the value set
// at the given URL. A value is either a bare code string, a Coding map, or a
// CodeableConcept map (one or more codings). When the value set cannot be
// resolved, membership is treated as satisfied (skip) so the validator never
// over-rejects on a missing terminology source.
func (v *ProfileValidator) isMemberOf(valueSetURL string, val any) bool {
	vs, ok := v.reg.ValueSet(valueSetURL)
	if !ok || vs == nil {
		return true // cannot resolve the value set — do not over-reject
	}
	for _, code := range extractCodes(val) {
		if valueSetContains(vs, code.System, code.Code) {
			return true
		}
	}
	return false
}

// codeRef is a (system, code) pair extracted from a value.
type codeRef struct {
	System string
	Code   string
}

// extractCodes pulls (system, code) pairs from a value: a bare string is a code
// with empty system; a Coding map has "system"+"code"; a CodeableConcept map
// has "coding":[{...}]. Empty codes are ignored.
func extractCodes(val any) []codeRef {
	var out []codeRef
	switch t := val.(type) {
	case string:
		if t != "" {
			out = append(out, codeRef{Code: t})
		}
	case map[string]any:
		if code, _ := t["code"].(string); code != "" {
			system, _ := t["system"].(string)
			out = append(out, codeRef{System: system, Code: code})
		}
		if rawCoding, ok := t["coding"].([]any); ok {
			for _, c := range rawCoding {
				out = append(out, extractCodes(c)...)
			}
		}
	case []any:
		for _, e := range t {
			out = append(out, extractCodes(e)...)
		}
	}
	return out
}

// valueSetContains reports whether the value set includes the (system, code)
// pair, checking ComposeIncludes (explicit concepts and code-system includes)
// and ExpansionContains. When the value's system is empty (a bare code, common
// for a code-typed element), only the code is matched against every include
// regardless of system.
func valueSetContains(vs *model.ValueSet, system, code string) bool {
	for _, inc := range vs.ComposeIncludes {
		if system != "" && inc.System != "" && inc.System != system {
			continue
		}
		for _, c := range inc.Concepts {
			if c.Code == code {
				return true
			}
		}
	}
	for _, c := range vs.ExpansionContains {
		if c.Code == code {
			return true
		}
	}
	return false
}
