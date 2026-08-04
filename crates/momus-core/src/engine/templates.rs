/// Resolve `{template}` references in URLs, headers, and bodies.
///
/// Supported templates:
/// - `{base_url}` → the configured base URL
/// - `{steps.<name>.id}` → the `id` field from a saved step response
/// - `{steps.<name>.<field.path>}` → a field value from a saved step response
/// - `{env.VAR}` → the value of environment variable `VAR`
use serde_json::Value;
use std::collections::HashMap;

/// Resolve templates in a URL string.
pub fn resolve_url(url: &str, base_url: &str, step_responses: &HashMap<String, Value>) -> String {
    let mut result = url.to_string();
    result = result.replace("{base_url}", base_url);
    resolve_env_templates(&mut result);
    resolve_step_templates(&mut result, step_responses);
    result
}

/// Resolve templates in a JSON value (for request bodies).
pub fn resolve_body(body: &mut Value, step_responses: &HashMap<String, Value>) {
    match body {
        Value::String(s) => {
            resolve_env_templates(s);
            resolve_step_templates(s, step_responses);
        }
        Value::Object(obj) => {
            for v in obj.values_mut() {
                resolve_body(v, step_responses);
            }
        }
        Value::Array(arr) => {
            for v in arr.iter_mut() {
                resolve_body(v, step_responses);
            }
        }
        _ => {}
    }
}

/// Resolve templates in a string map (for headers).
pub fn resolve_headers(
    headers: &mut HashMap<String, String>,
    step_responses: &HashMap<String, Value>,
) {
    for value in headers.values_mut() {
        resolve_env_templates(value);
        resolve_step_templates(value, step_responses);
    }
}

/// Resolve `{env.VAR}` templates in a string.
fn resolve_env_templates(s: &mut String) {
    // Match {env.VAR_NAME} patterns
    let re = regex::Regex::new(r"\{env\.([A-Za-z_][A-Za-z0-9_]*)\}").unwrap();
    *s = re
        .replace_all(s, |caps: &regex::Captures| {
            let var_name = &caps[1];
            std::env::var(var_name).unwrap_or_else(|_| {
                tracing::warn!("Environment variable '{}' not set, leaving template unresolved", var_name);
                caps[0].to_string()
            })
        })
        .to_string();
}

/// Resolve `{steps.<name>.id}` and `{steps.<name>.<field>}` templates in a string.
fn resolve_step_templates(s: &mut String, step_responses: &HashMap<String, Value>) {
    for (step_name, response) in step_responses {
        let id_pattern = format!("{{steps.{}.id}}", step_name);
        if let Some(id) = response.get("id").and_then(|v| v.as_str())
            && s.contains(&id_pattern)
        {
            *s = s.replace(&id_pattern, id);
        }

        if let Some(obj) = response.as_object() {
            resolve_step_fields(s, step_name, "", obj);
        }
    }
}

/// Recursively walk a JSON object and resolve `{steps.<name>.<path>}` templates.
fn resolve_step_fields(
    s: &mut String,
    step_name: &str,
    prefix: &str,
    obj: &serde_json::Map<String, Value>,
) {
    for (key, value) in obj {
        let current_path = if prefix.is_empty() {
            key.clone()
        } else {
            format!("{}.{}", prefix, key)
        };

        let pattern = format!("{{steps.{}.{}}}", step_name, current_path);

        match value {
            Value::String(v) => {
                if s.contains(&pattern) {
                    *s = s.replace(&pattern, v);
                }
            }
            Value::Number(n) => {
                let v = n.to_string();
                if s.contains(&pattern) {
                    *s = s.replace(&pattern, &v);
                }
            }
            Value::Array(arr) => {
                // For arrays, try index 0 as a shorthand
                if let Some(Value::Object(child)) = arr.first() {
                    resolve_step_fields(s, step_name, &current_path, child);
                }
            }
            Value::Object(child) => {
                resolve_step_fields(s, step_name, &current_path, child);
            }
            _ => {}
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn resolve_base_url() {
        let result = resolve_url(
            "{base_url}/Patient",
            "http://localhost:8080",
            &HashMap::new(),
        );
        assert_eq!(result, "http://localhost:8080/Patient");
    }

    #[test]
    fn resolve_step_id() {
        let mut steps = HashMap::new();
        steps.insert(
            "create_patient".into(),
            json!({
                "resourceType": "Patient",
                "id": "pat-001"
            }),
        );

        let result = resolve_url("/Patient/{steps.create_patient.id}", "", &steps);
        assert_eq!(result, "/Patient/pat-001");
    }

    #[test]
    fn resolve_step_field() {
        let mut steps = HashMap::new();
        steps.insert(
            "create_patient".into(),
            json!({
                "resourceType": "Patient",
                "id": "pat-001",
                "name": [{"family": "Smith"}]
            }),
        );

        let result = resolve_url(
            "/Patient?name={steps.create_patient.name.family}",
            "",
            &steps,
        );
        assert_eq!(result, "/Patient?name=Smith");
    }

    #[test]
    fn resolve_body_templates() {
        let mut steps = HashMap::new();
        steps.insert(
            "parent".into(),
            json!({
                "id": "org-001"
            }),
        );

        let mut body = json!({
            "resourceType": "Organization",
            "partOf": {"reference": "Organization/{steps.parent.id}"}
        });

        resolve_body(&mut body, &steps);

        assert_eq!(
            body,
            json!({
                "resourceType": "Organization",
                "partOf": {"reference": "Organization/org-001"}
            })
        );
    }

    #[test]
    fn resolve_headers_templates() {
        let mut steps = HashMap::new();
        steps.insert(
            "auth".into(),
            json!({
                "token": "abc-123"
            }),
        );

        let mut headers = HashMap::new();
        headers.insert("Authorization".into(), "Bearer {steps.auth.token}".into());

        resolve_headers(&mut headers, &steps);

        assert_eq!(headers.get("Authorization").unwrap(), "Bearer abc-123");
    }

    #[test]
    fn no_templates_unchanged() {
        let result = resolve_url("/plain/url", "http://base", &HashMap::new());
        assert_eq!(result, "/plain/url");
    }

    #[test]
    fn resolve_env_var() {
        // Set a test env var
        unsafe { std::env::set_var("MOMUS_TEST_URL", "http://test:8080"); }
        let result = resolve_url("{env.MOMUS_TEST_URL}/api", "http://fallback", &HashMap::new());
        assert_eq!(result, "http://test:8080/api");
        unsafe { std::env::remove_var("MOMUS_TEST_URL"); }
    }

    #[test]
    fn resolve_env_var_missing() {
        // Missing env var should leave template unresolved
        let result = resolve_url("{env.NONEXISTENT_VAR}/path", "http://base", &HashMap::new());
        assert_eq!(result, "{env.NONEXISTENT_VAR}/path");
    }
}
