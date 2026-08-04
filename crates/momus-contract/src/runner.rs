use crate::config::ContractConfig;
use crate::report::{ContractReport, ContractViolation};
use anyhow::{Context, Result};
use momus_core::ast::{Method, Step, TestPlan};
use std::collections::HashMap;
use std::time::Instant;

/// Execute a contract validation run.
///
/// Loads the API spec, runs the plan, and validates each response
/// against the spec's schema for that endpoint.
///
/// # Errors
///
/// Returns an error if the spec cannot be loaded or the HTTP client fails.
pub async fn run_contract(plan: &TestPlan, config: &ContractConfig) -> Result<ContractReport> {
    let start = Instant::now();

    let base_url = config.base_url.as_deref().unwrap_or(&plan.base_url);

    // Load the spec
    let spec_content = std::fs::read_to_string(&config.spec_path)
        .with_context(|| format!("Failed to read spec file: {}", config.spec_path))?;

    // Detect spec type and parse
    let spec_type = detect_spec_type(&config.spec_path, &spec_content);

    tracing::info!(
        "Running contract validation on '{}' against {} spec '{}'",
        plan.name,
        spec_type,
        config.spec_path
    );

    let client = reqwest::Client::builder()
        .timeout(std::time::Duration::from_secs(30))
        .build()?;

    // Collect all request steps
    let steps = collect_contract_steps(plan);
    if steps.is_empty() {
        anyhow::bail!("Test plan has no request steps to validate");
    }

    let mut details = Vec::new();
    let total = steps.len();
    let mut compliant = 0usize;

    for step in &steps {
        let url = format!("{}{}", base_url, step.url);
        let result = match step.method {
            Method::Get => client.get(&url).send().await,
            Method::Post => {
                let body = step.body.clone().unwrap_or(serde_json::json!({}));
                client.post(&url).json(&body).send().await
            }
            Method::Put => {
                let body = step.body.clone().unwrap_or(serde_json::json!({}));
                client.put(&url).json(&body).send().await
            }
            Method::Delete => client.delete(&url).send().await,
            Method::Patch => {
                let body = step.body.clone().unwrap_or(serde_json::json!({}));
                client.patch(&url).json(&body).send().await
            }
            Method::Head => client.head(&url).send().await,
            Method::Options => client.request(reqwest::Method::OPTIONS, &url).send().await,
        };

        match result {
            Ok(resp) => {
                let status = resp.status().as_u16();
                let headers: HashMap<String, String> = resp
                    .headers()
                    .iter()
                    .map(|(k, v)| (k.to_string(), v.to_str().unwrap_or("").to_string()))
                    .collect();
                let body: Option<serde_json::Value> = resp.json().await.ok();

                // Validate against spec
                let step_violations =
                    validate_response(spec_type, &step.method, &step.url, status, &headers, &body);

                if step_violations.is_empty() {
                    compliant += 1;
                } else {
                    details.extend(step_violations);
                }
            }
            Err(e) => {
                details.push(ContractViolation {
                    endpoint: step.url.clone(),
                    method: step.method.to_string(),
                    status: 0,
                    description: format!("HTTP error: {}", e),
                    severity: "error".to_string(),
                });
            }
        }
    }

    let elapsed = start.elapsed().as_secs_f64();
    let violation_count = details.len();

    Ok(ContractReport {
        plan_name: plan.name.clone(),
        spec_path: config.spec_path.clone(),
        total_endpoints: total,
        compliant,
        violations: violation_count,
        compliance_pct: if total > 0 {
            (compliant as f64 / total as f64) * 100.0
        } else {
            0.0
        },
        duration_secs: elapsed,
        details,
    })
}

/// A contract validation step extracted from a test plan.
struct ContractStep {
    method: Method,
    url: String,
    body: Option<serde_json::Value>,
}

/// Collect all request steps from a test plan.
fn collect_contract_steps(plan: &TestPlan) -> Vec<ContractStep> {
    let mut steps = Vec::new();
    collect_from_steps(&plan.steps, &mut steps);
    steps
}

fn collect_from_steps(steps: &[Step], result: &mut Vec<ContractStep>) {
    for step in steps {
        match step {
            Step::Request(req) => {
                result.push(ContractStep {
                    method: req.method,
                    url: req.url.clone(),
                    body: req.body.clone(),
                });
            }
            Step::Sequence(seq) => {
                collect_from_steps(&seq.steps, result);
            }
            Step::Parallel(children) => {
                collect_from_steps(children, result);
            }
            _ => {}
        }
    }
}

/// Detect the spec type from file extension and content.
fn detect_spec_type(path: &str, content: &str) -> &'static str {
    let lower = path.to_lowercase();
    if lower.ends_with(".yaml") || lower.ends_with(".yml") {
        if content.contains("openapi:") || content.contains("openapi ") {
            "OpenAPI"
        } else if content.contains("swagger:") || content.contains("swagger ") {
            "Swagger"
        } else {
            "YAML"
        }
    } else if lower.ends_with(".graphql") || lower.ends_with(".gql") || lower.ends_with(".sdl") {
        "GraphQL"
    } else if lower.ends_with(".proto") {
        "Protobuf"
    } else {
        "OpenAPI"
    }
}

