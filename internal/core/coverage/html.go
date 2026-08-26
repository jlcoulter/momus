package coverage

import (
	"fmt"
	"html/template"
	"sort"
)

// HTMLItem is one executed case rendered in the HTML report, with pass/fail
// status and, for drill-down, its assertion and request/response detail.
type HTMLItem struct {
	ID            string
	HumanID       string
	Domain        string
	Resource      string
	ElementPath   string
	SearchCode    string
	SearchCodeB   string
	Variant       string
	Description   string
	Expression    string
	Passed        bool
	StatusCode    int
	RequestMethod string
	RequestURL    string
	RequestBody   string
	ResponseBody  string
}

// htmlDrill is one expandable group (a domain, resource type, or variant)
// with its items split into Positive (accept) and Negative (reject) polarity
// sub-groups, each with its own pass/fail summary.
type htmlDrill struct {
	Name    string
	Total   int
	Passed  int
	Failed  int
	Percent float64
	Groups  []htmlPolarityGroup
}

// htmlPolarityGroup is a Positive or Negative sub-grouping within a drill.
type htmlPolarityGroup struct {
	Name    string // "Positive" or "Negative"
	Total   int
	Passed  int
	Failed  int
	Percent float64
	Items   []HTMLItem
}

// htmlReport is the data model for the rendered HTML report.
type htmlReport struct {
	CoveragePercent       float64
	TotalRequirements     int
	CoveredRequirements   int
	UncoveredRequirements int
	Matrices              []coverageMatrix
	Sections              []htmlSection
	DomainGlossary        []glossaryEntry
	VariantGlossary       []glossaryEntry
}

// glossaryEntry is one row of a glossary table (a domain or variant name and its
// plain-English description).
type glossaryEntry struct {
	Name        string
	Description string
}

// htmlSection is one titled drill-down section (By Domain / By Resource Type /
// By Variant).
type htmlSection struct {
	Title  string
	Drills []htmlDrill
}

// matrixCell is one cell in the coverage matrix: the outcome of a single
// (element/parameter, variant) intersection for a resource type.
type matrixCell struct {
	RequirementID string
	HumanID       string
	Passed        bool
	Tested        bool
}

// matrixRow is one row of the coverage matrix (an element, search parameter,
// or operation/state label), with one cell per variant column.
type matrixRow struct {
	Label    string
	Cells    map[CoverageVariant]matrixCell
	Total    int
	Passed   int
	Failed   int
	Untested int
}

// coverageMatrix is the 2D pass/fail grid for one resource type: rows are
// elements/parameters, columns are variants.
type coverageMatrix struct {
	Resource string
	Rows     []matrixRow
	Columns  []CoverageVariant
}

type matrixRowKey struct {
	label string
}

func matrixKeyFor(it HTMLItem) matrixRowKey {
	switch CoverageDomain(it.Domain) {
	case CoverageDomainSearch:
		code := it.SearchCode
		if it.Variant == string(CoverageVariantSearchCombination) && it.SearchCodeB != "" {
			code = code + "+" + it.SearchCodeB
		}
		return matrixRowKey{label: it.Resource + "?" + code}
	case CoverageDomainOperation:
		return matrixRowKey{label: it.Resource + " · " + shortVariant(CoverageVariant(it.Variant))}
	case CoverageDomainState:
		return matrixRowKey{label: it.Resource + " · " + shortVariant(CoverageVariant(it.Variant))}
	default:
		elem := it.ElementPath
		if elem == "" {
			elem = it.Resource
		}
		return matrixRowKey{label: elem}
	}
}

