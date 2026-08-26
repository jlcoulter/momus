package report

import (
	"bytes"
	"fmt"
	"html/template"

	"github.com/jlcoulter/momus/internal/core/coverage"
	"github.com/jlcoulter/momus/internal/core/runner"
)

// renderHTML produces a self-contained navigable index.html for a run: a
// summary table plus a per-case table linking to the case files.
func renderHTML(report *runner.Report, evaluation coverage.EvaluationReport) ([]byte, error) {
	type row struct {
		ID      string
		HumanID string
		Passed  bool
		Status  int
		File    string
		Err     string
	}
	rows := make([]row, 0, len(report.Cases))
	for _, c := range report.Cases {
		humanID := ""
		if c.Trace != nil {
			humanID = c.Trace.HumanID
		}
		rows = append(rows, row{
			ID:      c.RequirementID,
			HumanID: humanID,
			Passed:  c.Passed,
			Status:  c.StatusCode,
			File:    caseFileName(c),
			Err:     c.Error,
		})
	}

	data := struct {
		Total, Passed, Failed int
		Coverage              string
		Rows                  []row
	}{
		Total:    report.Total,
		Passed:   report.Passed,
		Failed:   report.Failed,
		Coverage: coverageString(evaluation),
		Rows:     rows,
	}

	var buf bytes.Buffer
	if err := indexTemplate.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("render html index: %w", err)
	}
	return buf.Bytes(), nil
}

func coverageString(e coverage.EvaluationReport) string {
	if e.TotalRequirements == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.1f%% (%d/%d)", e.CoveragePercent, e.CoveredRequirements, e.TotalRequirements)
}

var indexTemplate = template.Must(template.New("index").Parse(`<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>Momus run</title>
<style>
body{font-family:system-ui,sans-serif;margin:2rem}
table{border-collapse:collapse;width:100%}
th,td{border:1px solid #ddd;padding:.4rem .6rem;text-align:left}
th{background:#f5f5f5}
tr.pass td{background:#f0fff4}
tr.fail td{background:#fff5f5}
a{color:#0366d6;text-decoration:none}
</style></head><body>
<h1>Momus run report</h1>
<p><strong>{{.Total}}</strong> cases: <strong class="pass">{{.Passed}} passed</strong>, <strong class="fail">{{.Failed}} failed</strong>{{if .Coverage}}; coverage {{.Coverage}}{{end}}</p>
<table>
<thead><tr><th>Requirement</th><th>Result</th><th>Status</th><th>Detail</th><th>Case file</th></tr></thead>
<tbody>
{{range .Rows}}<tr class="{{if .Passed}}pass{{else}}fail{{end}}">
<td>{{if .HumanID}}{{.HumanID}}{{else}}{{.ID}}{{end}}</td><td>{{if .Passed}}PASS{{else}}FAIL{{end}}</td>
<td>{{if .Status}}{{.Status}}{{end}}</td>
<td>{{.Err}}</td>
<td><a href="cases/{{.File}}">{{.File}}</a></td>
</tr>{{end}}
</tbody></table>
</body></html>`))
