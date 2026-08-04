/// Core AST types for the Momus API test harness.
///
/// A test plan is a sequence of steps. Each step is either:
/// - A **request** (HTTP call with assertions)
/// - A **sequence** (ordered sub-steps, with state passed between them)
/// - A **script** (inline code for custom logic)
use serde::{Deserialize, Serialize};
use std::collections::HashMap;

pub mod assertion;

pub use assertion::*;

// ---------------------------------------------------------------------------
// Top-level plan
// ---------------------------------------------------------------------------

/// A complete test plan.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TestPlan {
    /// Plan name / description.
    pub name: String,
    /// Optional base URL for all requests (can be overridden per-step).
    #[serde(default)]
    pub base_url: String,
    /// Default headers applied to every request.
    #[serde(default)]
    pub default_headers: HashMap<String, String>,
    /// Ordered list of test steps.
    #[serde(default)]
    pub steps: Vec<Step>,
    /// Global setup steps run before all tests.
    #[serde(default)]
    pub setup: Vec<Step>,
    /// Global teardown steps run after all tests.
    #[serde(default)]
    pub teardown: Vec<Step>,
}

impl TestPlan {
    /// Count total test cases (leaf requests) in the plan.
    pub fn total_tests(&self) -> usize {
        self.steps.iter().map(|s| s.count_tests()).sum()
    }
}

// ---------------------------------------------------------------------------
// Steps
// ---------------------------------------------------------------------------

/// A single step in a test plan.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum Step {
    /// A single HTTP request with assertions.
    Request(RequestStep),
    /// A named sequence of sub-steps (state flows between them).
    Sequence(SequenceStep),
    /// A group of steps run in parallel.
    Parallel(Vec<Step>),
    /// A script step for custom logic.
    Script(ScriptStep),
    /// A step that does nothing (useful for placeholders / disabled tests).
    Noop {
        #[serde(default)]
        description: String,
    },
}

impl Step {
    /// Count leaf request steps (recurses into sequences and parallels).
    pub fn count_tests(&self) -> usize {
        match self {
            Step::Request(_) => 1,
            Step::Sequence(s) => s.steps.iter().map(|s| s.count_tests()).sum(),
            Step::Parallel(steps) => steps.iter().map(|s| s.count_tests()).sum(),
            Step::Script(_) => 1,
            Step::Noop { .. } => 0,
        }
    }
}

// ---------------------------------------------------------------------------
// Request step
// ---------------------------------------------------------------------------

/// A single HTTP request with assertions.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RequestStep {
    /// Display name.
    pub name: String,
    /// HTTP method.
    pub method: Method,
    /// URL (may contain `{base_url}` and `{steps.<name>.*}` templates).
    pub url: String,
    /// Per-request headers (merged over defaults).
    #[serde(default)]
    pub headers: HashMap<String, String>,
    /// Request body (if applicable).
    #[serde(default)]
    pub body: Option<serde_json::Value>,
    /// Assertions to evaluate against the response.
    #[serde(default)]
    pub assert: Vec<Assertion>,
    /// If set, save the response under this name for `{steps.<name>.*}` references.
    #[serde(default)]
    pub save_as: String,
    /// If true, a failure in this step does not abort the sequence.
    #[serde(default)]
    pub soft_fail: bool,
}

/// HTTP method.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(rename_all = "UPPERCASE")]
pub enum Method {
    Get,
    Post,
    Put,
    Delete,
    Patch,
    Head,
    Options,
}

impl std::fmt::Display for Method {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Method::Get => write!(f, "GET"),
            Method::Post => write!(f, "POST"),
            Method::Put => write!(f, "PUT"),
            Method::Delete => write!(f, "DELETE"),
            Method::Patch => write!(f, "PATCH"),
            Method::Head => write!(f, "HEAD"),
            Method::Options => write!(f, "OPTIONS"),
        }
    }
}

// ---------------------------------------------------------------------------
// Sequence step
// ---------------------------------------------------------------------------

/// A named sequence of sub-steps with state passing.
///
/// Each step can reference values from previous steps via `{steps.<name>.*}`
/// templates. The sequence name is used as the namespace for saved responses.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SequenceStep {
    /// Sequence name (used for `{steps.<name>.*}` references).
    pub name: String,
    /// Ordered sub-steps.
    pub steps: Vec<Step>,
    /// If true, continue executing remaining steps even if one fails.
    #[serde(default)]
    pub continue_on_failure: bool,
}

// ---------------------------------------------------------------------------
// Script step
// ---------------------------------------------------------------------------

/// A script step for custom logic.
///
/// Scripts run after all previous steps have completed and can access
/// saved responses via the context. The script can add new assertions
/// or modify the test state.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ScriptStep {
    /// Display name.
    pub name: String,
    /// The script language (e.g. "rhai", "js", "python").
    pub language: String,
    /// Inline script source.
    pub source: String,
}

