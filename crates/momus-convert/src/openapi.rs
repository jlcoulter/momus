use anyhow::{Context, Result};
use momus_core::ast::*;
use openapiv3::*;
use std::collections::{HashMap, HashSet};

/// Convert an OpenAPI 3.x spec to a TestPlan.
pub fn convert(path: &str) -> Result<TestPlan> {
    let content = std::fs::read_to_string(path)
        .with_context(|| format!("Failed to read OpenAPI spec: {path}"))?;

    let spec: OpenAPI = if path.ends_with(".yaml") || path.ends_with(".yml") {
        serde_yaml::from_str(&content)
            .with_context(|| format!("Failed to parse YAML OpenAPI spec: {path}"))?
    } else {
        serde_json::from_str(&content)
            .with_context(|| format!("Failed to parse JSON OpenAPI spec: {path}"))?
    };

    let title = spec.info.title.clone();
    let mut steps = Vec::new();

    for (path_str, path_item) in spec.paths.paths {
        let path_item = match path_item {
            ReferenceOr::Item(item) => item,
            ReferenceOr::Reference { .. } => continue,
        };

        let operations = vec![
            ("GET", path_item.get),
            ("POST", path_item.post),
            ("PUT", path_item.put),
            ("DELETE", path_item.delete),
            ("PATCH", path_item.patch),
            ("HEAD", path_item.head),
            ("OPTIONS", path_item.options),
        ];

        for (method, operation) in operations {
            let operation = match operation {
                Some(op) => op,
                None => continue,
            };

            let operation_id = operation.operation_id.clone().unwrap_or_else(|| {
                format!("{}_{}", method.to_lowercase(), sanitize_path(&path_str))
            });

            let url = build_url(&path_str, &operation.parameters);
            let mut headers = HashMap::new();
            let mut query_params = Vec::new();

            for param in &operation.parameters {
                let param = match param {
                    ReferenceOr::Item(p) => p,
                    ReferenceOr::Reference { .. } => continue,
                };
                match param {
                    Parameter::Header { parameter_data, .. } => {
                        headers.insert(parameter_data.name.clone(), "example".to_string());
                    }
                    Parameter::Query { parameter_data, .. } => {
                        query_params.push(format!("{}={}", parameter_data.name, "example"));
                    }
                    _ => {}
                }
            }

            let final_url = if query_params.is_empty() {
                url
            } else {
                format!("{}?{}", url, query_params.join("&"))
            };

            let body = build_request_body(&operation.request_body);

            let mut assertions = Vec::new();
            if let Some((status_code, response)) = operation.responses.responses.iter().next() {
                let status = match status_code {
                    StatusCode::Code(n) => *n,
                    StatusCode::Range(n) => *n * 100,
                };
                assertions.push(Assertion::Status(status));
                if let Some(response) = response.as_item()
                    && let Some((content_type, _)) = response.content.iter().next()
                {
                    assertions.push(Assertion::ContentType(content_type.clone()));
                }
            }

            let step = RequestStep {
                name: operation_id,
                method: parse_method(method)?,
                url: final_url,
                headers,
                body,
                assert: assertions,
                save_as: String::new(),
                soft_fail: false,
            };
            steps.push(Step::Request(step));
        }
    }

    Ok(TestPlan {
        name: format!("OpenAPI: {title}"),
        base_url: spec
            .servers
            .first()
            .map(|s| s.url.clone())
            .unwrap_or_default(),
        default_headers: HashMap::new(),
        steps,
        setup: vec![],
        teardown: vec![],
    })
}

