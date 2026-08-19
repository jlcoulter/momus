package coverage

import "sort"

// EvaluateCoverage computes contractual coverage for executed requirement results.
func EvaluateCoverage(plan *CoveragePlan, executed []ExecutedRequirementResult) EvaluationReport {
	report := EvaluationReport{
		ByDomain:       make(map[CoverageDomain]DomainCoverageSummary),
		ByResourceType: make(map[string]DomainCoverageSummary),
		ByVariant:      make(map[CoverageVariant]DomainCoverageSummary),
	}
	if plan == nil || len(plan.Requirements) == 0 {
		// An empty or absent plan proves nothing: report zero coverage rather
		// than claiming 100% of an undefined obligation set.
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
	hasResultByID := make(map[string]bool, len(executed))
	for _, result := range executed {
		if result.RequirementID == "" {
			continue
		}
		if _, exists := requirementsByID[result.RequirementID]; !exists {
			continue
		}
		hasResultByID[result.RequirementID] = true
		// A requirement is covered only when every one of its executed cases
		// passes. This matters for obligations that expand to multiple steps
		// (e.g. a CRUD sequence): a single passing step must not mark the whole
		// obligation covered.
		if !result.Passed {
			passedByID[result.RequirementID] = false
		} else if _, seen := passedByID[result.RequirementID]; !seen {
			passedByID[result.RequirementID] = true
		}
	}

	for _, reqID := range requirementIDs {
		req := requirementsByID[reqID]
		domain := report.ByDomain[req.Domain]
		resource := report.ByResourceType[req.ResourceType]
		variant := report.ByVariant[req.Variant]
		domain.Total++
		resource.Total++
		variant.Total++
		if hasResultByID[reqID] && passedByID[reqID] {
			report.CoveredRequirements++
			domain.Covered++
			resource.Covered++
			variant.Covered++
			report.Covered = append(report.Covered, req)
		} else {
			report.UncoveredRequirements++
			domain.Uncovered++
			resource.Uncovered++
			variant.Uncovered++
			report.Uncovered = append(report.Uncovered, req)
		}
		report.ByDomain[req.Domain] = domain
		report.ByResourceType[req.ResourceType] = resource
		report.ByVariant[req.Variant] = variant
	}

	report.CoveragePercent = coveragePercent(report.CoveredRequirements, report.TotalRequirements)
	for domain, summary := range report.ByDomain {
		summary.CoveragePercent = coveragePercent(summary.Covered, summary.Total)
		report.ByDomain[domain] = summary
	}
	for resourceType, summary := range report.ByResourceType {
		summary.CoveragePercent = coveragePercent(summary.Covered, summary.Total)
		report.ByResourceType[resourceType] = summary
	}
	for variant, summary := range report.ByVariant {
		summary.CoveragePercent = coveragePercent(summary.Covered, summary.Total)
		report.ByVariant[variant] = summary
	}
	return report
}

func coveragePercent(covered, total int) float64 {
	if total <= 0 {
		return 100
	}
	return (float64(covered) / float64(total)) * 100
}