// ---------------------------------------------------------------------------
// Test result types
// ---------------------------------------------------------------------------

/// The result of executing a single request step.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TestResult {
    /// Test name.
    pub name: String,
    /// Whether the test passed (all assertions satisfied).
    pub passed: bool,
    /// HTTP status code received.
    pub status_code: u16,
    /// Request details.
    pub request_method: String,
    pub request_url: String,
    pub request_headers: HashMap<String, String>,
    pub request_body: Option<serde_json::Value>,
    /// Response details.
    pub response_headers: HashMap<String, String>,
    pub response_body: Option<serde_json::Value>,
    /// Assertion results.
    #[serde(default)]
    pub assertion_results: Vec<AssertionResult>,
    /// Human-readable errors.
    #[serde(default)]
    pub errors: Vec<String>,
}

/// A group of test results (one per sequence or top-level group).
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TestGroupResult {
    pub name: String,
    pub passed: usize,
    pub failed: usize,
    pub total: usize,
    pub results: Vec<TestResult>,
}

/// Full run report.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RunReport {
    pub plan_name: String,
    pub total: usize,
    pub passed: usize,
    pub failed: usize,
    pub groups: Vec<TestGroupResult>,
    pub duration_ms: u64,
}

impl RunReport {
    pub fn write_results(&self, output_dir: &std::path::Path) -> anyhow::Result<()> {
        let results_dir = output_dir.join("results");
        std::fs::create_dir_all(&results_dir)?;

        // Write per-group results
        for group in &self.groups {
            let path = results_dir.join(format!("{}.json", group.name));
            let json = serde_json::to_string_pretty(group)?;
            std::fs::write(path, json)?;
        }

        // Write summary
        let summary = serde_json::json!({
            "plan": self.plan_name,
            "total": self.total,
            "passed": self.passed,
            "failed": self.failed,
            "duration_ms": self.duration_ms,
            "groups": self.groups.iter().map(|g| serde_json::json!({
                "name": g.name,
                "total": g.total,
                "passed": g.passed,
                "failed": g.failed,
            })).collect::<Vec<_>>(),
        });
        std::fs::write(results_dir.join("summary.json"), serde_json::to_string_pretty(&summary)?)?;

        // Write failed tests
        let failed: Vec<&TestResult> = self
            .groups
            .iter()
            .flat_map(|g| g.results.iter())
            .filter(|r| !r.passed)
            .collect();
        if !failed.is_empty() {
            std::fs::write(
                results_dir.join("failed.json"),
                serde_json::to_string_pretty(&failed)?,
            )?;
        }

        Ok(())
    }
}

impl std::fmt::Display for RunReport {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        for group in &self.groups {
            writeln!(f, "\n── {} ──", group.name)?;
            for result in &group.results {
                let status = if result.passed { "✓" } else { "✗" };
                writeln!(
                    f,
                    "  {} {} {} [{}]",
                    status, result.request_method, result.request_url, result.status_code
                )?;
                for err in &result.errors {
                    writeln!(f, "    └─ {}", err)?;
                }
            }
        }
        writeln!(f)?;
        writeln!(f, "=== Results ===")?;
        writeln!(
            f,
            "Total: {} | Passed: {} | Failed: {} | Duration: {}ms",
            self.total, self.passed, self.failed, self.duration_ms
        )?;
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_plan_total_tests() {
        let plan = TestPlan {
            name: "test".into(),
            base_url: "http://localhost".into(),
            default_headers: HashMap::new(),
            steps: vec![
                Step::Request(RequestStep {
                    name: "r1".into(),
                    method: Method::Get,
                    url: "/".into(),
                    headers: HashMap::new(),
                    body: None,
                    assert: vec![],
                    save_as: String::new(),
                    soft_fail: false,
                }),
                Step::Sequence(SequenceStep {
                    name: "seq".into(),
                    steps: vec![
                        Step::Request(RequestStep {
                            name: "r2".into(),
                            method: Method::Post,
                            url: "/".into(),
                            headers: HashMap::new(),
                            body: None,
                            assert: vec![],
                            save_as: String::new(),
                            soft_fail: false,
                        }),
                        Step::Request(RequestStep {
                            name: "r3".into(),
                            method: Method::Get,
                            url: "/".into(),
                            headers: HashMap::new(),
                            body: None,
                            assert: vec![],
                            save_as: String::new(),
                            soft_fail: false,
                        }),
                    ],
                    continue_on_failure: false,
                }),
                Step::Noop {
                    description: "skip".into(),
                },
            ],
            setup: vec![],
            teardown: vec![],
        };

        assert_eq!(plan.total_tests(), 3);
    }

    #[test]
    fn method_display() {
        assert_eq!(Method::Get.to_string(), "GET");
        assert_eq!(Method::Post.to_string(), "POST");
        assert_eq!(Method::Delete.to_string(), "DELETE");
    }
}
