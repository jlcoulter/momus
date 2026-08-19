package provisioning

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/jlcoulter/momus/internal/fhir/model"
)

// Options configures a ServerProvisioner.
type Options struct {
	// HTTPClient is used for requests. When nil a default client is used.
	HTTPClient *http.Client
	// Headers are added to every request.
	Headers map[string]string
	// BearerToken, when non-empty, is sent as a Bearer authorization header.
	BearerToken string
	// BasicUsername and BasicPassword enable HTTP basic auth.
	BasicUsername string
	BasicPassword string
}

// ServerProvisioner writes a Dataset to a FHIR server by PUTting each
// resource to {baseURL}/{resourceType}/{id}, ordered so referenced targets are
// created before the resources that reference them.
type ServerProvisioner struct {
	baseURL string
	options Options
}

// New returns a ServerProvisioner that writes to baseURL (e.g.
// "http://host/fhir"). Pass nil Options for defaults.
func New(baseURL string, options *Options) *ServerProvisioner {
	opts := Options{}
	if options != nil {
		opts = *options
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &ServerProvisioner{baseURL: baseURL, options: opts}
}

// Result reports the outcome of a best-effort provisioning pass.
type Result struct {
	// Provisioned is the number of resources successfully uploaded.
	Provisioned int
	// Failed is the number of resources the server rejected.
	Failed int
	// FailedIDs lists the local ids of the failed resources, in provisioning
	// order.
	FailedIDs []string
	// Failures describes each failed resource in provisioning order, including
	// why the server rejected it (parsed from the OperationOutcome response when
	// available) and the payload that was rejected.
	Failures []Failure
}

// Failure describes a single resource the server rejected during provisioning.
type Failure struct {
	// ID is the local id of the failed resource.
	ID string
	// ResourceType is the FHIR resource type of the failed resource.
	ResourceType string
	// Status is the HTTP status code the server returned; 0 when the request
	// itself failed (transport error, marshal error).
	Status int
	// Reason is a human-readable summary of why the resource was rejected:
	// OperationOutcome issue diagnostics when the server returned one, the
	// response body otherwise.
	Reason string
	// Response is the raw response body, when one was received.
	Response string
	// Resource is the JSON payload that was rejected, when it could be
	// marshalled.
	Resource json.RawMessage
}

// Describe returns a one-line summary of the failure suitable for CLI output.
func (f Failure) Describe() string {
	status := fmt.Sprintf("HTTP %d", f.Status)
	if f.Status == 0 {
		status = "request failed"
	}
	return fmt.Sprintf("%s/%s (%s): %s", f.ResourceType, f.ID, status, f.Reason)
}

// Complete reports whether every resource in the dataset was provisioned.
func (r *Result) Complete() bool {
	return r != nil && r.Failed == 0 && r.Provisioned > 0
}

// ProvisionAll attempts to upload every resource in ds, in dependency order
// (targets before dependents), continuing past per-resource failures and
// recording them so the caller can report exactly what was seeded and what was
// not.
func (p *ServerProvisioner) ProvisionAll(ctx context.Context, ds *model.Dataset) *Result {
	res := &Result{}
	if p.baseURL == "" || ds == nil {
		return res
	}
	for _, id := range provisionOrder(ds) {
		instance := ds.Resources[id]
		if instance == nil {
			continue
		}
		if err := p.provisionInstance(ctx, instance); err != nil {
			res.Failed++
			res.FailedIDs = append(res.FailedIDs, id)
			res.Failures = append(res.Failures, failureFromError(id, instance, err))
			continue
		}
		res.Provisioned++
	}
	return res
}

func (p *ServerProvisioner) provisionInstance(ctx context.Context, instance *model.ResourceInstance) error {
	body, err := json.Marshal(instance.Resource)
	if err != nil {
		return fmt.Errorf("marshal %s/%s: %w", instance.ResourceType, instance.LocalID, err)
	}
	url := fmt.Sprintf("%s/%s/%s", p.baseURL, instance.ResourceType, instance.LocalID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/fhir+json")
	for k, v := range p.options.Headers {
		req.Header.Set(k, v)
	}
	p.applyAuth(req)

	resp, err := p.options.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("provision %s/%s: %w", instance.ResourceType, instance.LocalID, err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("provision %s/%s: read response: %w", instance.ResourceType, instance.LocalID, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &provisionError{
			resourceType: instance.ResourceType,
			localID:      instance.LocalID,
			status:       resp.StatusCode,
			body:         respBody,
		}
	}
	instance.ServerID = instance.LocalID
	instance.Version = resp.Header.Get("ETag")
	return nil
}

// provisionError carries the server's rejection of a single resource, including
// the response body so the caller can surface why the server rejected it.
type provisionError struct {
	resourceType string
	localID      string
	status       int
	body         []byte
}

func (e *provisionError) Error() string {
	reason := operationOutcomeReason(e.body)
	if reason == "" {
		if len(e.body) == 0 {
			reason = "no response body"
		} else {
			reason = truncate(strings.TrimSpace(string(e.body)), maxReasonLength)
		}
	}
	return fmt.Sprintf("provision %s/%s: unexpected status %d: %s", e.resourceType, e.localID, e.status, reason)
}

// maxReasonLength caps the reason carried in a failure so a verbose server
// response cannot flood CLI output; full bodies are kept in Failure.Response.
const maxReasonLength = 500

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// failureFromError converts a provisioning error into a structured Failure,
// attaching the rejected payload so the failure can be replayed or inspected.
func failureFromError(id string, instance *model.ResourceInstance, err error) Failure {
	f := Failure{ID: id, ResourceType: instance.ResourceType, Reason: err.Error()}
	if body, marshalErr := json.Marshal(instance.Resource); marshalErr == nil {
		f.Resource = body
	}
	var pe *provisionError
	if errors.As(err, &pe) {
		f.Status = pe.status
		f.Reason = operationOutcomeReason(pe.body)
		if f.Reason == "" {
			f.Reason = truncate(strings.TrimSpace(string(pe.body)), maxReasonLength)
		}
		if f.Reason == "" {
			f.Reason = "no response body"
		}
		f.Response = string(pe.body)
	}
	return f
}

// operationOutcomeReason extracts a concise, human-readable reason from a FHIR
// OperationOutcome response body (as returned by HAPI and most FHIR servers on
// validation failure). Returns "" when the body is not a parseable
// OperationOutcome.
func operationOutcomeReason(body []byte) string {
	var outcome struct {
		ResourceType string `json:"resourceType"`
		Issue        []struct {
			Severity    string `json:"severity"`
			Diagnostics string `json:"diagnostics"`
			Details     *struct {
				Text string `json:"text"`
			} `json:"details"`
			Location   []string `json:"location"`
			Expression []string `json:"expression"`
		} `json:"issue"`
	}
	if err := json.Unmarshal(body, &outcome); err != nil || outcome.ResourceType != "OperationOutcome" {
		return ""
	}
	if len(outcome.Issue) == 0 {
		return ""
	}
	parts := make([]string, 0, len(outcome.Issue))
	for _, issue := range outcome.Issue {
		msg := issue.Diagnostics
		if msg == "" && issue.Details != nil {
			msg = issue.Details.Text
		}
		loc := ""
		if len(issue.Location) > 0 {
			loc = strings.Join(issue.Location, ", ")
		} else if len(issue.Expression) > 0 {
			loc = strings.Join(issue.Expression, ", ")
		}
		if msg == "" && loc == "" {
			continue
		}
		switch {
		case msg != "" && loc != "":
			parts = append(parts, fmt.Sprintf("%s: %s (%s)", issue.Severity, msg, loc))
		case msg != "":
			parts = append(parts, fmt.Sprintf("%s: %s", issue.Severity, msg))
		default:
			parts = append(parts, fmt.Sprintf("%s: at %s", issue.Severity, loc))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	// Cap the number of issues surfaced inline; the full response is retained
	// on the Failure for inspection.
	const maxIssues = 3
	if len(parts) > maxIssues {
		parts = append(parts[:maxIssues], fmt.Sprintf("(+%d more issues)", len(parts)-maxIssues))
	}
	return strings.Join(parts, "; ")
}

func (p *ServerProvisioner) applyAuth(req *http.Request) {
	if req.Header.Get("Authorization") != "" {
		return
	}
	if p.options.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+p.options.BearerToken)
		return
	}
	if p.options.BasicUsername != "" || p.options.BasicPassword != "" {
		req.SetBasicAuth(p.options.BasicUsername, p.options.BasicPassword)
	}
}

// provisionOrder returns resource ids in dependency order (targets first)
// using the dataset's recorded relationships. Resources without relationships
// are ordered deterministically.
func provisionOrder(ds *model.Dataset) []string {
	ids := make([]string, 0, len(ds.Resources))
	for id := range ds.Resources {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	dependents := make(map[string][]string)
	depended := make(map[string]int)
	for id := range ds.Resources {
		depended[id] = 0
	}
	for _, rel := range ds.Relationships {
		if _, ok := depended[rel.TargetID]; !ok {
			continue
		}
		if _, ok := depended[rel.SourceID]; !ok {
			continue
		}
		dependents[rel.TargetID] = append(dependents[rel.TargetID], rel.SourceID)
		depended[rel.SourceID]++
	}
	for target := range dependents {
		sort.Strings(dependents[target])
	}

	ready := make([]string, 0)
	for id, d := range depended {
		if d == 0 {
			ready = append(ready, id)
		}
	}
	sort.Strings(ready)

	order := make([]string, 0, len(ids))
	remaining := len(depended)
	for remaining > 0 && len(ready) > 0 {
		next := ready[0]
		ready = ready[1:]
		order = append(order, next)
		remaining--
		for _, child := range dependents[next] {
			depended[child]--
			if depended[child] == 0 {
				ready = append(ready, child)
				sort.Strings(ready)
			}
		}
	}
	// Append any ids not reached (e.g. cyclic relationships) in a stable order.
	reached := make(map[string]bool, len(order))
	for _, id := range order {
		reached[id] = true
	}
	for _, id := range ids {
		if !reached[id] {
			order = append(order, id)
		}
	}
	return order
}
