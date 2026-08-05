use anyhow::{Context, Result};
use momus_core::ast::*;
use serde::Deserialize;
use std::collections::HashMap;

/// Convert a HAR (HTTP Archive) file to a TestPlan.
///
/// Reads a HAR JSON file, maps each log entry to a RequestStep with
/// a Status assertion matching the recorded response.
///
/// The generated plan is a starting point — users add `JsonPath`, `Schema`,
/// and `ResponseTime` assertions on top.
pub fn convert(path: &str) -> Result<TestPlan> {
    let content = std::fs::read_to_string(path)
        .with_context(|| format!("Failed to read HAR file: {path}"))?;
    let har: HarFile = serde_json::from_str(&content)
        .with_context(|| format!("Failed to parse HAR JSON: {path}"))?;

    let entries = &har.log.entries;
    if entries.is_empty() {
        anyhow::bail!("HAR file contains no entries");
    }

    let mut steps = Vec::new();
    for (i, entry) in entries.iter().enumerate() {
        let method = entry.request.method.to_uppercase();
        let url = entry.request.url.clone();

        // Build headers
        let mut headers: HashMap<String, String> = HashMap::new();
        for h in &entry.request.headers {
            headers.insert(h.name.clone(), h.value.clone());
        }

        // Build body
        let body = if entry.request.post_data.is_some() {
            entry.request.post_data.as_ref().and_then(|p| {
                serde_json::from_str(&p.text)
                    .ok()
                    .or_else(|| Some(serde_json::Value::String(p.text.clone())))
            })
        } else {
            None
        };

        // Build assertions from the recorded response
        let mut assertions = vec![];
        assertions.push(Assertion::Status(entry.response.status));

        // Add content-type assertion if present
        for h in &entry.response.headers {
            if h.name.to_lowercase() == "content-type" {
                assertions.push(Assertion::ContentType(h.value.clone()));
                break;
            }
        }

        let step_name = if entry.request.url.len() > 40 {
            format!("req_{i}")
        } else {
            format!(
                "req_{i}_{}",
                entry
                    .request
                    .url
                    .trim_start_matches("https://")
                    .trim_start_matches("http://")
                    .replace('/', "_")
                    .chars()
                    .take(30)
                    .collect::<String>()
            )
        };

        let step = RequestStep {
            name: step_name,
            method: parse_method(&method)?,
            url,
            headers,
            body,
            assert: assertions,
            save_as: String::new(),
            soft_fail: false,
        };

        steps.push(Step::Request(step));
    }

    let plan_name = format!(
        "HAR: {} entries from {}",
        entries.len(),
        path.rsplit('/').next().unwrap_or(path)
    );

    Ok(TestPlan {
        name: plan_name,
        base_url: String::new(),
        default_headers: HashMap::new(),
        steps,
        setup: vec![],
        teardown: vec![],
    })
}

/// Generate seed data setup steps from a HAR file.
///
/// Extracts POST and PUT request bodies from recorded traffic and generates
/// setup steps that replay those requests to pre-populate the server.
pub fn generate_seed_data(path: &str) -> Result<Vec<Step>> {
    let content = std::fs::read_to_string(path)
        .with_context(|| format!("Failed to read HAR file: {path}"))?;
    let har: HarFile = serde_json::from_str(&content)
        .with_context(|| format!("Failed to parse HAR JSON: {path}"))?;

    let mut seed_steps = Vec::new();
    for (i, entry) in har.log.entries.iter().enumerate() {
        let method = entry.request.method.to_uppercase();
        if method != "POST" && method != "PUT" {
            continue;
        }

        let body = entry.request.post_data.as_ref().and_then(|p| {
            serde_json::from_str(&p.text)
                .ok()
                .or_else(|| Some(serde_json::Value::String(p.text.clone())))
        });

        if let Some(body) = body {
            let http_method = if method == "POST" {
                Method::Post
            } else {
                Method::Put
            };
            let expected_status = if method == "POST" { 201 } else { 200 };
            seed_steps.push(Step::Request(RequestStep {
                name: format!("seed_{i}"),
                method: http_method,
                url: entry.request.url.clone(),
                headers: HashMap::new(),
                body: Some(body),
                assert: vec![Assertion::Status(expected_status)],
                save_as: String::new(),
                soft_fail: true,
            }));
        }
    }

    Ok(seed_steps)
}

