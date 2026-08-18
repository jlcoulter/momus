package model

// RequirementPurpose describes why a data requirement exists in a test plan.
type RequirementPurpose string

const (
	PurposeExistence    RequirementPurpose = "existence"
	PurposeSearch       RequirementPurpose = "search"
	PurposeRelationship RequirementPurpose = "relationship"
	PurposeFixture      RequirementPurpose = "fixture"
	PurposeNegative     RequirementPurpose = "negative"
)

// Operator is a comparison operator used in a constraint.
type Operator string

const (
	OpEquals             Operator = "equals"
	OpNotEquals          Operator = "notEquals"
	OpLessThan           Operator = "lessThan"
	OpLessThanOrEqual    Operator = "lessThanOrEqual"
	OpGreaterThan        Operator = "greaterThan"
	OpGreaterThanOrEqual Operator = "greaterThanOrEqual"
	OpIn                 Operator = "in"
	OpNotIn              Operator = "notIn"
)

// CardinalityRequirement expresses a required number of resources.
//
// Max is -1 when the upper bound is unbounded.
type CardinalityRequirement struct {
	Min int
	Max int
}

// Exactly requires exactly n resources.
func Exactly(n int) CardinalityRequirement { return CardinalityRequirement{Min: n, Max: n} }

// DataRequirement is a declarative description of FHIR data that a test
// needs. It is the bridge between test planning and resource generation.
type DataRequirement struct {
	ID            string
	Resource      ResourceRequirement
	Purpose       RequirementPurpose
	Constraints   []Constraint
	Relationships []RelationshipRequirement
	Cardinality   CardinalityRequirement
	Tags          []string
}

// ResourceRequirement identifies a set of resources of a given type,
// optionally constrained to one or more profiles.
type ResourceRequirement struct {
	Type    string
	Profile []string
}

// RelationshipRequirement describes a reference from one resource to another.
type RelationshipRequirement struct {
	Path        string
	Target      ResourceRequirement
	Cardinality CardinalityRequirement
}

// Constraint is a predicate on a canonical FHIR element path.
type Constraint struct {
	Path     string
	Operator Operator
	Value    any
}
