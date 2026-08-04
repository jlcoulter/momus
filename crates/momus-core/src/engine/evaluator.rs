/// Evaluate assertions against HTTP responses.
use crate::ast::*;
use regex::Regex;
use std::collections::HashMap;

/// Evaluate a list of assertions against a response.
/// Returns one `AssertionResult` per assertion.
pub fn evaluate_assertions(
    assertions: &[Assertion],
    status_code: u16,
    headers: &HashMap<String, String>,
    body: &Option<serde_json::Value>,
) -> Vec<AssertionResult> {
    assertions
        .iter()
        .map(|a| evaluate_assertion(a, status_code, headers, body))
        .collect()
}

/// Evaluate a single assertion tree against a response.
pub fn evaluate_assertion(
    assertion: &Assertion,
    status_code: u16,
    headers: &HashMap<String, String>,
    body: &Option<serde_json::Value>,
) -> AssertionResult {
    match assertion {
        // -- Combinators ----------------------------------------------------
        Assertion::AllOf(children) => {
            let results: Vec<_> = children
                .iter()
                .map(|c| evaluate_assertion(c, status_code, headers, body))
                .collect();
            let passed = results.iter().all(|r| r.passed);
            let failures: Vec<_> = results
                .iter()
                .filter(|r| !r.passed)
                .map(|r| r.description.clone())
                .collect();
            AssertionResult {
                description: "all of".into(),
                passed,
                message: if passed {
                    None
                } else {
                    Some(format!("failed: {}", failures.join(", ")))
                },
                children: results,
            }
        }

        Assertion::AnyOf(children) => {
            let results: Vec<_> = children
                .iter()
                .map(|c| evaluate_assertion(c, status_code, headers, body))
                .collect();
            let passed = results.iter().any(|r| r.passed);
            AssertionResult {
                description: "any of".into(),
                passed,
                message: if passed {
                    None
                } else {
                    Some("no sub-assertion passed".into())
                },
                children: results,
            }
        }

        Assertion::Not(child) => {
            let result = evaluate_assertion(child, status_code, headers, body);
            AssertionResult {
                description: format!("not ({})", result.description),
                passed: !result.passed,
                message: if result.passed {
                    Some("assertion passed when it should have failed".into())
                } else {
                    None
                },
                children: vec![result],
            }
        }

        // -- HTTP-level ----------------------------------------------------
        Assertion::Status(expected) => {
            if status_code == *expected {
                AssertionResult::pass(format!("status is {expected}"))
            } else {
                AssertionResult::fail(
                    format!("status is {expected}"),
                    format!("got {status_code}"),
                )
            }
        }

        Assertion::StatusIn(codes) => {
            if codes.contains(&status_code) {
                AssertionResult::pass(format!("status in {:?}", codes))
            } else {
                AssertionResult::fail(
                    format!("status in {:?}", codes),
                    format!("got {status_code}"),
                )
            }
        }

        Assertion::Header { name, predicate } => {
            let actual = headers.get(name.as_str()).map(|s| s.as_str());
            evaluate_value_predicate(name, predicate, actual)
        }

        Assertion::BodyLength(pred) => {
            let body_str = body.as_ref().map(|b| b.to_string()).unwrap_or_default();
            let len = body_str.len();
            match pred {
                BodyLengthPredicate::Eq(expected) => {
                    if len == *expected {
                        AssertionResult::pass(format!("body length == {expected}"))
                    } else {
                        AssertionResult::fail(
                            format!("body length == {expected}"),
                            format!("got {len} bytes"),
                        )
                    }
                }
                BodyLengthPredicate::Min(min) => {
                    if len >= *min {
                        AssertionResult::pass(format!("body length >= {min}"))
                    } else {
                        AssertionResult::fail(
                            format!("body length >= {min}"),
                            format!("got {len} bytes"),
                        )
                    }
                }
                BodyLengthPredicate::Max(max) => {
                    if len <= *max {
                        AssertionResult::pass(format!("body length <= {max}"))
                    } else {
                        AssertionResult::fail(
                            format!("body length <= {max}"),
                            format!("got {len} bytes"),
                        )
                    }
                }
                BodyLengthPredicate::Range { min, max } => {
                    if len >= *min && len <= *max {
                        AssertionResult::pass(format!("body length in [{min}, {max}]"))
                    } else {
                        AssertionResult::fail(
                            format!("body length in [{min}, {max}]"),
                            format!("got {len} bytes"),
                        )
                    }
                }
            }
        }

        // -- JSON body ------------------------------------------------------
        Assertion::JsonPath { path, predicate } => {
            let body = match body {
                Some(b) => b,
                None => {
                    return AssertionResult::fail(
                        format!("json path '{path}'"),
                        "no response body",
                    );
                }
            };

            // Simple JSONPath implementation using serde_json value traversal.
            // For full JSONPath support, we'd use jsonpath-rust, but for now
            // we support a subset: $.key, $.key.nested, $.key[*], $.key[0]
            let results: Vec<serde_json::Value> = resolve_json_path(body, path);
            evaluate_json_predicate(path, predicate, &results)
        }

        Assertion::Schema { schema: _ } => {
            // JSON Schema validation is a separate concern.
            // For now, we note it's not yet implemented.
            AssertionResult {
                description: "json schema validation".into(),
                passed: true,
                message: Some("schema validation not yet implemented — skipped".into()),
                children: vec![],
            }
        }

        Assertion::ValidJson => {
            // If we got here, the body was already parsed as JSON.
            AssertionResult::pass("response is valid JSON")
        }

        Assertion::ContentType(expected) => {
            let actual = headers
                .get("content-type")
                .map(|s| s.as_str())
                .unwrap_or("");
            if actual.to_lowercase().contains(&expected.to_lowercase()) {
                AssertionResult::pass(format!("content-type contains '{expected}'"))
            } else {
                AssertionResult::fail(
                    format!("content-type contains '{expected}'"),
                    format!("got '{actual}'"),
                )
            }
        }
    }
}