/// Parse a method string into a Method enum.
fn parse_method(method: &str) -> Result<Method> {
    match method.to_uppercase().as_str() {
        "GET" => Ok(Method::Get),
        "POST" => Ok(Method::Post),
        "PUT" => Ok(Method::Put),
        "DELETE" => Ok(Method::Delete),
        "PATCH" => Ok(Method::Patch),
        "HEAD" => Ok(Method::Head),
        "OPTIONS" => Ok(Method::Options),
        other => anyhow::bail!("Unsupported HTTP method: {other}"),
    }
}

// ---------------------------------------------------------------------------
// HAR JSON model (subset of HAR 1.2 spec)
// ---------------------------------------------------------------------------

/// Top-level HAR file structure.
#[derive(Debug, Deserialize)]
struct HarFile {
    log: HarLog,
}

#[derive(Debug, Deserialize)]
struct HarLog {
    entries: Vec<HarEntry>,
}

#[derive(Debug, Deserialize)]
struct HarEntry {
    request: HarRequest,
    response: HarResponse,
}

#[derive(Debug, Deserialize)]
struct HarRequest {
    method: String,
    url: String,
    #[serde(default)]
    headers: Vec<HarHeader>,
    #[serde(default, rename = "postData")]
    post_data: Option<HarPostData>,
}

#[derive(Debug, Deserialize)]
struct HarResponse {
    status: u16,
    #[serde(default)]
    headers: Vec<HarHeader>,
}

#[derive(Debug, Deserialize)]
struct HarHeader {
    name: String,
    value: String,
}

#[derive(Debug, Deserialize)]
struct HarPostData {
    #[serde(default)]
    text: String,
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;
    use std::io::Write;

    fn create_test_har(entries: Vec<serde_json::Value>) -> String {
        let har = json!({
            "log": {
                "entries": entries
            }
        });
        serde_json::to_string_pretty(&har).unwrap()
    }

