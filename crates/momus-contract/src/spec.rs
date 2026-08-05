//! API spec parsing and response validation.
//!
//! Supports OpenAPI 3.x (YAML/JSON) and GraphQL SDL specs.
//! Validates status codes, response bodies, and content types
//! against the declared spec for each endpoint.

use crate::report::{ContractViolation, FieldCoverage};
use anyhow::{Context, Result};
use momus_core::ast::Method;
use std::collections::HashMap;

// ---------------------------------------------------------------------------
// Spec type detection
// ---------------------------------------------------------------------------

/// Detected spec format.
#[derive(Debug, Clone, Copy, PartialEq)]
pub enum SpecType {
    OpenAPI,
    GraphQL,
    Unknown,
}

impl std::fmt::Display for SpecType {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            SpecType::OpenAPI => write!(f, "OpenAPI"),
            SpecType::GraphQL => write!(f, "GraphQL"),
            SpecType::Unknown => write!(f, "Unknown"),
        }
    }
}

/// Detect the spec type from file extension and content.
pub fn detect_spec_type(path: &str, content: &str) -> SpecType {
    let lower = path.to_lowercase();
    if lower.ends_with(".yaml") || lower.ends_with(".yml") {
        if content.contains("openapi:")
            || content.contains("openapi ")
            || content.contains("swagger:")
            || content.contains("swagger ")
        {
            SpecType::OpenAPI
        } else {
            SpecType::Unknown
        }
    } else if lower.ends_with(".json") {
        if content.contains("\"openapi\"") || content.contains("\"swagger\"") {
            SpecType::OpenAPI
        } else {
            SpecType::Unknown
        }
    } else if lower.ends_with(".graphql") || lower.ends_with(".gql") || lower.ends_with(".sdl") {
        SpecType::GraphQL
    } else {
        SpecType::Unknown
    }
}

// ---------------------------------------------------------------------------
// Parsed spec representation
// ---------------------------------------------------------------------------

/// A parsed API spec ready for response validation.
#[derive(Debug)]
pub enum ParsedSpec {
    OpenAPI(OpenApiSpec),
    GraphQL(GraphQlSpec),
}

impl std::fmt::Display for ParsedSpec {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            ParsedSpec::OpenAPI(_) => write!(f, "OpenAPI"),
            ParsedSpec::GraphQL(_) => write!(f, "GraphQL"),
        }
    }
}

impl ParsedSpec {
    /// Parse a spec from file content.
    pub fn parse(path: &str, content: &str) -> Result<Self> {
        let spec_type = detect_spec_type(path, content);
        match spec_type {
            SpecType::OpenAPI => {
                let spec = OpenApiSpec::parse(content)
                    .with_context(|| format!("Failed to parse OpenAPI spec: {path}"))?;
                Ok(ParsedSpec::OpenAPI(spec))
            }
            SpecType::GraphQL => {
                let spec = GraphQlSpec::parse(content)
                    .with_context(|| format!("Failed to parse GraphQL spec: {path}"))?;
                Ok(ParsedSpec::GraphQL(spec))
            }
            SpecType::Unknown => {
                anyhow::bail!(
                    "Unknown spec type for '{path}'. Supported: .yaml/.yml/.json (OpenAPI), .graphql/.gql/.sdl (GraphQL)"
                );
            }
        }
    }

    /// Validate a response against this spec.
    ///
    /// Returns violations and field coverage information.
    pub fn validate(
        &self,
        method: &Method,
        url: &str,
        status_code: u16,
        headers: &HashMap<String, String>,
        body: &Option<serde_json::Value>,
    ) -> (Vec<ContractViolation>, Vec<FieldCoverage>) {
        match self {
            ParsedSpec::OpenAPI(spec) => spec.validate(method, url, status_code, headers, body),
            ParsedSpec::GraphQL(spec) => spec.validate(method, url, status_code, headers, body),
        }
    }
}

// ---------------------------------------------------------------------------
// OpenAPI spec
// ---------------------------------------------------------------------------

/// Parsed OpenAPI 3.x spec.
#[derive(Debug)]
pub struct OpenApiSpec {
    /// Map of (method, path) -> endpoint info.
    endpoints: HashMap<(String, String), EndpointInfo>,
}

/// Information about a single endpoint in an OpenAPI spec.
#[derive(Debug)]
struct EndpointInfo {
    /// Declared status codes and their response schemas.
    responses: HashMap<u16, ResponseInfo>,
    /// Declared content types for responses.
    content_types: Vec<String>,
}

/// Information about a single response in an OpenAPI spec.
#[derive(Debug)]
struct ResponseInfo {
    /// JSON Schema for the response body (if any).
    schema: Option<serde_json::Value>,
}

