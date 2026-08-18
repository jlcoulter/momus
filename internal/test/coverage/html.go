package coverage

import (
	"html/template"
	"sort"
)

// HTMLItem is one executed case rendered in the HTML report, with pass/fail
// status and, for drill-down, its assertion and request/response detail.
type HTMLItem struct {
	ID            string
	Domain        string
	Resource      string
	Variant       string
	Expression    string
	Passed        bool
	StatusCode    int
	RequestMethod string
	RequestURL    string
	RequestBody   string
	ResponseBody  string
}

// htmlDrill is one expandable group (a domain, resource type, or variant) with
// its items and pass/fail summary.
type htmlDrill struct {
	Name    string
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
	Sections              []htmlSection
}

// htmlSection is one titled drill-down section (By Domain / By Resource Type /
// By Variant).
type htmlSection struct {
	Title  string
	Drills []htmlDrill
}

// RenderHTML produces a self-contained HTML report with drill-down navigation:
// overall contractual coverage, per-domain percentages, and per-domain /
// per-resource / per-variant lists where every item shows pass/fail and
// expands to its assertion, request URL/body, and response body.
func RenderHTML(evaluation EvaluationReport, items []HTMLItem) ([]byte, error) {
	rep := buildHTMLReport(evaluation, items)
	var buf bytesBuffer
	if err := reportTemplate.Execute(&buf, rep); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func buildHTMLReport(evaluation EvaluationReport, items []HTMLItem) htmlReport {
	return htmlReport{
		CoveragePercent:       evaluation.CoveragePercent,
		TotalRequirements:     evaluation.TotalRequirements,
		CoveredRequirements:   evaluation.CoveredRequirements,
		UncoveredRequirements: evaluation.UncoveredRequirements,
		Sections: []htmlSection{
			{Title: "By Domain", Drills: groupItems(items, func(it HTMLItem) string { return it.Domain }, func(name string) float64 { return percentOf(evaluation.ByDomain, name) })},
			{Title: "By Resource Type", Drills: groupItems(items, func(it HTMLItem) string { return it.Resource }, func(name string) float64 { return percentOfResource(evaluation.ByResourceType, name) })},
			{Title: "By Variant", Drills: groupItems(items, func(it HTMLItem) string { return it.Variant }, func(name string) float64 { return percentOfVariant(evaluation.ByVariant, name) })},
		},
	}
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
		drill := htmlDrill{Name: name, Percent: percent(name), Items: byName[name]}
		for _, it := range drill.Items {
			drill.Total++
			if it.Passed {
				drill.Passed++
			} else {
				drill.Failed++
			}
		}
		drills = append(drills, drill)
	}
	return drills
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

var reportTemplate = template.Must(template.New("report").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Momus Coverage Report</title>
<style>
  body { font-family: system-ui, sans-serif; margin: 2rem; color: #222; }
  h1 { font-size: 1.5rem; }
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
  dl { margin:0.5rem 0; }
  dt { font-weight:600; margin-top:0.4rem; }
  dd { margin:0 0 0 1rem; }
  pre { background:#f8f8f8; border:1px solid #eee; padding:0.5rem; overflow-x:auto; font-size:0.8rem; white-space:pre-wrap; word-break:break-word; }
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

{{range .Sections}}{{template "section" .}}{{end}}

{{define "section"}}
<h2>{{.Title}}</h2>
{{range $group := .Drills}}
<details>
  <summary>{{$group.Name}} — {{printf "%.1f" $group.Percent}}% ({{$group.Passed}} passed / {{$group.Failed}} failed / {{$group.Total}} total)</summary>
  {{range $item := $group.Items}}
  <details>
    <summary>{{if $item.Passed}}<span class="pass">PASS</span>{{else}}<span class="fail">FAIL</span>{{end}} — {{$item.ID}}</summary>
    <dl>
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
