/// Resolve `{template}` references in URLs, headers, and bodies.
///
/// Supported templates:
/// - `{base_url}` → the configured base URL
/// - `{steps.<name>.id}` → the `id` field from a saved step response
/// - `{steps.<name>.<field.path>}` → a field value from a saved step response
/// - `{env.VAR}` → the value of environment variable `VAR`
/// - `{random.uuid}` → a random UUID v4 string
/// - `{random.int}` → a random i64 (0..=i64::MAX)
/// - `{random.int(N,M)}` → a random integer in [N, M]
/// - `{random.string}` → a random alphanumeric string of length 8
/// - `{random.string(N)}` → a random alphanumeric string of length N
use rand::Rng;
use serde_json::Value;
use std::collections::HashMap;
use uuid::Uuid;

/// Resolve templates in a URL string.
pub fn resolve_url(url: &str, base_url: &str, step_responses: &HashMap<String, Value>) -> String {
    let mut result = url.to_string();
    result = result.replace("{base_url}", base_url);
    resolve_env_templates(&mut result);
    resolve_step_templates(&mut result, step_responses);
    resolve_random_templates(&mut result);
    result
}

/// Resolve templates in a JSON value (for request bodies).
pub fn resolve_body(body: &mut Value, step_responses: &HashMap<String, Value>) {
    match body {
        Value::String(s) => {
            resolve_env_templates(s);
            resolve_step_templates(s, step_responses);
            resolve_random_templates(s);
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
        resolve_random_templates(value);
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
                tracing::warn!(
                    "Environment variable '{}' not set, leaving template unresolved",
                    var_name
                );
                caps[0].to_string()
            })
        })
        .to_string();
}

/// Resolve `{random.*}` templates in a string.
///
/// Supported patterns:
/// - `{random.uuid}` → UUID v4
/// - `{random.int}` → random i64 in 0..=i64::MAX
/// - `{random.int(N,M)}` → random integer in [N, M]
/// - `{random.string}` → random alphanumeric of length 8
/// - `{random.string(N)}` → random alphanumeric of length N
fn resolve_random_templates(s: &mut String) {
    let mut rng = rand::rng();

    // {random.uuid}
    let re_uuid = regex::Regex::new(r"\{random\.uuid\}").unwrap();
    *s = re_uuid
        .replace_all(s, |_: &regex::Captures| Uuid::new_v4().to_string())
        .to_string();

    // {random.int(N,M)} — must be checked before bare {random.int}
    let re_int_range = regex::Regex::new(r"\{random\.int\((\d+),\s*(\d+)\)\}").unwrap();
    *s = re_int_range
        .replace_all(s, |caps: &regex::Captures| {
            let lo: i64 = caps[1].parse().unwrap_or(0);
            let hi: i64 = caps[2].parse().unwrap_or(i64::MAX);
            if lo <= hi {
                rng.random_range(lo..=hi).to_string()
            } else {
                caps[0].to_string()
            }
        })
        .to_string();

    // {random.int}
    let re_int = regex::Regex::new(r"\{random\.int\}").unwrap();
    *s = re_int
        .replace_all(s, |_: &regex::Captures| {
            rng.random_range(0..=i64::MAX).to_string()
        })
        .to_string();

    // {random.string(N)}
    let re_str_len = regex::Regex::new(r"\{random\.string\((\d+)\)\}").unwrap();
    *s = re_str_len
        .replace_all(s, |caps: &regex::Captures| {
            let len: usize = caps[1].parse().unwrap_or(8);
            random_alphanumeric(&mut rng, len)
        })
        .to_string();

    // {random.string}
    let re_str = regex::Regex::new(r"\{random\.string\}").unwrap();
    *s = re_str
        .replace_all(s, |_: &regex::Captures| random_alphanumeric(&mut rng, 8))
        .to_string();
}