impl OpenApiSpec {
    /// Parse an OpenAPI 3.x spec from YAML or JSON content.
    fn parse(content: &str) -> Result<Self> {
        let spec: openapiv3::OpenAPI = if content.trim().starts_with('{') {
            serde_json::from_str(content).with_context(|| "Failed to parse JSON OpenAPI spec")?
        } else {
            serde_yaml::from_str(content).with_context(|| "Failed to parse YAML OpenAPI spec")?
        };

        let mut endpoints: HashMap<(String, String), EndpointInfo> = HashMap::new();

        for (path_str, path_item) in &spec.paths.paths {
            let path_item = match path_item {
                openapiv3::ReferenceOr::Item(item) => item,
                openapiv3::ReferenceOr::Reference { .. } => continue,
            };

            let operations: Vec<(&'static str, Option<&openapiv3::Operation>)> = vec![
                ("GET", path_item.get.as_ref()),
                ("POST", path_item.post.as_ref()),
                ("PUT", path_item.put.as_ref()),
                ("DELETE", path_item.delete.as_ref()),
                ("PATCH", path_item.patch.as_ref()),
                ("HEAD", path_item.head.as_ref()),
                ("OPTIONS", path_item.options.as_ref()),
            ];

            for (method, operation) in operations {
                let operation = match operation {
                    Some(op) => op,
                    None => continue,
                };

                let mut responses: HashMap<u16, ResponseInfo> = HashMap::new();
                let mut path_content_types: Vec<String> = Vec::new();

                for (status_code, response_ref) in &operation.responses.responses {
                    let response = match response_ref {
                        openapiv3::ReferenceOr::Item(r) => r,
                        openapiv3::ReferenceOr::Reference { .. } => continue,
                    };

                    let code = match status_code {
                        openapiv3::StatusCode::Code(n) => *n,
                        openapiv3::StatusCode::Range(n) => *n * 100,
                    };

                    let mut content_types: Vec<String> = Vec::new();
                    let mut schema: Option<serde_json::Value> = None;

                    for (ct, media_type) in &response.content {
                        content_types.push(ct.clone());
                        if let Some(s) = &media_type.schema
                            && schema.is_none()
                        {
                            schema = resolve_schema_ref(s, &spec);
                        }
                    }

                    path_content_types.extend(content_types.clone());

                    responses.insert(code, ResponseInfo { schema });
                }

                endpoints.insert(
                    (method.to_string(), path_str.clone()),
                    EndpointInfo {
                        responses,
                        content_types: path_content_types,
                    },
                );
            }
        }

        Ok(OpenApiSpec { endpoints })
    }

    /// Validate a response against this OpenAPI spec.
    fn validate(
        &self,
        method: &Method,
        url: &str,
        status_code: u16,
        headers: &HashMap<String, String>,
        body: &Option<serde_json::Value>,
    ) -> (Vec<ContractViolation>, Vec<FieldCoverage>) {
        let mut violations = Vec::new();
        let mut coverage = Vec::new();

        // Normalise the URL path (strip query string)
        let path = url.split('?').next().unwrap_or(url);
        let method_str = method.to_string();

        // Try exact match first, then try matching path parameters
        let endpoint = self
            .endpoints
            .get(&(method_str.clone(), path.to_string()))
            .or_else(|| self.match_path(&method_str, path));

        let endpoint = match endpoint {
            Some(e) => e,
            None => {
                violations.push(ContractViolation {
                    endpoint: url.to_string(),
                    method: method_str,
                    status: status_code,
                    description: format!("Endpoint not declared in spec: {method} {path}"),
                    severity: "error".to_string(),
                });
                return (violations, coverage);
            }
        };

        // Validate status code
        if let Some(response_info) = endpoint.responses.get(&status_code) {
            // Status code is declared — validate body and content type
            let content_type = headers
                .get("content-type")
                .map(|s| s.as_str())
                .unwrap_or("");

            // Validate Content-Type
            if !content_type.is_empty() && !endpoint.content_types.is_empty() {
                let ct_matches = endpoint.content_types.iter().any(|declared_ct| {
                    content_type
                        .to_lowercase()
                        .contains(&declared_ct.to_lowercase())
                        || declared_ct
                            .to_lowercase()
                            .contains(&content_type.to_lowercase())
                });
                if !ct_matches {
                    violations.push(ContractViolation {
                        endpoint: url.to_string(),
                        method: method_str.clone(),
                        status: status_code,
                        description: format!(
                            "Content-Type '{}' not declared in spec. Declared: {:?}",
                            content_type, endpoint.content_types
                        ),
                        severity: "warning".to_string(),
                    });
                }
            }

            // Validate body against schema
            if let Some(schema) = &response_info.schema {
                if let Some(body_val) = body {
                    match validate_json_schema(schema, body_val) {
                        Ok(fields) => {
                            coverage.extend(fields.into_iter().map(|f| FieldCoverage {
                                endpoint: url.to_string(),
                                method: method_str.clone(),
                                field_path: f,
                                exercised: true,
                            }));
                        }
                        Err(errors) => {
                            violations.push(ContractViolation {
                                endpoint: url.to_string(),
                                method: method_str.clone(),
                                status: status_code,
                                description: format!(
                                    "Response body does not match schema:\n{}",
                                    errors.join("\n")
                                ),
                                severity: "error".to_string(),
                            });
                        }
                    }
                } else {
                    violations.push(ContractViolation {
                        endpoint: url.to_string(),
                        method: method_str.clone(),
                        status: status_code,
                        description: "Response body is not valid JSON for a declared schema"
                            .to_string(),
                        severity: "error".to_string(),
                    });
                }
            }

            // Track which fields from the response were exercised
            if let Some(body_val) = body {
                let exercised = extract_exercised_fields(body_val);
                for field in exercised {
                    coverage.push(FieldCoverage {
                        endpoint: url.to_string(),
                        method: method_str.clone(),
                        field_path: field,
                        exercised: true,
                    });
                }
            }
        } else {
            // Status code not declared in spec
            let declared_codes: Vec<u16> = endpoint.responses.keys().copied().collect();
            violations.push(ContractViolation {
                endpoint: url.to_string(),
                method: method_str,
                status: status_code,
                description: format!(
                    "Status code {status_code} not declared in spec. Declared: {declared_codes:?}"
                ),
                severity: "error".to_string(),
            });
        }

        (violations, coverage)
    }

