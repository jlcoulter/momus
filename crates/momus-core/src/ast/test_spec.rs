/// Composable test specification AST.
///
/// A test specification describes **what tests to generate** from an API
/// definition. It is the mirror image of the assertion AST: assertions
/// describe what to check in a response, while test specs describe what
/// tests to generate from an API model.
///
/// # Examples
///
/// ```ignore
/// // Generate CRUD + search + negative tests with 5 varied resources
/// TestSpec::AllOf(vec![
///     TestSpec::Data(DataSpec {
///         count: 5,
///         variations: vec![
///             DataVariation::HappyPath,
///             DataVariation::Minimal,
///             DataVariation::SpecialChars,
///             DataVariation::ToBeDeleted,
///         ],
///     }),
///     TestSpec::Crud(CrudSpec::default()),
///     TestSpec::Search(SearchSpec::default()),
///     TestSpec::Negative(NegativeSpec::default()),
/// ])
/// ```
use serde::{Deserialize, Serialize};

// ---------------------------------------------------------------------------
// Top-level test specification
// ---------------------------------------------------------------------------

/// A composable test specification — describes what tests to generate
/// from an API definition.
///
/// Like the assertion AST, test specs form a tree: `AllOf` and `OneOf`
/// combine sub-specs, while leaf nodes define specific test categories.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(tag = "type", rename_all = "snake_case")]
#[non_exhaustive]
pub enum TestSpec {
    // -- Combinators ---------------------------------------------------------
    /// Run all sub-specs (logical AND for test generation).
    AllOf(Vec<TestSpec>),
    /// Run one sub-spec (useful for A/B test selection).
    OneOf(Vec<TestSpec>),

    // -- Data generation -----------------------------------------------------
    /// Define how many resources to generate and with what variations.
    Data(DataSpec),

    // -- Operation tests -----------------------------------------------------
    /// Generate CRUD tests for each resource/endpoint.
    Crud(CrudSpec),
    /// Generate search/filter tests.
    Search(SearchSpec),
    /// Generate operation/action tests.
    Operation(OperationSpec),

    // -- Quality tests -------------------------------------------------------
    /// Generate negative tests (invalid inputs, undeclared operations).
    Negative(NegativeSpec),
    /// Generate edge case tests (boundaries, special chars, concurrency).
    EdgeCase(EdgeCaseSpec),
    /// Generate conformance tests (profile validation, mustSupport).
    Conformance(ConformanceSpec),
    /// Generate security tests (auth, CORS, info leaks).
    Security(SecuritySpec),
    /// Generate performance tests (response time, pagination).
    Performance(PerformanceSpec),
}

// ---------------------------------------------------------------------------
// Data generation
// ---------------------------------------------------------------------------

/// Data generation specification — controls how many resources to create
/// and what variations to include for comprehensive testing.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DataSpec {
    /// Number of resources to generate per type (default: 3).
    #[serde(default = "default_data_count")]
    pub count: u64,
    /// What variations to include in the generated data.
    #[serde(default)]
    pub variations: Vec<DataVariation>,
}

impl Default for DataSpec {
    fn default() -> Self {
        Self {
            count: 3,
            variations: vec![
                DataVariation::HappyPath,
                DataVariation::Minimal,
                DataVariation::ToBeDeleted,
            ],
        }
    }
}

/// How to vary generated data for comprehensive testing.
///
/// Each variation produces a resource with different characteristics,
/// enabling different test categories:
///
/// | Variation | Purpose |
/// |-----------|---------|
/// | `HappyPath` | All fields populated — tests happy-path CRUD |
/// | `Minimal` | Only required fields — tests server accepts minimal input |
/// | `DuplicateValue` | Same value as another resource — tests search dedup |
/// | `MissingField` | Missing a specific field — tests partial updates |
/// | `SpecialChars` | Unicode, HTML, SQL patterns — tests input sanitization |
/// | `Boundary` | Min/max values — tests boundary conditions |
/// | `ToBeDeleted` | Will be deleted during test — tests post-delete state |
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(tag = "type", rename_all = "snake_case")]
#[non_exhaustive]
pub enum DataVariation {
    /// All fields populated with valid values.
    HappyPath,
    /// Only required/minimal fields.
    Minimal,
    /// Same value as another resource (for search dedup tests).
    DuplicateValue {
        /// The field that should have a duplicate value.
        field: String,
    },
    /// Missing a specific field.
    MissingField {
        /// The field to omit.
        field: String,
    },
    /// Special characters in string fields (unicode, HTML, SQL injection).
    SpecialChars,
    /// Boundary values (min/max dates, numbers, etc.).
    Boundary {
        /// The field to set to a boundary value.
        field: String,
    },
    /// Will be deleted during test (for post-delete verification).
    ToBeDeleted,
}

// ---------------------------------------------------------------------------
// CRUD tests
// ---------------------------------------------------------------------------

/// CRUD test configuration.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CrudSpec {
    /// Generate create tests.
    #[serde(default = "default_true")]
    pub create: bool,
    /// Generate read tests.
    #[serde(default = "default_true")]
    pub read: bool,
    /// Generate vread (version read) tests.
    #[serde(default)]
    pub vread: bool,
    /// Generate update tests.
    #[serde(default = "default_true")]
    pub update: bool,
    /// Generate delete tests.
    #[serde(default = "default_true")]
    pub delete: bool,
    /// Generate patch tests.
    #[serde(default)]
    pub patch: bool,
    /// Generate history-instance tests (per-resource history).
    #[serde(default)]
    pub history_instance: bool,
    /// Generate history-type tests (type-level history).
    #[serde(default)]
    pub history_type: bool,
    /// Generate conditional create/update/delete tests.
    #[serde(default)]
    pub conditional: bool,
    /// Chain CRUD operations into sequences with state passing.
    #[serde(default = "default_true")]
    pub chain: bool,
}

