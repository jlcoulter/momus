package coverage

import "sort"

// EvaluateCoverage computes contractual coverage for executed requirement results.
func EvaluateCoverage(plan *CoveragePlan, executed []ExecutedRequirementResult) EvaluationReport {
	report := EvaluationReport{
		ByDomain: make(map[CoverageDomain]DomainCoverageSummary),
	}
	if plan == nil || len(plan.Requirements) == 0 {
		report.CoveragePercent = 100
		return report
	}

	requirementsByID := make(map[string]CoverageRequirement, len(plan.Requirements))
	requirementIDs := make([]string, 0, len(plan.Requirements))
	for _, req := range plan.Requirements {
		if req.ID == "" {
			continue
		}
		if _, exists := requirementsByID[req.ID]; exists {
			continue
		}
		requirementsByID[req.ID] = req
		requirementIDs = append(requirementIDs, req.ID)
	}
	sort.Strings(requirementIDs)
	report.TotalRequirements = len(requirementIDs)

	passedByID := make(map[string]bool, len(executed))
	for _, result := range executed {
		if result.RequirementID == "" {
			continue
		}
		if !result.Passed {
			continue
		}
		if _, exists := requirementsByID[result.RequirementID]; !exists {
			continue
		}
		passedByID[result.RequirementID] = true
	}

	for _, reqID := range requirementIDs {
		req := requirementsByID[reqID]
		domain := report.ByDomain[req.Domain]
		domain.Total++
		if passedByID[reqID] {
			report.CoveredRequirements++
			domain.Covered++
		} else {
			report.UncoveredRequirements++
			domain.Uncovered++
			report.Uncovered = append(report.Uncovered, req)
		}
		report.ByDomain[req.Domain] = domain
	}

	report.CoveragePercent = coveragePercent(report.CoveredRequirements, report.TotalRequirements)
	for domain, summary := range report.ByDomain {
		summary.CoveragePercent = coveragePercent(summary.Covered, summary.Total)
		report.ByDomain[domain] = summary
	}
	return report
}

func coveragePercent(covered, total int) float64 {
	if total <= 0 {
		return 100
	}
	return (float64(covered) / float64(total)) * 100
}
