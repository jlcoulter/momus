use crate::config::ContractConfig;
use crate::report::{ContractReport, ContractViolation, EndpointCompliance, FieldCoverage};
use crate::spec::ParsedSpec;
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

    // Load and parse the spec
    let spec_content = std::fs::read_to_string(&config.spec_path)
        .with_context(|| format!("Failed to read spec file: {}", config.spec_path))?;

    let spec = ParsedSpec::parse(&config.spec_path, &spec_content)
        .with_context(|| format!("Failed to parse spec: {}", config.spec_path))?;

    tracing::info!(
        "Running contract validation on '{}' against {} spec '{}'",
        plan.name,
        spec,
        config.spec_path
    );

    let client = reqwest::Client::builder()
        .timeout(std::time::Duration::from_secs(config.timeout_secs))
        .build()?;

    // Collect all request steps
    let steps = collect_contract_steps(plan);
    if steps.is_empty() {
        anyhow::bail!("Test plan has no request steps to validate");
    }

    let mut details = Vec::new();
    let mut all_field_coverage: Vec<FieldCoverage> = Vec::new();
    let mut endpoint_compliance: Vec<EndpointCompliance> = Vec::new();
    let mut undocumented_fields: Vec<String> = Vec::new();
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
                let (step_violations, step_coverage) =
                    spec.validate(&step.method, &step.url, status, &headers, &body);

                // Track field coverage
                all_field_coverage.extend(step_coverage);

                // Track undocumented fields
                for v in &step_violations {
                    if v.severity == "info" && v.description.contains("Undocumented") {
                        undocumented_fields
                            .push(format!("{} {}: {}", v.method, v.endpoint, v.description));
                    }
                }

                // In strict mode, escalate undocumented fields to errors
                let step_violations: Vec<ContractViolation> = if config.strict {
                    step_violations
                        .into_iter()
                        .map(|mut v| {
                            if v.severity == "info" {
                                v.severity = "error".to_string();
                            }
                            v
                        })
                        .collect()
                } else {
                    step_violations
                };

                // Per-endpoint compliance
                let checks_passed = step_violations
                    .iter()
                    .filter(|v| v.severity != "error")
                    .count();
                let checks_failed = step_violations
                    .iter()
                    .filter(|v| v.severity == "error")
                    .count();
                let total_checks = checks_passed + checks_failed;
                let pct = if total_checks > 0 {
                    (checks_passed as f64 / total_checks as f64) * 100.0
                } else {
                    100.0
                };

                endpoint_compliance.push(EndpointCompliance {
                    endpoint: step.url.clone(),
                    method: step.method.to_string(),
                    passed: step_violations.is_empty(),
                    pct,
                    checks_passed,
                    checks_failed,
                });

                if step_violations.is_empty() {
                    compliant += 1;
                } else {
                    details.extend(step_violations);
                }
            }
            Err(e) => {
                endpoint_compliance.push(EndpointCompliance {
                    endpoint: step.url.clone(),
                    method: step.method.to_string(),
                    passed: false,
                    pct: 0.0,
                    checks_passed: 0,
                    checks_failed: 1,
                });

                details.push(ContractViolation {
                    endpoint: step.url.clone(),
                    method: step.method.to_string(),
                    status: 0,
                    description: format!("HTTP error: {e}"),
                    severity: "error".to_string(),
                });
            }
        }
    }

    // In strict mode, fail on undocumented endpoints
    if config.strict && !undocumented_fields.is_empty() {
        details.push(ContractViolation {
            endpoint: "*".to_string(),
            method: "ANY".to_string(),
            status: 0,
            description: format!(
                "Strict mode: {} undocumented field(s) found",
                undocumented_fields.len()
            ),
            severity: "error".to_string(),
        });
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
        endpoint_compliance,
        field_coverage: all_field_coverage,
        undocumented_fields,
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

#[cfg(test)]
mod tests {
    use super::*;
    use momus_core::ast::*;
    use serde_json::json;
    use std::collections::HashMap;

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

    #[test]
    fn test_collect_nested_steps() {
        let plan = TestPlan {
            name: "nested".into(),
            base_url: "http://localhost".into(),
            default_headers: HashMap::new(),
            steps: vec![Step::Sequence(SequenceStep {
                name: "seq".into(),
                steps: vec![
                    Step::Request(RequestStep {
                        name: "r1".into(),
                        method: Method::Get,
                        url: "/a".into(),
                        headers: HashMap::new(),
                        body: None,
                        assert: vec![],
                        save_as: String::new(),
                        soft_fail: false,
                    }),
                    Step::Parallel(vec![
                        Step::Request(RequestStep {
                            name: "r2".into(),
                            method: Method::Get,
                            url: "/b".into(),
                            headers: HashMap::new(),
                            body: None,
                            assert: vec![],
                            save_as: String::new(),
                            soft_fail: false,
                        }),
                        Step::Request(RequestStep {
                            name: "r3".into(),
                            method: Method::Get,
                            url: "/c".into(),
                            headers: HashMap::new(),
                            body: None,
                            assert: vec![],
                            save_as: String::new(),
                            soft_fail: false,
                        }),
                    ]),
                ],
                continue_on_failure: false,
            })],
            setup: vec![],
            teardown: vec![],
        };

        let steps = collect_contract_steps(&plan);
        assert_eq!(steps.len(), 3);
    }
}
