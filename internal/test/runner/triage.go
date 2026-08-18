package runner

import (
	"sort"
	"strings"
)

// TriageOutcome classifies a failed case so a user can decide whether the
// failure is a broken generated test or a server defect.
type TriageOutcome string

const (
	// TriageOutcomeAcceptRejected: a payload expected to be accepted was
	// rejected (4xx validation). Either the generated payload is invalid (a
	// test-generation issue) or the server rejects valid data.
	TriageOutcomeAcceptRejected TriageOutcome = "accept-rejected"
	// TriageOutcomeRejectAccepted: a payload expected to be rejected was
	// accepted (2xx). Either the negative mutation was ineffective (a test
	// issue) or the server is missing the validation (a server issue).
	TriageOutcomeRejectAccepted TriageOutcome = "reject-accepted"
	// TriageOutcomeServerError: the server returned a 5xx, which is almost
	// always a server defect.
	TriageOutcomeServerError TriageOutcome = "server-error"
	// TriageOutcomeAmbiguous: the status neither matches acceptance nor
	// rejection expectations; inspect individually.
	TriageOutcomeAmbiguous TriageOutcome = "ambiguous"
)

// TriageGroup aggregates failures sharing an outcome and obligation variant so
// a user can see, at a glance, whether a whole variant failed together (a
// systematic test bug) or failures are scattered (more likely server issues).
type TriageGroup struct {
	Outcome              TriageOutcome `json:"outcome"`
	Domain               string        `json:"domain,omitempty"`
	Variant              string        `json:"variant,omitempty"`
	Expected             string        `json:"expected"`
	Count                int           `json:"count"`
	ExampleRequirementID string        `json:"exampleRequirementId"`
	ExampleDescription   string        `json:"exampleDescription"`
	ExampleElementPath   string        `json:"exampleElementPath,omitempty"`
	ExampleStatus        int           `json:"exampleStatus,omitempty"`
	Hint                 string        `json:"hint"`
}

// TriageSummary is the run-level triage roll-up attached to a report.
type TriageSummary struct {
	AcceptRejected int           `json:"acceptRejected"`
	RejectAccepted int           `json:"rejectAccepted"`
	ServerError    int           `json:"serverError"`
	Ambiguous      int           `json:"ambiguous"`
	Groups         []TriageGroup `json:"groups,omitempty"`
	Hint           string        `json:"hint,omitempty"`
}

// classifyTriage maps a failed case to an outcome based on the expected
// outcome (from the assertion trace) and the actual status. It returns the
// empty string for success, which callers skip.
func classifyTriage(expected string, passed bool, statusCode int) TriageOutcome {
	switch strings.ToLower(strings.TrimSpace(expected)) {
	case "accept":
		if passed {
			return ""
		}
		if statusCode >= 500 {
			return TriageOutcomeServerError
		}
		if statusCode >= 400 && statusCode < 500 {
			return TriageOutcomeAcceptRejected
		}
		return TriageOutcomeAmbiguous
	case "reject":
		if passed {
			return ""
		}
		if statusCode >= 500 {
			return TriageOutcomeServerError
		}
		if statusCode >= 200 && statusCode < 300 {
			return TriageOutcomeRejectAccepted
		}
		return TriageOutcomeAmbiguous
	}
	return TriageOutcomeAmbiguous
}

// buildTriageSummary scans executed cases and rolls failed ones into triage
// groups keyed by outcome + domain + variant, so a systematic test bug (one
// variant failing en masse) is visible next to scattered server issues.
func buildTriageSummary(cases []CaseResult) *TriageSummary {
	summary := &TriageSummary{}
	counts := map[TriageOutcome]int{}
	type groupKey struct {
		outcome TriageOutcome
		domain  string
		variant string
	}
	groups := map[groupKey]*TriageGroup{}

	for _, c := range cases {
		if c.Passed || c.Trace == nil {
			continue
		}
		outcome := classifyTriage(c.Trace.Expected, c.Passed, c.StatusCode)
		if outcome == "" {
			continue
		}
		counts[outcome]++
		key := groupKey{outcome, c.Trace.Domain, c.Trace.Variant}
		g := groups[key]
		if g == nil {
			g = &TriageGroup{
				Outcome:              outcome,
				Domain:               c.Trace.Domain,
				Variant:              c.Trace.Variant,
				Expected:             c.Trace.Expected,
				ExampleRequirementID: c.RequirementID,
				ExampleDescription:   c.Description,
				ExampleElementPath:   c.Trace.ElementPath,
				ExampleStatus:        c.StatusCode,
				Hint:                 hintForOutcome(outcome),
			}
			groups[key] = g
		}
		g.Count++
	}

	summary.AcceptRejected = counts[TriageOutcomeAcceptRejected]
	summary.RejectAccepted = counts[TriageOutcomeRejectAccepted]
	summary.ServerError = counts[TriageOutcomeServerError]
	summary.Ambiguous = counts[TriageOutcomeAmbiguous]

	for _, g := range groups {
		summary.Groups = append(summary.Groups, *g)
	}
	sort.Slice(summary.Groups, func(i, j int) bool {
		a, b := summary.Groups[i], summary.Groups[j]
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		if a.Outcome != b.Outcome {
			return a.Outcome < b.Outcome
		}
		if a.Domain != b.Domain {
			return a.Domain < b.Domain
		}
		return a.Variant < b.Variant
	})

	summary.Hint = summaryHint(summary)
	if summary.Hint == "" && len(summary.Groups) == 0 {
		return nil
	}
	return summary
}

func hintForOutcome(outcome TriageOutcome) string {
	switch outcome {
	case TriageOutcomeAcceptRejected:
		return "expected accepted but rejected. If a whole variant/profile fails together, the generated payload is likely invalid (test-generation issue); an isolated failure more likely means the server rejects valid data."
	case TriageOutcomeRejectAccepted:
		return "expected rejected but accepted. The negative mutation may be ineffective (test issue) or the server may be missing this validation (server issue)."
	case TriageOutcomeServerError:
		return "server returned a 5xx; this is almost always a server defect to fix."
	case TriageOutcomeAmbiguous:
		return "status neither matches acceptance nor rejection expectations; inspect the individual case."
	}
	return ""
}

// summaryHint picks the most salient triage message: a systematic accept
// failure (e.g. the interaction domain) signals a broken shared payload, while
// mass reject-accepted failures signal missing server validation or ineffective
// mutations.
func summaryHint(summary *TriageSummary) string {
	if len(summary.Groups) == 0 {
		return ""
	}
	lead := summary.Groups[0]
	if lead.Outcome == TriageOutcomeAcceptRejected && lead.Count > 1 {
		if lead.Domain == "interaction" {
			return "Most failures are interaction accepts sharing one payload: the shared valid payload is likely invalid, so this is probably a test-generation bug to fix, not a server issue."
		}
		return "Most failures are accept asserts rejected for variant " + lead.Variant + ": if they share a payload, check the generated payload; if scattered, inspect the server's validation."
	}
	if lead.Outcome == TriageOutcomeRejectAccepted {
		return "Most failures are reject asserts the server accepted for variant " + lead.Variant + ": check whether the mutation is effective (test) and whether the server should reject it (server)."
	}
	if lead.Outcome == TriageOutcomeServerError {
		return "Most failures are server 5xx errors; fix the server or check for unprovisioned dependencies."
	}
	return ""
}
