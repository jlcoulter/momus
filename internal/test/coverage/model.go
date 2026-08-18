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
)

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
}

// DeriveOptions controls which profile elements become coverage obligations.
type DeriveOptions struct {
	IncludeResourceTypes []string
	IncludeProfileURLs   []string
	ExcludePathPrefixes  []string
	MustSupportOnly      bool
	IncludeOptional      bool
	IncludeLowValuePaths bool
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
}

// CoveragePlan is the list of obligations derived from selected contracts.
type CoveragePlan struct {
	Requirements []CoverageRequirement `json:"requirements"`
	Summary      CoverageSummary       `json:"summary"`
}
