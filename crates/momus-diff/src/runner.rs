use crate::config::DiffConfig;
use crate::report::{DiffEntry, DiffReport};
use anyhow::Result;
use momus_core::ast::{Method, Step, TestPlan};
use std::collections::HashMap;
use std::time::Instant;

/// Execute a diff run between two environments.
///
/// Runs the plan against both `baseline_url` and `target_url`,
/// then compares responses field-by-field.
pub async fn run_diff(plan: &TestPlan, config: &DiffConfig) -> Result<DiffReport> {
    let start = Instant::now();

    let client = reqwest::Client::builder()
        .timeout(std::time::Duration::from_secs(config.timeout_secs))
        .build()?;

    tracing::info!(
        "Running diff on '{}': baseline={}, target={}",
        plan.name,
        config.baseline_url,
        config.target_url
    );

    // Collect all request steps
    let steps = collect_diff_steps(plan);
    if steps.is_empty() {
        anyhow::bail!("Test plan has no request steps to diff");
    }

    let mut diffs = Vec::new();
    let mut identical = 0u64;
    let mut different = 0u64;
    let mut fields_added = 0u64;
    let mut fields_removed = 0u64;
    let mut fields_modified = 0u64;

    for step in &steps {
        let baseline_url = format!("{}{}", config.baseline_url.trim_end_matches('/'), step.url);
        let target_url = format!("{}{}", config.target_url.trim_end_matches('/'), step.url);

        let baseline_resp = send_request(&client, &step.method, &baseline_url, &step.body).await;
        let target_resp = send_request(&client, &step.method, &target_url, &step.body).await;

        match (baseline_resp, target_resp) {
            (Ok(baseline), Ok(target)) => {
                let mut step_diffs = Vec::new();

                // Compare status codes
                if config.diff_status && baseline.status != target.status {
                    step_diffs.push(DiffEntry {
                        endpoint: step.url.clone(),
                        method: step.method.to_string(),
                        change_type: "modified".to_string(),
                        field: "status".to_string(),
                        baseline: Some(serde_json::json!(baseline.status)),
                        target: Some(serde_json::json!(target.status)),
                    });
                    fields_modified += 1;
                }

                // Compare headers
                if config.diff_headers {
                    let all_keys: std::collections::HashSet<&str> = baseline
                        .headers
                        .keys()
                        .chain(target.headers.keys())
                        .map(|k| k.as_str())
                        .collect();

                    for key in all_keys {
                        let bv = baseline.headers.get(key);
                        let tv = target.headers.get(key);
                        if bv != tv {
                            step_diffs.push(DiffEntry {
                                endpoint: step.url.clone(),
                                method: step.method.to_string(),
                                change_type: "modified".to_string(),
                                field: format!("header.{}", key),
                                baseline: bv.map(|v| serde_json::json!(v)),
                                target: tv.map(|v| serde_json::json!(v)),
                            });
                            fields_modified += 1;
                        }
                    }
                }

                // Compare bodies
                if config.diff_bodies
                    && let (Some(b_body), Some(t_body)) = (&baseline.body, &target.body)
                {
                    let body_diffs = diff_json_values("$", b_body, t_body);
                    for d in &body_diffs {
                        match d.change_type.as_str() {
                            "added" => fields_added += 1,
                            "removed" => fields_removed += 1,
                            _ => fields_modified += 1,
                        }
                    }
                    step_diffs.extend(body_diffs);
                }

                if step_diffs.is_empty() {
                    identical += 1;
                } else {
                    different += 1;
                    diffs.extend(step_diffs);
                }
            }
            (Err(e), _) | (_, Err(e)) => {
                different += 1;
                diffs.push(DiffEntry {
                    endpoint: step.url.clone(),
                    method: step.method.to_string(),
                    change_type: "error".to_string(),
                    field: "http".to_string(),
                    baseline: None,
                    target: Some(serde_json::json!(format!("HTTP error: {}", e))),
                });
            }
        }
    }

    let elapsed = start.elapsed().as_secs_f64();
    let total = steps.len();

    Ok(DiffReport {
        plan_name: plan.name.clone(),
        baseline_url: config.baseline_url.clone(),
        target_url: config.target_url.clone(),
        total_endpoints: total,
        identical: identical as usize,
        different: different as usize,
        fields_added: fields_added as usize,
        fields_removed: fields_removed as usize,
        fields_modified: fields_modified as usize,
        duration_secs: elapsed,
        diffs,
    })
}

