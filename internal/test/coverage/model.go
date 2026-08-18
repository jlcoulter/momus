package coverage

// CoverageDomain identifies the obligation category.
type CoverageDomain string

const (
	CoverageDomainCardinality CoverageDomain = "cardinality"
)

// CoverageVariant identifies a specific test obligation type for a domain.
type CoverageVariant string

const (
	CoverageVariantValidMin        CoverageVariant = "valid-min"
	CoverageVariantMissingRequired CoverageVariant = "missing-required"
	CoverageVariantMultipleValues  CoverageVariant = "multiple-values"
)

// CoverageRequirement is a machine-verifiable coverage obligation.
type CoverageRequirement struct {
	ID           string          `json:"id"`
	ProfileURL   string          `json:"profileUrl"`
	ResourceType string          `json:"resourceType"`
	ElementPath  string          `json:"elementPath"`
	Domain       CoverageDomain  `json:"domain"`
	Variant      CoverageVariant `json:"variant"`
	Min          int             `json:"min"`
	Max          string          `json:"max"`
}

// CoveragePlan is the list of obligations derived from selected contracts.
type CoveragePlan struct {
	Requirements []CoverageRequirement `json:"requirements"`
}
