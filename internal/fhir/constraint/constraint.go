// Package constraint defines the constraint model: a normalised,
// machine-testable representation of contractual rules derived from the
// FHIR registry.
//
// The registry knows definitions; the constraint model represents the
// testable rules extracted from those definitions. It is the bridge between
// registry knowledge and coverage derivation, and carries no I/O or
// execution logic.
package constraint

import "strings"

// Kind is the rule category a constraint represents.
type Kind string

const (
	// KindCardinality constrains an element's cardinality (min/max).
	KindCardinality Kind = "cardinality"
	// KindDatatype constrains the permitted datatype of an element.
	KindDatatype Kind = "datatype"
	// KindTerminology constrains an element to a terminology value set.
	KindTerminology Kind = "terminology"
	// KindInvariant is an FHIRPath invariant applied to an element.
	KindInvariant Kind = "invariant"
	// KindReference constrains the target profile/type of a reference.
	KindReference Kind = "reference"
	// KindFixed requires the element to equal a fixed value.
	KindFixed Kind = "fixed"
	// KindPattern requires the element to conform to a pattern value.
	KindPattern Kind = "pattern"
	// KindSearch is a search parameter a server must support.
	KindSearch Kind = "search"
	// KindInteraction is a REST interaction a server must support.
	KindInteraction Kind = "interaction"
	// KindOperation is a custom operation ($name) a server must support.
	KindOperation Kind = "operation"
)

// Constraint is a single normalised contractual rule.
//
// Only the fields relevant to the constraint's Kind are populated; the Kind
// field is the discriminator.
type Constraint struct {
	ID           string `json:"id"`
	Kind         Kind   `json:"kind"`
	ProfileURL   string `json:"profileUrl,omitempty"`
	ResourceType string `json:"resourceType,omitempty"`
	ElementPath  string `json:"elementPath,omitempty"`

	// Cardinality (KindCardinality).
	Min int    `json:"min,omitempty"`
	Max string `json:"max,omitempty"`

	// Datatype (KindDatatype).
	Datatype string `json:"datatype,omitempty"`

	// Terminology (KindTerminology).
	BindingStrength string `json:"bindingStrength,omitempty"`
	ValueSet        string `json:"valueSet,omitempty"`

	// Invariant (KindInvariant).
	InvariantKey string `json:"invariantKey,omitempty"`
	Severity     string `json:"severity,omitempty"`
	Expression   string `json:"expression,omitempty"`
	Human        string `json:"human,omitempty"`

	// Reference (KindReference).
	TargetProfiles []string `json:"targetProfiles,omitempty"`

	// Fixed (KindFixed) and Pattern (KindPattern).
	Value any `json:"value,omitempty"`

	// Search (KindSearch).
	SearchName       string `json:"searchName,omitempty"`
	SearchCode       string `json:"searchCode,omitempty"`
	SearchType       string `json:"searchType,omitempty"`
	SearchExpression string `json:"searchExpression,omitempty"`

	// Interaction (KindInteraction).
	Interaction string `json:"interaction,omitempty"`

	// OperationName (KindOperation).
	OperationName string `json:"operationName,omitempty"`
}

// ID builds a stable, deterministic constraint identifier from its parts.
// Empty parts are dropped so the scheme is uniform across element-derived
// (profile, path, kind) and registry-derived (source URL, kind, discriminator)
// constraints. Coverage requirements and test traceability anchor on these
// identifiers.
func ID(parts ...string) string {
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		filtered = append(filtered, part)
	}
	return strings.Join(filtered, "|")
}
