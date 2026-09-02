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
	"sync"
	"time"

	"github.com/jlcoulter/momus/internal/core/tracing"
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
	// Tracer, when set, logs every provisioning request and response as it is
	// made (used for --debug request/response tracing).
	Tracer *tracing.Tracer
}

// ServerProvisioner writes a Dataset to a FHIR server by PUTting each
// resource to {baseURL}/{resourceType}/{id}, ordered so referenced targets are
// created before the resources that reference them.
type ServerProvisioner struct {
	baseURL string
	options Options

	// failedIDs records local ids the server has rejected across all
	// ProvisionBatch/ProvisionAll calls on this provisioner. It lets later
	// batches strip references to targets that failed in an earlier batch (e.g.
	// a HealthcareService whose Location target failed in the Location batch),
	// avoiding a cascade of HAPI-1094 "not found" referential-integrity
	// failures. Guarded by mu.
	mu        sync.Mutex
	failedIDs map[string]bool
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
	return &ServerProvisioner{baseURL: baseURL, options: opts, failedIDs: make(map[string]bool)}
}

// snapshotFailedIDs returns a copy of the ids rejected in earlier calls so a
// batch can strip references to them. The copy is safe to read concurrently.
func (p *ServerProvisioner) snapshotFailedIDs() map[string]bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make(map[string]bool, len(p.failedIDs))
	for id := range p.failedIDs {
		out[id] = true
	}
	return out
}

