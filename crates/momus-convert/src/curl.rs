use anyhow::{Context, Result};
use momus_core::ast::*;
use std::collections::HashMap;

/// Convert a cURL command to a TestPlan.
///
/// Parses a curl command string and produces a single RequestStep
/// with method, URL, headers, and body extracted from the flags.
///
/// Supported flags:
/// - `-X`/`--request` → HTTP method
/// - `-H`/`--header` → request headers
/// - `-d`/`--data`/`--data-raw`/`--data-binary` → request body
/// - URL (first non-flag argument)
/// - `--max-time` → timeout (stored as metadata)
/// - `-u`/`--user` → basic auth header
/// - `-b`/`--cookie` → cookie header
pub fn convert(command: &str) -> Result<TestPlan> {
    let args = parse_curl_command(command)?;
    let (method, url, headers, body) = extract_request_parts(&args)?;

    let mut assertions = vec![];
    // Default: expect 200 OK for most requests, 201 for POST/PUT
    let default_status = match method.as_str() {
        "POST" | "PUT" | "PATCH" => 201,
        "DELETE" => 204,
        _ => 200,
    };
    assertions.push(Assertion::Status(default_status));

    let step = RequestStep {
        name: format!("curl_{}", method.to_lowercase()),
        method: parse_method(&method)?,
        url,
        headers,
        body,
        assert: assertions,
        save_as: String::new(),
        soft_fail: false,
    };

    Ok(TestPlan {
        name: format!("cURL: {}", truncate(command, 60)),
        base_url: String::new(),
        default_headers: HashMap::new(),
        steps: vec![Step::Request(step)],
        setup: vec![],
        teardown: vec![],
    })
}

/// Parse a curl command string into a list of tokens.
fn parse_curl_command(command: &str) -> Result<Vec<String>> {
    let trimmed = command.trim();
    if trimmed.is_empty() {
        anyhow::bail!("Empty cURL command");
    }

    // Remove leading "curl " if present
    let without_prefix = if let Some(rest) = trimmed
        .strip_prefix("curl ")
        .or_else(|| trimmed.strip_prefix("curl\t"))
    {
        rest
    } else {
        trimmed
    };

    // Simple tokenizer that handles single and double quotes
    let mut tokens = Vec::new();
    let mut current = String::new();
    let mut in_single_quote = false;
    let mut in_double_quote = false;
    let mut escape = false;

    for ch in without_prefix.chars() {
        if escape {
            current.push(ch);
            escape = false;
            continue;
        }

        if ch == '\\' && in_double_quote {
            escape = true;
            continue;
        }

        if ch == '\'' && !in_double_quote {
            in_single_quote = !in_single_quote;
            continue;
        }

        if ch == '"' && !in_single_quote {
            in_double_quote = !in_double_quote;
            continue;
        }

        if ch.is_whitespace() && !in_single_quote && !in_double_quote {
            if !current.is_empty() {
                tokens.push(current.clone());
                current.clear();
            }
        } else {
            current.push(ch);
        }
    }

    if !current.is_empty() {
        tokens.push(current);
    }

    if in_single_quote || in_double_quote {
        anyhow::bail!("Unterminated quote in cURL command");
    }

    Ok(tokens)
}

