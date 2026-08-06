/// Format-agnostic API model — common intermediate representation
/// that any converter (OpenAPI, FHIR, GraphQL, Postman, etc.) can produce.
///
/// This is the bridge between API definitions and test generation:
///
/// ```text
/// API Definition (OpenAPI / FHIR IG / GraphQL / ...)
///     │  parse
///     ▼
///   ApiModel  ←  format-agnostic
///     │  apply TestSpec
///     ▼
///   TestPlan  ←  executable test plan
/// ```
use serde::{Deserialize, Serialize};

/// A format-agnostic API description.
///
/// Any converter (OpenAPI, FHIR, GraphQL, etc.) produces this model.
/// The test generator then applies a `TestSpec` to produce a `TestPlan`.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ApiModel {
    /// API name / description.
    pub name: String,
    /// Resource types or endpoint groups exposed by the API.
    #[serde(default)]
    pub resources: Vec<ResourceModel>,
}

/// A resource type or endpoint group in the API.
///
/// Examples: `Patient` (FHIR), `Pet` (OpenAPI Pet Store), `User` (GraphQL).
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ResourceModel {
    /// Resource type name.
    pub name: String,
    /// Optional profile/conformance URL (e.g. FHIR StructureDefinition URL).
    #[serde(default)]
    pub profile_url: Option<String>,
    /// Operations available on this resource.
    #[serde(default)]
    pub operations: Vec<OperationModel>,
    /// Search/filter parameters.
    #[serde(default)]
    pub search_params: Vec<SearchParamModel>,
    /// _include search parameters for including referenced resources.
    #[serde(default)]
    pub search_include: Vec<String>,
    /// _revinclude search parameters for reverse including resources.
    #[serde(default)]
    pub search_revinclude: Vec<String>,
    /// Supported profiles for conformance testing.
    #[serde(default)]
    pub supported_profiles: Vec<String>,
}

/// An operation on a resource (e.g. create, read, update, delete, search).
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct OperationModel {
    /// Operation name (e.g. "create", "read", "search-type").
    pub name: String,
    /// HTTP method.
    pub method: String,
    /// URL path template (may contain `{id}`, `{param}` placeholders).
    pub path: String,
    /// Request body schema (if applicable).
    #[serde(default)]
    pub request_body: Option<BodyModel>,
    /// Expected responses.
    #[serde(default)]
    pub responses: Vec<ResponseModel>,
}

/// A request body schema.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct BodyModel {
    /// Content-Type (e.g. "application/json").
    #[serde(default = "default_json_content_type")]
    pub content_type: String,
    /// JSON Schema for the body (if available).
    #[serde(default)]
    pub schema: Option<serde_json::Value>,
    /// Names of required fields.
    #[serde(default)]
    pub required_fields: Vec<String>,
}

/// A response specification.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ResponseModel {
    /// Expected HTTP status code.
    pub status_code: u16,
    /// Expected Content-Type.
    #[serde(default)]
    pub content_type: Option<String>,
    /// JSON Schema for the response body (if available).
    #[serde(default)]
    pub schema: Option<serde_json::Value>,
}

/// A search/filter parameter.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SearchParamModel {
    /// Parameter name.
    pub name: String,
    /// Parameter type (e.g. "string", "token", "reference", "date", "number").
    pub param_type: String,
    /// Applicable search modifiers (e.g. ":exact", ":contains", ":missing").
    #[serde(default)]
    pub modifiers: Vec<String>,
    /// Applicable comparison prefixes (e.g. "eq", "gt", "lt", "ge", "le").
    #[serde(default)]
    pub prefixes: Vec<String>,
}

fn default_json_content_type() -> String {
    "application/json".to_string()
}