/// A diff step extracted from a test plan.
struct DiffStep {
    method: Method,
    url: String,
    body: Option<serde_json::Value>,
}

/// Response from a single request.
struct DiffResponse {
    status: u16,
    headers: HashMap<String, String>,
    body: Option<serde_json::Value>,
}

/// Collect all request steps from a test plan.
fn collect_diff_steps(plan: &TestPlan) -> Vec<DiffStep> {
    let mut steps = Vec::new();
    collect_from_steps(&plan.steps, &mut steps);
    steps
}

fn collect_from_steps(steps: &[Step], result: &mut Vec<DiffStep>) {
    for step in steps {
        match step {
            Step::Request(req) => {
                result.push(DiffStep {
                    method: req.method,
                    url: req.url.clone(),
                    body: req.body.clone(),
                });
            }
            Step::Sequence(seq) => collect_from_steps(&seq.steps, result),
            Step::Parallel(children) => collect_from_steps(children, result),
            _ => {}
        }
    }
}

/// Send an HTTP request and return the response.
async fn send_request(
    client: &reqwest::Client,
    method: &Method,
    url: &str,
    body: &Option<serde_json::Value>,
) -> Result<DiffResponse> {
    let resp = match method {
        Method::Get => client.get(url).send().await?,
        Method::Post => {
            let b = body.clone().unwrap_or(serde_json::json!({}));
            client.post(url).json(&b).send().await?
        }
        Method::Put => {
            let b = body.clone().unwrap_or(serde_json::json!({}));
            client.put(url).json(&b).send().await?
        }
        Method::Delete => client.delete(url).send().await?,
        Method::Patch => {
            let b = body.clone().unwrap_or(serde_json::json!({}));
            client.patch(url).json(&b).send().await?
        }
        Method::Head => client.head(url).send().await?,
        Method::Options => client.request(reqwest::Method::OPTIONS, url).send().await?,
    };

    let status = resp.status().as_u16();
    let headers: HashMap<String, String> = resp
        .headers()
        .iter()
        .map(|(k, v)| (k.to_string(), v.to_str().unwrap_or("").to_string()))
        .collect();
    let body: Option<serde_json::Value> = resp.json().await.ok();

    Ok(DiffResponse {
        status,
        headers,
        body,
    })
}

/// Recursively diff two JSON values, returning a list of field-level differences.
fn diff_json_values(
    path: &str,
    baseline: &serde_json::Value,
    target: &serde_json::Value,
) -> Vec<DiffEntry> {
    let mut diffs = Vec::new();

    match (baseline, target) {
        (serde_json::Value::Object(b_map), serde_json::Value::Object(t_map)) => {
            // Check for removed and modified fields
            for (key, b_val) in b_map {
                let child_path = format!("{}.{}", path, key);
                match t_map.get(key) {
                    None => {
                        diffs.push(DiffEntry {
                            endpoint: String::new(),
                            method: String::new(),
                            change_type: "removed".to_string(),
                            field: child_path,
                            baseline: Some(b_val.clone()),
                            target: None,
                        });
                    }
                    Some(t_val) if b_val != t_val => {
                        if (b_val.is_object() && t_val.is_object())
                            || (b_val.is_array() && t_val.is_array())
                        {
                            diffs.extend(diff_json_values(&child_path, b_val, t_val));
                        } else {
                            diffs.push(DiffEntry {
                                endpoint: String::new(),
                                method: String::new(),
                                change_type: "modified".to_string(),
                                field: child_path,
                                baseline: Some(b_val.clone()),
                                target: Some(t_val.clone()),
                            });
                        }
                    }
                    _ => {}
                }
            }
            // Check for added fields
            for (key, t_val) in t_map {
                if !b_map.contains_key(key) {
                    let child_path = format!("{}.{}", path, key);
                    diffs.push(DiffEntry {
                        endpoint: String::new(),
                        method: String::new(),
                        change_type: "added".to_string(),
                        field: child_path,
                        baseline: None,
                        target: Some(t_val.clone()),
                    });
                }
            }
        }
        (serde_json::Value::Array(b_arr), serde_json::Value::Array(t_arr)) => {
            let max_len = b_arr.len().max(t_arr.len());
            for i in 0..max_len {
                let child_path = format!("{}[{}]", path, i);
                match (b_arr.get(i), t_arr.get(i)) {
                    (Some(b), Some(t)) if b != t => {
                        diffs.push(DiffEntry {
                            endpoint: String::new(),
                            method: String::new(),
                            change_type: "modified".to_string(),
                            field: child_path,
                            baseline: Some(b.clone()),
                            target: Some(t.clone()),
                        });
                    }
                    (Some(b), None) => {
                        diffs.push(DiffEntry {
                            endpoint: String::new(),
                            method: String::new(),
                            change_type: "removed".to_string(),
                            field: child_path,
                            baseline: Some(b.clone()),
                            target: None,
                        });
                    }
                    (None, Some(t)) => {
                        diffs.push(DiffEntry {
                            endpoint: String::new(),
                            method: String::new(),
                            change_type: "added".to_string(),
                            field: child_path,
                            baseline: None,
                            target: Some(t.clone()),
                        });
                    }
                    _ => {}
                }
            }
        }
        _ => {
            if baseline != target {
                diffs.push(DiffEntry {
                    endpoint: String::new(),
                    method: String::new(),
                    change_type: "modified".to_string(),
                    field: path.to_string(),
                    baseline: Some(baseline.clone()),
                    target: Some(target.clone()),
                });
            }
        }
    }

    diffs
}

