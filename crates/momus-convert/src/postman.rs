use anyhow::{Context, Result};
use momus_core::ast::*;
use serde_json::Value;
use std::collections::HashMap;

/// Convert a Postman Collection v2.1 to a TestPlan.
///
/// Reads a Postman collection JSON file, walks items recursively
/// (handling folders), and converts each request to a RequestStep.
pub fn convert(path: &str) -> Result<TestPlan> {
    let content = std::fs::read_to_string(path)
        .with_context(|| format!("Failed to read Postman collection: {path}"))?;

    let collection: Value = serde_json::from_str(&content)
        .with_context(|| format!("Failed to parse Postman collection JSON: {path}"))?;

    let info = collection
        .get("info")
        .or_else(|| collection.get("collection").and_then(|c| c.get("info")));
    let name = info
        .and_then(|i| i.get("name").and_then(|n| n.as_str()))
        .unwrap_or("Postman Collection");

    let items = collection
        .get("item")
        .or_else(|| collection.get("collection").and_then(|c| c.get("item")))
        .and_then(|i| i.as_array())
        .cloned()
        .unwrap_or_default();

    let mut steps = Vec::new();
    walk_items(&items, &mut steps);

    Ok(TestPlan {
        name: format!("Postman: {name}"),
        base_url: String::new(),
        default_headers: HashMap::new(),
        steps,
        setup: vec![],
        teardown: vec![],
    })
}

/// Generate seed data setup steps from a Postman collection.
///
/// Extracts POST and PUT request bodies from the collection and generates
/// setup steps that pre-populate the server with those resources.
pub fn generate_seed_data(path: &str) -> Result<Vec<Step>> {
    let content = std::fs::read_to_string(path)
        .with_context(|| format!("Failed to read Postman collection: {path}"))?;

    let collection: Value = serde_json::from_str(&content)
        .with_context(|| format!("Failed to parse Postman collection JSON: {path}"))?;

    let items = collection
        .get("item")
        .or_else(|| collection.get("collection").and_then(|c| c.get("item")))
        .and_then(|i| i.as_array())
        .cloned()
        .unwrap_or_default();

    let mut seed_steps = Vec::new();
    extract_seed_items(&items, &mut seed_steps);
    Ok(seed_steps)
}

/// Walk Postman items recursively, extracting POST/PUT bodies as seed data.
fn extract_seed_items(items: &[Value], steps: &mut Vec<Step>) {
    for item in items {
        if let Some(sub_items) = item.get("item").and_then(|v| v.as_array()) {
            extract_seed_items(sub_items, steps);
        } else if let Some(request) = item.get("request") {
            let method = request
                .get("method")
                .and_then(|m| m.as_str())
                .unwrap_or("GET")
                .to_uppercase();

            if (method == "POST" || method == "PUT")
                && let Some(body) = extract_body(request)
            {
                let url = extract_url(request);
                if !url.is_empty() {
                    let name = item.get("name").and_then(|n| n.as_str()).unwrap_or("seed");
                    let http_method = if method == "POST" {
                        Method::Post
                    } else {
                        Method::Put
                    };
                    let expected_status = if method == "POST" { 201 } else { 200 };
                    steps.push(Step::Request(RequestStep {
                        name: format!("seed_{name}"),
                        method: http_method,
                        url,
                        headers: HashMap::new(),
                        body: Some(body),
                        assert: vec![Assertion::Status(expected_status)],
                        save_as: String::new(),
                        soft_fail: true,
                    }));
                }
            }
        }
    }
}

/// Walk Postman items recursively, converting requests to steps.
fn walk_items(items: &[Value], steps: &mut Vec<Step>) {
    for item in items {
        if let Some(sub_items) = item.get("item").and_then(|v| v.as_array()) {
            // This is a folder — recurse into it
            walk_items(sub_items, steps);
        } else if let Some(request) = item.get("request")
            && let Some(step) = convert_request(item, request)
        {
            steps.push(step);
        }
    }
}