/// Extract method, URL, headers, and body from parsed curl arguments.
fn extract_request_parts(
    args: &[String],
) -> Result<(String, String, HashMap<String, String>, Option<serde_json::Value>)> {
    let mut method = String::from("GET");
    let mut url = String::new();
    let mut headers: HashMap<String, String> = HashMap::new();
    let mut body: Option<String> = None;

    let mut i = 0;
    while i < args.len() {
        let arg = &args[i];

        match arg.as_str() {
            // Method flags
            "-X" | "--request" => {
                i += 1;
                method = args
                    .get(i)
                    .cloned()
                    .context("Missing value after -X/--request")?;
            }

            // Header flags
            "-H" | "--header" => {
                i += 1;
                let header_line = args
                    .get(i)
                    .cloned()
                    .context("Missing value after -H/--header")?;
                if let Some((key, value)) = header_line.split_once(':') {
                    let k = key.trim().to_string();
                    let v = value.trim().to_string();
                    headers.insert(k, v);
                } else {
                    anyhow::bail!("Invalid header format: '{}' (expected 'Key: Value')", header_line);
                }
            }

            // Body flags
            "-d" | "--data" | "--data-raw" | "--data-binary" => {
                i += 1;
                let value = args
                    .get(i)
                    .cloned()
                    .context("Missing value after -d/--data")?;
                body = Some(value);
                // Data implies POST
                if method == "GET" {
                    method = "POST".to_string();
                }
            }

            // Auth flag
            "-u" | "--user" => {
                i += 1;
                let creds = args
                    .get(i)
                    .cloned()
                    .context("Missing value after -u/--user")?;
                let encoded = base64_encode(&creds);
                headers.insert(
                    "Authorization".to_string(),
                    format!("Basic {}", encoded),
                );
            }

            // Cookie flag
            "-b" | "--cookie" => {
                i += 1;
                let cookie = args
                    .get(i)
                    .cloned()
                    .context("Missing value after -b/--cookie")?;
                headers.insert("Cookie".to_string(), cookie);
            }

            // Timeout flag
            "--max-time" => {
                i += 1;
                // Skip the value — we don't store timeout in the plan yet
            }

            // Skip known flags that take no value
            "-s" | "--silent" | "-S" | "--show-error" | "-L" | "--location"
            | "-k" | "--insecure" | "-v" | "--verbose" | "-i" | "--include"
            | "-N" | "--no-buffer" | "-0" | "--http1.0" | "--http1.1"
            | "--http2" | "--compressed" | "-O" | "--remote-name" => {}

            // Skip flags that take a value (we don't process them)
            "--connect-timeout" | "--retry" | "--retry-delay" | "--retry-max-time"
            | "-o" | "--output" | "-w" | "--write-out"
            | "-T" | "--upload-file" | "-F" | "--form" | "-C" | "--continue-at"
            | "-z" | "--time-cond" | "-e" | "--referer" | "-A" | "--user-agent" => {
                i += 1; // skip the value
            }

            // If it doesn't start with '-', it's the URL
            _ if !arg.starts_with('-') => {
                if url.is_empty() {
                    url = arg.clone();
                }
                // Ignore subsequent non-flag args (they might be part of the command)
            }

            _ => {
                // Unknown flag — skip it and its value if it takes one
                if i + 1 < args.len() && args[i + 1].starts_with('-') {
                    // Next is a flag, so this flag takes no value
                } else if i + 1 < args.len() {
                    i += 1; // skip the value
                }
            }
        }

        i += 1;
    }

    if url.is_empty() {
        anyhow::bail!("No URL found in cURL command");
    }

    // Parse body as JSON if possible
    let body_json = match body {
        Some(b) => {
            if b.trim().starts_with('{') || b.trim().starts_with('[') {
                serde_json::from_str(&b)
                    .ok()
                    .or_else(|| Some(serde_json::Value::String(b)))
            } else {
                Some(serde_json::Value::String(b))
            }
        }
        None => None,
    };

    // If we have a body but method is still GET, switch to POST
    if body_json.is_some() && method == "GET" {
        method = "POST".to_string();
    }

    Ok((method, url, headers, body_json))
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
        other => anyhow::bail!("Unsupported HTTP method: {}", other),
    }
}

/// Truncate a string for display.
fn truncate(s: &str, max_len: usize) -> String {
    if s.len() <= max_len {
        s.to_string()
    } else {
        format!("{}...", &s[..max_len])
    }
}