    #[test]
    fn convert_single_get() {
        let har = create_test_har(vec![json!({
            "request": {
                "method": "GET",
                "url": "https://api.example.com/health",
                "headers": []
            },
            "response": {
                "status": 200,
                "headers": [{"name": "Content-Type", "value": "application/json"}]
            }
        })]);

        let mut tmp = tempfile::NamedTempFile::new().unwrap();
        write!(tmp, "{har}").unwrap();
        let path = tmp.path().to_str().unwrap().to_string();

        let plan = convert(&path).unwrap();
        assert_eq!(plan.steps.len(), 1);
        if let Step::Request(step) = &plan.steps[0] {
            assert_eq!(step.method, Method::Get);
            assert_eq!(step.url, "https://api.example.com/health");
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
        let har = create_test_har(vec![json!({
            "request": {
                "method": "POST",
                "url": "https://api.example.com/users",
                "headers": [{"name": "Content-Type", "value": "application/json"}],
                "postData": {
                    "text": "{\"name\":\"test\"}"
                }
            },
            "response": {
                "status": 201,
                "headers": []
            }
        })]);

        let mut tmp = tempfile::NamedTempFile::new().unwrap();
        write!(tmp, "{har}").unwrap();
        let path = tmp.path().to_str().unwrap().to_string();

        let plan = convert(&path).unwrap();
        assert_eq!(plan.steps.len(), 1);
        if let Step::Request(step) = &plan.steps[0] {
            assert_eq!(step.method, Method::Post);
            assert!(step.body.is_some());
            assert_eq!(step.body.as_ref().unwrap(), &json!({"name": "test"}));
        } else {
            panic!("Expected Request step");
        }
    }

    #[test]
    fn convert_multiple_entries() {
        let har = create_test_har(vec![
            json!({
                "request": {
                    "method": "GET",
                    "url": "https://api.example.com/users",
                    "headers": []
                },
                "response": {
                    "status": 200,
                    "headers": []
                }
            }),
            json!({
                "request": {
                    "method": "POST",
                    "url": "https://api.example.com/users",
                    "headers": [],
                    "postData": {"text": "{\"name\":\"test\"}"}
                },
                "response": {
                    "status": 201,
                    "headers": []
                }
            }),
        ]);

        let mut tmp = tempfile::NamedTempFile::new().unwrap();
        write!(tmp, "{har}").unwrap();
        let path = tmp.path().to_str().unwrap().to_string();

        let plan = convert(&path).unwrap();
        assert_eq!(plan.steps.len(), 2);
    }

    #[test]
    fn convert_empty_har() {
        let har = create_test_har(vec![]);
        let mut tmp = tempfile::NamedTempFile::new().unwrap();
        write!(tmp, "{har}").unwrap();
        let path = tmp.path().to_str().unwrap().to_string();

        let result = convert(&path);
        assert!(result.is_err());
    }

    #[test]
    fn convert_with_content_type_assertion() {
        let har = create_test_har(vec![json!({
            "request": {
                "method": "GET",
                "url": "https://api.example.com/data",
                "headers": []
            },
            "response": {
                "status": 200,
                "headers": [{"name": "Content-Type", "value": "application/json"}]
            }
        })]);

        let mut tmp = tempfile::NamedTempFile::new().unwrap();
        write!(tmp, "{har}").unwrap();
        let path = tmp.path().to_str().unwrap().to_string();

        let plan = convert(&path).unwrap();
        if let Step::Request(step) = &plan.steps[0] {
            assert!(
                step.assert
                    .iter()
                    .any(|a| matches!(a, Assertion::ContentType(ct) if ct == "application/json"))
            );
        } else {
            panic!("Expected Request step");
        }
    }

    #[test]
    fn snapshot_har_single_get() {
        let har = create_test_har(vec![json!({
            "request": {
                "method": "GET",
                "url": "https://api.example.com/health",
                "headers": []
            },
            "response": {
                "status": 200,
                "headers": [{"name": "Content-Type", "value": "application/json"}]
            }
        })]);

        let mut tmp = tempfile::NamedTempFile::new().unwrap();
        write!(tmp, "{har}").unwrap();
        let path = tmp.path().to_str().unwrap().to_string();

        let plan = convert(&path).unwrap();
        insta::assert_json_snapshot!(plan);
    }

    #[test]
    fn snapshot_har_post_with_body() {
        let har = create_test_har(vec![json!({
            "request": {
                "method": "POST",
                "url": "https://api.example.com/users",
                "headers": [{"name": "Content-Type", "value": "application/json"}],
                "postData": {
                    "text": "{\"name\":\"test\"}"
                }
            },
            "response": {
                "status": 201,
                "headers": []
            }
        })]);

        let mut tmp = tempfile::NamedTempFile::new().unwrap();
        write!(tmp, "{har}").unwrap();
        let path = tmp.path().to_str().unwrap().to_string();

        let plan = convert(&path).unwrap();
        insta::assert_json_snapshot!(plan);
    }

    #[test]
    fn convert_string_body() {
        let har = create_test_har(vec![json!({
            "request": {
                "method": "POST",
                "url": "https://api.example.com/echo",
                "headers": [],
                "postData": {
                    "text": "plain text body"
                }
            },
            "response": {
                "status": 200,
                "headers": []
            }
        })]);

        let mut tmp = tempfile::NamedTempFile::new().unwrap();
        write!(tmp, "{har}").unwrap();
        let path = tmp.path().to_str().unwrap().to_string();

        let plan = convert(&path).unwrap();
        if let Step::Request(step) = &plan.steps[0] {
            assert_eq!(
                step.body.as_ref().unwrap(),
                &serde_json::Value::String("plain text body".to_string())
            );
        } else {
            panic!("Expected Request step");
        }
    }
}
