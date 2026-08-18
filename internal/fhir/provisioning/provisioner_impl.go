package provisioning

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
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

// Provision writes every resource in ds to the server, creating targets before
// their dependents, and records each server-assigned id on the instance.
func (p *ServerProvisioner) Provision(ctx context.Context, ds *model.Dataset) error {
	if p.baseURL == "" {
		return errors.New("provisioner requires a base URL")
	}
	if ds == nil {
		return nil
	}
	for _, id := range provisionOrder(ds) {
		instance := ds.Resources[id]
		if instance == nil {
			continue
		}
		if err := p.provisionInstance(ctx, instance); err != nil {
			return err
		}
	}
	return nil
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
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("provision %s/%s: unexpected status %d", instance.ResourceType, instance.LocalID, resp.StatusCode)
	}
	instance.ServerID = instance.LocalID
	instance.Version = resp.Header.Get("ETag")
	return nil
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
