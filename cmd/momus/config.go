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

	// Derivation scoping
	includeResourceTypes []string
	includeProfileURLs   []string
	excludePathPrefixes  []string
	mustSupportOnly      bool
	includeOptional      bool
	includeLowValuePaths bool
	interactionStrength  int

	// Target FHIR server / API
	baseURL           string
	writeBaseURL      string
	capabilityBaseURL string
	scopeToCapability bool
	failOnUncovered   bool
	apiBearerToken    string
	apiBasicUsername  string
	apiBasicPassword  string
	includeCases      bool

	// Generation
	exhaustive        bool
	bulkCount         int
	bulkPerTypeCounts []string
}
