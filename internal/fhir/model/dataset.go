package model

// Dataset is the result of generating resources: generated state, not
// execution. It contains no knowledge of how tests are planned or run.
type Dataset struct {
	Resources     map[string]*ResourceInstance `json:"resources"`
	Relationships []Reference                  `json:"relationships,omitempty"`
}

// ResourceInstance is a single generated resource instance.
//
// LocalID is the Momus-assigned ID used within a dataset; ServerID is the
// ID assigned by the target FHIR server once the resource is provisioned.
type ResourceInstance struct {
	LocalID      string         `json:"localId"`
	ResourceType string         `json:"resourceType"`
	Profile      string         `json:"profile,omitempty"`
	Resource     map[string]any `json:"resource"`
	ServerID     string         `json:"serverId,omitempty"`
	Version      string         `json:"version,omitempty"`
	// MarshaledJSON is the resource body pre-marshalled to JSON at synthesis
	// time, so the provisioning hot path skips per-request marshalling. It is
	// populated by generators that produce many instances (e.g. bulk corpus)
	// and is otherwise nil.
	MarshaledJSON []byte `json:"-"`
}

// Reference is a relationship between two generated resource instances.
type Reference struct {
	SourceID string `json:"sourceId"`
	Path     string `json:"path"`
	TargetID string `json:"targetId"`
}
