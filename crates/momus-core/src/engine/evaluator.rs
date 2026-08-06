/// Evaluate assertions against HTTP responses.
use crate::ast::*;
use regex::Regex;
use std::collections::HashMap;

/// Compile a JSON Schema into a validator, caching the result.
fn compile_schema(schema: &serde_json::Value) -> Result<jsonschema::Validator, String> {
    jsonschema::validator_for(schema).map_err(|e| format!("invalid JSON Schema: {e}"))
}

/// Validate a JSON value against a compiled schema, returning errors.
fn validate_schema(
    validator: &jsonschema::Validator,
    instance: &serde_json::Value,
) -> Result<(), Vec<String>> {
    let errors: Vec<String> = validator
        .iter_errors(instance)
        .map(|e| format!("  - {}: {}", e.instance_path(), e))
        .collect();
    if errors.is_empty() {
        Ok(())
    } else {
        Err(errors)
    }
}

/// Evaluate a list of assertions against a response.
/// Returns one `AssertionResult` per assertion.
pub fn evaluate_assertions(
    assertions: &[Assertion],
    status_code: u16,
    headers: &HashMap<String, String>,
    body: &Option<serde_json::Value>,
    response_time_ms: u64,
) -> Vec<AssertionResult> {
    assertions
        .iter()
        .map(|a| evaluate_assertion(a, status_code, headers, body, response_time_ms))
        .collect()
}

/// Evaluate a single assertion tree against a response.
pub fn evaluate_assertion(
    assertion: &Assertion,
    status_code: u16,
    headers: &HashMap<String, String>,
    body: &Option<serde_json::Value>,
    response_time_ms: u64,
) -> AssertionResult {
    match assertion {
        // -- Combinators ----------------------------------------------------
        Assertion::AllOf(children) => {
            let results: Vec<_> = children
                .iter()
                .map(|c| evaluate_assertion(c, status_code, headers, body, response_time_ms))
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
                .map(|c| evaluate_assertion(c, status_code, headers, body, response_time_ms))
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
            let result = evaluate_assertion(child, status_code, headers, body, response_time_ms);
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
                AssertionResult::pass(format!("status in {codes:?}"))
            } else {
                AssertionResult::fail(format!("status in {codes:?}"), format!("got {status_code}"))
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

        Assertion::Schema { schema } => match compile_schema(schema) {
            Ok(validator) => {
                let body = match body {
                    Some(b) => b,
                    None => {
                        return AssertionResult::fail("json schema validation", "no response body");
                    }
                };
                match validate_schema(&validator, body) {
                    Ok(()) => AssertionResult::pass("json schema validation"),
                    Err(errors) => AssertionResult::fail(
                        "json schema validation",
                        format!("schema violations:\n{}", errors.join("\n")),
                    ),
                }
            }
            Err(e) => AssertionResult::fail("json schema validation", e),
        },

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

        // -- Performance ----------------------------------------------------
        Assertion::ResponseTime(max_millis) => {
            if response_time_ms <= *max_millis {
                AssertionResult::pass(format!(
                    "response time <= {max_millis}ms (was {response_time_ms}ms)"
                ))
            } else {
                AssertionResult::fail(
                    format!("response time <= {max_millis}ms"),
                    format!("took {response_time_ms}ms"),
                )
            }
        }
    }
}

// ---------------------------------------------------------------------------
// Value predicate evaluation
// ---------------------------------------------------------------------------

#[inline]
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
                AssertionResult::fail(format!("{desc} is absent"), format!("got '{actual:?}'"))
            }
        }
    }
}

// ---------------------------------------------------------------------------
// JSON predicate evaluation
// ---------------------------------------------------------------------------

#[inline]
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
        JsonPredicate::Schema(schema) => match compile_schema(schema) {
            Ok(validator) => {
                if results.is_empty() {
                    return AssertionResult::fail(
                        format!("{desc} schema validation"),
                        "path not found",
                    );
                }
                match validate_schema(&validator, &results[0]) {
                    Ok(()) => AssertionResult::pass(format!("{desc} schema validation")),
                    Err(errors) => AssertionResult::fail(
                        format!("{desc} schema validation"),
                        format!("schema violations:\n{}", errors.join("\n")),
                    ),
                }
            }
            Err(e) => AssertionResult::fail(format!("{desc} schema validation"), e),
        },
    }
}

// ---------------------------------------------------------------------------
// JSONPath resolver (via jsonpath-rust)
// ---------------------------------------------------------------------------