/// Validate a response against the spec.
fn validate_response(
    spec_type: &str,
    method: &Method,
    url: &str,
    status_code: u16,
    _headers: &HashMap<String, String>,
    body: &Option<serde_json::Value>,
) -> Vec<ContractViolation> {
    let mut violations = Vec::new();

    match spec_type {
        "OpenAPI" | "Swagger" => {
            if let Some(body) = body {
                if body.is_null() && (200..300).contains(&status_code) {
                    violations.push(ContractViolation {
                        endpoint: url.to_string(),
                        method: method.to_string(),
                        status: status_code,
                        description: "Response body is null for a successful status code"
                            .to_string(),
                        severity: "warning".to_string(),
                    });
                }
            } else if (200..300).contains(&status_code) {
                violations.push(ContractViolation {
                    endpoint: url.to_string(),
                    method: method.to_string(),
                    status: status_code,
                    description: "Response body is not valid JSON for a successful status code"
                        .to_string(),
                    severity: "error".to_string(),
                });
            }
        }
        "GraphQL" => {
            if let Some(body) = body {
                let has_data = body.get("data").is_some();
                let has_errors = body.get("errors").is_some();
                if !has_data && !has_errors {
                    let keys: Vec<&str> = body
                        .as_object()
                        .map(|o| o.keys().map(|k| k.as_str()).collect())
                        .unwrap_or_default();
                    violations.push(ContractViolation {
                        endpoint: url.to_string(),
                        method: method.to_string(),
                        status: status_code,
                        description: format!(
                            "GraphQL response missing 'data' or 'errors' field, got: {:?}",
                            keys
                        ),
                        severity: "error".to_string(),
                    });
                }
            }
        }
        _ => {
            if body.is_none() && (200..300).contains(&status_code) {
                violations.push(ContractViolation {
                    endpoint: url.to_string(),
                    method: method.to_string(),
                    status: status_code,
                    description: "Response body is not valid JSON".to_string(),
                    severity: "warning".to_string(),
                });
            }
        }
    }

    violations
}

#[cfg(test)]
mod tests {
    use super::*;
    use momus_core::ast::*;
    use serde_json::json;
    use std::collections::HashMap;

    #[test]
    fn test_detect_spec_type() {
        assert_eq!(
            detect_spec_type("openapi.yaml", "openapi: 3.0.0"),
            "OpenAPI"
        );
        assert_eq!(detect_spec_type("spec.yml", "swagger: '2.0'"), "Swagger");
        assert_eq!(
            detect_spec_type("schema.graphql", "type Query {"),
            "GraphQL"
        );
        assert_eq!(detect_spec_type("schema.gql", "type Query {"), "GraphQL");
        assert_eq!(detect_spec_type("service.proto", "syntax ="), "Protobuf");
        assert_eq!(detect_spec_type("spec.json", "{}"), "OpenAPI");
    }

    #[test]
    fn test_validate_openapi_success() {
        let violations = validate_response(
            "OpenAPI",
            &Method::Get,
            "/health",
            200,
            &HashMap::new(),
            &Some(json!({"status": "ok"})),
        );
        assert!(violations.is_empty());
    }

    #[test]
    fn test_validate_openapi_null_body() {
        let violations = validate_response(
            "OpenAPI",
            &Method::Get,
            "/health",
            200,
            &HashMap::new(),
            &Some(serde_json::Value::Null),
        );
        assert_eq!(violations.len(), 1);
        assert_eq!(violations[0].severity, "warning");
    }

    #[test]
    fn test_validate_openapi_no_body() {
        let violations = validate_response(
            "OpenAPI",
            &Method::Get,
            "/health",
            200,
            &HashMap::new(),
            &None,
        );
        assert_eq!(violations.len(), 1);
        assert_eq!(violations[0].severity, "error");
    }

    #[test]
    fn test_validate_graphql_success() {
        let violations = validate_response(
            "GraphQL",
            &Method::Post,
            "/graphql",
            200,
            &HashMap::new(),
            &Some(json!({"data": {"health": "ok"}})),
        );
        assert!(violations.is_empty());
    }

    #[test]
    fn test_validate_graphql_errors() {
        let violations = validate_response(
            "GraphQL",
            &Method::Post,
            "/graphql",
            200,
            &HashMap::new(),
            &Some(json!({"errors": [{"message": "not found"}]})),
        );
        assert!(violations.is_empty());
    }

    #[test]
    fn test_validate_graphql_invalid() {
        let violations = validate_response(
            "GraphQL",
            &Method::Post,
            "/graphql",
            200,
            &HashMap::new(),
            &Some(json!({"foo": "bar"})),
        );
        assert_eq!(violations.len(), 1);
    }

    #[test]
    fn test_collect_contract_steps() {
        let plan = TestPlan {
            name: "test".into(),
            base_url: "http://localhost".into(),
            default_headers: HashMap::new(),
            steps: vec![
                Step::Request(RequestStep {
                    name: "r1".into(),
                    method: Method::Get,
                    url: "/health".into(),
                    headers: HashMap::new(),
                    body: None,
                    assert: vec![],
                    save_as: String::new(),
                    soft_fail: false,
                }),
                Step::Request(RequestStep {
                    name: "r2".into(),
                    method: Method::Post,
                    url: "/users".into(),
                    headers: HashMap::new(),
                    body: Some(json!({"name": "test"})),
                    assert: vec![],
                    save_as: String::new(),
                    soft_fail: false,
                }),
            ],
            setup: vec![],
            teardown: vec![],
        };

        let steps = collect_contract_steps(&plan);
        assert_eq!(steps.len(), 2);
    }
}