    /// Try to match a URL path against spec paths with path parameters.
    fn match_path<'a>(&'a self, method: &str, path: &str) -> Option<&'a EndpointInfo> {
        let segments: Vec<&str> = path.split('/').filter(|s| !s.is_empty()).collect();

        for ((m, spec_path), info) in &self.endpoints {
            if m != method {
                continue;
            }
            let spec_segments: Vec<&str> = spec_path.split('/').filter(|s| !s.is_empty()).collect();

            if segments.len() != spec_segments.len() {
                continue;
            }

            let matched = segments.iter().zip(&spec_segments).all(|(actual, spec)| {
                spec.starts_with('{') && spec.ends_with('}') || actual == spec
            });

            if matched {
                return Some(info);
            }
        }

        None
    }
}

// ---------------------------------------------------------------------------
// GraphQL spec
// ---------------------------------------------------------------------------

/// Parsed GraphQL SDL spec.
#[derive(Debug)]
pub struct GraphQlSpec {
    /// Known query and mutation return types.
    operations: Vec<GraphQlOperation>,
}

/// A single GraphQL operation type.
#[derive(Debug)]
struct GraphQlOperation {
    /// Operation name (e.g. "Query", "Mutation").
    name: String,
    /// Field names and their return types.
    fields: HashMap<String, GraphQlField>,
}

/// A single GraphQL field.
#[derive(Debug)]
#[allow(dead_code)]
struct GraphQlField {
    /// Field name.
    name: String,
    /// Return type name (e.g. "String", "User", "[User]").
    type_name: String,
    /// Whether the field is non-nullable.
    non_null: bool,
}

impl GraphQlSpec {
    /// Parse a GraphQL SDL string.
    fn parse(content: &str) -> Result<Self> {
        let mut operations = Vec::new();

        // Simple line-based parser for common SDL patterns.
        let mut current_type: Option<String> = None;
        let mut current_fields: HashMap<String, GraphQlField> = HashMap::new();

        for line in content.lines() {
            let trimmed = line.trim();

            // Skip comments and empty lines
            if trimmed.is_empty() || trimmed.starts_with('#') {
                continue;
            }

            // Detect type definition start
            if trimmed.starts_with("type ") || trimmed.starts_with("extend type ") {
                // Save previous type
                if let Some(name) = current_type.take()
                    && !current_fields.is_empty()
                {
                    operations.push(GraphQlOperation {
                        name,
                        fields: std::mem::take(&mut current_fields),
                    });
                }

                let name = trimmed
                    .trim_start_matches("extend type ")
                    .trim_start_matches("type ")
                    .split('{')
                    .next()
                    .unwrap_or("")
                    .trim()
                    .to_string();

                if !name.is_empty() {
                    current_type = Some(name);
                }
                continue;
            }

            // Detect end of type definition
            if trimmed == "}" || trimmed == "} " {
                if let Some(name) = current_type.take()
                    && !current_fields.is_empty()
                {
                    operations.push(GraphQlOperation {
                        name,
                        fields: std::mem::take(&mut current_fields),
                    });
                }
                continue;
            }

            // Parse field definitions inside a type
            if current_type.is_some() && trimmed.contains(':') {
                // Handle arguments in parentheses: field(arg: Type): ReturnType
                let clean = trimmed.trim_end_matches(',').trim_end_matches(';');
                // Find the colon that separates field name from type (skip parenthesized args)
                let colon_pos = clean.find(':').unwrap();
                let before_colon = &clean[..colon_pos];
                let after_colon = &clean[colon_pos + 1..];

                // If there are parentheses in the field name part, the real colon is after them
                let field_name = if before_colon.contains('(') {
                    before_colon.split('(').next().unwrap_or("").trim()
                } else {
                    before_colon.trim()
                };

                if field_name.is_empty() {
                    continue;
                }

                let type_str = after_colon.trim().to_string();
                let non_null = type_str.ends_with('!');
                let type_name = type_str.trim_end_matches('!').to_string();

                current_fields.insert(
                    field_name.to_string(),
                    GraphQlField {
                        name: field_name.to_string(),
                        type_name,
                        non_null,
                    },
                );
            }
        }

        // Save last type
        if let Some(name) = current_type
            && !current_fields.is_empty()
        {
            operations.push(GraphQlOperation {
                name,
                fields: current_fields,
            });
        }

        Ok(GraphQlSpec { operations })
    }

