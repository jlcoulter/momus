package mock

import (
	"encoding/json"
	"net/http"
)

// profileAndResource extracts the declared profile URL (from meta.profile) and
// the resource as a map from a raw FHIR JSON body. It returns hasProfile=false
// when the payload is not valid JSON or carries no meta.profile.
func profileAndResource(body []byte) (profileURL string, resource map[string]any, hasProfile bool) {
	var res map[string]any
	if err := json.Unmarshal(body, &res); err != nil {
		return "", nil, false
	}
	meta, ok := res["meta"].(map[string]any)
	if !ok {
		return "", res, false
	}
	profiles, ok := meta["profile"].([]any)
	if !ok || len(profiles) == 0 {
		return "", res, false
	}
	url, ok := profiles[0].(string)
	if !ok || url == "" {
		return "", res, false
	}
	return url, res, true
}

// writeValidationFailure writes a 422 OperationOutcome naming each validation
// issue.
func writeValidationFailure(w http.ResponseWriter, issues []Issue) {
	entries := make([]any, 0, len(issues))
	for _, iss := range issues {
		diagnostics := iss.Message
		if iss.Path != "" {
			diagnostics = iss.Path + ": " + iss.Message
		}
		entries = append(entries, map[string]any{
			"severity":    "error",
			"code":        "invalid",
			"diagnostics": diagnostics,
		})
	}
	writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
		"resourceType": "OperationOutcome",
		"issue":        entries,
	})
}