/// Generate seed data setup steps from an OpenAPI spec.
///
/// Scans the spec for POST endpoints that create resources and generates
/// POST requests with example bodies to pre-populate the server. Also
/// generates seed data for resources that GET/PUT/DELETE endpoints reference
/// via path parameters.
pub fn generate_seed_data(path: &str) -> Result<Vec<Step>> {
    let content = std::fs::read_to_string(path)
        .with_context(|| format!("Failed to read OpenAPI spec: {path}"))?;

    let spec: OpenAPI = if path.ends_with(".yaml") || path.ends_with(".yml") {
        serde_yaml::from_str(&content)
            .with_context(|| format!("Failed to parse YAML OpenAPI spec: {path}"))?
    } else {
        serde_json::from_str(&content)
            .with_context(|| format!("Failed to parse JSON OpenAPI spec: {path}"))?
    };

    let mut seed_steps: Vec<Step> = Vec::new();
    let mut seen_urls: HashSet<String> = HashSet::new();

    for (path_str, path_item) in spec.paths.paths {
        let path_item = match path_item {
            ReferenceOr::Item(item) => item,
            ReferenceOr::Reference { .. } => continue,
        };

        // For POST endpoints with request bodies, generate a seed POST
        if let Some(op) = &path_item.post
            && let Some(body) = build_request_body(&op.request_body)
        {
            let url = build_url(&path_str, &op.parameters);
            let key = format!("POST {url}");
            if seen_urls.insert(key) {
                seed_steps.push(Step::Request(RequestStep {
                    name: format!("seed_{}", sanitize_path(&path_str)),
                    method: Method::Post,
                    url: url.clone(),
                    headers: HashMap::new(),
                    body: Some(body),
                    assert: vec![Assertion::Status(201)],
                    save_as: String::new(),
                    soft_fail: true,
                }));
            }
        }

        // For PUT endpoints with request bodies, generate a seed PUT
        if let Some(op) = &path_item.put
            && let Some(body) = build_request_body(&op.request_body)
        {
            let url = build_url(&path_str, &op.parameters);
            let key = format!("PUT {url}");
            if seen_urls.insert(key) {
                seed_steps.push(Step::Request(RequestStep {
                    name: format!("seed_{}", sanitize_path(&path_str)),
                    method: Method::Put,
                    url: url.clone(),
                    headers: HashMap::new(),
                    body: Some(body),
                    assert: vec![Assertion::Status(200)],
                    save_as: String::new(),
                    soft_fail: true,
                }));
            }
        }
    }

    Ok(seed_steps)
}

fn build_url(path: &str, parameters: &[ReferenceOr<Parameter>]) -> String {
    let mut url = path.to_string();
    for param in parameters {
        let param = match param {
            ReferenceOr::Item(p) => p,
            ReferenceOr::Reference { .. } => continue,
        };
        if let Parameter::Path { parameter_data, .. } = param {
            let placeholder = format!("{{{}}}", parameter_data.name);
            url = url.replace(&placeholder, "1");
        }
    }
    url
}

fn build_request_body(
    request_body: &Option<ReferenceOr<RequestBody>>,
) -> Option<serde_json::Value> {
    let body = match request_body {
        Some(ReferenceOr::Item(b)) => b,
        _ => return None,
    };
    if let Some(media_type) = body.content.get("application/json")
        && let Some(schema) = &media_type.schema
    {
        return schema_to_value(schema);
    }
    for (_, media_type) in &body.content {
        if let Some(schema) = &media_type.schema {
            return schema_to_value(schema);
        }
    }
    None
}

fn schema_to_value(schema: &ReferenceOr<Schema>) -> Option<serde_json::Value> {
    let schema = match schema {
        ReferenceOr::Item(s) => s,
        ReferenceOr::Reference { .. } => return None,
    };
    Some(schema_kind_to_value(&schema.schema_kind))
}

fn schema_kind_to_value(kind: &SchemaKind) -> serde_json::Value {
    match kind {
        SchemaKind::Type(t) => type_to_value(t),
        SchemaKind::AllOf { all_of } => {
            let mut map = serde_json::Map::new();
            for sub in all_of {
                if let Some(val) = schema_to_value(sub)
                    && let Some(obj) = val.as_object()
                {
                    map.extend(obj.clone());
                }
            }
            serde_json::Value::Object(map)
        }
        _ => serde_json::json!({}),
    }
}

fn boxed_schema_to_value(schema: &ReferenceOr<Box<Schema>>) -> serde_json::Value {
    match schema {
        ReferenceOr::Item(s) => schema_kind_to_value(&s.schema_kind),
        ReferenceOr::Reference { .. } => serde_json::json!({}),
    }
}