impl Default for CrudSpec {
    fn default() -> Self {
        Self {
            create: true,
            read: true,
            vread: false,
            update: true,
            delete: true,
            patch: false,
            history_instance: false,
            history_type: false,
            conditional: false,
            chain: true,
        }
    }
}

// ---------------------------------------------------------------------------
// Search tests
// ---------------------------------------------------------------------------

/// Search/filter test configuration.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SearchSpec {
    /// Generate single-parameter search tests.
    #[serde(default = "default_true")]
    pub single_param: bool,
    /// Generate combined (multi-parameter) search tests.
    #[serde(default)]
    pub combined_params: bool,
    /// Generate search modifier tests (:exact, :contains, :missing, etc.).
    #[serde(default)]
    pub modifiers: bool,
    /// Generate comparison prefix tests (gt, lt, ge, le, etc.).
    #[serde(default)]
    pub prefixes: bool,
    /// Generate chained search tests (param.reference:chain).
    #[serde(default)]
    pub chained: bool,
    /// Generate _include tests.
    #[serde(default)]
    pub include: bool,
    /// Generate _revinclude tests.
    #[serde(default)]
    pub revinclude: bool,
    /// Generate result parameter tests (_summary, _count, _sort, etc.).
    #[serde(default)]
    pub result_params: Vec<String>,
    /// Values that should return empty results (negative search).
    #[serde(default)]
    pub negative_values: Vec<String>,
}

impl Default for SearchSpec {
    fn default() -> Self {
        Self {
            single_param: true,
            combined_params: false,
            modifiers: false,
            prefixes: false,
            chained: false,
            include: false,
            revinclude: false,
            result_params: vec![],
            negative_values: vec![],
        }
    }
}

// ---------------------------------------------------------------------------
// Operation tests
// ---------------------------------------------------------------------------

/// Operation/action test configuration.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct OperationSpec {
    /// Generate tests for custom operations.
    #[serde(default = "default_true")]
    pub enabled: bool,
}

impl Default for OperationSpec {
    fn default() -> Self {
        Self { enabled: true }
    }
}

// ---------------------------------------------------------------------------
// Negative tests
// ---------------------------------------------------------------------------

/// Negative test configuration.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct NegativeSpec {
    /// Test undeclared interactions (operations not in the spec).
    #[serde(default = "default_true")]
    pub undeclared_interactions: bool,
    /// Test with invalid/malformed request bodies.
    #[serde(default = "default_true")]
    pub invalid_bodies: bool,
    /// Test with malformed requests (invalid JSON, wrong Content-Type).
    #[serde(default)]
    pub malformed_requests: bool,
    /// Test authentication/authorization errors.
    #[serde(default)]
    pub auth_errors: bool,
    /// Test version conflicts (stale version, wrong If-Match).
    #[serde(default)]
    pub version_conflicts: bool,
}

impl Default for NegativeSpec {
    fn default() -> Self {
        Self {
            undeclared_interactions: true,
            invalid_bodies: true,
            malformed_requests: false,
            auth_errors: false,
            version_conflicts: false,
        }
    }
}

// ---------------------------------------------------------------------------
// Edge case tests
// ---------------------------------------------------------------------------

/// Edge case test configuration.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct EdgeCaseSpec {
    /// Test boundary values (min/max dates, numbers, etc.).
    #[serde(default = "default_true")]
    pub boundary_values: bool,
    /// Test special characters (unicode, HTML, SQL injection patterns).
    #[serde(default = "default_true")]
    pub special_characters: bool,
    /// Test with large payloads.
    #[serde(default)]
    pub large_payloads: bool,
    /// Test concurrent operations.
    #[serde(default)]
    pub concurrent_operations: bool,
    /// Test dangling references (references to non-existent resources).
    #[serde(default)]
    pub dangling_references: bool,
}

impl Default for EdgeCaseSpec {
    fn default() -> Self {
        Self {
            boundary_values: true,
            special_characters: true,
            large_payloads: false,
            concurrent_operations: false,
            dangling_references: false,
        }
    }
}

// ---------------------------------------------------------------------------
// Conformance tests
// ---------------------------------------------------------------------------

/// Conformance test configuration.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ConformanceSpec {
    /// Validate responses against profile/schema definitions.
    #[serde(default = "default_true")]
    pub profile_validation: bool,
    /// Test mustSupport field presence.
    #[serde(default = "default_true")]
    pub must_support: bool,
}

impl Default for ConformanceSpec {
    fn default() -> Self {
        Self {
            profile_validation: true,
            must_support: true,
        }
    }
}

// ---------------------------------------------------------------------------
// Security tests
// ---------------------------------------------------------------------------

/// Security test configuration.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct SecuritySpec {
    /// Test that auth is required for protected endpoints.
    #[serde(default)]
    pub auth_required: bool,
    /// Test CORS headers.
    #[serde(default)]
    pub cors: bool,
    /// Test for information leaks in error responses.
    #[serde(default)]
    pub info_leak: bool,
}

// ---------------------------------------------------------------------------
// Performance tests
// ---------------------------------------------------------------------------

/// Performance test configuration.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct PerformanceSpec {
    /// Assert response time constraints.
    #[serde(default)]
    pub response_time: bool,
    /// Test pagination (_count, _page, next links).
    #[serde(default)]
    pub pagination: bool,
}

// ---------------------------------------------------------------------------
// Default helpers
// ---------------------------------------------------------------------------

fn default_true() -> bool {
    true
}

fn default_data_count() -> u64 {
    3
}