    /// Validate a response against this GraphQL spec.
    fn validate(
        &self,
        _method: &Method,
        url: &str,
        status_code: u16,
        _headers: &HashMap<String, String>,
        body: &Option<serde_json::Value>,
    ) -> (Vec<ContractViolation>, Vec<FieldCoverage>) {
        let mut violations = Vec::new();
        let mut coverage = Vec::new();

        let body = match body {
            Some(b) => b,
            None => {
                violations.push(ContractViolation {
                    endpoint: url.to_string(),
                    method: "POST".to_string(),
                    status: status_code,
                    description: "GraphQL response body is not valid JSON".to_string(),
                    severity: "error".to_string(),
                });
                return (violations, coverage);
            }
        };

        // GraphQL responses must have 'data' or 'errors'
        let has_data = body.get("data").is_some();
        let has_errors = body.get("errors").is_some();

        if !has_data && !has_errors {
            let keys: Vec<&str> = body
                .as_object()
                .map(|o| o.keys().map(|k| k.as_str()).collect())
                .unwrap_or_default();
            violations.push(ContractViolation {
                endpoint: url.to_string(),
                method: "POST".to_string(),
                status: status_code,
                description: format!(
                    "GraphQL response missing 'data' or 'errors' field, got: {keys:?}"
                ),
                severity: "error".to_string(),
            });
            return (violations, coverage);
        }

        // Validate 'data' shape against schema
        if let Some(data) = body.get("data").and_then(|d| d.as_object()) {
            // Only check against root operation types (Query, Mutation, Subscription)
            let root_ops: Vec<&GraphQlOperation> = self
                .operations
                .iter()
                .filter(|op| {
                    op.name == "Query" || op.name == "Mutation" || op.name == "Subscription"
                })
                .collect();

            for (field_name, field_value) in data {
                // Try to find this field in any root operation type
                let mut found = false;
                for op in &root_ops {
                    if let Some(spec_field) = op.fields.get(field_name) {
                        found = true;
                        coverage.push(FieldCoverage {
                            endpoint: url.to_string(),
                            method: "POST".to_string(),
                            field_path: format!("data.{}.{}", op.name, field_name),
                            exercised: true,
                        });

                        // Check for non-null fields that are null
                        if spec_field.non_null && field_value.is_null() {
                            violations.push(ContractViolation {
                                endpoint: url.to_string(),
                                method: "POST".to_string(),
                                status: status_code,
                                description: format!(
                                    "Non-nullable field '{field_name}' is null in response"
                                ),
                                severity: "error".to_string(),
                            });
                        }
                    }
                }

                if !found {
                    // Undocumented field
                    violations.push(ContractViolation {
                        endpoint: url.to_string(),
                        method: "POST".to_string(),
                        status: status_code,
                        description: format!("Undocumented field '{field_name}' in response data"),
                        severity: "info".to_string(),
                    });
                }
            }
        }

        // Check for errors
        if let Some(errors) = body.get("errors").and_then(|e| e.as_array()) {
            for (i, err) in errors.iter().enumerate() {
                if let Some(msg) = err.get("message").and_then(|m| m.as_str()) {
                    violations.push(ContractViolation {
                        endpoint: url.to_string(),
                        method: "POST".to_string(),
                        status: status_code,
                        description: format!("GraphQL error #{i}: {msg}"),
                        severity: "warning".to_string(),
                    });
                }
            }
        }

        (violations, coverage)
    }
}

// ---------------------------------------------------------------------------
// JSON Schema validation helpers
// ---------------------------------------------------------------------------

/// Validate a JSON value against a JSON Schema.
/// Returns Ok with exercised field paths, or Err with validation errors.
fn validate_json_schema(
    schema: &serde_json::Value,
    instance: &serde_json::Value,
) -> Result<Vec<String>, Vec<String>> {
    let validator =
        jsonschema::validator_for(schema).map_err(|e| vec![format!("Invalid JSON Schema: {e}")])?;

    let errors: Vec<String> = validator
        .iter_errors(instance)
        .map(|e| format!("  - {}: {}", e.instance_path(), e))
        .collect();

    if errors.is_empty() {
        Ok(extract_schema_field_paths(schema))
    } else {
        Err(errors)
    }
}

/// Extract field paths from a JSON Schema for coverage tracking.
fn extract_schema_field_paths(schema: &serde_json::Value) -> Vec<String> {
    let mut paths = Vec::new();
    collect_schema_paths(schema, "$", &mut paths);
    paths
}