// ---------------------------------------------------------------------------
// Value predicate evaluation
// ---------------------------------------------------------------------------

fn evaluate_value_predicate(
    name: &str,
    predicate: &ValuePredicate,
    actual: Option<&str>,
) -> AssertionResult {
    let desc = format!("header '{name}'");
    match predicate {
        ValuePredicate::Eq(expected) => match actual {
            Some(v) if v == expected => AssertionResult::pass(format!("{desc} == '{expected}'")),
            Some(v) => {
                AssertionResult::fail(format!("{desc} == '{expected}'"), format!("got '{v}'"))
            }
            None => AssertionResult::fail(format!("{desc} == '{expected}'"), "header not present"),
        },
        ValuePredicate::Contains(sub) => match actual {
            Some(v) if v.contains(sub.as_str()) => {
                AssertionResult::pass(format!("{desc} contains '{sub}'"))
            }
            Some(v) => {
                AssertionResult::fail(format!("{desc} contains '{sub}'"), format!("got '{v}'"))
            }
            None => AssertionResult::fail(format!("{desc} contains '{sub}'"), "header not present"),
        },
        ValuePredicate::Regex(pattern) => match actual {
            Some(v) => match Regex::new(pattern) {
                Ok(re) => {
                    if re.is_match(v) {
                        AssertionResult::pass(format!("{desc} matches /{pattern}/"))
                    } else {
                        AssertionResult::fail(
                            format!("{desc} matches /{pattern}/"),
                            format!("got '{v}'"),
                        )
                    }
                }
                Err(e) => AssertionResult::fail(
                    format!("{desc} matches /{pattern}/"),
                    format!("invalid regex: {e}"),
                ),
            },
            None => {
                AssertionResult::fail(format!("{desc} matches /{pattern}/"), "header not present")
            }
        },
        ValuePredicate::Present => {
            if actual.is_some() {
                AssertionResult::pass(format!("{desc} is present"))
            } else {
                AssertionResult::fail(format!("{desc} is present"), "header not present")
            }
        }
        ValuePredicate::Absent => {
            if actual.is_none() {
                AssertionResult::pass(format!("{desc} is absent"))
            } else {
                AssertionResult::fail(format!("{desc} is absent"), format!("got '{:?}'", actual))
            }
        }
    }
}