// buildMatrices pivots executed items into per-resource coverage matrices. Rows
// are keyed by the item's element path, search code, or operation/state variant;
// columns are the distinct variants exercised for that resource. Cells are
// colored pass/fail/untested.
func buildMatrices(items []HTMLItem) []coverageMatrix {
	// rowIdx maps resource -> rowKey -> *matrixRow.
	rowIdx := make(map[string]map[matrixRowKey]*matrixRow)
	// colIdx maps resource -> rowKey -> ordered distinct variants.
	colIdx := make(map[string]map[matrixRowKey][]CoverageVariant)
	resourceOrder := make([]string, 0)
	resourceSeen := make(map[string]struct{})

	for _, it := range items {
		if it.Resource == "" || it.Variant == "" {
			continue
		}
		if _, ok := resourceSeen[it.Resource]; !ok {
			resourceSeen[it.Resource] = struct{}{}
			resourceOrder = append(resourceOrder, it.Resource)
			rowIdx[it.Resource] = make(map[matrixRowKey]*matrixRow)
			colIdx[it.Resource] = make(map[matrixRowKey][]CoverageVariant)
		}

		key := matrixKeyFor(it)
		row := rowIdx[it.Resource][key]
		if row == nil {
			row = &matrixRow{Label: key.label, Cells: make(map[CoverageVariant]matrixCell)}
			rowIdx[it.Resource][key] = row
		}
		variant := CoverageVariant(it.Variant)
		row.Cells[variant] = matrixCell{RequirementID: it.ID, HumanID: it.HumanID, Passed: it.Passed, Tested: true}
		row.Total++
		if it.Passed {
			row.Passed++
		} else {
			row.Failed++
		}
		// Track column order (variant) for the row.
		found := false
		for _, c := range colIdx[it.Resource][key] {
			if c == variant {
				found = true
				break
			}
		}
		if !found {
			colIdx[it.Resource][key] = append(colIdx[it.Resource][key], variant)
		}
	}

	// Compute the global ordered variant columns across all rows of a resource.
	matrices := make([]coverageMatrix, 0, len(resourceOrder))
	for _, r := range resourceOrder {
		colSet := make(map[CoverageVariant]struct{})
		for _, variants := range colIdx[r] {
			for _, v := range variants {
				colSet[v] = struct{}{}
			}
		}
		columns := make([]CoverageVariant, 0, len(colSet))
		for v := range colSet {
			columns = append(columns, v)
		}
		sort.Slice(columns, func(i, j int) bool { return columns[i] < columns[j] })

		rows := make([]matrixRow, 0, len(rowIdx[r]))
		for _, row := range rowIdx[r] {
			// Fill untested columns.
			for _, v := range columns {
				if _, ok := row.Cells[v]; !ok {
					row.Cells[v] = matrixCell{}
					row.Untested++
				}
			}
			rows = append(rows, *row)
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].Label < rows[j].Label })

		matrices = append(matrices, coverageMatrix{Resource: r, Rows: rows, Columns: columns})
	}
	return matrices
}