// recordFailedIDs merges the given ids into the provisioner's cross-batch
// failure set.
func (p *ServerProvisioner) recordFailedIDs(ids ...string) {
	if len(ids) == 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, id := range ids {
		p.failedIDs[id] = true
	}
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

// Complete reports whether every resource in the dataset was provisioned. It is
// true whenever nothing failed, even when the dataset was empty (nothing to
// provision), so an empty, successful run is not reported as incomplete.
func (r *Result) Complete() bool {
	return r != nil && r.Failed == 0
}

// provisionOutcome records the result of provisioning a single resource.
type provisionOutcome struct {
	id       string
	instance *model.ResourceInstance
	err      error
}

// provisionLevel is a batch of resource ids to provision together. A level is
// provisioned concurrently unless serial is set, in which case ids are
// provisioned one at a time in order. Serial levels are used for cyclic
// dependencies, where a resource may reference another in the same level and
// servers enforcing referential integrity reject a resource whose target does
// not yet exist.
type provisionLevel struct {
	ids    []string
	serial bool
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
	// Cross-batch failures are carried over so a dependent whose target failed
	// in a previous call does not cascade a HAPI-1094 "not found".
	failed := p.snapshotFailedIDs()
	// Provision one dependency level at a time: every resource in a level depends
	// only on resources in earlier levels, so all of a level's PUTs can run
	// concurrently, while later levels wait for their targets to exist. Failures
	// within a level are reported in deterministic (level, sorted-id) order.
	// Cyclic levels are provisioned serially so targets precede dependents even
	// within the cycle.
	for _, level := range provisionLevels(ds) {
		for _, id := range level.ids {
			if inst := ds.Resources[id]; inst != nil && inst.Resource != nil {
				stripFailedReferences(inst.Resource, failed)
			}
		}
		outcomes := p.provisionBatch(ctx, ds, level)
		// Collect results in level order (level is already sorted) so failure
		// reporting is deterministic despite concurrent execution.
		for _, o := range outcomes {
			if o.err != nil {
				res.Failed++
				res.FailedIDs = append(res.FailedIDs, o.id)
				res.Failures = append(res.Failures, failureFromError(o.id, o.instance, o.err))
				failed[o.id] = true
				continue
			}
			res.Provisioned++
		}
	}
	p.recordFailedIDs(res.FailedIDs...)
	return res
}

// ProvisionBatch uploads the given instances and returns the outcome. It is the
// streaming counterpart to ProvisionAll: callers that generate resources
// incrementally (e.g. bulk corpus batches) can provision each batch as it is
// ready, without building a full Dataset first.
//
// A batch is not assumed to be internally independent: references are resolved
// from the instance bodies (e.g. Location.partOf → an earlier Location of the
// same batch), and instances are grouped into dependency levels so targets are
// PUT before the resources that reference them. Instances without intra-batch
// dependencies are uploaded concurrently, as are all members of a level;
// cyclic references fall back to a serial pass so targets still precede
// dependents. Failures are reported in provisioning order for determinism.
// Instance LocalIDs must be unique within the batch (the corpus generator
// guarantees this).
func (p *ServerProvisioner) ProvisionBatch(ctx context.Context, instances []*model.ResourceInstance) *Result {
	res := &Result{}
	if p.baseURL == "" || len(instances) == 0 {
		return res
	}
	ds := &model.Dataset{Resources: make(map[string]*model.ResourceInstance, len(instances))}
	for _, instance := range instances {
		if instance == nil {
			continue
		}
		ds.Resources[instance.LocalID] = instance
	}
	// Record the references that actually appear in the bodies so the
	// dependency graph reflects only real, intra-batch edges.
	recordInstanceBodyReferences(ds)
	// failed tracks local ids the server rejected: both earlier batches (via the
	// provisioner's cross-batch set) and failures within this batch. A resource
	// that references a failed target would otherwise cascade a HAPI-1094 "not
	// found" (referential integrity) failure, so references to failed targets
	// are stripped from each level's bodies before it is provisioned.
	failed := p.snapshotFailedIDs()
	for _, level := range provisionLevels(ds) {
		for _, id := range level.ids {
			if inst := ds.Resources[id]; inst != nil && inst.Resource != nil {
				stripFailedReferences(inst.Resource, failed)
			}
		}
		outcomes := p.provisionBatch(ctx, ds, level)
		for _, o := range outcomes {
			if o.err != nil {
				res.Failed++
				res.FailedIDs = append(res.FailedIDs, o.id)
				res.Failures = append(res.Failures, failureFromError(o.id, o.instance, o.err))
				failed[o.id] = true
				continue
			}
			res.Provisioned++
		}
	}
	p.recordFailedIDs(res.FailedIDs...)
	return res
}

// stripFailedReferences removes references to targets that failed to provision,
// so a dependent resource does not cascade a HAPI-1094 "not found" failure when
// the resource it references was itself rejected (e.g. for a validation error).
// Repeatable reference arrays are filtered element-wise; a scalar reference is
// removed from its parent map. This is the provisioning-time counterpart to the
// corpus's dangling-reference stripping, keyed on targets that actually failed
// during this batch rather than unresolved generation placeholders.
func stripFailedReferences(value any, failed map[string]bool) {
	if failed == nil || len(failed) == 0 {
		return
	}
	stripFailedAt(value, failed)
}

// stripFailedAt walks value, removing reference objects (or array elements)
// whose target id is in the failed set.
func stripFailedAt(value any, failed map[string]bool) {
	switch typed := value.(type) {
	case map[string]any:
		for k, v := range typed {
			if arr, ok := v.([]any); ok {
				kept := make([]any, 0, len(arr))
				for _, item := range arr {
					if refTargetFailed(item, failed) {
						continue
					}
					kept = append(kept, item)
				}
				if len(kept) != len(arr) {
					if len(kept) == 0 {
						delete(typed, k)
					} else {
						typed[k] = kept
					}
				}
				for _, item := range kept {
					stripFailedAt(item, failed)
				}
				continue
			}
			if refTargetFailed(v, failed) {
				delete(typed, k)
				continue
			}
			stripFailedAt(v, failed)
		}
	case []any:
		for _, item := range typed {
			stripFailedAt(item, failed)
		}
	}
}

// refTargetFailed reports whether a value is a reference object (or a
// single-element array of one) whose target id is in the failed set.
func refTargetFailed(v any, failed map[string]bool) bool {
	switch t := v.(type) {
	case map[string]any:
		if ref, ok := t["reference"].(string); ok {
			return failed[batchReferenceTargetID(ref)]
		}
	case []any:
		if len(t) == 1 {
			if m, ok := t[0].(map[string]any); ok {
				if ref, ok := m["reference"].(string); ok {
					return failed[batchReferenceTargetID(ref)]
				}
			}
		}
	}
	return false
}

// provisionBatch uploads the resources with the given ids, either concurrently
// (serial=false) or one at a time in order (serial=true, used for cyclic levels
// so targets precede dependents). It returns one outcome per id, in id order.
func (p *ServerProvisioner) provisionBatch(ctx context.Context, ds *model.Dataset, level provisionLevel) []provisionOutcome {
	outcomes := make([]provisionOutcome, len(level.ids))
	if level.serial {
		for i, id := range level.ids {
			instance := ds.Resources[id]
			if instance == nil {
				continue
			}
			outcomes[i] = provisionOutcome{id: instance.LocalID, instance: instance, err: p.provisionInstance(ctx, instance)}
		}
		return outcomes
	}
	var wg sync.WaitGroup
	for i, id := range level.ids {
		instance := ds.Resources[id]
		if instance == nil {
			continue
		}
		wg.Add(1)
		go func(i int, instance *model.ResourceInstance) {
			defer wg.Done()
			outcomes[i] = provisionOutcome{id: instance.LocalID, instance: instance, err: p.provisionInstance(ctx, instance)}
		}(i, instance)
	}
	wg.Wait()
	return outcomes
}

// recordInstanceBodyReferences scans every instance body in ds for "reference"
// fields of the form "<Type>/<id>" and records a relationship for each one whose
// target id is present in ds, so provisioning orders targets before dependents
// for intra-batch dependencies. Self-references are skipped: they are validated
// against the resource itself, which the server can resolve at create time.
func recordInstanceBodyReferences(ds *model.Dataset) {
	if ds == nil {
		return
	}
	ids := make(map[string]struct{}, len(ds.Resources))
	for id := range ds.Resources {
		ids[id] = struct{}{}
	}
	for _, inst := range ds.Resources {
		if inst == nil || inst.Resource == nil {
			continue
		}
		walkInstanceRefs(inst, inst.Resource, ids, ds)
	}
}

// walkInstanceRefs recursively descends an instance body, recording a
// relationship for every "reference" value of the form "<Type>/<id>" whose
// target id exists in the batch and is not the instance itself.
func walkInstanceRefs(inst *model.ResourceInstance, node any, ids map[string]struct{}, ds *model.Dataset) {
	switch v := node.(type) {
	case map[string]any:
		for key, val := range v {
			if key == "reference" {
				if ref, ok := val.(string); ok {
					if targetID := batchReferenceTargetID(ref); targetID != "" && targetID != inst.LocalID {
						if _, ok := ids[targetID]; ok {
							ds.Relationships = append(ds.Relationships, model.Reference{
								SourceID: inst.LocalID,
								Path:     key,
								TargetID: targetID,
							})
						}
					}
				}
				continue
			}
			walkInstanceRefs(inst, val, ids, ds)
		}
	case []any:
		for _, el := range v {
			walkInstanceRefs(inst, el, ids, ds)
		}
	}
}

// batchReferenceTargetID parses a FHIR reference string "<Type>/<id>" and
// returns the id portion, or "" when the reference is not a relative Type/id
// reference (e.g. "#fragment", "http://...", "urn:uuid:...").
func batchReferenceTargetID(ref string) string {
	trimmed := strings.TrimSpace(ref)
	if trimmed == "" {
		return ""
	}
	slash := strings.Index(trimmed, "/")
	if slash <= 0 || slash == len(trimmed)-1 {
		return ""
	}
	if strings.ContainsAny(trimmed, ":#?") {
		return ""
	}
	return trimmed[slash+1:]
}

func (p *ServerProvisioner) provisionInstance(ctx context.Context, instance *model.ResourceInstance) error {
	if instance.Resource == nil {
		return fmt.Errorf("provision %s/%s: resource body is nil", instance.ResourceType, instance.LocalID)
	}
	body, err := json.Marshal(instance.Resource)
	if err != nil {
		return fmt.Errorf("marshal %s/%s: %w", instance.ResourceType, instance.LocalID, err)
	}
	url := fmt.Sprintf("%s/%s/%s", strings.TrimRight(p.baseURL, "/"), instance.ResourceType, instance.LocalID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/fhir+json")
	for k, v := range p.options.Headers {
		req.Header.Set(k, v)
	}
	p.applyAuth(req)

	var reqSeq int
	if p.options.Tracer != nil {
		reqSeq = p.options.Tracer.LogRequest(req, body)
	}

	resp, err := p.options.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("provision %s/%s: %w", instance.ResourceType, instance.LocalID, err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("provision %s/%s: read response: %w", instance.ResourceType, instance.LocalID, err)
	}
	if p.options.Tracer != nil {
		p.options.Tracer.LogResponse(req, reqSeq, resp.StatusCode, resp.Header, respBody)
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

// provisionLevels returns resource ids grouped into dependency levels such that
// every resource in a level depends only on resources in earlier levels, so each
// level can be provisioned concurrently once the previous level completes.
// Resources without relationships are ordered deterministically; ids not reached
// by the dependency graph (e.g. cyclic relationships) form a final, serial level
// in an order that keeps targets ahead of dependents.
func provisionLevels(ds *model.Dataset) []provisionLevel {
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

	var levels []provisionLevel
	remaining := len(depended)
	for remaining > 0 {
		ready := make([]string, 0)
		for id, d := range depended {
			if d == 0 {
				ready = append(ready, id)
			}
		}
		if len(ready) == 0 {
			// Cycle: emit the not-yet-emitted ids in a second topological pass so
			// targets precede dependents even within the cycle, and mark the level
			// serial so it is provisioned one resource at a time. This avoids
			// PUTting a resource before its (same-cycle) target exists, which
			// servers enforcing referential integrity would reject.
			level := cycleOrder(ids, depended, dependents)
			if len(level) > 0 {
				levels = append(levels, provisionLevel{ids: level, serial: true})
			}
			break
		}
		sort.Strings(ready)
		levels = append(levels, provisionLevel{ids: ready})
		for _, id := range ready {
			depended[id] = -1 // emitted; must not be reconsidered as ready
			remaining--
			for _, child := range dependents[id] {
				depended[child]--
			}
		}
	}
	return levels
}

// cycleOrder orders the not-yet-emitted ids (those still depended upon) so that
// targets precede dependents, breaking any remaining cycle by emitting the
// smallest id that lies on a cycle. This lets a cyclic level be provisioned one
// resource at a time with targets created before the resources that reference
// them.
func cycleOrder(ids []string, depended map[string]int, dependents map[string][]string) []string {
	// remaining ids are those still depended upon (not yet emitted).
	remaining := make(map[string]bool)
	for _, id := range ids {
		if depended[id] > 0 {
			remaining[id] = true
		}
	}
	// targets[id] lists the targets id depends on that are also remaining.
	targets := make(map[string][]string)
	for target, sources := range dependents {
		for _, source := range sources {
			if remaining[source] && remaining[target] {
				targets[source] = append(targets[source], target)
			}
		}
	}
	for id := range targets {
		sort.Strings(targets[id])
	}
	// indeg counts un-emitted targets within the remaining set.
	indeg := make(map[string]int, len(remaining))
	for id := range remaining {
		indeg[id] = len(targets[id])
	}

	order := make([]string, 0, len(remaining))
	for len(remaining) > 0 {
		ready := make([]string, 0)
		for id := range remaining {
			if indeg[id] == 0 {
				ready = append(ready, id)
			}
		}
		if len(ready) == 0 {
			// Cycle: emit the smallest id that is part of a cycle so targets
			// precede dependents as much as possible.
			ready = []string{smallestCycleMember(remaining, targets)}
		}
		sort.Strings(ready)
		for _, id := range ready {
			order = append(order, id)
			delete(remaining, id)
			for _, child := range dependents[id] {
				if remaining[child] {
					indeg[child]--
				}
			}
		}
	}
	return order
}

// smallestCycleMember returns the smallest remaining id that lies on a cycle
// (can reach itself through its targets). When no node in the remaining set is
// ready, at least one such id exists.
func smallestCycleMember(remaining map[string]bool, targets map[string][]string) string {
	ids := make([]string, 0, len(remaining))
	for id := range remaining {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if onCycle(id, remaining, targets) {
			return id
		}
	}
	return ids[0]
}

// onCycle reports whether id can reach itself by following targets within the
// remaining set.
func onCycle(id string, remaining map[string]bool, targets map[string][]string) bool {
	visited := make(map[string]bool)
	var dfs func(string) bool
	dfs = func(cur string) bool {
		if cur == id {
			return true
		}
		if visited[cur] {
			return false
		}
		visited[cur] = true
		for _, t := range targets[cur] {
			if remaining[t] && dfs(t) {
				return true
			}
		}
		return false
	}
	for _, t := range targets[id] {
		if remaining[t] && dfs(t) {
			return true
		}
	}
	return false
}