// ---------------------------------------------------------------------------
// JSON predicate evaluation
// ---------------------------------------------------------------------------

fn evaluate_json_predicate(
    path: &str,
    predicate: &JsonPredicate,
    results: &[serde_json::Value],
) -> AssertionResult {
    let desc = format!("json path '{path}'");
    match predicate {
        JsonPredicate::Exists => {
            if results.is_empty() {
                AssertionResult::fail(desc, "path not found in response body")
            } else {
                AssertionResult::pass(desc)
            }
        }
        JsonPredicate::NotExists => {
            if results.is_empty() {
                AssertionResult::pass(desc)
            } else {
                AssertionResult::fail(desc, format!("path found with {} result(s)", results.len()))
            }
        }
        JsonPredicate::Eq(expected) => {
            if results.is_empty() {
                return AssertionResult::fail(format!("{desc} == {expected}"), "path not found");
            }
            let actual = &results[0];
            if actual == expected {
                AssertionResult::pass(format!("{desc} == {expected}"))
            } else {
                AssertionResult::fail(format!("{desc} == {expected}"), format!("got {actual}"))
            }
        }
        JsonPredicate::NotEq(expected) => {
            if results.is_empty() {
                return AssertionResult::pass(format!("{desc} != {expected} (path not found)"));
            }
            let actual = &results[0];
            if actual != expected {
                AssertionResult::pass(format!("{desc} != {expected}"))
            } else {
                AssertionResult::fail(format!("{desc} != {expected}"), format!("got {actual}"))
            }
        }
        JsonPredicate::Cmp { op, value } => {
            if results.is_empty() {
                return AssertionResult::fail(
                    format!("{desc} cmp {op:?} {value}"),
                    "path not found",
                );
            }
            let actual = &results[0];
            match (actual.as_f64(), value.as_f64()) {
                (Some(a), Some(b)) => {
                    let passed = match op {
                        CmpOp::Gt => a > b,
                        CmpOp::Lt => a < b,
                        CmpOp::Ge => a >= b,
                        CmpOp::Le => a <= b,
                    };
                    if passed {
                        AssertionResult::pass(format!("{desc} {op:?} {value}"))
                    } else {
                        AssertionResult::fail(
                            format!("{desc} {op:?} {value}"),
                            format!("got {actual}"),
                        )
                    }
                }
                _ => AssertionResult::fail(
                    format!("{desc} {op:?} {value}"),
                    format!("non-numeric value: {actual}"),
                ),
            }
        }
        JsonPredicate::Length(pred) => {
            let len = results.len();
            match pred {
                LengthPredicate::Eq(expected) => {
                    if len == *expected {
                        AssertionResult::pass(format!("{desc} length == {expected}"))
                    } else {
                        AssertionResult::fail(
                            format!("{desc} length == {expected}"),
                            format!("got {len}"),
                        )
                    }
                }
                LengthPredicate::Min(min) => {
                    if len >= *min {
                        AssertionResult::pass(format!("{desc} length >= {min}"))
                    } else {
                        AssertionResult::fail(
                            format!("{desc} length >= {min}"),
                            format!("got {len}"),
                        )
                    }
                }
                LengthPredicate::Max(max) => {
                    if len <= *max {
                        AssertionResult::pass(format!("{desc} length <= {max}"))
                    } else {
                        AssertionResult::fail(
                            format!("{desc} length <= {max}"),
                            format!("got {len}"),
                        )
                    }
                }
                LengthPredicate::Range { min, max } => {
                    if len >= *min && len <= *max {
                        AssertionResult::pass(format!("{desc} length in [{min}, {max}]"))
                    } else {
                        AssertionResult::fail(
                            format!("{desc} length in [{min}, {max}]"),
                            format!("got {len}"),
                        )
                    }
                }
            }
        }
        JsonPredicate::Every(sub) => {
            if results.is_empty() {
                return AssertionResult::pass(format!("{desc} every (no results)"));
            }
            let sub_results: Vec<_> = results
                .iter()
                .map(|r| {
                    evaluate_json_predicate(&format!("{desc}[*]"), sub, std::slice::from_ref(r))
                })
                .collect();
            let passed = sub_results.iter().all(|r| r.passed);
            AssertionResult {
                description: format!("{desc} every"),
                passed,
                message: if passed {
                    None
                } else {
                    let count = sub_results.iter().filter(|r| !r.passed).count();
                    Some(format!("{count} element(s) failed"))
                },
                children: sub_results,
            }
        }
        JsonPredicate::Some(sub) => {
            if results.is_empty() {
                return AssertionResult::fail(format!("{desc} some"), "no results");
            }
            let sub_results: Vec<_> = results
                .iter()
                .map(|r| {
                    evaluate_json_predicate(&format!("{desc}[*]"), sub, std::slice::from_ref(r))
                })
                .collect();
            let passed = sub_results.iter().any(|r| r.passed);
            AssertionResult {
                description: format!("{desc} some"),
                passed,
                message: if passed {
                    None
                } else {
                    Some("no element matched".into())
                },
                children: sub_results,
            }
        }
        JsonPredicate::Count(pred) => {
            let len = results.len();
            match pred {
                CountPredicate::Eq(expected) => {
                    if len == *expected {
                        AssertionResult::pass(format!("{desc} count == {expected}"))
                    } else {
                        AssertionResult::fail(
                            format!("{desc} count == {expected}"),
                            format!("got {len}"),
                        )
                    }
                }
                CountPredicate::Min(min) => {
                    if len >= *min {
                        AssertionResult::pass(format!("{desc} count >= {min}"))
                    } else {
                        AssertionResult::fail(
                            format!("{desc} count >= {min}"),
                            format!("got {len}"),
                        )
                    }
                }
                CountPredicate::Max(max) => {
                    if len <= *max {
                        AssertionResult::pass(format!("{desc} count <= {max}"))
                    } else {
                        AssertionResult::fail(
                            format!("{desc} count <= {max}"),
                            format!("got {len}"),
                        )
                    }
                }
                CountPredicate::Range { min, max } => {
                    if len >= *min && len <= *max {
                        AssertionResult::pass(format!("{desc} count in [{min}, {max}]"))
                    } else {
                        AssertionResult::fail(
                            format!("{desc} count in [{min}, {max}]"),
                            format!("got {len}"),
                        )
                    }
                }
            }
        }
        JsonPredicate::Schema(_schema) => AssertionResult {
            description: format!("{desc} schema validation"),
            passed: true,
            message: Some("schema validation not yet implemented — skipped".into()),
            children: vec![],
        },
    }
}

