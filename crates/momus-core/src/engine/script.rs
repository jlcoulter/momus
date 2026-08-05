/// Execute script steps using the rhai scripting engine.
use crate::ast::{ScriptStep, TestResult};
use rhai::{Dynamic, Engine, Map, Scope};
use std::collections::HashMap;

/// Result of executing a script step.
pub struct ScriptResult {
    pub passed: bool,
    pub errors: Vec<String>,
}

/// Execute a script step and return a TestResult.
pub fn execute_script(
    script: &ScriptStep,
    step_responses: &HashMap<String, serde_json::Value>,
) -> TestResult {
    let result = match execute_script_inner(script, step_responses) {
        Ok(sr) => sr,
        Err(e) => ScriptResult {
            passed: false,
            errors: vec![e],
        },
    };

    TestResult {
        name: script.name.clone(),
        passed: result.passed,
        status_code: 0,
        request_method: String::new(),
        request_url: String::new(),
        request_headers: HashMap::new(),
        request_body: None,
        response_headers: HashMap::new(),
        response_body: None,
        assertion_results: vec![],
        errors: result.errors,
    }
}

/// Internal execution logic.
fn execute_script_inner(
    script: &ScriptStep,
    step_responses: &HashMap<String, serde_json::Value>,
) -> Result<ScriptResult, String> {
    if script.language != "rhai" {
        return Err(format!(
            "unsupported script language '{}' (supported: 'rhai')",
            script.language
        ));
    }

    let engine = Engine::new();

    // Build the context object as a rhai Dynamic::Map
    let mut context_map = Map::new();
    let mut responses_map = Map::new();

    for (key, value) in step_responses {
        let rhai_val = json_value_to_dynamic(value);
        responses_map.insert(key.trim().to_string().into(), rhai_val);
    }
    context_map.insert("step_responses".into(), Dynamic::from_map(responses_map));

    // Register the context variable
    let mut scope = Scope::new();
    scope.push_constant("context", Dynamic::from_map(context_map));

    // We'll capture the result from a `result` variable set by the script.
    // The script can set `result = #{}` with `passed` and `errors` fields.
    // Default result: passed=true, no errors.
    let mut result_map = Map::new();
    result_map.insert("passed".into(), Dynamic::from_bool(true));
    result_map.insert("errors".into(), Dynamic::from_array(vec![]));
    scope.push("result", Dynamic::from_map(result_map));

    // Execute the script
    let ast = engine
        .compile(script.source.as_str())
        .map_err(|e| format!("script compile error: {e}"))?;

    if let Err(e) = engine.run_ast_with_scope(&mut scope, &ast) {
        return Err(format!("script runtime error: {e}"));
    }

    // Extract the result variable
    let result_dynamic = scope
        .get_value::<Dynamic>("result")
        .ok_or_else(|| "script did not set a 'result' variable".to_string())?;

    let result_map = result_dynamic
        .try_cast::<Map>()
        .ok_or_else(|| "script 'result' must be an object map".to_string())?;

    let passed = result_map
        .get("passed")
        .cloned()
        .and_then(|v| v.try_cast::<bool>())
        .unwrap_or(true);

    let errors: Vec<String> = result_map
        .get("errors")
        .cloned()
        .and_then(|v| v.try_cast::<rhai::Array>())
        .map(|arr| {
            arr.into_iter()
                .filter_map(|v| v.try_cast::<String>())
                .collect()
        })
        .unwrap_or_default();

    Ok(ScriptResult { passed, errors })
}

