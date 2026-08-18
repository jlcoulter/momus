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
)

// CapabilityFetchOptions configures live CapabilityStatement retrieval.
type CapabilityFetchOptions struct {
	HTTPClient    *http.Client
	BearerToken   string
	BasicUsername string
	BasicPassword string
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

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
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
			resources = append(resources, model.CapabilityStatementRestResource{
				Type:             resource.Type,
				Profile:          resource.Profile,
				SupportedProfile: resource.SupportedProfile,
				Interaction:      interactions,
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
