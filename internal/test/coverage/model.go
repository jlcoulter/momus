package coverage

// CoverageDomain identifies the obligation category.
type CoverageDomain string

const (
	CoverageDomainCardinality CoverageDomain = "cardinality"
	CoverageDomainDatatype    CoverageDomain = "datatype"
	CoverageDomainTerminology CoverageDomain = "terminology"
	CoverageDomainStructure   CoverageDomain = "structure"
	CoverageDomainInvariant   CoverageDomain = "invariant"
	CoverageDomainReference   CoverageDomain = "reference"
	CoverageDomainInteraction CoverageDomain = "interaction"
	CoverageDomainSearch      CoverageDomain = "search"
	CoverageDomainOperation   CoverageDomain = "operation"
	CoverageDomainState       CoverageDomain = "state"
)

// CoverageVariant identifies a specific test obligation type for a domain.
//
// Variants are globally unique (they are domain-prefixed) because they are
// used as map keys in coverage summaries and evaluation reports.
type CoverageVariant string

const (
	// Cardinality domain.
	CoverageVariantValidMin        CoverageVariant = "valid-min"
	CoverageVariantMissingRequired CoverageVariant = "missing-required"
	CoverageVariantMultipleValues  CoverageVariant = "multiple-values"

	// Datatype domain.
	CoverageVariantDatatypeValid          CoverageVariant = "datatype-valid"
	CoverageVariantDatatypeInvalidLexical CoverageVariant = "datatype-invalid-lexical"
	CoverageVariantDatatypeWrongJSONType  CoverageVariant = "datatype-wrong-json-type"
	CoverageVariantDatatypeNull           CoverageVariant = "datatype-null"

	// Terminology domain.
	CoverageVariantTerminologyValid   CoverageVariant = "terminology-valid"
	CoverageVariantTerminologyInvalid CoverageVariant = "terminology-invalid"
	CoverageVariantTerminologyAbsent  CoverageVariant = "terminology-absent"

	// Structure domain.
	CoverageVariantStructureSlicePresent CoverageVariant = "structure-slice-present"

	// Invariant domain.
	CoverageVariantInvariantSatisfies CoverageVariant = "invariant-satisfies"
	CoverageVariantInvariantViolates  CoverageVariant = "invariant-violates"

	// Reference domain.
	CoverageVariantReferenceValid       CoverageVariant = "reference-valid"
	CoverageVariantReferenceWrongTarget CoverageVariant = "reference-wrong-target"
	CoverageVariantReferenceDangling    CoverageVariant = "reference-dangling"

	// Interaction domain.
	CoverageVariantInteractionPair CoverageVariant = "interaction-pair"

	// Search domain.
	CoverageVariantSearchValid        CoverageVariant = "search-valid"
	CoverageVariantSearchNoResults    CoverageVariant = "search-no-results"
	CoverageVariantSearchInvalidValue CoverageVariant = "search-invalid-value"

	// Operation domain.
	CoverageVariantOperationRead    CoverageVariant = "operation-read"
	CoverageVariantOperationUpdate  CoverageVariant = "operation-update"
	CoverageVariantOperationPatch   CoverageVariant = "operation-patch"
	CoverageVariantOperationDelete  CoverageVariant = "operation-delete"
	CoverageVariantOperationHistory CoverageVariant = "operation-history"

	// State domain.
	CoverageVariantStateCRUDSequence      CoverageVariant = "state-crud-sequence"
	CoverageVariantStateReadNonexistent   CoverageVariant = "state-read-nonexistent"
	CoverageVariantStateDeleteNonexistent CoverageVariant = "state-delete-nonexistent"
)