// RenderHTML produces a self-contained HTML report with drill-down navigation:
// overall contractual coverage, a pass/fail coverage matrix, per-domain
// percentages, and per-domain / per-resource / per-variant lists where every
// item shows pass/fail and expands to its assertion, request URL/body, and
// response body.
func RenderHTML(evaluation EvaluationReport, items []HTMLItem) ([]byte, error) {
	rep := buildHTMLReport(evaluation, items)
	var buf bytesBuffer
	if err := reportTemplate.Execute(&buf, rep); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func buildHTMLReport(evaluation EvaluationReport, items []HTMLItem) htmlReport {
	hasEvaluation := evaluation.TotalRequirements > 0
	matrices := buildMatrices(items)
	return htmlReport{
		CoveragePercent:       overallPercent(evaluation, items),
		TotalRequirements:     totalRequirements(evaluation, items),
		CoveredRequirements:   coveredRequirements(evaluation, items),
		UncoveredRequirements: totalRequirements(evaluation, items) - coveredRequirements(evaluation, items),
		Matrices:              matrices,
		DomainGlossary:        sortedGlossaryDomains(),
		VariantGlossary:       sortedGlossaryVariants(),
		Sections: []htmlSection{
			{Title: "By Domain", Drills: groupItems(items, func(it HTMLItem) string { return it.Domain }, func(name string) float64 {
				return drillPercent(evaluation, items, name, hasEvaluation, func(it HTMLItem) string { return it.Domain }, func(n string) float64 { return percentOf(evaluation.ByDomain, n) })
			})},
			{Title: "By Resource Type", Drills: groupItems(items, func(it HTMLItem) string { return it.Resource }, func(name string) float64 {
				return drillPercent(evaluation, items, name, hasEvaluation, func(it HTMLItem) string { return it.Resource }, func(n string) float64 { return percentOfResource(evaluation.ByResourceType, n) })
			})},
			{Title: "By Variant", Drills: groupItems(items, func(it HTMLItem) string { return it.Variant }, func(name string) float64 {
				return drillPercent(evaluation, items, name, hasEvaluation, func(it HTMLItem) string { return it.Variant }, func(n string) float64 { return percentOfVariant(evaluation.ByVariant, n) })
			})},
		},
	}
}

// overallPercent returns the coverage percentage: from the evaluation when it
// has obligations, otherwise derived from the executed items (passed / total).
func overallPercent(evaluation EvaluationReport, items []HTMLItem) float64 {
	if evaluation.TotalRequirements > 0 {
		return evaluation.CoveragePercent
	}
	passed := 0
	for _, it := range items {
		if it.Passed {
			passed++
		}
	}
	return percent(passed, len(items))
}

func totalRequirements(evaluation EvaluationReport, items []HTMLItem) int {
	if evaluation.TotalRequirements > 0 {
		return evaluation.TotalRequirements
	}
	return len(items)
}

func coveredRequirements(evaluation EvaluationReport, items []HTMLItem) int {
	if evaluation.TotalRequirements > 0 {
		return evaluation.CoveredRequirements
	}
	covered := 0
	for _, it := range items {
		if it.Passed {
			covered++
		}
	}
	return covered
}

// drillPercent returns the coverage percentage for a drill group. When the
// evaluation has per-group data it uses that; otherwise it derives the
// percentage from the executed items in the group (passed / total).
func drillPercent(evaluation EvaluationReport, items []HTMLItem, name string, hasEvaluation bool, key func(HTMLItem) string, fromEvaluation func(string) float64) float64 {
	if hasEvaluation {
		return fromEvaluation(name)
	}
	passed, total := 0, 0
	for _, it := range items {
		if key(it) != name {
			continue
		}
		total++
		if it.Passed {
			passed++
		}
	}
	return percent(passed, total)
}

func percent(passed, total int) float64 {
	if total <= 0 {
		return 0
	}
	return (float64(passed) / float64(total)) * 100
}

func groupItems(items []HTMLItem, key func(HTMLItem) string, percent func(string) float64) []htmlDrill {
	byName := make(map[string][]HTMLItem)
	for _, it := range items {
		byName[key(it)] = append(byName[key(it)], it)
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)

	drills := make([]htmlDrill, 0, len(names))
	for _, name := range names {
		groupItems := byName[name]
		drill := htmlDrill{Name: name, Percent: percent(name)}
		for _, it := range groupItems {
			drill.Total++
			if it.Passed {
				drill.Passed++
			} else {
				drill.Failed++
			}
		}
		drill.Groups = splitByPolarity(groupItems)
		drills = append(drills, drill)
	}
	return drills
}

// splitByPolarity divides items into Positive (accept) and Negative (reject)
// sub-groups using CoverageVariant.IsReject, preserving the original item order
// within each sub-group.
func splitByPolarity(items []HTMLItem) []htmlPolarityGroup {
	pos := htmlPolarityGroup{Name: "Positive"}
	neg := htmlPolarityGroup{Name: "Negative"}
	for _, it := range items {
		if CoverageVariant(it.Variant).IsReject() {
			neg.Total++
			if it.Passed {
				neg.Passed++
			} else {
				neg.Failed++
			}
			neg.Items = append(neg.Items, it)
		} else {
			pos.Total++
			if it.Passed {
				pos.Passed++
			} else {
				pos.Failed++
			}
			pos.Items = append(pos.Items, it)
		}
	}
	pos.Percent = percent(pos.Passed, pos.Total)
	neg.Percent = percent(neg.Passed, neg.Total)
	groups := make([]htmlPolarityGroup, 0, 2)
	if pos.Total > 0 {
		groups = append(groups, pos)
	}
	if neg.Total > 0 {
		groups = append(groups, neg)
	}
	return groups
}

func percentOf(m map[CoverageDomain]DomainCoverageSummary, name string) float64 {
	if s, ok := m[CoverageDomain(name)]; ok {
		return s.CoveragePercent
	}
	return 0
}

func percentOfResource(m map[string]DomainCoverageSummary, name string) float64 {
	if s, ok := m[name]; ok {
		return s.CoveragePercent
	}
	return 0
}

func percentOfVariant(m map[CoverageVariant]DomainCoverageSummary, name string) float64 {
	if s, ok := m[CoverageVariant(name)]; ok {
		return s.CoveragePercent
	}
	return 0
}

// sortedGlossaryDomains returns the domain glossary sorted by name.
func sortedGlossaryDomains() []glossaryEntry {
	descs := DomainDescriptions()
	names := make([]CoverageDomain, 0, len(descs))
	for d := range descs {
		names = append(names, d)
	}
	sort.Slice(names, func(i, j int) bool { return names[i] < names[j] })
	entries := make([]glossaryEntry, 0, len(names))
	for _, d := range names {
		entries = append(entries, glossaryEntry{Name: string(d), Description: descs[d]})
	}
	return entries
}

// sortedGlossaryVariants returns the variant glossary sorted by name.
func sortedGlossaryVariants() []glossaryEntry {
	descs := VariantDescriptions()
	names := make([]CoverageVariant, 0, len(descs))
	for v := range descs {
		names = append(names, v)
	}
	sort.Slice(names, func(i, j int) bool { return names[i] < names[j] })
	entries := make([]glossaryEntry, 0, len(names))
	for _, v := range names {
		entries = append(entries, glossaryEntry{Name: string(v), Description: descs[v]})
	}
	return entries
}

// matrixCellClass returns the CSS class for a matrix cell based on test outcome.
func matrixCellClass(c matrixCell) template.CSS {
	if !c.Tested {
		return template.CSS("cell-untested")
	}
	if c.Passed {
		return template.CSS("cell-pass")
	}
	return template.CSS("cell-fail")
}

// matrixCellGlyph returns a symbol for a matrix cell outcome.
func matrixCellGlyph(c matrixCell) string {
	if !c.Tested {
		return "—"
	}
	if c.Passed {
		return "✓"
	}
	return "✗"
}

func rowFillStyle(percent float64) template.CSS {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	return template.CSS(fmt.Sprintf("--success-pct: %.1f%%;", percent))
}

var reportTemplate = template.Must(template.New("report").Funcs(template.FuncMap{
	"rowFillStyle": rowFillStyle,
	"itemPercent": func(passed bool) float64 {
		if passed {
			return 100
		}
		return 0
	},
	"matrixCellClass": matrixCellClass,
	"matrixCellGlyph": matrixCellGlyph,
	"itemLabel": func(it HTMLItem) string {
		if it.HumanID != "" {
			return it.HumanID
		}
		return it.ID
	},
}).Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Momus Coverage Report</title>
<style>
  body { font-family: system-ui, sans-serif; margin: 2rem; color: #222; }
  h1 { font-size: 1.5rem; }
  h2 { font-size: 1.2rem; margin-top: 2rem; }
  .overall { background:#f5f5f5; padding:1rem; border-radius:8px; margin-bottom:1.5rem; }
  .overall .pct { font-size:2rem; font-weight:700; }
  .stats { display:flex; gap:2.5rem; flex-wrap:wrap; margin-top:0.5rem; }
  .stat .num { font-size:1.3rem; font-weight:600; }
  .ok { color:#1b7f2b; } .uncovered { color:#c62828; }
  .pass { color:#1b7f2b; font-weight:600; }
  .fail { color:#c62828; font-weight:600; }
  details { margin:0.25rem 0; border:1px solid #ddd; border-radius:6px; padding:0.25rem 0.75rem; }
  details details { margin-left:0.5rem; }
	summary { cursor:pointer; font-weight:600; }
	.coverage-row {
		background-image: linear-gradient(90deg, #d9f5de 0, #d9f5de var(--success-pct), transparent var(--success-pct), transparent 100%);
		border-radius:4px;
		padding:0.25rem 0.5rem;
		margin:-0.25rem -0.5rem;
	}
  dl { margin:0.5rem 0; }
  dt { font-weight:600; margin-top:0.4rem; }
  dd { margin:0 0 0 1rem; }
  pre { background:#f8f8f8; border:1px solid #eee; padding:0.5rem; overflow-x:auto; font-size:0.8rem; white-space:pre-wrap; word-break:break-word; }

  .matrix { border-collapse:collapse; margin:1rem 0; width:100%; }
  .matrix th, .matrix td { border:1px solid #ddd; padding:.4rem .6rem; text-align:center; font-size:0.85rem; }
  .matrix th { background:#f5f5f5; }
  .matrix td:first-child { text-align:left; font-weight:600; }
  .cell-pass { background:#d9f5de; }
  .cell-fail { background:#f5d5d5; }
  .cell-untested { background:#eee; color:#999; }
  .glossary table { border-collapse:collapse; width:100%; margin-top:1rem; }
  .glossary th, .glossary td { border:1px solid #ddd; padding:.4rem .6rem; text-align:left; }
  .glossary th { background:#f5f5f5; }
</style>
</head>
<body>
<h1>Momus Coverage Report</h1>
<div class="overall">
  <div class="pct">{{printf "%.1f" .CoveragePercent}}%</div>
  <div class="stats">
    <div class="stat"><div class="num ok">{{.CoveredRequirements}}</div>covered</div>
    <div class="stat"><div class="num uncovered">{{.UncoveredRequirements}}</div>uncovered</div>
    <div class="stat"><div class="num">{{.TotalRequirements}}</div>obligations</div>
  </div>
</div>

{{range $m := .Matrices}}
<h2>Coverage Matrix: {{$m.Resource}}</h2>
<table class="matrix">
<thead><tr><th>Element / Parameter</th>{{range $col := $m.Columns}}<th>{{$col}}</th>{{end}}</tr></thead>
<tbody>
{{range $row := $m.Rows}}
<tr>
<td>{{$row.Label}}</td>
{{range $col := $m.Columns}}
{{$cell := index $row.Cells $col}}
<td class="{{matrixCellClass $cell}}" title="{{$cell.RequirementID}}">{{matrixCellGlyph $cell}}</td>
{{end}}
</tr>
{{end}}
</tbody>
</table>
{{end}}

{{range .Sections}}{{template "section" .}}{{end}}

<details class="glossary">
<summary>Glossary</summary>
<h3>Domains</h3>
<table>
<thead><tr><th>Domain</th><th>Description</th></tr></thead>
<tbody>
{{range $g := .DomainGlossary}}<tr><td>{{$g.Name}}</td><td>{{$g.Description}}</td></tr>{{end}}
</tbody>
</table>
<h3>Variants</h3>
<table>
<thead><tr><th>Variant</th><th>Description</th></tr></thead>
<tbody>
{{range $g := .VariantGlossary}}<tr><td>{{$g.Name}}</td><td>{{$g.Description}}</td></tr>{{end}}
</tbody>
</table>
</details>

{{define "section"}}
<h2>{{.Title}}</h2>
{{range $group := .Drills}}
<details>
	<summary class="coverage-row" style="{{rowFillStyle $group.Percent}}">{{$group.Name}} — {{printf "%.1f" $group.Percent}}% ({{$group.Passed}} passed / {{$group.Failed}} failed / {{$group.Total}} total)</summary>
  {{range $pol := $group.Groups}}
  <details>
		<summary class="coverage-row" style="{{rowFillStyle $pol.Percent}}">{{$pol.Name}} — {{printf "%.1f" $pol.Percent}}% ({{$pol.Passed}} passed / {{$pol.Failed}} failed / {{$pol.Total}} total)</summary>
    {{range $item := $pol.Items}}
    <details>
			<summary class="coverage-row" style="{{rowFillStyle (itemPercent $item.Passed)}}">{{if $item.Passed}}<span class="pass">PASS</span>{{else}}<span class="fail">FAIL</span>{{end}} — {{itemLabel $item}}{{if $item.Description}} — {{$item.Description}}{{end}}</summary>
      <dl>
        <dt>Requirement ID</dt><dd>{{$item.ID}}</dd>
        <dt>Assert</dt><dd>{{$item.Expression}}</dd>
        <dt>Domain</dt><dd>{{$item.Domain}}</dd>
        <dt>Resource</dt><dd>{{$item.Resource}}</dd>
        <dt>Variant</dt><dd>{{$item.Variant}}</dd>
        <dt>Status</dt><dd>{{$item.StatusCode}}</dd>
        <dt>Request</dt><dd>{{$item.RequestMethod}} {{$item.RequestURL}}</dd>
        <dt>Request Body</dt><dd><pre>{{$item.RequestBody}}</pre></dd>
        <dt>Response Body</dt><dd><pre>{{$item.ResponseBody}}</pre></dd>
      </dl>
    </details>
    {{end}}
  </details>
  {{else}}<p>No items.</p>{{end}}
</details>
{{else}}<p>No items.</p>{{end}}
{{end}}
</body>
</html>`))

// bytesBuffer is a minimal in-memory writer used to render the HTML report.
type bytesBuffer struct {
	b []byte
}

func (b *bytesBuffer) Write(p []byte) (int, error) {
	b.b = append(b.b, p...)
	return len(p), nil
}

func (b *bytesBuffer) Bytes() []byte { return b.b }