/// Resolve a JSONPath expression against a JSON value using `jsonpath-rust`.
///
/// Supports the full JSONPath syntax including:
/// - `$.key` — root key access
/// - `$.key.nested` — nested key access
/// - `$.key[*]` — array wildcard
/// - `$.key[0]` — array index
/// - `$..key` — recursive descent
/// - `[?(@.key==val)]` — filter expressions
/// - `[0,1]` — union indices
/// - `[start:end:step]` — slice operator
pub fn resolve_json_path(value: &serde_json::Value, path: &str) -> Vec<serde_json::Value> {
    use jsonpath_rust::JsonPath;

    match value.query(path) {
        Ok(results) => results.into_iter().cloned().collect(),
        Err(_) => vec![],
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn test_status_pass() {
        let result = evaluate_assertion(&Assertion::Status(200), 200, &HashMap::new(), &None, 0);
        assert!(result.passed);
    }

    #[test]
    fn test_status_fail() {
        let result = evaluate_assertion(&Assertion::Status(200), 404, &HashMap::new(), &None, 0);
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
            0,
        );
        assert!(result.passed);
    }

    #[test]
    fn test_status_in_fail() {
        let result = evaluate_assertion(
            &Assertion::StatusIn(vec![200, 304]),
            500,
            &HashMap::new(),
            &None,
            0,
        );
        assert!(!result.passed);
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
            0,
        );
        assert!(result.passed);
    }

    #[test]
    fn test_header_present_fail() {
        let result = evaluate_assertion(
            &Assertion::header("x-missing", ValuePredicate::Present),
            200,
            &HashMap::new(),
            &None,
            0,
        );
        assert!(!result.passed);
        assert!(result.message.unwrap().contains("not present"));
    }

    #[test]
    fn test_header_absent() {
        let result = evaluate_assertion(
            &Assertion::header("x-missing", ValuePredicate::Absent),
            200,
            &HashMap::new(),
            &None,
            0,
        );
        assert!(result.passed);
    }

    #[test]
    fn test_header_absent_fail() {
        let mut headers = HashMap::new();
        headers.insert("x-present".into(), "value".into());
        let result = evaluate_assertion(
            &Assertion::header("x-present", ValuePredicate::Absent),
            200,
            &headers,
            &None,
            0,
        );
        assert!(!result.passed);
    }

    #[test]
    fn test_header_contains() {
        let mut headers = HashMap::new();
        headers.insert("content-type".into(), "application/fhir+json".into());
        let result = evaluate_assertion(
            &Assertion::header("content-type", ValuePredicate::Contains("fhir".into())),
            200,
            &headers,
            &None,
            0,
        );
        assert!(result.passed);
    }

    #[test]
    fn test_header_contains_fail() {
        let mut headers = HashMap::new();
        headers.insert("content-type".into(), "application/xml".into());
        let result = evaluate_assertion(
            &Assertion::header("content-type", ValuePredicate::Contains("json".into())),
            200,
            &headers,
            &None,
            0,
        );
        assert!(!result.passed);
    }

    #[test]
    fn test_header_contains_missing_header() {
        let result = evaluate_assertion(
            &Assertion::header("x-missing", ValuePredicate::Contains("val".into())),
            200,
            &HashMap::new(),
            &None,
            0,
        );
        assert!(!result.passed);
    }

    #[test]
    fn test_header_regex() {
        let mut headers = HashMap::new();
        headers.insert("x-request-id".into(), "req-abc-123".into());
        let result = evaluate_assertion(
            &Assertion::header("x-request-id", ValuePredicate::Regex("^req-".into())),
            200,
            &headers,
            &None,
            0,
        );
        assert!(result.passed);
    }

    #[test]
    fn test_header_regex_fail() {
        let mut headers = HashMap::new();
        headers.insert("x-request-id".into(), "abc-123".into());
        let result = evaluate_assertion(
            &Assertion::header("x-request-id", ValuePredicate::Regex("^req-".into())),
            200,
            &headers,
            &None,
            0,
        );
        assert!(!result.passed);
    }

    #[test]
    fn test_header_regex_invalid() {
        let mut headers = HashMap::new();
        headers.insert("x-id".into(), "value".into());
        let result = evaluate_assertion(
            &Assertion::header("x-id", ValuePredicate::Regex("[".into())),
            200,
            &headers,
            &None,
            0,
        );
        assert!(!result.passed);
        assert!(result.message.unwrap().contains("invalid regex"));
    }

    #[test]
    fn test_header_regex_missing_header() {
        let result = evaluate_assertion(
            &Assertion::header("x-missing", ValuePredicate::Regex("^val".into())),
            200,
            &HashMap::new(),
            &None,
            0,
        );
        assert!(!result.passed);
    }

    #[test]
    fn test_header_eq_fail_wrong_value() {
        let mut headers = HashMap::new();
        headers.insert("x-request-id".into(), "abc-123".into());
        let result = evaluate_assertion(
            &Assertion::header("x-request-id", ValuePredicate::Eq("wrong".into())),
            200,
            &headers,
            &None,
            0,
        );
        assert!(!result.passed);
    }

    #[test]
    fn test_header_eq_fail_missing_header() {
        let result = evaluate_assertion(
            &Assertion::header("x-missing", ValuePredicate::Eq("value".into())),
            200,
            &HashMap::new(),
            &None,
            0,
        );
        assert!(!result.passed);
        assert!(result.message.unwrap().contains("not present"));
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
            0,
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
            0,
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
            0,
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
            0,
        );
        assert!(result.passed);
    }

    #[test]
    fn test_json_path_eq_fail() {
        let body = json!({"total": 42});
        let result = evaluate_assertion(
            &Assertion::json_path_eq("$.total", json!(99)),
            200,
            &HashMap::new(),
            &Some(body),
            0,
        );
        assert!(!result.passed);
    }

    #[test]
    fn test_json_path_eq_no_body() {
        let result = evaluate_assertion(
            &Assertion::json_path_eq("$.total", json!(42)),
            200,
            &HashMap::new(),
            &None,
            0,
        );
        assert!(!result.passed);
    }

    #[test]
    fn test_json_path_not_eq() {
        let body = json!({"total": 42});
        let result = evaluate_assertion(
            &Assertion::JsonPath {
                path: "$.total".into(),
                predicate: JsonPredicate::NotEq(json!(99)),
            },
            200,
            &HashMap::new(),
            &Some(body),
            0,
        );
        assert!(result.passed);
    }

    #[test]
    fn test_json_path_not_eq_fail() {
        let body = json!({"total": 42});
        let result = evaluate_assertion(
            &Assertion::JsonPath {
                path: "$.total".into(),
                predicate: JsonPredicate::NotEq(json!(42)),
            },
            200,
            &HashMap::new(),
            &Some(body),
            0,
        );
        assert!(!result.passed);
    }

    #[test]
    fn test_json_path_not_eq_path_not_found() {
        let body = json!({"a": 1});
        let result = evaluate_assertion(
            &Assertion::JsonPath {
                path: "$.missing".into(),
                predicate: JsonPredicate::NotEq(json!(42)),
            },
            200,
            &HashMap::new(),
            &Some(body),
            0,
        );
        assert!(result.passed);
    }

    #[test]
    fn test_json_path_cmp_lt() {
        let body = json!({"value": 5});
        let result = evaluate_assertion(
            &Assertion::JsonPath {
                path: "$.value".into(),
                predicate: JsonPredicate::Cmp {
                    op: CmpOp::Lt,
                    value: json!(10),
                },
            },
            200,
            &HashMap::new(),
            &Some(body),
            0,
        );
        assert!(result.passed);
    }

    #[test]
    fn test_json_path_cmp_ge() {
        let body = json!({"value": 10});
        let result = evaluate_assertion(
            &Assertion::JsonPath {
                path: "$.value".into(),
                predicate: JsonPredicate::Cmp {
                    op: CmpOp::Ge,
                    value: json!(10),
                },
            },
            200,
            &HashMap::new(),
            &Some(body),
            0,
        );
        assert!(result.passed);
    }

    #[test]
    fn test_json_path_cmp_le() {
        let body = json!({"value": 5});
        let result = evaluate_assertion(
            &Assertion::JsonPath {
                path: "$.value".into(),
                predicate: JsonPredicate::Cmp {
                    op: CmpOp::Le,
                    value: json!(10),
                },
            },
            200,
            &HashMap::new(),
            &Some(body),
            0,
        );
        assert!(result.passed);
    }

    #[test]
    fn test_json_path_cmp_fail() {
        let body = json!({"value": 20});
        let result = evaluate_assertion(
            &Assertion::JsonPath {
                path: "$.value".into(),
                predicate: JsonPredicate::Cmp {
                    op: CmpOp::Lt,
                    value: json!(10),
                },
            },
            200,
            &HashMap::new(),
            &Some(body),
            0,
        );
        assert!(!result.passed);
    }

    #[test]
    fn test_json_path_cmp_non_numeric() {
        let body = json!({"value": "not a number"});
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
            0,
        );
        assert!(!result.passed);
    }

    #[test]
    fn test_json_path_cmp_path_not_found() {
        let body = json!({"a": 1});
        let result = evaluate_assertion(
            &Assertion::JsonPath {
                path: "$.missing".into(),
                predicate: JsonPredicate::Cmp {
                    op: CmpOp::Gt,
                    value: json!(10),
                },
            },
            200,
            &HashMap::new(),
            &Some(body),
            0,
        );
        assert!(!result.passed);
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
            0,
        );
        assert!(result.passed);
    }

    #[test]
    fn test_json_path_length_eq() {
        let body = json!([1, 2, 3]);
        let result = evaluate_assertion(
            &Assertion::JsonPath {
                path: "$[*]".into(),
                predicate: JsonPredicate::Length(LengthPredicate::Eq(3)),
            },
            200,
            &HashMap::new(),
            &Some(body),
            0,
        );
        assert!(result.passed);
    }

    #[test]
    fn test_json_path_length_min() {
        let body = json!([1, 2, 3]);
        let result = evaluate_assertion(
            &Assertion::JsonPath {
                path: "$[*]".into(),
                predicate: JsonPredicate::Length(LengthPredicate::Min(2)),
            },
            200,
            &HashMap::new(),
            &Some(body),
            0,
        );
        assert!(result.passed);
    }

    #[test]
    fn test_json_path_length_max() {
        let body = json!([1, 2, 3]);
        let result = evaluate_assertion(
            &Assertion::JsonPath {
                path: "$[*]".into(),
                predicate: JsonPredicate::Length(LengthPredicate::Max(5)),
            },
            200,
            &HashMap::new(),
            &Some(body),
            0,
        );
        assert!(result.passed);
    }

    #[test]
    fn test_json_path_length_range() {
        let body = json!([1, 2, 3]);
        let result = evaluate_assertion(
            &Assertion::JsonPath {
                path: "$[*]".into(),
                predicate: JsonPredicate::Length(LengthPredicate::Range { min: 2, max: 5 }),
            },
            200,
            &HashMap::new(),
            &Some(body),
            0,
        );
        assert!(result.passed);
    }

    #[test]
    fn test_json_path_length_fail() {
        let body = json!([1, 2, 3]);
        let result = evaluate_assertion(
            &Assertion::JsonPath {
                path: "$[*]".into(),
                predicate: JsonPredicate::Length(LengthPredicate::Eq(5)),
            },
            200,
            &HashMap::new(),
            &Some(body),
            0,
        );
        assert!(!result.passed);
    }

    #[test]
    fn test_json_path_count_min() {
        let body = json!({"items": [1, 2, 3]});
        let result = evaluate_assertion(
            &Assertion::JsonPath {
                path: "$.items[*]".into(),
                predicate: JsonPredicate::Count(CountPredicate::Min(2)),
            },
            200,
            &HashMap::new(),
            &Some(body),
            0,
        );
        assert!(result.passed);
    }

    #[test]
    fn test_json_path_count_max() {
        let body = json!({"items": [1, 2, 3]});
        let result = evaluate_assertion(
            &Assertion::JsonPath {
                path: "$.items[*]".into(),
                predicate: JsonPredicate::Count(CountPredicate::Max(5)),
            },
            200,
            &HashMap::new(),
            &Some(body),
            0,
        );
        assert!(result.passed);
    }

    #[test]
    fn test_json_path_count_range() {
        let body = json!({"items": [1, 2, 3]});
        let result = evaluate_assertion(
            &Assertion::JsonPath {
                path: "$.items[*]".into(),
                predicate: JsonPredicate::Count(CountPredicate::Range { min: 2, max: 5 }),
            },
            200,
            &HashMap::new(),
            &Some(body),
            0,
        );
        assert!(result.passed);
    }

    #[test]
    fn test_json_path_count_fail() {
        let body = json!({"items": [1, 2, 3]});
        let result = evaluate_assertion(
            &Assertion::JsonPath {
                path: "$.items[*]".into(),
                predicate: JsonPredicate::Count(CountPredicate::Eq(5)),
            },
            200,
            &HashMap::new(),
            &Some(body),
            0,
        );
        assert!(!result.passed);
    }

    #[test]
    fn test_json_path_schema_pass() {
        let schema = json!({"type": "object", "properties": {"name": {"type": "string"}}, "required": ["name"]});
        let body = json!({"data": {"name": "test"}});
        let result = evaluate_assertion(
            &Assertion::JsonPath {
                path: "$.data".into(),
                predicate: JsonPredicate::Schema(schema),
            },
            200,
            &HashMap::new(),
            &Some(body),
            0,
        );
        assert!(result.passed);
    }

    #[test]
    fn test_json_path_schema_fail() {
        let schema = json!({"type": "object", "properties": {"name": {"type": "string"}}, "required": ["name"]});
        let body = json!({"data": {"age": 42}});
        let result = evaluate_assertion(
            &Assertion::JsonPath {
                path: "$.data".into(),
                predicate: JsonPredicate::Schema(schema),
            },
            200,
            &HashMap::new(),
            &Some(body),
            0,
        );
        assert!(!result.passed);
    }

    #[test]
    fn test_json_path_schema_path_not_found() {
        let schema = json!({"type": "object"});
        let body = json!({"a": 1});
        let result = evaluate_assertion(
            &Assertion::JsonPath {
                path: "$.missing".into(),
                predicate: JsonPredicate::Schema(schema),
            },
            200,
            &HashMap::new(),
            &Some(body),
            0,
        );
        assert!(!result.passed);
    }

    #[test]
    fn test_json_path_every_empty_results() {
        let body = json!({"items": []});
        let result = evaluate_assertion(
            &Assertion::JsonPath {
                path: "$.items[*]".into(),
                predicate: JsonPredicate::Every(Box::new(JsonPredicate::Eq(json!(1)))),
            },
            200,
            &HashMap::new(),
            &Some(body),
            0,
        );
        assert!(result.passed);
    }

    #[test]
    fn test_json_path_every_fail() {
        let body = json!({"items": [1, 2, -1]});
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
            0,
        );
        assert!(!result.passed);
    }

    #[test]
    fn test_json_path_some_empty_results() {
        let body = json!({"items": []});
        let result = evaluate_assertion(
            &Assertion::JsonPath {
                path: "$.items[*]".into(),
                predicate: JsonPredicate::Some(Box::new(JsonPredicate::Eq(json!(1)))),
            },
            200,
            &HashMap::new(),
            &Some(body),
            0,
        );
        assert!(!result.passed);
    }

    #[test]
    fn test_json_path_some_fail() {
        let body = json!({"items": [1, 2, 3]});
        let result = evaluate_assertion(
            &Assertion::JsonPath {
                path: "$.items[*]".into(),
                predicate: JsonPredicate::Some(Box::new(JsonPredicate::Eq(json!(99)))),
            },
            200,
            &HashMap::new(),
            &Some(body),
            0,
        );
        assert!(!result.passed);
    }

    #[test]
    fn test_json_path_exists_fail() {
        let body = json!({"a": 1});
        let result = evaluate_assertion(
            &Assertion::json_path_exists("$.missing"),
            200,
            &HashMap::new(),
            &Some(body),
            0,
        );
        assert!(!result.passed);
    }

    #[test]
    fn test_json_path_not_exists_fail() {
        let body = json!({"a": 1});
        let result = evaluate_assertion(
            &Assertion::JsonPath {
                path: "$.a".into(),
                predicate: JsonPredicate::NotExists,
            },
            200,
            &HashMap::new(),
            &Some(body),
            0,
        );
        assert!(!result.passed);
    }

    #[test]
    fn test_json_path_no_body() {
        let result = evaluate_assertion(
            &Assertion::json_path_exists("$.a"),
            200,
            &HashMap::new(),
            &None,
            0,
        );
        assert!(!result.passed);
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
            0,
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
            0,
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
            0,
        );
        assert!(result.passed);
    }

    #[test]
    fn test_any_of_fail() {
        let result = evaluate_assertion(
            &Assertion::AnyOf(vec![Assertion::Status(200), Assertion::Status(304)]),
            500,
            &HashMap::new(),
            &None,
            0,
        );
        assert!(!result.passed);
    }

    #[test]
    fn test_any_of_empty() {
        let result = evaluate_assertion(&Assertion::AnyOf(vec![]), 200, &HashMap::new(), &None, 0);
        assert!(!result.passed);
    }

    #[test]
    fn test_not() {
        let result = evaluate_assertion(
            &Assertion::Not(Box::new(Assertion::Status(404))),
            200,
            &HashMap::new(),
            &None,
            0,
        );
        assert!(result.passed);
    }

    #[test]
    fn test_not_fail() {
        let result = evaluate_assertion(
            &Assertion::Not(Box::new(Assertion::Status(200))),
            200,
            &HashMap::new(),
            &None,
            0,
        );
        assert!(!result.passed);
    }

    #[test]
    fn test_all_of_empty() {
        let result = evaluate_assertion(&Assertion::AllOf(vec![]), 200, &HashMap::new(), &None, 0);
        assert!(result.passed);
    }

    #[test]
    fn test_content_type() {
        let mut headers = HashMap::new();
        headers.insert("content-type".into(), "application/fhir+json".into());
        let result = evaluate_assertion(&Assertion::content_type("json"), 200, &headers, &None, 0);
        assert!(result.passed);
    }

    #[test]
    fn test_content_type_fail() {
        let mut headers = HashMap::new();
        headers.insert("content-type".into(), "application/xml".into());
        let result = evaluate_assertion(&Assertion::content_type("json"), 200, &headers, &None, 0);
        assert!(!result.passed);
    }

    #[test]
    fn test_valid_json() {
        let body = json!({"key": "value"});
        let result =
            evaluate_assertion(&Assertion::ValidJson, 200, &HashMap::new(), &Some(body), 0);
        assert!(result.passed);
    }

    #[test]
    fn test_schema_validation_pass() {
        let schema = json!({"type": "object", "properties": {"name": {"type": "string"}}, "required": ["name"]});
        let body = json!({"name": "test"});
        let result = evaluate_assertion(
            &Assertion::Schema { schema },
            200,
            &HashMap::new(),
            &Some(body),
            0,
        );
        assert!(result.passed);
    }

    #[test]
    fn test_schema_validation_fail() {
        let schema = json!({"type": "object", "properties": {"name": {"type": "string"}}, "required": ["name"]});
        let body = json!({"age": 42});
        let result = evaluate_assertion(
            &Assertion::Schema { schema },
            200,
            &HashMap::new(),
            &Some(body),
            0,
        );
        assert!(!result.passed);
    }

    #[test]
    fn test_schema_validation_no_body() {
        let schema = json!({"type": "object"});
        let result = evaluate_assertion(
            &Assertion::Schema { schema },
            200,
            &HashMap::new(),
            &None,
            0,
        );
        assert!(!result.passed);
    }

    #[test]
    fn test_schema_validation_invalid_schema() {
        let result = evaluate_assertion(
            &Assertion::Schema {
                schema: json!("not a schema"),
            },
            200,
            &HashMap::new(),
            &Some(json!({"key": "value"})),
            0,
        );
        assert!(!result.passed);
    }

    #[test]
    fn test_body_length() {
        let body = json!({"key": "value"});
        let result = evaluate_assertion(
            &Assertion::BodyLength(BodyLengthPredicate::Min(10)),
            200,
            &HashMap::new(),
            &Some(body),
            0,
        );
        assert!(result.passed);
    }

    #[test]
    fn test_body_length_eq() {
        let body = json!({"a": "b"});
        let len = body.to_string().len();
        let result = evaluate_assertion(
            &Assertion::BodyLength(BodyLengthPredicate::Eq(len)),
            200,
            &HashMap::new(),
            &Some(body),
            0,
        );
        assert!(result.passed);
    }

    #[test]
    fn test_body_length_eq_fail() {
        let body = json!({"a": "b"});
        let result = evaluate_assertion(
            &Assertion::BodyLength(BodyLengthPredicate::Eq(999)),
            200,
            &HashMap::new(),
            &Some(body),
            0,
        );
        assert!(!result.passed);
    }

    #[test]
    fn test_body_length_max() {
        let body = json!({"a": "b"});
        let result = evaluate_assertion(
            &Assertion::BodyLength(BodyLengthPredicate::Max(100)),
            200,
            &HashMap::new(),
            &Some(body),
            0,
        );
        assert!(result.passed);
    }

    #[test]
    fn test_body_length_max_fail() {
        let body = json!({"a": "b"});
        let result = evaluate_assertion(
            &Assertion::BodyLength(BodyLengthPredicate::Max(1)),
            200,
            &HashMap::new(),
            &Some(body),
            0,
        );
        assert!(!result.passed);
    }

    #[test]
    fn test_body_length_range() {
        let body = json!({"a": "b"});
        let result = evaluate_assertion(
            &Assertion::BodyLength(BodyLengthPredicate::Range { min: 5, max: 15 }),
            200,
            &HashMap::new(),
            &Some(body),
            0,
        );
        assert!(result.passed);
    }

    #[test]
    fn test_body_length_range_fail_below() {
        let body = json!({"a": "b"});
        let result = evaluate_assertion(
            &Assertion::BodyLength(BodyLengthPredicate::Range { min: 100, max: 200 }),
            200,
            &HashMap::new(),
            &Some(body),
            0,
        );
        assert!(!result.passed);
    }

    #[test]
    fn test_body_length_range_fail_above() {
        let body = json!({"a": "b"});
        let result = evaluate_assertion(
            &Assertion::BodyLength(BodyLengthPredicate::Range { min: 1, max: 3 }),
            200,
            &HashMap::new(),
            &Some(body),
            0,
        );
        assert!(!result.passed);
    }

    #[test]
    fn test_body_length_empty_body() {
        let result = evaluate_assertion(
            &Assertion::BodyLength(BodyLengthPredicate::Eq(0)),
            200,
            &HashMap::new(),
            &None,
            0,
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
            0,
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
            0,
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
            0,
        );
        assert!(result.passed);
    }

    #[test]
    fn test_response_time_pass() {
        let result = evaluate_assertion(
            &Assertion::response_time(500),
            200,
            &HashMap::new(),
            &None,
            150,
        );
        assert!(result.passed);
        assert!(result.description.contains("500ms"));
    }

    #[test]
    fn test_response_time_fail() {
        let result = evaluate_assertion(
            &Assertion::response_time(100),
            200,
            &HashMap::new(),
            &None,
            500,
        );
        assert!(!result.passed);
        assert!(result.message.unwrap().contains("500ms"));
    }

    #[test]
    fn test_response_time_exact_boundary() {
        let result = evaluate_assertion(
            &Assertion::response_time(200),
            200,
            &HashMap::new(),
            &None,
            200,
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

    #[test]
    fn test_resolve_json_path_recursive_descent() {
        let v = json!({"a": {"b": {"c": 1}}});
        let results = resolve_json_path(&v, "$..c");
        assert_eq!(results, vec![json!(1)]);
    }

    #[test]
    fn test_resolve_json_path_filter() {
        let v = json!({"items": [{"x": 1}, {"x": 2}, {"x": 3}]});
        let results = resolve_json_path(&v, "$.items[?(@.x>1)].x");
        assert_eq!(results, vec![json!(2), json!(3)]);
    }

    #[test]
    fn test_resolve_json_path_union() {
        let v = json!(["a", "b", "c"]);
        let results = resolve_json_path(&v, "$[0,2]");
        assert_eq!(results, vec![json!("a"), json!("c")]);
    }

    #[test]
    fn test_resolve_json_path_slice() {
        let v = json!([1, 2, 3, 4, 5]);
        let results = resolve_json_path(&v, "$[1:4]");
        assert_eq!(results, vec![json!(2), json!(3), json!(4)]);
    }

    #[test]
    fn test_resolve_json_path_invalid() {
        let v = json!({"a": 1});
        let results = resolve_json_path(&v, "[[invalid");
        assert!(results.is_empty());
    }

    // -------------------------------------------------------------------------
    // Property-based tests with proptest
    // -------------------------------------------------------------------------

    use proptest::prelude::*;

    proptest! {
        /// Not(Not(x)) should be equivalent to x for any assertion.
        ///
        /// This tests the double-negation elimination algebraic property.
        #[test]
        fn prop_not_not_equivalent_to_identity(
            status_code in 100u16..599u16,
            response_time in 0u64..10000u64,
        ) {
            // Test with a Status assertion
            let inner = Assertion::Status(status_code);
            let double_not = Assertion::Not(Box::new(Assertion::Not(Box::new(inner.clone()))));

            let direct = evaluate_assertion(&inner, status_code, &HashMap::new(), &None, response_time);
            let indirect = evaluate_assertion(&double_not, status_code, &HashMap::new(), &None, response_time);

            assert_eq!(direct.passed, indirect.passed,
                "Not(Not(Status({status_code}))) should be equivalent to Status({status_code})");
        }

        /// AllOf with a single element should be equivalent to that element.
        #[test]
        fn prop_all_of_single_element(
            status_code in 100u16..599u16,
        ) {
            let inner = Assertion::Status(status_code);
            let all_of = Assertion::AllOf(vec![inner.clone()]);

            let direct = evaluate_assertion(&inner, status_code, &HashMap::new(), &None, 0);
            let indirect = evaluate_assertion(&all_of, status_code, &HashMap::new(), &None, 0);

            assert_eq!(direct.passed, indirect.passed,
                "AllOf([Status({status_code})]) should be equivalent to Status({status_code})");
        }

        /// AnyOf with a single element should be equivalent to that element.
        #[test]
        fn prop_any_of_single_element(
            status_code in 100u16..599u16,
        ) {
            let inner = Assertion::Status(status_code);
            let any_of = Assertion::AnyOf(vec![inner.clone()]);

            let direct = evaluate_assertion(&inner, status_code, &HashMap::new(), &None, 0);
            let indirect = evaluate_assertion(&any_of, status_code, &HashMap::new(), &None, 0);

            assert_eq!(direct.passed, indirect.passed,
                "AnyOf([Status({status_code})]) should be equivalent to Status({status_code})");
        }

        /// Not(AllOf([a, b])) should be equivalent to AnyOf([Not(a), Not(b)]) (De Morgan's law).
        #[test]
        fn prop_de_morgan_all_of(
            code1 in 100u16..599u16,
            code2 in 100u16..599u16,
            actual in 100u16..599u16,
        ) {
            let a = Assertion::Status(code1);
            let b = Assertion::Status(code2);
            let not_all = Assertion::Not(Box::new(Assertion::AllOf(vec![a.clone(), b.clone()])));
            let any_not = Assertion::AnyOf(vec![
                Assertion::Not(Box::new(a.clone())),
                Assertion::Not(Box::new(b.clone())),
            ]);

            let r1 = evaluate_assertion(&not_all, actual, &HashMap::new(), &None, 0);
            let r2 = evaluate_assertion(&any_not, actual, &HashMap::new(), &None, 0);

            assert_eq!(r1.passed, r2.passed,
                "De Morgan: Not(AllOf([a, b])) should equal AnyOf([Not(a), Not(b)]) for status codes {code1}, {code2}, actual {actual}");
        }

        /// Not(AnyOf([a, b])) should be equivalent to AllOf([Not(a), Not(b)]) (De Morgan's law).
        #[test]
        fn prop_de_morgan_any_of(
            code1 in 100u16..599u16,
            code2 in 100u16..599u16,
            actual in 100u16..599u16,
        ) {
            let a = Assertion::Status(code1);
            let b = Assertion::Status(code2);
            let not_any = Assertion::Not(Box::new(Assertion::AnyOf(vec![a.clone(), b.clone()])));
            let all_not = Assertion::AllOf(vec![
                Assertion::Not(Box::new(a.clone())),
                Assertion::Not(Box::new(b.clone())),
            ]);

            let r1 = evaluate_assertion(&not_any, actual, &HashMap::new(), &None, 0);
            let r2 = evaluate_assertion(&all_not, actual, &HashMap::new(), &None, 0);

            assert_eq!(r1.passed, r2.passed,
                "De Morgan: Not(AnyOf([a, b])) should equal AllOf([Not(a), Not(b)]) for status codes {code1}, {code2}, actual {actual}");
        }

        /// ResponseTime should pass when time <= max and fail when time > max.
        #[test]
        fn prop_response_time_boundary(
            max_millis in 0u64..10000u64,
            actual_time in 0u64..20000u64,
        ) {
            let assertion = Assertion::ResponseTime(max_millis);
            let result = evaluate_assertion(&assertion, 200, &HashMap::new(), &None, actual_time);

            let expected_pass = actual_time <= max_millis;
            assert_eq!(result.passed, expected_pass,
                "ResponseTime({max_millis}ms) should pass when actual {actual_time}ms <= {max_millis}ms");
        }

        /// StatusIn should pass when the actual status is in the list.
        #[test]
        fn prop_status_in(
            actual in 100u16..599u16,
        ) {
            let codes = vec![200u16, 201, 204, 304, 400, 404, 500];
            let assertion = Assertion::StatusIn(codes.clone());
            let result = evaluate_assertion(&assertion, actual, &HashMap::new(), &None, 0);

            let expected_pass = codes.contains(&actual);
            assert_eq!(result.passed, expected_pass,
                "StatusIn({codes:?}) should pass for {actual}");
        }
    }
}