/// Generate a random alphanumeric string of the given length.
fn random_alphanumeric(rng: &mut impl Rng, len: usize) -> String {
    const CHARSET: &[u8] = b"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789";
    (0..len)
        .map(|_| {
            let idx = rng.random_range(0..CHARSET.len());
            CHARSET[idx] as char
        })
        .collect()
}

/// Resolve `{steps.<name>.id}` and `{steps.<name>.<field>}` templates in a string.
fn resolve_step_templates(s: &mut String, step_responses: &HashMap<String, Value>) {
    for (step_name, response) in step_responses {
        let id_pattern = format!("{{steps.{step_name}.id}}");
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
            format!("{prefix}.{key}")
        };

        let pattern = format!("{{steps.{step_name}.{current_path}}}");

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
        unsafe {
            std::env::set_var("MOMUS_TEST_URL", "http://test:8080");
        }
        let result = resolve_url(
            "{env.MOMUS_TEST_URL}/api",
            "http://fallback",
            &HashMap::new(),
        );
        assert_eq!(result, "http://test:8080/api");
        unsafe {
            std::env::remove_var("MOMUS_TEST_URL");
        }
    }

    #[test]
    fn resolve_env_var_missing() {
        // Missing env var should leave template unresolved
        let result = resolve_url("{env.NONEXISTENT_VAR}/path", "http://base", &HashMap::new());
        assert_eq!(result, "{env.NONEXISTENT_VAR}/path");
    }

    #[test]
    fn random_uuid_format() {
        let result = resolve_url("{random.uuid}", "", &HashMap::new());
        // UUID v4 format: 8-4-4-4-12 hex digits
        let re = regex::Regex::new(
            r"^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$",
        )
        .unwrap();
        assert!(re.is_match(&result), "not a valid UUID v4: {result}");
    }

    #[test]
    fn random_uuid_multiple_different() {
        let a = resolve_url("{random.uuid}", "", &HashMap::new());
        let b = resolve_url("{random.uuid}", "", &HashMap::new());
        assert_ne!(a, b, "consecutive UUIDs should differ");
    }

    #[test]
    fn random_int_default() {
        let result = resolve_url("{random.int}", "", &HashMap::new());
        let val: i64 = result.parse().expect("should be a valid i64");
        assert!(val >= 0, "default random.int should be >= 0");
    }

    #[test]
    fn random_int_range() {
        let result = resolve_url("{random.int(1,100)}", "", &HashMap::new());
        let val: i64 = result.parse().expect("should be a valid i64");
        assert!((1..=100).contains(&val), "value {val} not in [1, 100]");
    }

    #[test]
    fn random_int_range_edge() {
        let result = resolve_url("{random.int(5,5)}", "", &HashMap::new());
        assert_eq!(result, "5");
    }

    #[test]
    fn random_string_default() {
        let result = resolve_url("{random.string}", "", &HashMap::new());
        assert_eq!(result.len(), 8, "default random.string should be length 8");
        assert!(result.chars().all(|c| c.is_ascii_alphanumeric()));
    }

    #[test]
    fn random_string_explicit_length() {
        let result = resolve_url("{random.string(16)}", "", &HashMap::new());
        assert_eq!(result.len(), 16);
        assert!(result.chars().all(|c| c.is_ascii_alphanumeric()));
    }

    #[test]
    fn random_string_multiple_different() {
        let a = resolve_url("{random.string}", "", &HashMap::new());
        let b = resolve_url("{random.string}", "", &HashMap::new());
        assert_ne!(a, b, "consecutive random strings should differ");
    }

    #[test]
    fn random_unknown_pattern_left_unresolved() {
        let result = resolve_url("{random.unknown}", "", &HashMap::new());
        assert_eq!(result, "{random.unknown}");
    }

    #[test]
    fn random_in_body() {
        let mut body = serde_json::json!({
            "id": "{random.uuid}",
            "name": "test-{random.string(4)}"
        });
        resolve_body(&mut body, &HashMap::new());
        if let serde_json::Value::String(id) = &body["id"] {
            let re = regex::Regex::new(
                r"^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$",
            )
            .unwrap();
            assert!(re.is_match(id), "not a valid UUID v4: {id}");
        } else {
            panic!("id field should be a string");
        }
        if let serde_json::Value::String(name) = &body["name"] {
            assert!(name.starts_with("test-"));
            assert_eq!(name.len(), 9); // "test-" + 4 chars
        } else {
            panic!("name field should be a string");
        }
    }

    #[test]
    fn random_in_headers() {
        let mut headers = HashMap::new();
        headers.insert("X-Trace-Id".into(), "req-{random.string(12)}".into());
        resolve_headers(&mut headers, &HashMap::new());
        let val = headers.get("X-Trace-Id").unwrap();
        assert!(val.starts_with("req-"));
        assert_eq!(val.len(), 16); // "req-" + 12 chars
    }

    #[test]
    fn random_int_multiple_different() {
        let a = resolve_url("{random.int}", "", &HashMap::new());
        let b = resolve_url("{random.int}", "", &HashMap::new());
        // Extremely unlikely to collide
        assert_ne!(a, b, "consecutive random ints should differ");
    }

    #[test]
    fn random_int_zero_range() {
        let result = resolve_url("{random.int(0,0)}", "", &HashMap::new());
        assert_eq!(result, "0");
    }

    #[test]
    fn random_int_invalid_range_left_unresolved() {
        // lo > hi should leave template unresolved
        let result = resolve_url("{random.int(10,1)}", "", &HashMap::new());
        assert_eq!(result, "{random.int(10,1)}");
    }

    #[test]
    fn random_string_zero_length() {
        let result = resolve_url("{random.string(0)}", "", &HashMap::new());
        assert_eq!(result, "");
    }

    #[test]
    fn random_string_single_char() {
        let result = resolve_url("{random.string(1)}", "", &HashMap::new());
        assert_eq!(result.len(), 1);
        assert!(result.chars().all(|c| c.is_ascii_alphanumeric()));
    }

    #[test]
    fn random_multiple_in_same_string() {
        let result = resolve_url("{random.uuid}-{random.uuid}", "", &HashMap::new());
        // Two UUIDs separated by a dash = 36 + 1 + 36 = 73 chars
        assert_eq!(result.len(), 73);
        // Each UUID has 4 dashes, so two UUIDs + separator = 9 dashes total
        let dash_count = result.chars().filter(|&c| c == '-').count();
        assert_eq!(dash_count, 9);
    }

    #[test]
    fn random_mixed_with_base_url() {
        let result = resolve_url(
            "{base_url}/patients/{random.uuid}",
            "http://localhost:8080",
            &HashMap::new(),
        );
        assert!(result.starts_with("http://localhost:8080/patients/"));
        let uuid_part = result
            .strip_prefix("http://localhost:8080/patients/")
            .unwrap();
        let re = regex::Regex::new(
            r"^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$",
        )
        .unwrap();
        assert!(re.is_match(uuid_part), "not a valid UUID v4: {uuid_part}");
    }

    #[test]
    fn random_int_in_body() {
        let mut body = serde_json::json!({
            "count": "{random.int(1,100)}"
        });
        resolve_body(&mut body, &HashMap::new());
        if let serde_json::Value::String(val) = &body["count"] {
            let n: i64 = val.parse().expect("should be a valid i64");
            assert!((1..=100).contains(&n), "value {n} not in [1, 100]");
        } else {
            panic!("count field should be a string");
        }
    }

    #[test]
    fn random_string_in_url() {
        let result = resolve_url("/search?q={random.string(6)}", "", &HashMap::new());
        assert!(result.starts_with("/search?q="));
        let param = result.strip_prefix("/search?q=").unwrap();
        assert_eq!(param.len(), 6);
        assert!(param.chars().all(|c| c.is_ascii_alphanumeric()));
    }
}
