package validate

import (
	"time"

	"github.com/jlcoulter/momus/internal/fhir/model"
)

// matchesDatatype reports whether a JSON value conforms to at least one of the
// declared element types. Unknown or unresolvable type codes are treated as
// matching (skip) so the validator never over-rejects on types it cannot judge.
func matchesDatatype(val any, types []model.ElementType) bool {
	for _, t := range types {
		if jsonTypeMatches(val, t.Code) {
			return true
		}
	}
	// If none of the declared codes is a known datatype we cannot judge — do
	// not reject.
	for _, t := range types {
		if isKnownDatatype(t.Code) {
			return false
		}
	}
	return true
}

// isKnownDatatype reports whether the FHIR type code is one the validator can
// judge. Codes like complex BackboneElement/Resource types and unknown names
// are treated as unknowable.
func isKnownDatatype(code string) bool {
	switch code {
	case "string", "code", "id", "uri", "url", "canonical", "oid", "uuid",
		"integer", "integer64", "positiveInt", "unsignedInt", "decimal",
		"boolean", "date", "dateTime", "instant", "time",
		"base64Binary", "markdown", "xhtml",
		"Reference", "Coding", "CodeableConcept", "Quantity",
		"HumanName", "Address", "Identifier", "ContactPoint", "Period",
		"Extension", "Ratio", "Range", "Annotation", "Timing", "Age",
		"Count", "Distance", "Duration", "Money", "SampledData",
		"Meta", "Signature":
		return true
	}
	return false
}

// jsonTypeMatches reports whether a JSON value has the correct shape for a FHIR
// type code.
func jsonTypeMatches(val any, code string) bool {
	switch code {
	case "string", "code", "id", "uri", "url", "canonical", "oid", "uuid",
		"markdown", "base64Binary", "xhtml":
		_, ok := val.(string)
		return ok
	case "integer", "integer64", "positiveInt", "unsignedInt", "decimal":
		switch val.(type) {
		case float64, int, int64:
			return true
		}
		return false
	case "boolean":
		_, ok := val.(bool)
		return ok
	case "date", "dateTime", "instant", "time":
		s, ok := val.(string)
		if !ok {
			return false
		}
		return validDateShape(s)
	case "Reference", "Coding", "CodeableConcept", "Identifier", "HumanName",
		"Address", "ContactPoint", "Period", "Extension", "Annotation",
		"Ratio", "Range", "Timing", "Signature", "Age", "Count", "Distance",
		"Duration", "Money", "Quantity", "SampledData", "Meta":
		_, ok := val.(map[string]any)
		return ok
	default:
		// Complex/unresolvable types: treat any JSON as acceptable (skip).
		return true
	}
}

// validDateShape reports whether s is a plausible FHIR date/dateTime/instant/
// time string.
func validDateShape(s string) bool {
	layouts := []string{
		"2006", "2006-01", "2006-01-02",
		"15:04:05", // time
	}
	for _, l := range layouts {
		if _, err := time.Parse(l, s); err == nil {
			return true
		}
	}
	// dateTime/instant: ISO 8601. Accept a fractional seconds suffix by
	// trimming to whole seconds.
	if len(s) >= 19 && s[4] == '-' && s[7] == '-' && (s[10] == 'T' || s[10] == 't') {
		base := s[:19]
		if _, err := time.Parse("2006-01-02T15:04:05", base); err == nil {
			return true
		}
	}
	return false
}