fn type_to_value(t: &Type) -> serde_json::Value {
    match t {
        Type::String(st) => {
            if let Some(first) = st.enumeration.first().and_then(|v| v.as_ref()) {
                serde_json::json!(first)
            } else {
                serde_json::json!("example")
            }
        }
        Type::Number(_) => serde_json::json!(0),
        Type::Integer(_) => serde_json::json!(0),
        Type::Boolean(_) => serde_json::json!(true),
        Type::Array(arr) => {
            if let Some(items) = &arr.items {
                serde_json::json!([boxed_schema_to_value(items)])
            } else {
                serde_json::json!([])
            }
        }
        Type::Object(obj) => {
            let mut map = serde_json::Map::new();
            for (name, prop) in &obj.properties {
                map.insert(name.clone(), boxed_schema_to_value(prop));
            }
            serde_json::Value::Object(map)
        }
    }
}

fn parse_method(method: &str) -> Result<Method> {
    match method.to_uppercase().as_str() {
        "GET" => Ok(Method::Get),
        "POST" => Ok(Method::Post),
        "PUT" => Ok(Method::Put),
        "DELETE" => Ok(Method::Delete),
        "PATCH" => Ok(Method::Patch),
        "HEAD" => Ok(Method::Head),
        "OPTIONS" => Ok(Method::Options),
        other => anyhow::bail!("Unsupported HTTP method: {}", other),
    }
}

fn sanitize_path(path: &str) -> String {
    path.trim_start_matches('/')
        .replace('/', "_")
        .replace(['{', '}'], "")
        .replace('-', "_")
}

#[cfg(test)]
mod tests {
    use super::*;

    fn create_test_spec() -> (String, tempfile::TempDir) {
        let spec = r#"openapi: "3.0.0"
info:
  title: Test API
  version: "1.0"
servers:
  - url: http://localhost:8080
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
"#
        .to_string();
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("spec.yaml");
        std::fs::write(&path, &spec).unwrap();
        (
            dir.path().join("spec.yaml").to_str().unwrap().to_string(),
            dir,
        )
    }

    #[test]
    fn convert_openapi_yaml() {
        let (path, _dir) = create_test_spec();
        let plan = convert(&path).unwrap();
        assert_eq!(plan.name, "OpenAPI: Test API");
        assert_eq!(plan.base_url, "http://localhost:8080");
        assert_eq!(plan.steps.len(), 3);
    }

    #[test]
    fn convert_health_endpoint() {
        let (path, _dir) = create_test_spec();
        let plan = convert(&path).unwrap();
        if let Step::Request(step) = &plan.steps[0] {
            assert_eq!(step.method, Method::Get);
            assert_eq!(step.url, "/health");
            assert!(
                step.assert
                    .iter()
                    .any(|a| matches!(a, Assertion::Status(200)))
            );
        } else {
            panic!("Expected Request step");
        }
    }

    #[test]
    fn convert_post_with_body() {
        let (path, _dir) = create_test_spec();
        let plan = convert(&path).unwrap();
        if let Step::Request(step) = &plan.steps[1] {
            assert_eq!(step.method, Method::Post);
            assert_eq!(step.url, "/users");
            assert!(step.body.is_some());
        } else {
            panic!("Expected Request step");
        }
    }

    #[test]
    fn convert_path_param() {
        let (path, _dir) = create_test_spec();
        let plan = convert(&path).unwrap();
        if let Step::Request(step) = &plan.steps[2] {
            assert_eq!(step.method, Method::Get);
            assert!(step.url.contains("/users/"));
            assert_ne!(step.url, "/users/{id}");
        } else {
            panic!("Expected Request step");
        }
    }

    #[test]
    fn snapshot_openapi_plan() {
        let (path, _dir) = create_test_spec();
        let plan = convert(&path).unwrap();
        insta::assert_json_snapshot!(plan);
    }

    #[test]
    fn test_sanitize_path() {
        assert_eq!(sanitize_path("/users/{id}"), "users_id");
        assert_eq!(sanitize_path("/api/v1/items"), "api_v1_items");
    }
}
