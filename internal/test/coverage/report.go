package coverage

// ExecutedRequirementResult is the execution outcome for a single requirement ID.
type ExecutedRequirementResult struct {
	RequirementID string
	Passed        bool
}

// DomainCoverageSummary reports coverage status for a single domain.
type DomainCoverageSummary struct {
	Total           int     `json:"total"`
	Covered         int     `json:"covered"`
	Uncovered       int     `json:"uncovered"`
	CoveragePercent float64 `json:"coveragePercent"`
}

// EvaluationReport compares planned obligations against execution outcomes.
type EvaluationReport struct {
	TotalRequirements     int                                      `json:"totalRequirements"`
	CoveredRequirements   int                                      `json:"coveredRequirements"`
	UncoveredRequirements int                                      `json:"uncoveredRequirements"`
	CoveragePercent       float64                                  `json:"coveragePercent"`
	ByDomain              map[CoverageDomain]DomainCoverageSummary `json:"byDomain,omitempty"`
	Uncovered             []CoverageRequirement                    `json:"uncovered,omitempty"`
}