fn collect_schema_paths(schema: &serde_json::Value, prefix: &str, paths: &mut Vec<String>) {
    match schema.get("type").and_then(|t| t.as_str()) {
        Some("object") => {
            if let Some(properties) = schema.get("properties").and_then(|p| p.as_object()) {
                for (name, prop_schema) in properties {
                    let path = format!("{prefix}.{name}");
                    paths.push(path.clone());
                    collect_schema_paths(prop_schema, &path, paths);
                }
            }
        }
        Some("array") => {
            if let Some(items) = schema.get("items") {
                let path = format!("{prefix}[]");
                collect_schema_paths(items, &path, paths);
            }
        }
        _ => {}
    }

    // Handle allOf/oneOf/anyOf
    for key in &["allOf", "oneOf", "anyOf"] {
        if let Some(schemas) = schema.get(*key).and_then(|s| s.as_array()) {
            for sub in schemas {
                collect_schema_paths(sub, prefix, paths);
            }
        }
    }
}

/// Extract exercised field paths from a response body.
fn extract_exercised_fields(value: &serde_json::Value) -> Vec<String> {
    let mut paths = Vec::new();
    collect_exercised_paths(value, "$", &mut paths);
    paths
}

fn collect_exercised_paths(value: &serde_json::Value, prefix: &str, paths: &mut Vec<String>) {
    match value {
        serde_json::Value::Object(map) => {
            for (key, val) in map {
                let path = format!("{prefix}.{key}");
                paths.push(path.clone());
                collect_exercised_paths(val, &path, paths);
            }
        }
        serde_json::Value::Array(arr) => {
            paths.push(prefix.to_string());
            for (i, val) in arr.iter().enumerate() {
                let path = format!("{prefix}[{i}]");
                paths.push(path.clone());
                collect_exercised_paths(val, &path, paths);
            }
        }
        _ => {
            paths.push(prefix.to_string());
        }
    }
}

// ---------------------------------------------------------------------------
// OpenAPI schema resolution
// ---------------------------------------------------------------------------

/// Resolve a `$ref` reference or inline schema to a JSON Value.
fn resolve_schema_ref(
    schema: &openapiv3::ReferenceOr<openapiv3::Schema>,
    spec: &openapiv3::OpenAPI,
) -> Option<serde_json::Value> {
    match schema {
        openapiv3::ReferenceOr::Item(s) => Some(schema_to_json_value(s)),
        openapiv3::ReferenceOr::Reference { reference } => resolve_ref(reference, spec),
    }
}

/// Resolve a boxed schema reference.
fn resolve_boxed_schema_ref(
    schema: &openapiv3::ReferenceOr<Box<openapiv3::Schema>>,
    spec: &openapiv3::OpenAPI,
) -> Option<serde_json::Value> {
    match schema {
        openapiv3::ReferenceOr::Item(s) => Some(schema_to_json_value(s)),
        openapiv3::ReferenceOr::Reference { reference } => resolve_ref(reference, spec),
    }
}

/// Convert an openapiv3 Schema to a serde_json::Value (JSON Schema representation).
fn schema_to_json_value(schema: &openapiv3::Schema) -> serde_json::Value {
    let mut map = serde_json::Map::new();

    // Map the schema kind to JSON Schema
    match &schema.schema_kind {
        openapiv3::SchemaKind::Type(t) => {
            type_to_json_schema(t, &mut map);
        }
        openapiv3::SchemaKind::AllOf { all_of } => {
            let items: Vec<serde_json::Value> = all_of
                .iter()
                .filter_map(|s| match s {
                    openapiv3::ReferenceOr::Item(sub) => Some(schema_to_json_value(sub)),
                    openapiv3::ReferenceOr::Reference { .. } => None,
                })
                .collect();
            map.insert("allOf".to_string(), serde_json::Value::Array(items));
        }
        openapiv3::SchemaKind::OneOf { one_of } => {
            let items: Vec<serde_json::Value> = one_of
                .iter()
                .filter_map(|s| match s {
                    openapiv3::ReferenceOr::Item(sub) => Some(schema_to_json_value(sub)),
                    openapiv3::ReferenceOr::Reference { .. } => None,
                })
                .collect();
            map.insert("oneOf".to_string(), serde_json::Value::Array(items));
        }
        openapiv3::SchemaKind::AnyOf { any_of } => {
            let items: Vec<serde_json::Value> = any_of
                .iter()
                .filter_map(|s| match s {
                    openapiv3::ReferenceOr::Item(sub) => Some(schema_to_json_value(sub)),
                    openapiv3::ReferenceOr::Reference { .. } => None,
                })
                .collect();
            map.insert("anyOf".to_string(), serde_json::Value::Array(items));
        }
        openapiv3::SchemaKind::Not { not } => {
            if let Some(val) = resolve_schema_ref(not, &openapiv3::OpenAPI::default()) {
                map.insert("not".to_string(), val);
            }
        }
        openapiv3::SchemaKind::Any(_) => {
            // Any schema — no constraints
        }
    }

    // Add nullable
    if schema.schema_data.nullable {
        map.insert("nullable".to_string(), serde_json::Value::Bool(true));
    }

    // Add description
    if let Some(desc) = &schema.schema_data.description {
        map.insert(
            "description".to_string(),
            serde_json::Value::String(desc.clone()),
        );
    }

    serde_json::Value::Object(map)
}

