package coverage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/tracing"
)

// CapabilityFetchOptions configures live CapabilityStatement retrieval.
type CapabilityFetchOptions struct {
	HTTPClient    *http.Client
	BearerToken   string
	BasicUsername string
	BasicPassword string
	// Tracer, when set, logs the capability request and response as it is made
	// (used for --debug request/response tracing).
	Tracer *tracing.Tracer
}

// ResourceTypesFromCapabilityStatement returns resource types advertised by a single CapabilityStatement.
func ResourceTypesFromCapabilityStatement(cs *model.CapabilityStatement, requireCreateInteraction bool) []string {
	if cs == nil {
		return nil
	}

	types := make(map[string]struct{})
	for _, rest := range cs.Rest {
		if rest.Mode != "" && !strings.EqualFold(rest.Mode, "server") {
			continue
		}
		for _, resource := range rest.Resource {
			resourceType := strings.TrimSpace(resource.Type)
			if resourceType == "" {
				continue
			}
			if requireCreateInteraction && !hasInteraction(resource.Interaction, "create") {
				continue
			}
			types[resourceType] = struct{}{}
		}
	}
	if len(types) == 0 {
		return nil
	}
	out := make([]string, 0, len(types))
	for resourceType := range types {
		out = append(out, resourceType)
	}
	sort.Strings(out)
	return out
}

// SupportedProfileURLsFromCapabilityStatement returns explicit supportedProfile canonicals
// for matching server resource entries. When no supported profiles are declared,
// it returns nil so callers can fall back to broader resource-type scoping.
func SupportedProfileURLsFromCapabilityStatement(cs *model.CapabilityStatement, requireCreateInteraction bool) []string {
	if cs == nil {
		return nil
	}

	profiles := make(map[string]struct{})
	for _, rest := range cs.Rest {
		if rest.Mode != "" && !strings.EqualFold(rest.Mode, "server") {
			continue
		}
		for _, resource := range rest.Resource {
			if requireCreateInteraction && !hasInteraction(resource.Interaction, "create") {
				continue
			}
			for _, profileURL := range resource.SupportedProfile {
				profileURL = strings.TrimSpace(profileURL)
				if profileURL == "" {
					continue
				}
				profiles[profileURL] = struct{}{}
			}
		}
	}
	if len(profiles) == 0 {
		return nil
	}
	out := make([]string, 0, len(profiles))
	for profileURL := range profiles {
		out = append(out, profileURL)
	}
	sort.Strings(out)
	return out
}

// SupportedProfileURLsByResourceFromCapabilityStatement returns supportedProfile
// canonicals grouped by resource type for server mode entries.
func SupportedProfileURLsByResourceFromCapabilityStatement(cs *model.CapabilityStatement, requireCreateInteraction bool) map[string][]string {
	if cs == nil {
		return nil
	}

	grouped := make(map[string]map[string]struct{})
	for _, rest := range cs.Rest {
		if rest.Mode != "" && !strings.EqualFold(rest.Mode, "server") {
			continue
		}
		for _, resource := range rest.Resource {
			resourceType := strings.TrimSpace(resource.Type)
			if resourceType == "" {
				continue
			}
			if requireCreateInteraction && !hasInteraction(resource.Interaction, "create") {
				continue
			}
			for _, profileURL := range resource.SupportedProfile {
				profileURL = strings.TrimSpace(profileURL)
				if profileURL == "" {
					continue
				}
				if _, ok := grouped[resourceType]; !ok {
					grouped[resourceType] = make(map[string]struct{})
				}
				grouped[resourceType][profileURL] = struct{}{}
			}
		}
	}

	if len(grouped) == 0 {
		return nil
	}

	out := make(map[string][]string, len(grouped))
	for resourceType, profiles := range grouped {
		values := make([]string, 0, len(profiles))
		for profileURL := range profiles {
			values = append(values, profileURL)
		}
		sort.Strings(values)
		out[resourceType] = values
	}
	return out
}

// SearchCodesFromCapabilityStatement returns the set of search parameter codes
// declared by the server's CapabilityStatement, grouped by resource type. A
// resource entry that declares no searchParam maps to an empty slice, so a
// present-but-empty entry is distinguishable from an absent one: the former
// restricts the type to no allowed codes, the latter applies no restriction.
func SearchCodesFromCapabilityStatement(cs *model.CapabilityStatement) map[string][]string {
	if cs == nil {
		return nil
	}

	grouped := make(map[string]map[string]struct{})
	for _, rest := range cs.Rest {
		if rest.Mode != "" && !strings.EqualFold(rest.Mode, "server") {
			continue
		}
		for _, resource := range rest.Resource {
			resourceType := strings.TrimSpace(resource.Type)
			if resourceType == "" {
				continue
			}
			// Record the resource type even when it declares no searchParam so a
			// present-but-empty entry is distinguishable from an absent one: the
			// former restricts the type to no allowed codes, the latter applies no
			// restriction.
			if _, ok := grouped[resourceType]; !ok {
				grouped[resourceType] = make(map[string]struct{})
			}
			for _, sp := range resource.SearchParam {
				name := strings.TrimSpace(sp.Name)
				if name == "" {
					continue
				}
				grouped[resourceType][name] = struct{}{}
			}
		}
	}

	if len(grouped) == 0 {
		return nil
	}

	out := make(map[string][]string, len(grouped))
	for resourceType, codes := range grouped {
		values := make([]string, 0, len(codes))
		for code := range codes {
			values = append(values, code)
		}
		sort.Strings(values)
		out[resourceType] = values
	}
	return out
}

