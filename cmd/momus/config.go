package main

// config carries the CLI's shared configuration bound to command flags.
// Commands register their flags against these fields, so state is shared across
// the command tree without threading a large set of local variables through
// main.
type config struct {
	// Global
	debug bool

	// Package resolution
	depsDir        string
	downloadDir    string
	conflictPolicy string

	// Output
	outputPath string
	htmlReport string
	outputDir  string // default ".momus/output"; navigable directory report

	// Derivation scoping
	includeResourceTypes   []string
	includeProfileURLs     []string
	excludePathPrefixes    []string
	mustSupportOnly        bool
	includeOptional        bool
	includeLowValuePaths   bool
	interactionStrength    int
	includeUniversalSearch bool

	// Target FHIR server / API
	baseURL           string
	writeBaseURL      string
	capabilityBaseURL string
	metadataFile      string
	failOnUncovered   bool
	coveragePlanPath  string
	includeCases      bool

	// Mock server
	mock     bool
	mockPort int

	// API / write authentication
	apiBearerToken     string
	apiBasicUsername   string
	apiBasicPassword   string
	writeBasicUsername string
	writeBasicPassword string

	// Generation
	exhaustive        bool
	bulkCount         int
	bulkPerTypeCounts []string

	// Validate
	profileURLs []string
	packagePath string

	// Conformance self-test
	goldenDir       string
	conformanceOut  string
	conformanceJSON bool
}