/// Convert a single Postman request to a Step.
fn convert_request(item: &Value, request: &Value) -> Option<Step> {
    let name = item
        .get("name")
        .and_then(|n| n.as_str())
        .unwrap_or("postman_request")
        .to_string();

    // Method
    let method_str = request
        .get("method")
        .and_then(|m| m.as_str())
        .unwrap_or("GET");
    let method = match method_str.to_uppercase().as_str() {
        "GET" => Method::Get,
        "POST" => Method::Post,
        "PUT" => Method::Put,
        "DELETE" => Method::Delete,
        "PATCH" => Method::Patch,
        "HEAD" => Method::Head,
        "OPTIONS" => Method::Options,
        _ => Method::Get,
    };

    // URL
    let url = extract_url(request);

    // Headers
    let mut headers = HashMap::new();
    if let Some(header_array) = request.get("header").and_then(|h| h.as_array()) {
        for h in header_array {
            let key = h.get("key").and_then(|k| k.as_str()).unwrap_or("");
            let value = h.get("value").and_then(|v| v.as_str()).unwrap_or("");
            if !key.is_empty() {
                headers.insert(key.to_string(), value.to_string());
            }
        }
    }

    // Body
    let body = extract_body(request);

    // Assertions from responses
    let mut assertions = Vec::new();
    if let Some(responses) = item.get("response").and_then(|r| r.as_array())
        && let Some(first_resp) = responses.first()
        && let Some(status) = first_resp
            .get("code")
            .and_then(|c| c.as_u64())
            .map(|c| c as u16)
    {
        assertions.push(Assertion::Status(status));
    }
    if assertions.is_empty() {
        assertions.push(Assertion::Status(200));
    }

    Some(Step::Request(RequestStep {
        name,
        method,
        url,
        headers,
        body,
        assert: assertions,
        save_as: String::new(),
        soft_fail: false,
    }))
}

/// Extract URL from a Postman request object.
fn extract_url(request: &Value) -> String {
    // Try url.raw first
    if let Some(raw) = request
        .get("url")
        .and_then(|u| u.get("raw").and_then(|r| r.as_str()))
    {
        return raw.to_string();
    }

    // Try url.path + host
    if let Some(url_obj) = request.get("url") {
        let host = url_obj
            .get("host")
            .and_then(|h| h.as_array())
            .map(|parts| {
                parts
                    .iter()
                    .filter_map(|p| p.as_str())
                    .collect::<Vec<_>>()
                    .join(".")
            })
            .unwrap_or_default();

        let path = url_obj
            .get("path")
            .and_then(|p| p.as_array())
            .map(|parts| {
                parts
                    .iter()
                    .filter_map(|p| p.as_str())
                    .collect::<Vec<_>>()
                    .join("/")
            })
            .unwrap_or_default();

        let protocol = url_obj
            .get("protocol")
            .and_then(|p| p.as_str())
            .unwrap_or("https");

        if !host.is_empty() {
            return format!("{protocol}://{host}/{path}");
        }
    }

    String::new()
}

