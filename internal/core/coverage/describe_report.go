package coverage

import (
	"fmt"
	"sort"
	"strings"
)

// DescribePlan renders a coverage plan as a human-readable Markdown document,
// grouped by domain with plain-English descriptions of each obligation. It
// answers "what conformance coverage am I getting" at a glance before any test
// runs.
func DescribePlan(plan *CoveragePlan) string {
	var b strings.Builder
	b.WriteString("# Coverage Plan\n\n")

	if plan != nil {
		fmt.Fprintf(&b, "- **Total obligations**: %d\n", len(plan.Requirements))
		if plan.Summary.TotalRequirements > 0 {
			fmt.Fprintf(&b, "- **Requirement types**: %d\n", len(plan.Summary.ByResourceType))
		}
		fmt.Fprintf(&b, "- **Interaction strength**: %d\n", plan.Strength)
		b.WriteString("\n")
	}

	if plan == nil || len(plan.Requirements) == 0 {
		b.WriteString("No obligations in this plan.\n")
		return b.String()
	}

	byDomain := make(map[CoverageDomain][]CoverageRequirement)
	for _, req := range plan.Requirements {
		byDomain[req.Domain] = append(byDomain[req.Domain], req)
	}

	domains := make([]CoverageDomain, 0, len(byDomain))
	for d := range byDomain {
		domains = append(domains, d)
	}
	sort.Slice(domains, func(i, j int) bool { return domains[i] < domains[j] })

	for _, domain := range domains {
		reqs := byDomain[domain]
		sort.Slice(reqs, func(i, j int) bool { return reqs[i].ID < reqs[j].ID })
		desc, _ := domainDescriptions[domain]
		fmt.Fprintf(&b, "## %s (%d)\n", domain, len(reqs))
		if desc != "" {
			fmt.Fprintf(&b, "\n_%s_\n\n", desc)
		}
		renderRequirementTable(&b, reqs)
		b.WriteString("\n")
	}

	appendGlossary(&b)
	return b.String()
}

// renderRequirementTable renders a per-domain table of requirements. The column
// set varies by domain: element-constrained domains show the element path and
// datatype; search shows the parameter; operation/state show the variant.
func renderRequirementTable(b *strings.Builder, reqs []CoverageRequirement) {
	b.WriteString("| # | Human ID | Constraint | Variant | What is tested |\n")
	b.WriteString("|---|----------|-----------|---------|----------------|\n")
	for i, req := range reqs {
		humanID := req.HumanID
		if humanID == "" {
			humanID = req.ID
		}
		desc := req.Description
		if desc == "" {
			desc = DescribeCoverageRequirement(req)
		}
		fmt.Fprintf(b, "| %d | `%s` | `%s` | %s | %s |\n", i+1, humanID, req.ID, req.Variant, desc)
	}
}

// appendGlossary writes the domain/variant glossary that explains what each
// coverage category tests.
func appendGlossary(b *strings.Builder) {
	b.WriteString("## Glossary\n\n")
	b.WriteString("### Domains\n\n")
	b.WriteString("| Domain | Description |\n")
	b.WriteString("|--------|-------------|\n")
	domains := make([]CoverageDomain, 0, len(domainDescriptions))
	for d := range domainDescriptions {
		domains = append(domains, d)
	}
	sort.Slice(domains, func(i, j int) bool { return domains[i] < domains[j] })
	for _, d := range domains {
		fmt.Fprintf(b, "| %s | %s |\n", d, domainDescriptions[d])
	}

	b.WriteString("\n### Variants\n\n")
	b.WriteString("| Variant | Description |\n")
	b.WriteString("|---------|-------------|\n")
	variants := make([]CoverageVariant, 0, len(variantDescriptions))
	for v := range variantDescriptions {
		variants = append(variants, v)
	}
	sort.Slice(variants, func(i, j int) bool { return variants[i] < variants[j] })
	for _, v := range variants {
		fmt.Fprintf(b, "| %s | %s |\n", v, variantDescriptions[v])
	}
}