/// Convert an openapiv3 Type to JSON Schema properties.
fn type_to_json_schema(t: &openapiv3::Type, map: &mut serde_json::Map<String, serde_json::Value>) {
    match t {
        openapiv3::Type::String(st) => {
            map.insert(
                "type".to_string(),
                serde_json::Value::String("string".to_string()),
            );
            if let Some(pattern) = &st.pattern {
                map.insert(
                    "pattern".to_string(),
                    serde_json::Value::String(pattern.clone()),
                );
            }
            if !st.enumeration.is_empty() {
                let values: Vec<serde_json::Value> = st
                    .enumeration
                    .iter()
                    .filter_map(|v| v.as_ref().cloned().map(serde_json::Value::String))
                    .collect();
                map.insert("enum".to_string(), serde_json::Value::Array(values));
            }
        }
        openapiv3::Type::Number(_) => {
            map.insert(
                "type".to_string(),
                serde_json::Value::String("number".to_string()),
            );
        }
        openapiv3::Type::Integer(_) => {
            map.insert(
                "type".to_string(),
                serde_json::Value::String("integer".to_string()),
            );
        }
        openapiv3::Type::Boolean(_) => {
            map.insert(
                "type".to_string(),
                serde_json::Value::String("boolean".to_string()),
            );
        }
        openapiv3::Type::Array(arr) => {
            map.insert(
                "type".to_string(),
                serde_json::Value::String("array".to_string()),
            );
            if let Some(items) = &arr.items
                && let Some(val) = resolve_boxed_schema_ref(items, &openapiv3::OpenAPI::default())
            {
                map.insert("items".to_string(), val);
            }
        }
        openapiv3::Type::Object(obj) => {
            map.insert(
                "type".to_string(),
                serde_json::Value::String("object".to_string()),
            );
            let mut properties = serde_json::Map::new();
            for (name, prop) in &obj.properties {
                if let Some(val) = resolve_boxed_schema_ref(prop, &openapiv3::OpenAPI::default()) {
                    properties.insert(name.clone(), val);
                }
            }
            if !properties.is_empty() {
                map.insert(
                    "properties".to_string(),
                    serde_json::Value::Object(properties),
                );
            }
            if !obj.required.is_empty() {
                let required: Vec<serde_json::Value> = obj
                    .required
                    .iter()
                    .map(|r| serde_json::Value::String(r.clone()))
                    .collect();
                map.insert("required".to_string(), serde_json::Value::Array(required));
            }
        }
    }
}