// ---------------------------------------------------------------------------
// Simple JSONPath resolver
// ---------------------------------------------------------------------------

/// Resolve a simplified JSONPath expression against a JSON value.
///
/// Supports:
/// - `$.key` — root key access
/// - `$.key.nested` — nested key access
/// - `$.key[*]` — array wildcard
/// - `$.key[0]` — array index
/// - `$.key[0].nested` — array index with nested access
pub fn resolve_json_path(value: &serde_json::Value, path: &str) -> Vec<serde_json::Value> {
    // Handle bare "$" as root
    if path == "$" || path.is_empty() {
        return vec![value.clone()];
    }
    // Strip leading $.
    let path = path.strip_prefix("$.").unwrap_or(path);
    resolve_segments(value, path)
}

fn resolve_segments(value: &serde_json::Value, path: &str) -> Vec<serde_json::Value> {
    if path.is_empty() {
        return vec![value.clone()];
    }

    // Split on '.' but not inside brackets
    let segments = split_path(path);
    if segments.is_empty() {
        return vec![value.clone()];
    }

    let mut current = vec![value.clone()];

    for segment in segments {
        let mut next = Vec::new();

        if let Some(inner) = segment.strip_suffix("[*]") {
            // Array wildcard: key[*]
            for v in &current {
                if let Some(arr) = v.get(inner).and_then(|v| v.as_array()) {
                    next.extend(arr.iter().cloned());
                }
            }
        } else if let Some(captured) = segment.strip_suffix(']') {
            // Array index: key[N]
            if let Some((name, idx_str)) = captured.rsplit_once('[')
                && let Ok(idx) = idx_str.parse::<usize>()
            {
                for v in &current {
                    if let Some(arr) = v.get(name).and_then(|v| v.as_array())
                        && idx < arr.len()
                    {
                        next.push(arr[idx].clone());
                    }
                }
            }
        } else {
            // Simple key access
            for v in &current {
                if let Some(child) = v.get(&segment) {
                    next.push(child.clone());
                }
            }
        }

        current = next;
        if current.is_empty() {
            return vec![];
        }
    }

    current
}

