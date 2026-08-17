package model

// Dataset is the result of generating resources: generated state, not
// execution. It contains no knowledge of how tests are planned or run.
type Dataset struct {
	Resources     map[string]*ResourceInstance
	Relationships []Reference
}

// ResourceInstance is a single generated resource instance.
//
// LocalID is the Momus-assigned ID used within a dataset; ServerID is the
// ID assigned by the target FHIR server once the resource is provisioned.
type ResourceInstance struct {
	LocalID      string
	ResourceType string
	Profile      string
	Resource     map[string]any
	ServerID     string
	Version      string
}

// Reference is a relationship between two generated resource instances.
type Reference struct {
	SourceID string
	Path     string
	TargetID string
}