#[cfg(test)]
mod tests {
    use super::*;
    use momus_core::ast::*;
    use serde_json::json;
    use std::collections::HashMap;

    #[test]
    fn test_diff_json_values_identical() {
        let a = json!({"name": "John", "age": 30});
        let b = json!({"name": "John", "age": 30});
        let diffs = diff_json_values("$", &a, &b);
        assert!(diffs.is_empty());
    }

    #[test]
    fn test_diff_json_values_modified() {
        let a = json!({"name": "John", "age": 30});
        let b = json!({"name": "Jane", "age": 30});
        let diffs = diff_json_values("$", &a, &b);
        assert_eq!(diffs.len(), 1);
        assert_eq!(diffs[0].change_type, "modified");
        assert_eq!(diffs[0].field, "$.name");
    }

    #[test]
    fn test_diff_json_values_added() {
        let a = json!({"name": "John"});
        let b = json!({"name": "John", "email": "john@test.com"});
        let diffs = diff_json_values("$", &a, &b);
        assert_eq!(diffs.len(), 1);
        assert_eq!(diffs[0].change_type, "added");
        assert_eq!(diffs[0].field, "$.email");
    }

    #[test]
    fn test_diff_json_values_removed() {
        let a = json!({"name": "John", "age": 30});
        let b = json!({"name": "John"});
        let diffs = diff_json_values("$", &a, &b);
        assert_eq!(diffs.len(), 1);
        assert_eq!(diffs[0].change_type, "removed");
        assert_eq!(diffs[0].field, "$.age");
    }

    #[test]
    fn test_diff_json_values_nested() {
        let a = json!({"user": {"name": "John", "address": {"city": "NYC"}}});
        let b = json!({"user": {"name": "John", "address": {"city": "LA"}}});
        let diffs = diff_json_values("$", &a, &b);
        assert_eq!(diffs.len(), 1);
        assert_eq!(diffs[0].field, "$.user.address.city");
    }

    #[test]
    fn test_diff_json_values_array() {
        let a = json!({"items": [1, 2, 3]});
        let b = json!({"items": [1, 4, 3]});
        let diffs = diff_json_values("$", &a, &b);
        assert_eq!(diffs.len(), 1);
        assert_eq!(diffs[0].field, "$.items[1]");
    }

    #[test]
    fn test_collect_diff_steps() {
        let plan = TestPlan {
            name: "test".into(),
            base_url: "http://localhost".into(),
            default_headers: HashMap::new(),
            steps: vec![Step::Request(RequestStep {
                name: "r1".into(),
                method: Method::Get,
                url: "/health".into(),
                headers: HashMap::new(),
                body: None,
                assert: vec![],
                save_as: String::new(),
                soft_fail: false,
            })],
            setup: vec![],
            teardown: vec![],
        };

        let steps = collect_diff_steps(&plan);
        assert_eq!(steps.len(), 1);
    }
}