/// Convert a serde_json::Value to a rhai Dynamic.
fn json_value_to_dynamic(value: &serde_json::Value) -> Dynamic {
    match value {
        serde_json::Value::Null => Dynamic::UNIT,
        serde_json::Value::Bool(b) => Dynamic::from_bool(*b),
        serde_json::Value::Number(n) => {
            if let Some(i) = n.as_i64() {
                Dynamic::from_int(i)
            } else if let Some(f) = n.as_f64() {
                Dynamic::from_float(f)
            } else {
                Dynamic::UNIT
            }
        }
        serde_json::Value::String(s) => Dynamic::from(s.clone()),
        serde_json::Value::Array(arr) => {
            let items: rhai::Array = arr.iter().map(json_value_to_dynamic).collect();
            Dynamic::from_array(items)
        }
        serde_json::Value::Object(obj) => {
            let mut map = Map::new();
            for (k, v) in obj {
                map.insert(k.clone().into(), json_value_to_dynamic(v));
            }
            Dynamic::from_map(map)
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_unsupported_language() {
        let script = ScriptStep {
            name: "bad".into(),
            language: "python".into(),
            source: "print('hello')".into(),
        };
        let result = execute_script(&script, &HashMap::new());
        assert!(!result.passed);
        assert!(result.errors[0].contains("unsupported script language 'python'"));
    }

    #[test]
    fn test_rhai_simple_pass() {
        let script = ScriptStep {
            name: "simple".into(),
            language: "rhai".into(),
            source: r#"
                result.passed = true;
                result.errors = [];
            "#
            .into(),
        };
        let result = execute_script(&script, &HashMap::new());
        assert!(
            result.passed,
            "expected pass, got errors: {:?}",
            result.errors
        );
        assert!(result.errors.is_empty());
    }

    #[test]
    fn test_rhai_simple_fail() {
        let script = ScriptStep {
            name: "fail".into(),
            language: "rhai".into(),
            source: r#"
                result.passed = false;
                result.errors = ["something went wrong"];
            "#
            .into(),
        };
        let result = execute_script(&script, &HashMap::new());
        assert!(!result.passed);
        assert_eq!(result.errors, vec!["something went wrong"]);
    }

    #[test]
    fn test_rhai_with_context() {
        let mut responses = HashMap::new();
        responses.insert(
            "login".into(),
            serde_json::json!({"token": "abc123", "user": "admin"}),
        );

        let script = ScriptStep {
            name: "check_context".into(),
            language: "rhai".into(),
            source: r#"
                let token = context.step_responses["login"].token;
                if token == "abc123" {
                    result.passed = true;
                } else {
                    result.passed = false;
                    result.errors = ["token mismatch: " + token];
                }
            "#
            .into(),
        };
        let result = execute_script(&script, &responses);
        assert!(
            result.passed,
            "expected pass, got errors: {:?}",
            result.errors
        );
    }

    #[test]
    fn test_rhai_compile_error() {
        let script = ScriptStep {
            name: "bad_syntax".into(),
            language: "rhai".into(),
            source: "this is not valid rhai @@".into(),
        };
        let result = execute_script(&script, &HashMap::new());
        assert!(!result.passed);
        assert!(result.errors[0].contains("compile error"));
    }

    #[test]
    fn test_rhai_runtime_error() {
        let script = ScriptStep {
            name: "runtime_err".into(),
            language: "rhai".into(),
            source: r#"
                let x = 1 / 0;
            "#
            .into(),
        };
        let result = execute_script(&script, &HashMap::new());
        assert!(!result.passed);
        assert!(result.errors[0].contains("runtime error"));
    }

    #[test]
    fn test_rhai_default_result() {
        // Script that doesn't touch result at all — should default to passed=true
        let script = ScriptStep {
            name: "default".into(),
            language: "rhai".into(),
            source: r#"
                let x = 42;
            "#
            .into(),
        };
        let result = execute_script(&script, &HashMap::new());
        assert!(result.passed);
        assert!(result.errors.is_empty());
    }

    #[test]
    fn test_rhai_with_nested_context() {
        let mut responses = HashMap::new();
        responses.insert(
            "create".into(),
            serde_json::json!({
                "id": "item-001",
                "metadata": {
                    "tags": ["a", "b", "c"],
                    "count": 3
                }
            }),
        );

        let script = ScriptStep {
            name: "nested".into(),
            language: "rhai".into(),
            source: r#"
                let item = context.step_responses["create"];
                let tags = item.metadata.tags;
                if tags.len() == 3 && item.metadata.count == 3 {
                    result.passed = true;
                } else {
                    result.passed = false;
                    result.errors = ["unexpected data"];
                }
            "#
            .into(),
        };
        let result = execute_script(&script, &responses);
        assert!(
            result.passed,
            "expected pass, got errors: {:?}",
            result.errors
        );
    }
}
