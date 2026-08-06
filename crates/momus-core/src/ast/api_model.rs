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

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_api_model_construction() {
        let model = ApiModel {
            name: "Test API".into(),
            resources: vec![ResourceModel {
                name: "Patient".into(),
                profile_url: Some("http://example.com/Patient".into()),
                operations: vec![OperationModel {
                    name: "create".into(),
                    method: "POST".into(),
                    path: "/Patient".into(),
                    request_body: Some(BodyModel {
                        content_type: "application/json".into(),
                        schema: Some(serde_json::json!({"type": "object"})),
                        required_fields: vec!["name".into()],
                    }),
                    responses: vec![ResponseModel {
                        status_code: 201,
                        content_type: Some("application/json".into()),
                        schema: Some(serde_json::json!({"type": "object"})),
                    }],
                }],
                search_params: vec![SearchParamModel {
                    name: "name".into(),
                    param_type: "string".into(),
                    modifiers: vec!["exact".into(), "contains".into()],
                    prefixes: vec![],
                }],
                search_include: vec!["Patient:organization".into()],
                search_revinclude: vec!["Observation:patient".into()],
                supported_profiles: vec!["http://example.com/Patient".into()],
            }],
        };

        assert_eq!(model.name, "Test API");
        assert_eq!(model.resources.len(), 1);
        assert_eq!(model.resources[0].name, "Patient");
        assert_eq!(model.resources[0].operations.len(), 1);
        assert_eq!(model.resources[0].operations[0].name, "create");
        assert_eq!(model.resources[0].search_params.len(), 1);
        assert_eq!(model.resources[0].search_include.len(), 1);
        assert_eq!(model.resources[0].search_revinclude.len(), 1);
        assert_eq!(model.resources[0].supported_profiles.len(), 1);
    }

    #[test]
    fn test_api_model_serialization_roundtrip() {
        let model = ApiModel {
            name: "Test".into(),
            resources: vec![ResourceModel {
                name: "Pet".into(),
                profile_url: None,
                operations: vec![OperationModel {
                    name: "create".into(),
                    method: "POST".into(),
                    path: "/pet".into(),
                    request_body: None,
                    responses: vec![ResponseModel {
                        status_code: 201,
                        content_type: None,
                        schema: None,
                    }],
                }],
                search_params: vec![],
                search_include: vec![],
                search_revinclude: vec![],
                supported_profiles: vec![],
            }],
        };

        let json = serde_json::to_string(&model).unwrap();
        let deserialized: ApiModel = serde_json::from_str(&json).unwrap();
        assert_eq!(deserialized.name, model.name);
        assert_eq!(deserialized.resources.len(), model.resources.len());
        assert_eq!(deserialized.resources[0].name, model.resources[0].name);
        assert_eq!(
            deserialized.resources[0].operations.len(),
            model.resources[0].operations.len()
        );
    }

    #[test]
    fn test_api_model_empty() {
        let model = ApiModel {
            name: String::new(),
            resources: vec![],
        };
        assert!(model.name.is_empty());
        assert!(model.resources.is_empty());
    }

    #[test]
    fn test_body_model_default_content_type() {
        let body = BodyModel {
            content_type: "application/json".into(),
            schema: None,
            required_fields: vec![],
        };
        assert_eq!(body.content_type, "application/json");
    }

    #[test]
    fn test_search_param_model() {
        let param = SearchParamModel {
            name: "birthdate".into(),
            param_type: "date".into(),
            modifiers: vec!["exact".into()],
            prefixes: vec!["eq".into(), "gt".into(), "lt".into()],
        };
        assert_eq!(param.name, "birthdate");
        assert_eq!(param.modifiers.len(), 1);
        assert_eq!(param.prefixes.len(), 3);
    }

    #[test]
    fn test_response_model() {
        let response = ResponseModel {
            status_code: 200,
            content_type: Some("application/fhir+json".into()),
            schema: Some(serde_json::json!({"type": "object"})),
        };
        assert_eq!(response.status_code, 200);
        assert!(response.content_type.is_some());
        assert!(response.schema.is_some());
    }

    #[test]
    fn test_resource_model_defaults() {
        let resource = ResourceModel {
            name: "Test".into(),
            profile_url: None,
            operations: vec![],
            search_params: vec![],
            search_include: vec![],
            search_revinclude: vec![],
            supported_profiles: vec![],
        };
        assert!(resource.profile_url.is_none());
        assert!(resource.operations.is_empty());
        assert!(resource.search_params.is_empty());
    }
}