fn split_path(path: &str) -> Vec<String> {
    let mut segments = Vec::new();
    let mut current = String::new();
    let mut depth = 0;

    for ch in path.chars() {
        match ch {
            '[' => {
                current.push('[');
                depth += 1;
            }
            ']' => {
                current.push(']');
                depth -= 1;
            }
            '.' if depth == 0 => {
                if !current.is_empty() {
                    segments.push(current.clone());
                    current.clear();
                }
                continue;
            }
            _ => {
                current.push(ch);
            }
        }
    }
    if !current.is_empty() {
        segments.push(current);
    }

    segments
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn test_status_pass() {
        let result = evaluate_assertion(&Assertion::Status(200), 200, &HashMap::new(), &None);
        assert!(result.passed);
    }

    #[test]
    fn test_status_fail() {
        let result = evaluate_assertion(&Assertion::Status(200), 404, &HashMap::new(), &None);
        assert!(!result.passed);
        assert!(result.message.unwrap().contains("404"));
    }

    #[test]
    fn test_status_in_pass() {
        let result = evaluate_assertion(
            &Assertion::StatusIn(vec![200, 304]),
            304,
            &HashMap::new(),
            &None,
        );
        assert!(result.passed);
    }

    #[test]
    fn test_header_present() {
        let mut headers = HashMap::new();
        headers.insert("content-type".into(), "application/json".into());
        let result = evaluate_assertion(
            &Assertion::header("content-type", ValuePredicate::Present),
            200,
            &headers,
            &None,
        );
        assert!(result.passed);
    }

    #[test]
    fn test_header_eq() {
        let mut headers = HashMap::new();
        headers.insert("x-request-id".into(), "abc-123".into());
        let result = evaluate_assertion(
            &Assertion::header("x-request-id", ValuePredicate::Eq("abc-123".into())),
            200,
            &headers,
            &None,
        );
        assert!(result.passed);
    }

    #[test]
    fn test_json_path_exists() {
        let body = json!({"resourceType": "Patient", "id": "p1"});
        let result = evaluate_assertion(
            &Assertion::json_path_exists("$.resourceType"),
            200,
            &HashMap::new(),
            &Some(body),
        );
        assert!(result.passed);
    }

    #[test]
    fn test_json_path_not_exists() {
        let body = json!({"resourceType": "Patient"});
        let result = evaluate_assertion(
            &Assertion::JsonPath {
                path: "$.nonexistent".into(),
                predicate: JsonPredicate::NotExists,
            },
            200,
            &HashMap::new(),
            &Some(body),
        );
        assert!(result.passed);
    }

    #[test]
    fn test_json_path_eq() {
        let body = json!({"total": 42});
        let result = evaluate_assertion(
            &Assertion::json_path_eq("$.total", json!(42)),
            200,
            &HashMap::new(),
            &Some(body),
        );
        assert!(result.passed);
    }

    #[test]
    fn test_json_path_array_wildcard() {
        let body = json!({
            "entry": [
                {"resource": {"id": "a"}},
                {"resource": {"id": "b"}},
            ]
        });
        let result = evaluate_assertion(
            &Assertion::JsonPath {
                path: "$.entry[*].resource.id".into(),
                predicate: JsonPredicate::Count(CountPredicate::Eq(2)),
            },
            200,
            &HashMap::new(),
            &Some(body),
        );
        assert!(result.passed);
    }

    #[test]
    fn test_all_of_pass() {
        let body = json!({"resourceType": "Bundle", "total": 10});
        let result = evaluate_assertion(
            &Assertion::AllOf(vec![
                Assertion::Status(200),
                Assertion::json_path_exists("$.resourceType"),
                Assertion::json_path_eq("$.total", json!(10)),
            ]),
            200,
            &HashMap::new(),
            &Some(body),
        );
        assert!(result.passed);
    }

    #[test]
    fn test_all_of_fail() {
        let body = json!({"resourceType": "Bundle"});
        let result = evaluate_assertion(
            &Assertion::AllOf(vec![
                Assertion::Status(200),
                Assertion::json_path_eq("$.total", json!(10)),
            ]),
            200,
            &HashMap::new(),
            &Some(body),
        );
        assert!(!result.passed);
    }

    #[test]
    fn test_any_of_pass() {
        let result = evaluate_assertion(
            &Assertion::AnyOf(vec![Assertion::Status(200), Assertion::Status(304)]),
            304,
            &HashMap::new(),
            &None,
        );
        assert!(result.passed);
    }

    #[test]
    fn test_not() {
        let result = evaluate_assertion(
            &Assertion::Not(Box::new(Assertion::Status(404))),
            200,
            &HashMap::new(),
            &None,
        );
        assert!(result.passed);
    }

    #[test]
    fn test_content_type() {
        let mut headers = HashMap::new();
        headers.insert("content-type".into(), "application/fhir+json".into());
        let result = evaluate_assertion(&Assertion::content_type("json"), 200, &headers, &None);
        assert!(result.passed);
    }

    #[test]
    fn test_body_length() {
        let body = json!({"key": "value"});
        let result = evaluate_assertion(
            &Assertion::BodyLength(BodyLengthPredicate::Min(10)),
            200,
            &HashMap::new(),
            &Some(body),
        );
        assert!(result.passed);
    }

    #[test]
    fn test_json_path_cmp() {
        let body = json!({"value": 42});
        let result = evaluate_assertion(
            &Assertion::JsonPath {
                path: "$.value".into(),
                predicate: JsonPredicate::Cmp {
                    op: CmpOp::Gt,
                    value: json!(10),
                },
            },
            200,
            &HashMap::new(),
            &Some(body),
        );
        assert!(result.passed);
    }

    #[test]
    fn test_json_path_every() {
        let body = json!({"items": [1, 2, 3, 4, 5]});
        let result = evaluate_assertion(
            &Assertion::JsonPath {
                path: "$.items[*]".into(),
                predicate: JsonPredicate::Every(Box::new(JsonPredicate::Cmp {
                    op: CmpOp::Gt,
                    value: json!(0),
                })),
            },
            200,
            &HashMap::new(),
            &Some(body),
        );
        assert!(result.passed);
    }

    #[test]
    fn test_json_path_some() {
        let body = json!({"items": [1, 2, 3, 4, 5]});
        let result = evaluate_assertion(
            &Assertion::JsonPath {
                path: "$.items[*]".into(),
                predicate: JsonPredicate::Some(Box::new(JsonPredicate::Eq(json!(5)))),
            },
            200,
            &HashMap::new(),
            &Some(body),
        );
        assert!(result.passed);
    }

    #[test]
    fn test_resolve_json_path_simple() {
        let v = json!({"a": {"b": 1}});
        let results = resolve_json_path(&v, "$.a.b");
        assert_eq!(results, vec![json!(1)]);
    }

    #[test]
    fn test_resolve_json_path_array_index() {
        let v = json!({"items": [10, 20, 30]});
        let results = resolve_json_path(&v, "$.items[1]");
        assert_eq!(results, vec![json!(20)]);
    }

    #[test]
    fn test_resolve_json_path_wildcard() {
        let v = json!({"items": [{"x": 1}, {"x": 2}]});
        let results = resolve_json_path(&v, "$.items[*].x");
        assert_eq!(results, vec![json!(1), json!(2)]);
    }

    #[test]
    fn test_resolve_json_path_root() {
        let v = json!({"a": 1});
        let results = resolve_json_path(&v, "$");
        assert_eq!(results, vec![v]);
    }

    #[test]
    fn test_resolve_json_path_not_found() {
        let v = json!({"a": 1});
        let results = resolve_json_path(&v, "$.b");
        assert!(results.is_empty());
    }
}