/// Extract body from a Postman request object.
fn extract_body(request: &Value) -> Option<Value> {
    let body = request.get("body")?;
    let mode = body.get("mode").and_then(|m| m.as_str()).unwrap_or("");

    match mode {
        "raw" => {
            let raw = body.get("raw").and_then(|r| r.as_str()).unwrap_or("");
            serde_json::from_str(raw).ok().or_else(|| {
                if raw.is_empty() {
                    None
                } else {
                    Some(Value::String(raw.to_string()))
                }
            })
        }
        "urlencoded" => {
            let mut params = HashMap::new();
            if let Some(form) = body.get("urlencoded").and_then(|f| f.as_array()) {
                for param in form {
                    let key = param.get("key").and_then(|k| k.as_str()).unwrap_or("");
                    let value = param.get("value").and_then(|v| v.as_str()).unwrap_or("");
                    if !key.is_empty() {
                        params.insert(key.to_string(), Value::String(value.to_string()));
                    }
                }
            }
            if params.is_empty() {
                None
            } else {
                Some(Value::Object(params.into_iter().collect()))
            }
        }
        "formdata" => {
            let mut params = HashMap::new();
            if let Some(form) = body.get("formdata").and_then(|f| f.as_array()) {
                for param in form {
                    let key = param.get("key").and_then(|k| k.as_str()).unwrap_or("");
                    let value = param.get("value").and_then(|v| v.as_str()).unwrap_or("");
                    if !key.is_empty() {
                        params.insert(key.to_string(), Value::String(value.to_string()));
                    }
                }
            }
            if params.is_empty() {
                None
            } else {
                Some(Value::Object(params.into_iter().collect()))
            }
        }
        _ => None,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn create_test_collection() -> String {
        r#"{
            "info": {
                "name": "Test API",
                "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"
            },
            "item": [
                {
                    "name": "Health Check",
                    "request": {
                        "method": "GET",
                        "header": [
                            {"key": "Accept", "value": "application/json"}
                        ],
                        "url": {
                            "raw": "https://api.example.com/health",
                            "protocol": "https",
                            "host": ["api", "example", "com"],
                            "path": ["health"]
                        }
                    },
                    "response": [
                        {"code": 200, "body": "{\"status\":\"ok\"}"}
                    ]
                },
                {
                    "name": "Create User",
                    "request": {
                        "method": "POST",
                        "header": [
                            {"key": "Content-Type", "value": "application/json"}
                        ],
                        "body": {
                            "mode": "raw",
                            "raw": "{\"name\":\"John\",\"email\":\"john@test.com\"}"
                        },
                        "url": {
                            "raw": "https://api.example.com/users",
                            "protocol": "https",
                            "host": ["api", "example", "com"],
                            "path": ["users"]
                        }
                    }
                },
                {
                    "name": "Folder",
                    "item": [
                        {
                            "name": "Get User",
                            "request": {
                                "method": "GET",
                                "url": {
                                    "raw": "https://api.example.com/users/1",
                                    "protocol": "https",
                                    "host": ["api", "example", "com"],
                                    "path": ["users", "1"]
                                }
                            }
                        }
                    ]
                }
            ]
        }"#
        .to_string()
    }

    #[test]
    fn convert_postman_collection() {
        let spec = create_test_collection();
        let mut tmp = tempfile::NamedTempFile::new().unwrap();
        use std::io::Write;
        write!(tmp, "{spec}").unwrap();
        let path = tmp.path().to_str().unwrap().to_string();
        let plan = convert(&path).unwrap();
        assert_eq!(plan.name, "Postman: Test API");
        assert_eq!(plan.steps.len(), 3);
    }

    #[test]
    fn convert_get_request() {
        let spec = create_test_collection();
        let mut tmp = tempfile::NamedTempFile::new().unwrap();
        use std::io::Write;
        write!(tmp, "{spec}").unwrap();
        let path = tmp.path().to_str().unwrap().to_string();
        let plan = convert(&path).unwrap();
        if let Step::Request(step) = &plan.steps[0] {
            assert_eq!(step.method, Method::Get);
            assert_eq!(step.url, "https://api.example.com/health");
            assert_eq!(step.headers.get("Accept").unwrap(), "application/json");
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
        let spec = create_test_collection();
        let mut tmp = tempfile::NamedTempFile::new().unwrap();
        use std::io::Write;
        write!(tmp, "{spec}").unwrap();
        let path = tmp.path().to_str().unwrap().to_string();
        let plan = convert(&path).unwrap();
        if let Step::Request(step) = &plan.steps[1] {
            assert_eq!(step.method, Method::Post);
            assert_eq!(step.url, "https://api.example.com/users");
            assert!(step.body.is_some());
        } else {
            panic!("Expected Request step");
        }
    }

    #[test]
    fn convert_folder_items() {
        let spec = create_test_collection();
        let mut tmp = tempfile::NamedTempFile::new().unwrap();
        use std::io::Write;
        write!(tmp, "{spec}").unwrap();
        let path = tmp.path().to_str().unwrap().to_string();
        let plan = convert(&path).unwrap();
        // Third step should be from the folder
        if let Step::Request(step) = &plan.steps[2] {
            assert_eq!(step.name, "Get User");
            assert_eq!(step.url, "https://api.example.com/users/1");
        } else {
            panic!("Expected Request step");
        }
    }

    #[test]
    fn snapshot_postman_collection() {
        let spec = create_test_collection();
        let mut tmp = tempfile::NamedTempFile::new().unwrap();
        use std::io::Write;
        write!(tmp, "{spec}").unwrap();
        let path = tmp.path().to_str().unwrap().to_string();
        let plan = convert(&path).unwrap();
        insta::assert_json_snapshot!(plan);
    }

    #[test]
    fn test_extract_url_raw() {
        let request = serde_json::json!({
            "url": {"raw": "https://api.example.com/test"}
        });
        assert_eq!(extract_url(&request), "https://api.example.com/test");
    }

    #[test]
    fn test_extract_url_parts() {
        let request = serde_json::json!({
            "url": {
                "protocol": "http",
                "host": ["localhost", "8080"],
                "path": ["api", "v1"]
            }
        });
        assert_eq!(extract_url(&request), "http://localhost.8080/api/v1");
    }

    #[test]
    fn test_extract_body_raw() {
        let request = serde_json::json!({
            "body": {
                "mode": "raw",
                "raw": "{\"key\":\"value\"}"
            }
        });
        let body = extract_body(&request);
        assert!(body.is_some());
        assert_eq!(body.unwrap()["key"], "value");
    }

    #[test]
    fn test_extract_body_urlencoded() {
        let request = serde_json::json!({
            "body": {
                "mode": "urlencoded",
                "urlencoded": [
                    {"key": "name", "value": "John"},
                    {"key": "age", "value": "30"}
                ]
            }
        });
        let body = extract_body(&request);
        assert!(body.is_some());
        let obj = body.unwrap();
        assert_eq!(obj["name"], "John");
        assert_eq!(obj["age"], "30");
    }
}
