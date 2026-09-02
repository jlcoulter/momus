package main

// config carries the CLI's shared configuration bound to command flags.
// Commands register their flags against these fields, so state is shared across
// the command tree without threading a large set of local variables through
// main.
//
// Values are populated from (highest to lowest priority):
//  1. Explicitly-set CLI flags
//  2. Environment variables (MOMUS_ prefix)
//  3. Config file (--config path, ./momus.toml, or ~/.momus/config.toml)
//  4. Defaults
//
// The mapstructure tags give the config file / env var key. The flag tags give
// the CLI flag name. Fields are exported so that viper's mapstructure decoder
// can set them.
type config struct {
	// Config file
	ConfigFile string `mapstructure:"config_file" flag:"config"`

	// Global
	Debug bool `mapstructure:"debug" flag:"debug"`

	// Package resolution
	DepsDir        string `mapstructure:"deps_dir" flag:"deps-dir"`
	DownloadDir    string `mapstructure:"download_dir" flag:"download-dir"`
	ConflictPolicy string `mapstructure:"conflict_policy" flag:"conflict-policy"`

	// Output
	OutputPath string `mapstructure:"output_path" flag:"output"`
	HtmlReport string `mapstructure:"html_report" flag:"html"`
	OutputDir  string `mapstructure:"output_dir" flag:"output-dir,dir"` // default "$HOME/.momus/output"; navigable directory report

	// Derivation scoping
	IncludeResourceTypes   []string `mapstructure:"include_resource_types" flag:"include-resource"`
	IncludeProfileURLs     []string `mapstructure:"include_profile_urls" flag:"include-profile-url"`
	ExcludePathPrefixes    []string `mapstructure:"exclude_path_prefixes" flag:"exclude-path-prefix"`
	MustSupportOnly        bool     `mapstructure:"must_support_only" flag:"must-support-only"`
	IncludeOptional        bool     `mapstructure:"include_optional" flag:"include-optional"`
	IncludeLowValuePaths   bool     `mapstructure:"include_low_value_paths" flag:"include-low-value-paths"`
	InteractionStrength    int      `mapstructure:"interaction_strength" flag:"strength"`
	IncludeUniversalSearch bool     `mapstructure:"include_universal_search" flag:"include-universal-search"`
	IncludeDomains         []string `mapstructure:"include_domains" flag:"include-domain"`
	ExcludeVariants        []string `mapstructure:"exclude_variants" flag:"exclude-variant"`
	ExcludeExtensionURLs   []string `mapstructure:"exclude_extension_urls" flag:"exclude-extension-url"`

	// Target FHIR server / API
	BaseURL           string `mapstructure:"base_url" flag:"base-url"`
	WriteBaseURL      string `mapstructure:"write_base_url" flag:"write-base-url"`
	CapabilityBaseURL string `mapstructure:"capability_base_url" flag:"capability-base-url"`
	MetadataFile      string `mapstructure:"metadata_file" flag:"metadata"`
	FailOnUncovered   bool   `mapstructure:"fail_on_uncovered" flag:"fail-on-uncovered"`
	CoveragePlanPath  string `mapstructure:"coverage_plan_path" flag:"coverage-plan"`
	IncludeCases      bool   `mapstructure:"include_cases" flag:"include-cases"`

	// Mock server
	Mock     bool `mapstructure:"mock" flag:"mock"`
	MockPort int  `mapstructure:"mock_port" flag:"mock-port"`

	// API / write authentication
	ApiBearerToken     string `mapstructure:"api_bearer_token" flag:"api-bearer-token"`
	ApiBasicUsername   string `mapstructure:"api_basic_username" flag:"api-basic-username"`
	ApiBasicPassword   string `mapstructure:"api_basic_password" flag:"api-basic-password"`
	WriteBasicUsername string `mapstructure:"write_basic_username" flag:"write-basic-username"`
	WriteBasicPassword string `mapstructure:"write_basic_password" flag:"write-basic-password"`

	// Generation
	Exhaustive        bool     `mapstructure:"exhaustive" flag:"exhaustive"`
	BulkCount         int      `mapstructure:"bulk_count" flag:"count"`
	BulkBatchSize     int      `mapstructure:"bulk_batch_size" flag:"batch-size"`
	BulkPipelineDepth int      `mapstructure:"bulk_pipeline_depth" flag:"pipeline-depth"`
	Concurrency       int      `mapstructure:"concurrency" flag:"concurrency"`
	BulkPerTypeCounts []string `mapstructure:"bulk_per_type_counts" flag:"per-type"`

	// Validate
	ProfileURLs []string `mapstructure:"profile_urls" flag:"profile"`
	PackagePath string   `mapstructure:"package_path" flag:"package"`

	// Karate export
	KarateOutDir      string `mapstructure:"karate_out_dir" flag:"output-dir,dir"`
	GenerateKarateCfg bool   `mapstructure:"generate_karate_config" flag:"karate-config"`

	// Conformance self-test
	GoldenDir       string `mapstructure:"golden_dir" flag:"fixtures"`
	ConformanceOut  string `mapstructure:"conformance_out" flag:"output"`
	ConformanceJSON bool   `mapstructure:"conformance_json" flag:"output-format"`
}