// IsReject reports whether a variant's generated test must be rejected by a
// conformant server (i.e. it asserts a constraint violation). Variants that
// are not rejects are accept variants: they assert that a valid payload is
// accepted. This is the single source of truth for the negative variant set,
// shared by derivation (interaction eligibility) and generation (assertions).
func (v CoverageVariant) IsReject() bool {
	switch v {
	case CoverageVariantMissingRequired,
		CoverageVariantDatatypeInvalidLexical,
		CoverageVariantDatatypeWrongJSONType,
		CoverageVariantDatatypeNull,
		CoverageVariantTerminologyInvalid,
		CoverageVariantTerminologyAbsent,
		CoverageVariantInvariantViolates,
		CoverageVariantReferenceWrongTarget,
		CoverageVariantReferenceDangling,
		CoverageVariantSearchInvalidValue:
		return true
	default:
		return false
	}
}

// CoverageRequirement is a machine-verifiable coverage obligation.
type CoverageRequirement struct {
	ID                string          `json:"id"`
	ConstraintID      string          `json:"constraintId,omitempty"`
	ProfileURL        string          `json:"profileUrl"`
	ResourceType      string          `json:"resourceType"`
	ElementPath       string          `json:"elementPath"`
	DependencyTargets []string        `json:"dependencyTargets,omitempty"`
	Domain            CoverageDomain  `json:"domain"`
	Variant           CoverageVariant `json:"variant"`
	Min               int             `json:"min"`
	Max               string          `json:"max"`
	// PairA and PairB reference the two source requirements of an interaction
	// obligation. They are set only for coverage-domain requirements derived at
	// interaction strength >= 2.
	PairA string `json:"pairA,omitempty"`
	PairB string `json:"pairB,omitempty"`
	// SearchCode is the search parameter code for search-domain obligations.
	SearchCode string `json:"searchCode,omitempty"`
}

// InteractionRequirement records a pairwise interaction obligation: the two
// base requirements that must be satisfiable together in a single payload.
type InteractionRequirement struct {
	ID           string `json:"id"`
	ProfileURL   string `json:"profileUrl"`
	ResourceType string `json:"resourceType"`
	RequirementA string `json:"requirementA"`
	RequirementB string `json:"requirementB"`
}

// DeriveOptions controls which profile elements become coverage obligations.
type DeriveOptions struct {
	IncludeResourceTypes []string
	IncludeProfileURLs   []string
	ExcludePathPrefixes  []string
	MustSupportOnly      bool
	IncludeOptional      bool
	IncludeLowValuePaths bool
	// Strength is the interaction strength of the plan (1 = individual
	// requirement coverage, 2 = pairwise interaction coverage).
	Strength int
}

// PruneReason describes why an element was excluded from derivation.
type PruneReason string

const (
	PruneReasonResourceFiltered    PruneReason = "resource-filtered"
	PruneReasonProfileFiltered     PruneReason = "profile-filtered"
	PruneReasonRootPath            PruneReason = "root-path"
	PruneReasonLowValuePath        PruneReason = "low-value-path"
	PruneReasonExcludedPathPrefix  PruneReason = "excluded-path-prefix"
	PruneReasonMustSupportFiltered PruneReason = "must-support-filtered"
	PruneReasonOptionalFiltered    PruneReason = "optional-filtered"
)

// CoverageSummary provides explainability for a derived plan.
type CoverageSummary struct {
	TotalRequirements int                     `json:"totalRequirements"`
	ByDomain          map[CoverageDomain]int  `json:"byDomain"`
	ByResourceType    map[string]int          `json:"byResourceType"`
	ByVariant         map[CoverageVariant]int `json:"byVariant"`
	PrunedByReason    map[PruneReason]int     `json:"prunedByReason"`
	Interactions      int                     `json:"interactions,omitempty"`
}

// CoveragePlan is the list of obligations derived from selected contracts.
//
// Strength is the interaction strength of the plan: 1 (default) covers each
// obligation individually; 2 additionally derives pairwise interaction
// obligations and groups compatible obligations into shared payloads.
type CoveragePlan struct {
	Requirements []CoverageRequirement    `json:"requirements"`
	Interactions []InteractionRequirement `json:"interactions,omitempty"`
	Strength     int                      `json:"strength"`
	Summary      CoverageSummary          `json:"summary"`
}