/// Resolve a `$ref` reference within an OpenAPI spec.
fn resolve_ref(ref_path: &str, spec: &openapiv3::OpenAPI) -> Option<serde_json::Value> {
    // Handle `#/components/schemas/Name` references
    if let Some(schema_name) = ref_path.strip_prefix("#/components/schemas/")
        && let Some(components) = &spec.components
        && let Some(schema_ref) = components.schemas.get(schema_name)
    {
        return match schema_ref {
            openapiv3::ReferenceOr::Item(s) => Some(schema_to_json_value(s)),
            openapiv3::ReferenceOr::Reference { reference } => resolve_ref(reference, spec),
        };
    }
    None
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    use super::*;

    // -----------------------------------------------------------------------
    // Spec type detection
    // -----------------------------------------------------------------------

    #[test]
    fn test_detect_openapi_yaml() {
        assert_eq!(
            detect_spec_type("openapi.yaml", "openapi: 3.0.0"),
            SpecType::OpenAPI
        );
    }

    #[test]
    fn test_detect_openapi_yml() {
        assert_eq!(
            detect_spec_type("spec.yml", "openapi: 3.0.0"),
            SpecType::OpenAPI
        );
    }

    #[test]
    fn test_detect_swagger() {
        assert_eq!(
            detect_spec_type("swagger.yaml", "swagger: '2.0'"),
            SpecType::OpenAPI
        );
    }

    #[test]
    fn test_detect_openapi_json() {
        assert_eq!(
            detect_spec_type("spec.json", r#"{"openapi":"3.0.0"}"#),
            SpecType::OpenAPI
        );
    }

    #[test]
    fn test_detect_graphql() {
        assert_eq!(
            detect_spec_type("schema.graphql", "type Query {"),
            SpecType::GraphQL
        );
        assert_eq!(
            detect_spec_type("schema.gql", "type Query {"),
            SpecType::GraphQL
        );
        assert_eq!(
            detect_spec_type("schema.sdl", "type Query {"),
            SpecType::GraphQL
        );
    }

    #[test]
    fn test_detect_unknown() {
        assert_eq!(detect_spec_type("data.xml", "<xml/>"), SpecType::Unknown);
        assert_eq!(
            detect_spec_type("service.proto", "syntax ="),
            SpecType::Unknown
        );
    }

    // -----------------------------------------------------------------------
    // OpenAPI spec parsing
    // -----------------------------------------------------------------------

    fn openapi_yaml_spec() -> String {
        r#"openapi: "3.0.0"
info:
  title: Test API
  version: "1.0"
paths:
  /health:
    get:
      operationId: healthCheck
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                type: object
                properties:
                  status:
                    type: string
  /users:
    post:
      operationId: createUser
      requestBody:
        content:
          application/json:
            schema:
              type: object
              properties:
                name:
                  type: string
      responses:
        "201":
          description: Created
          content:
            application/json:
              schema:
                type: object
                properties:
                  id:
                    type: integer
                  name:
                    type: string
  /users/{id}:
    get:
      operationId: getUser
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: integer
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                type: object
                properties:
                  id:
                    type: integer
                  name:
                    type: string
                  email:
                    type: string
"#
        .to_string()
    }

    #[test]
    fn test_parse_openapi_yaml() {
        let spec = OpenApiSpec::parse(&openapi_yaml_spec()).unwrap();
        assert!(
            spec.endpoints
                .contains_key(&("GET".to_string(), "/health".to_string()))
        );
        assert!(
            spec.endpoints
                .contains_key(&("POST".to_string(), "/users".to_string()))
        );
        assert!(
            spec.endpoints
                .contains_key(&("GET".to_string(), "/users/{id}".to_string()))
        );
    }

    #[test]
    fn test_parse_openapi_json() {
        let json_spec = r#"{
            "openapi": "3.0.0",
            "info": { "title": "Test", "version": "1.0" },
            "paths": {
                "/ping": {
                    "get": {
                        "operationId": "ping",
                        "responses": {
                            "200": {
                                "description": "OK",
                                "content": {
                                    "application/json": {
                                        "schema": {
                                            "type": "object",
                                            "properties": {
                                                "pong": { "type": "string" }
                                            }
                                        }
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }"#;
        let spec = OpenApiSpec::parse(json_spec).unwrap();
        assert!(
            spec.endpoints
                .contains_key(&("GET".to_string(), "/ping".to_string()))
        );
    }

    #[test]
    fn test_openapi_validate_success() {
        let spec = OpenApiSpec::parse(&openapi_yaml_spec()).unwrap();
        let mut headers = HashMap::new();
        headers.insert("content-type".to_string(), "application/json".to_string());

        let (violations, coverage) = spec.validate(
            &Method::Get,
            "/health",
            200,
            &headers,
            &Some(serde_json::json!({"status": "ok"})),
        );
        assert!(
            violations.is_empty(),
            "Expected no violations, got: {violations:?}"
        );
        assert!(!coverage.is_empty(), "Expected coverage data");
    }

    #[test]
    fn test_openapi_validate_undocumented_status() {
        let spec = OpenApiSpec::parse(&openapi_yaml_spec()).unwrap();
        let (violations, _) = spec.validate(
            &Method::Get,
            "/health",
            404,
            &HashMap::new(),
            &Some(serde_json::json!({"error": "not found"})),
        );
        assert!(!violations.is_empty());
        assert!(violations[0].description.contains("404"));
    }

    #[test]
    fn test_openapi_validate_undocumented_endpoint() {
        let spec = OpenApiSpec::parse(&openapi_yaml_spec()).unwrap();
        let (violations, _) = spec.validate(
            &Method::Get,
            "/nonexistent",
            200,
            &HashMap::new(),
            &Some(serde_json::json!({})),
        );
        assert!(!violations.is_empty());
        assert!(violations[0].description.contains("not declared"));
    }

    #[test]
    fn test_openapi_validate_schema_mismatch() {
        let spec = OpenApiSpec::parse(&openapi_yaml_spec()).unwrap();
        // /health returns {status: string} but we send a number
        let (violations, _) = spec.validate(
            &Method::Get,
            "/health",
            200,
            &HashMap::new(),
            &Some(serde_json::json!({"status": 123})),
        );
        assert!(!violations.is_empty(), "Expected schema violation");
    }

    #[test]
    fn test_openapi_validate_path_param_match() {
        let spec = OpenApiSpec::parse(&openapi_yaml_spec()).unwrap();
        let (violations, _) = spec.validate(
            &Method::Get,
            "/users/42",
            200,
            &HashMap::new(),
            &Some(serde_json::json!({"id": 42, "name": "Alice", "email": "alice@test.com"})),
        );
        assert!(
            violations.is_empty(),
            "Expected no violations, got: {violations:?}"
        );
    }

    #[test]
    fn test_openapi_validate_content_type_mismatch() {
        let spec = OpenApiSpec::parse(&openapi_yaml_spec()).unwrap();
        let mut headers = HashMap::new();
        headers.insert("content-type".to_string(), "text/plain".to_string());

        let (violations, _) = spec.validate(
            &Method::Get,
            "/health",
            200,
            &headers,
            &Some(serde_json::json!({"status": "ok"})),
        );
        assert!(!violations.is_empty());
        assert!(violations[0].description.contains("Content-Type"));
    }

    // -----------------------------------------------------------------------
    // GraphQL spec parsing
    // -----------------------------------------------------------------------

    fn graphql_sdl() -> String {
        r#"
type Query {
  health: String!
  user(id: Int!): User
}

type Mutation {
  createUser(name: String!): User!
}

type User {
  id: Int!
  name: String!
  email: String
}
"#
        .to_string()
    }

    #[test]
    fn test_parse_graphql_sdl() {
        let spec = GraphQlSpec::parse(&graphql_sdl()).unwrap();
        assert_eq!(spec.operations.len(), 3);

        let query = spec.operations.iter().find(|o| o.name == "Query").unwrap();
        assert!(query.fields.contains_key("health"));
        assert!(query.fields.contains_key("user"));

        let mutation = spec
            .operations
            .iter()
            .find(|o| o.name == "Mutation")
            .unwrap();
        assert!(mutation.fields.contains_key("createUser"));

        let user = spec.operations.iter().find(|o| o.name == "User").unwrap();
        assert!(user.fields.contains_key("id"));
        assert!(user.fields.contains_key("name"));
        assert!(user.fields.contains_key("email"));
    }

    #[test]
    fn test_graphql_validate_success() {
        let spec = GraphQlSpec::parse(&graphql_sdl()).unwrap();
        let (violations, coverage) = spec.validate(
            &Method::Post,
            "/graphql",
            200,
            &HashMap::new(),
            &Some(serde_json::json!({
                "data": {
                    "health": "ok"
                }
            })),
        );
        assert!(
            violations.is_empty(),
            "Expected no violations, got: {violations:?}"
        );
        assert!(!coverage.is_empty(), "Expected coverage data");
    }

    #[test]
    fn test_graphql_validate_errors() {
        let spec = GraphQlSpec::parse(&graphql_sdl()).unwrap();
        let (violations, _) = spec.validate(
            &Method::Post,
            "/graphql",
            200,
            &HashMap::new(),
            &Some(serde_json::json!({
                "errors": [{"message": "Not found"}]
            })),
        );
        assert_eq!(violations.len(), 1);
        assert!(violations[0].description.contains("Not found"));
    }

    #[test]
    fn test_graphql_validate_missing_data_and_errors() {
        let spec = GraphQlSpec::parse(&graphql_sdl()).unwrap();
        let (violations, _) = spec.validate(
            &Method::Post,
            "/graphql",
            200,
            &HashMap::new(),
            &Some(serde_json::json!({"foo": "bar"})),
        );
        assert!(!violations.is_empty());
        assert!(violations[0].description.contains("missing 'data'"));
    }

    #[test]
    fn test_graphql_validate_no_body() {
        let spec = GraphQlSpec::parse(&graphql_sdl()).unwrap();
        let (violations, _) = spec.validate(&Method::Post, "/graphql", 200, &HashMap::new(), &None);
        assert!(!violations.is_empty());
    }

    #[test]
    fn test_graphql_validate_undocumented_field() {
        let spec = GraphQlSpec::parse(&graphql_sdl()).unwrap();
        let (violations, _) = spec.validate(
            &Method::Post,
            "/graphql",
            200,
            &HashMap::new(),
            &Some(serde_json::json!({
                "data": {
                    "unknownField": "value"
                }
            })),
        );
        assert!(!violations.is_empty());
        assert!(violations[0].description.contains("Undocumented"));
    }

    // -----------------------------------------------------------------------
    // ParsedSpec integration
    // -----------------------------------------------------------------------

    #[test]
    fn test_parsed_spec_openapi() {
        let spec = ParsedSpec::parse("spec.yaml", &openapi_yaml_spec()).unwrap();
        assert!(matches!(spec, ParsedSpec::OpenAPI(_)));
    }

    #[test]
    fn test_parsed_spec_graphql() {
        let spec = ParsedSpec::parse("schema.graphql", &graphql_sdl()).unwrap();
        assert!(matches!(spec, ParsedSpec::GraphQL(_)));
    }

    #[test]
    fn test_parsed_spec_unknown() {
        let result = ParsedSpec::parse("data.xml", "<xml/>");
        assert!(result.is_err());
    }

    // -----------------------------------------------------------------------
    // Field coverage extraction
    // -----------------------------------------------------------------------

    #[test]
    fn test_extract_exercised_fields() {
        let value = serde_json::json!({
            "a": 1,
            "b": {
                "c": "hello",
                "d": [1, 2, 3]
            }
        });
        let paths = extract_exercised_fields(&value);
        assert!(paths.contains(&"$.a".to_string()));
        assert!(paths.contains(&"$.b".to_string()));
        assert!(paths.contains(&"$.b.c".to_string()));
        assert!(paths.contains(&"$.b.d".to_string()));
        assert!(paths.contains(&"$.b.d[0]".to_string()));
    }

    #[test]
    fn test_extract_schema_field_paths() {
        let schema = serde_json::json!({
            "type": "object",
            "properties": {
                "id": {"type": "integer"},
                "name": {"type": "string"},
                "address": {
                    "type": "object",
                    "properties": {
                        "street": {"type": "string"},
                        "city": {"type": "string"}
                    }
                }
            }
        });
        let paths = extract_schema_field_paths(&schema);
        assert!(paths.contains(&"$.id".to_string()));
        assert!(paths.contains(&"$.name".to_string()));
        assert!(paths.contains(&"$.address".to_string()));
        assert!(paths.contains(&"$.address.street".to_string()));
        assert!(paths.contains(&"$.address.city".to_string()));
    }
}