/// Base64 encode a string (simple implementation without external crate).
fn base64_encode(input: &str) -> String {
    const CHARS: &[u8] = b"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
    let bytes = input.as_bytes();
    let mut result = String::new();

    for chunk in bytes.chunks(3) {
        let b0 = chunk[0] as u32;
        let b1 = chunk.get(1).copied().unwrap_or(0) as u32;
        let b2 = chunk.get(2).copied().unwrap_or(0) as u32;
        let triple = (b0 << 16) | (b1 << 8) | b2;

        result.push(CHARS[((triple >> 18) & 0x3F) as usize] as char);
        result.push(CHARS[((triple >> 12) & 0x3F) as usize] as char);

        if chunk.len() > 1 {
            result.push(CHARS[((triple >> 6) & 0x3F) as usize] as char);
        } else {
            result.push('=');
        }

        if chunk.len() > 2 {
            result.push(CHARS[(triple & 0x3F) as usize] as char);
        } else {
            result.push('=');
        }
    }

    result
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parse_simple_get() {
        let plan = convert("curl https://api.example.com/health").unwrap();
        assert_eq!(plan.name, "cURL: curl https://api.example.com/health");
        assert_eq!(plan.steps.len(), 1);
        if let Step::Request(step) = &plan.steps[0] {
            assert_eq!(step.method, Method::Get);
            assert_eq!(step.url, "https://api.example.com/health");
        } else {
            panic!("Expected Request step");
        }
    }

    #[test]
    fn parse_post_with_data() {
        let plan = convert(
            r#"curl -X POST https://api.example.com/users -H "Content-Type: application/json" -d '{"name":"test"}'"#,
        )
        .unwrap();
        if let Step::Request(step) = &plan.steps[0] {
            assert_eq!(step.method, Method::Post);
            assert_eq!(step.url, "https://api.example.com/users");
            assert_eq!(
                step.headers.get("Content-Type").unwrap(),
                "application/json"
            );
            assert_eq!(
                step.body.as_ref().unwrap(),
                &serde_json::json!({"name": "test"})
            );
        } else {
            panic!("Expected Request step");
        }
    }

    #[test]
    fn parse_put_with_body() {
        let plan = convert(
            r#"curl -X PUT https://api.example.com/users/1 -d '{"name":"updated"}'"#,
        )
        .unwrap();
        if let Step::Request(step) = &plan.steps[0] {
            assert_eq!(step.method, Method::Put);
            assert_eq!(step.url, "https://api.example.com/users/1");
            assert!(step.body.is_some());
        } else {
            panic!("Expected Request step");
        }
    }

    #[test]
    fn parse_delete() {
        let plan = convert("curl -X DELETE https://api.example.com/users/1").unwrap();
        if let Step::Request(step) = &plan.steps[0] {
            assert_eq!(step.method, Method::Delete);
            assert_eq!(step.url, "https://api.example.com/users/1");
        } else {
            panic!("Expected Request step");
        }
    }

    #[test]
    fn parse_with_auth() {
        let plan = convert("curl -u admin:secret https://api.example.com/admin").unwrap();
        if let Step::Request(step) = &plan.steps[0] {
            let auth = step.headers.get("Authorization").unwrap();
            assert!(auth.starts_with("Basic "));
            assert!(auth.len() > 6);
        } else {
            panic!("Expected Request step");
        }
    }

    #[test]
    fn parse_with_cookie() {
        let plan = convert(r#"curl -b "session=abc123" https://api.example.com/profile"#).unwrap();
        if let Step::Request(step) = &plan.steps[0] {
            assert_eq!(step.headers.get("Cookie").unwrap(), "session=abc123");
        } else {
            panic!("Expected Request step");
        }
    }

    #[test]
    fn parse_data_implies_post() {
        let plan = convert(r#"curl -d '{"key":"value"}' https://api.example.com/data"#).unwrap();
        if let Step::Request(step) = &plan.steps[0] {
            assert_eq!(step.method, Method::Post);
            assert!(step.body.is_some());
        } else {
            panic!("Expected Request step");
        }
    }

    #[test]
    fn parse_with_headers() {
        let plan = convert(
            "curl -H 'Authorization: Bearer token123' -H 'X-Custom: value' https://api.example.com/data",
        )
        .unwrap();
        if let Step::Request(step) = &plan.steps[0] {
            assert_eq!(step.headers.get("Authorization").unwrap(), "Bearer token123");
            assert_eq!(step.headers.get("X-Custom").unwrap(), "value");
        } else {
            panic!("Expected Request step");
        }
    }

    #[test]
    fn parse_empty_command() {
        let result = convert("");
        assert!(result.is_err());
    }

    #[test]
    fn parse_no_url() {
        let result = convert("curl -X GET");
        assert!(result.is_err());
    }

    #[test]
    fn parse_with_short_flags() {
        let plan = convert("curl -s -S https://api.example.com/ping").unwrap();
        if let Step::Request(step) = &plan.steps[0] {
            assert_eq!(step.method, Method::Get);
            assert_eq!(step.url, "https://api.example.com/ping");
        } else {
            panic!("Expected Request step");
        }
    }

    #[test]
    fn parse_string_body() {
        let plan = convert(r#"curl -X POST https://api.example.com/echo -d "plain text body""#)
            .unwrap();
        if let Step::Request(step) = &plan.steps[0] {
            assert_eq!(step.method, Method::Post);
            assert_eq!(
                step.body.as_ref().unwrap(),
                &serde_json::Value::String("plain text body".to_string())
            );
        } else {
            panic!("Expected Request step");
        }
    }

    #[test]
    fn base64_encode_works() {
        assert_eq!(base64_encode("admin:secret"), "YWRtaW46c2VjcmV0");
        assert_eq!(base64_encode("user:pass"), "dXNlcjpwYXNz");
        assert_eq!(base64_encode("a"), "YQ==");
        assert_eq!(base64_encode("ab"), "YWI=");
        assert_eq!(base64_encode("abc"), "YWJj");
    }
}