// maxCapabilityBodyBytes bounds the size of a CapabilityStatement /metadata
// response to protect against unbounded memory use.
const maxCapabilityBodyBytes = 1 << 20 // 1 MiB

// FetchCapabilityStatement loads the live server CapabilityStatement from /metadata.
func FetchCapabilityStatement(ctx context.Context, baseURL string, options CapabilityFetchOptions) (*model.CapabilityStatement, error) {
	trimmedBaseURL := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if trimmedBaseURL == "" {
		return nil, fmt.Errorf("base URL is required")
	}

	client := options.HTTPClient
	if client == nil {
		client = &http.Client{}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, trimmedBaseURL+"/metadata", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/fhir+json, application/json")
	applyCapabilityRequestAuth(req, options)

	if options.Tracer != nil {
		options.Tracer.LogRequest(req, nil)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxCapabilityBodyBytes))
	if err != nil {
		return nil, err
	}
	if options.Tracer != nil {
		options.Tracer.LogResponse(req, resp.StatusCode, resp.Header, body)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch capability statement from %s: status %d: %s", req.URL.String(), resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var raw struct {
		ResourceType string `json:"resourceType"`
		URL          string `json:"url"`
		Version      string `json:"version"`
		Name         string `json:"name"`
		Status       string `json:"status"`
		FhirVersion  string `json:"fhirVersion"`
		Rest         []struct {
			Mode     string `json:"mode"`
			Resource []struct {
				Type             string   `json:"type"`
				Profile          string   `json:"profile"`
				SupportedProfile []string `json:"supportedProfile"`
				Interaction      []struct {
					Code string `json:"code"`
				} `json:"interaction"`
				Operation []struct {
					Name       string `json:"name"`
					Definition string `json:"definition"`
				} `json:"operation"`
				SearchParam []struct {
					Name       string `json:"name"`
					Definition string `json:"definition"`
					Type       string `json:"type"`
				} `json:"searchParam"`
			} `json:"resource"`
		} `json:"rest"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode capability statement: %w", err)
	}
	if raw.ResourceType != "CapabilityStatement" {
		return nil, fmt.Errorf("expected CapabilityStatement from %s, got %q", req.URL.String(), raw.ResourceType)
	}

	rest := make([]model.CapabilityStatementRest, 0, len(raw.Rest))
	for _, restBlock := range raw.Rest {
		resources := make([]model.CapabilityStatementRestResource, 0, len(restBlock.Resource))
		for _, resource := range restBlock.Resource {
			interactions := make([]model.CapabilityStatementInteraction, 0, len(resource.Interaction))
			for _, interaction := range resource.Interaction {
				interactions = append(interactions, model.CapabilityStatementInteraction{Code: interaction.Code})
			}
			ops := make([]model.CapabilityStatementOperation, 0, len(resource.Operation))
			for _, op := range resource.Operation {
				ops = append(ops, model.CapabilityStatementOperation{Name: op.Name, Definition: op.Definition})
			}
			searchParams := make([]model.CapabilityStatementSearchParam, 0, len(resource.SearchParam))
			for _, sp := range resource.SearchParam {
				searchParams = append(searchParams, model.CapabilityStatementSearchParam{
					Name:       sp.Name,
					Definition: sp.Definition,
					Type:       sp.Type,
				})
			}
			resources = append(resources, model.CapabilityStatementRestResource{
				Type:             resource.Type,
				Profile:          resource.Profile,
				SupportedProfile: resource.SupportedProfile,
				Interaction:      interactions,
				Operation:        ops,
				SearchParam:      searchParams,
			})
		}
		rest = append(rest, model.CapabilityStatementRest{
			Mode:     restBlock.Mode,
			Resource: resources,
		})
	}

	return &model.CapabilityStatement{
		URL:         raw.URL,
		Version:     raw.Version,
		Name:        raw.Name,
		Status:      raw.Status,
		FhirVersion: raw.FhirVersion,
		Rest:        rest,
	}, nil
}

func applyCapabilityRequestAuth(req *http.Request, options CapabilityFetchOptions) {
	if req == nil || req.Header.Get("Authorization") != "" {
		return
	}
	if options.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+options.BearerToken)
		return
	}
	if options.BasicUsername != "" || options.BasicPassword != "" {
		req.SetBasicAuth(options.BasicUsername, options.BasicPassword)
	}
}

func hasInteraction(interactions []model.CapabilityStatementInteraction, code string) bool {
	for _, interaction := range interactions {
		if strings.EqualFold(strings.TrimSpace(interaction.Code), code) {
			return true
		}
	}
	return false
}
