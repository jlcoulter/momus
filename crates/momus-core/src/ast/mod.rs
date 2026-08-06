/// Core AST types for the Momus API test harness.
///
/// A test plan is a sequence of steps. Each step is either:
/// - A **request** (HTTP call with assertions)
/// - A **sequence** (ordered sub-steps, with state passed between them)
/// - A **script** (inline code for custom logic)
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::fmt::Write;

pub mod api_model;
pub mod assertion;
pub mod test_spec;

pub use api_model::*;
pub use assertion::*;
pub use test_spec::*;

/// Escape HTML special characters for safe embedding in HTML output.
fn html_escape(s: &str) -> String {
    s.replace('&', "&amp;")
        .replace('<', "&lt;")
        .replace('>', "&gt;")
        .replace('"', "&quot;")
        .replace('\'', "&#39;")
}

fn default_script_language() -> String {
    "rhai".to_string()
}

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

    /// Collect all `RequestStep` references from the plan, recursing into
    /// Sequence and Parallel containers. Includes setup, steps, and teardown.
    pub fn request_steps(&self) -> Vec<&RequestStep> {
        let mut result = Vec::new();
        collect_request_steps(&self.setup, &mut result);
        collect_request_steps(&self.steps, &mut result);
        collect_request_steps(&self.teardown, &mut result);
        result
    }

    /// Print a human-readable tree of all requests in the plan.
    pub fn display_plan(&self) -> String {
        let mut out = String::new();

        writeln!(out, "Plan: {}", self.name).ok();
        if !self.base_url.is_empty() {
            writeln!(out, "Base URL: {}", self.base_url).ok();
        }
        if !self.default_headers.is_empty() {
            writeln!(out, "Default headers:").ok();
            for (k, v) in &self.default_headers {
                writeln!(out, "  {k}: {v}").ok();
            }
        }

        let mut test_count = 0usize;
        let mut setup_count = 0usize;
        let mut teardown_count = 0usize;

        if !self.setup.is_empty() {
            writeln!(out, "\n── Setup ──").ok();
            for step in &self.setup {
                setup_count += step.display(&mut out, 0);
            }
        }

        if !self.steps.is_empty() {
            writeln!(out, "\n── Tests ──").ok();
            for step in &self.steps {
                test_count += step.display(&mut out, 0);
            }
        }

        if !self.teardown.is_empty() {
            writeln!(out, "\n── Teardown ──").ok();
            for step in &self.teardown {
                teardown_count += step.display(&mut out, 0);
            }
        }

        writeln!(
            out,
            "\nTotal requests: {} ({} setup, {} tests, {} teardown)",
            setup_count + test_count + teardown_count,
            setup_count,
            test_count,
            teardown_count,
        )
        .ok();

        out
    }
}

// ---------------------------------------------------------------------------
// Steps
// ---------------------------------------------------------------------------

/// A single step in a test plan.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(tag = "type", rename_all = "snake_case")]
pub enum Step {
    /// A single HTTP request with assertions.
    Request(RequestStep),
    /// A named sequence of sub-steps (state flows between them).
    Sequence(SequenceStep),
    /// A group of steps run in parallel.
    Parallel(ParallelStep),
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
            Step::Parallel(par) => par.steps.iter().map(|s| s.count_tests()).sum(),
            Step::Script(_) => 0,
            Step::Noop { .. } => 0,
        }
    }

    /// Render this step into the output buffer, returning the number of leaf requests.
    pub fn display(&self, out: &mut String, depth: usize) -> usize {
        let indent = "  ".repeat(depth);
        match self {
            Step::Request(req) => {
                let body_preview = match &req.body {
                    Some(b) => {
                        let s = serde_json::to_string(b).unwrap_or_default();
                        if s.len() > 60 {
                            // Safe UTF-8 truncation — take up to 57 chars without splitting
                            let truncated: String = s.chars().take(57).collect();
                            format!("  {truncated}")
                        } else {
                            format!("  {s}")
                        }
                    }
                    None => String::new(),
                };
                writeln!(out, "{}{}  {}{}", indent, req.method, req.url, body_preview,).ok();
                1
            }
            Step::Sequence(seq) => {
                writeln!(out, "{}── sequence: \"{}\" ──", indent, seq.name).ok();
                let mut count = 0usize;
                for step in &seq.steps {
                    count += step.display(out, depth + 1);
                }
                count
            }
            Step::Parallel(par) => {
                writeln!(out, "{indent}── parallel ──").ok();
                let mut count = 0usize;
                for step in &par.steps {
                    count += step.display(out, depth + 1);
                }
                count
            }
            Step::Script(script) => {
                writeln!(
                    out,
                    "{}[script] {} ({})",
                    indent, script.name, script.language
                )
                .ok();
                0
            }
            Step::Noop { description } => {
                if !description.is_empty() {
                    writeln!(out, "{indent}[noop] {description}").ok();
                }
                0
            }
        }
    }
}

/// Recursively collect `RequestStep` references from a slice of steps.
pub(crate) fn collect_request_steps<'a>(steps: &'a [Step], result: &mut Vec<&'a RequestStep>) {
    for step in steps {
        match step {
            Step::Request(req) => result.push(req),
            Step::Sequence(seq) => collect_request_steps(&seq.steps, result),
            Step::Parallel(par) => collect_request_steps(&par.steps, result),
            _ => {}
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
// Parallel step
// ---------------------------------------------------------------------------

/// A group of steps executed in parallel.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ParallelStep {
    /// Sub-steps to execute concurrently.
    pub steps: Vec<Step>,
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
    #[serde(default = "default_script_language")]
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
    /// Render the report as a self-contained HTML page.
    pub fn to_html(&self) -> String {
        let pass_pct = if self.total > 0 {
            (self.passed as f64 / self.total as f64) * 100.0
        } else {
            100.0
        };
        let fail_pct = if self.total > 0 {
            (self.failed as f64 / self.total as f64) * 100.0
        } else {
            0.0
        };

        let mut groups_rows = String::new();
        for group in &self.groups {
            let group_pass_pct = if group.total > 0 {
                (group.passed as f64 / group.total as f64) * 100.0
            } else {
                100.0
            };
            groups_rows.push_str(&format!(
                r#"<tr><td>{name}</td><td>{total}</td><td class="green">{passed}</td><td class="red">{failed}</td><td>{pct:.1}%</td></tr>"#,
                name = html_escape(&group.name),
                total = group.total,
                passed = group.passed,
                failed = group.failed,
                pct = group_pass_pct,
            ));

            for result in &group.results {
                let status_icon = if result.passed { "✓" } else { "✗" };
                let status_class = if result.passed { "pass" } else { "fail" };
                let errors_html: String = result
                    .errors
                    .iter()
                    .map(|e| format!("<div class=\"error\">{}</div>", html_escape(e)))
                    .collect();
                groups_rows.push_str(&format!(
                    r#"<tr class="detail {status_class}"><td></td><td colspan="4"><span class="{status_class}">{icon}</span> <strong>{method}</strong> {url} <span class="status-code">{status}</span>{errors}</td></tr>"#,
                    status_class = status_class,
                    icon = status_icon,
                    method = html_escape(&result.request_method),
                    url = html_escape(&result.request_url),
                    status = result.status_code,
                    errors = errors_html,
                ));
            }
        }

        format!(
            r#"<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Test Run Report — {plan_name}</title>
<style>
  * {{ box-sizing: border-box; margin: 0; padding: 0; }}
  body {{ font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; background: #f5f7fa; color: #1a1a2e; padding: 2rem; }}
  .container {{ max-width: 1000px; margin: 0 auto; }}
  h1 {{ font-size: 1.6rem; margin-bottom: 0.25rem; }}
  .subtitle {{ color: #666; margin-bottom: 1.5rem; }}
  .summary {{ display: grid; grid-template-columns: repeat(auto-fit, minmax(160px, 1fr)); gap: 1rem; margin-bottom: 2rem; }}
  .card {{ background: #fff; border-radius: 8px; padding: 1rem; box-shadow: 0 1px 3px rgba(0,0,0,0.08); }}
  .card .label {{ font-size: 0.75rem; text-transform: uppercase; color: #888; letter-spacing: 0.5px; }}
  .card .value {{ font-size: 1.5rem; font-weight: 700; margin-top: 0.25rem; }}
  .card .value.green {{ color: #22c55e; }}
  .card .value.red {{ color: #ef4444; }}
  .card .value.blue {{ color: #3b82f6; }}
  table {{ width: 100%; border-collapse: collapse; background: #fff; border-radius: 8px; overflow: hidden; box-shadow: 0 1px 3px rgba(0,0,0,0.08); }}
  th {{ background: #f0f2f5; text-align: left; padding: 0.75rem 1rem; font-size: 0.8rem; text-transform: uppercase; color: #666; letter-spacing: 0.5px; }}
  td {{ padding: 0.5rem 1rem; border-top: 1px solid #e5e7eb; font-size: 0.9rem; }}
  tr:hover td {{ background: #f9fafb; }}
  .pass {{ color: #22c55e; }}
  .fail {{ color: #ef4444; }}
  .green {{ color: #22c55e; font-weight: 600; }}
  .red {{ color: #ef4444; font-weight: 600; }}
  .status-code {{ color: #888; font-size: 0.8rem; margin-left: 0.5rem; }}
  .error {{ color: #ef4444; font-size: 0.8rem; margin-top: 0.25rem; padding-left: 1rem; }}
  tr.detail td {{ padding-left: 2rem; font-size: 0.85rem; }}
  .footer {{ margin-top: 1.5rem; font-size: 0.8rem; color: #999; text-align: center; }}
</style>
</head>
<body>
<div class="container">
  <h1>Test Run Report</h1>
  <p class="subtitle">Plan: {plan_name} &middot; {total} tests in {duration_ms}ms</p>

  <div class="summary">
    <div class="card">
      <div class="label">Total</div>
      <div class="value blue">{total}</div>
    </div>
    <div class="card">
      <div class="label">Passed</div>
      <div class="value green">{passed}</div>
    </div>
    <div class="card">
      <div class="label">Failed</div>
      <div class="value red">{failed}</div>
    </div>
    <div class="card">
      <div class="label">Pass Rate</div>
      <div class="value green">{pass_pct:.1}%</div>
    </div>
    <div class="card">
      <div class="label">Fail Rate</div>
      <div class="value red">{fail_pct:.1}%</div>
    </div>
    <div class="card">
      <div class="label">Duration</div>
      <div class="value">{duration_ms} <span style="font-size:0.8rem;font-weight:400;color:#888;">ms</span></div>
    </div>
  </div>

  <table>
    <thead>
      <tr><th>Group</th><th>Total</th><th>Passed</th><th>Failed</th><th>Rate</th></tr>
    </thead>
    <tbody>
      {groups_rows}
    </tbody>
  </table>

  <div class="footer">Generated by Momus</div>
</div>
</body>
</html>"#,
            plan_name = html_escape(&self.plan_name),
            total = self.total,
            passed = self.passed,
            failed = self.failed,
            duration_ms = self.duration_ms,
            pass_pct = pass_pct,
            fail_pct = fail_pct,
            groups_rows = groups_rows,
        )
    }

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
        std::fs::write(
            results_dir.join("summary.json"),
            serde_json::to_string_pretty(&summary)?,
        )?;

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

    /// Write a JUnit XML report to the given path.
    pub fn write_junit_xml(&self, output_dir: &std::path::Path) -> anyhow::Result<()> {
        let path = output_dir.join("junit.xml");
        let file = std::fs::File::create(&path)?;
        let mut writer = std::io::BufWriter::new(file);
        crate::junit::write_junit_xml(&mut writer, self)?;
        tracing::info!("JUnit XML report written to: {}", path.display());
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
                    writeln!(f, "    └─ {err}")?;
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
        assert_eq!(Method::Put.to_string(), "PUT");
        assert_eq!(Method::Delete.to_string(), "DELETE");
        assert_eq!(Method::Patch.to_string(), "PATCH");
        assert_eq!(Method::Head.to_string(), "HEAD");
        assert_eq!(Method::Options.to_string(), "OPTIONS");
    }

    #[test]
    fn test_step_count_tests() {
        // Request counts as 1
        assert_eq!(
            Step::Request(RequestStep {
                name: "r".into(),
                method: Method::Get,
                url: "/".into(),
                headers: HashMap::new(),
                body: None,
                assert: vec![],
                save_as: String::new(),
                soft_fail: false,
            })
            .count_tests(),
            1
        );

        // Sequence counts recursively
        assert_eq!(
            Step::Sequence(SequenceStep {
                name: "seq".into(),
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
                ],
                continue_on_failure: false,
            })
            .count_tests(),
            2
        );

        // Parallel counts recursively
        assert_eq!(
            Step::Parallel(ParallelStep {
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
                ],
            })
            .count_tests(),
            2
        );

        // Script counts as 0 (no HTTP request to assert against)
        assert_eq!(
            Step::Script(ScriptStep {
                name: "s".into(),
                language: "rhai".into(),
                source: "print(42);".into(),
            })
            .count_tests(),
            0
        );

        // Noop counts as 0
        assert_eq!(
            Step::Noop {
                description: "skip".into()
            }
            .count_tests(),
            0
        );
        assert_eq!(
            Step::Noop {
                description: String::new()
            }
            .count_tests(),
            0
        );
    }

    #[test]
    fn test_step_display() {
        let mut out = String::new();
        let count = Step::Request(RequestStep {
            name: "r".into(),
            method: Method::Get,
            url: "/test".into(),
            headers: HashMap::new(),
            body: None,
            assert: vec![],
            save_as: String::new(),
            soft_fail: false,
        })
        .display(&mut out, 0);
        assert_eq!(count, 1);
        assert!(out.contains("GET"));
        assert!(out.contains("/test"));

        let mut out = String::new();
        let count = Step::Script(ScriptStep {
            name: "my-script".into(),
            language: "rhai".into(),
            source: "print(42);".into(),
        })
        .display(&mut out, 0);
        assert_eq!(count, 0);
        assert!(out.contains("my-script"));
        assert!(out.contains("rhai"));

        let mut out = String::new();
        let count = Step::Noop {
            description: "placeholder".into(),
        }
        .display(&mut out, 0);
        assert_eq!(count, 0);
        assert!(out.contains("placeholder"));

        let mut out = String::new();
        let count = Step::Noop {
            description: String::new(),
        }
        .display(&mut out, 0);
        assert_eq!(count, 0);
        assert!(out.is_empty());

        let mut out = String::new();
        let count = Step::Parallel(ParallelStep {
            steps: vec![
                Step::Request(RequestStep {
                    name: "r1".into(),
                    method: Method::Get,
                    url: "/a".into(),
                    headers: HashMap::new(),
                    body: None,
                    assert: vec![],
                    save_as: String::new(),
                    soft_fail: false,
                }),
                Step::Request(RequestStep {
                    name: "r2".into(),
                    method: Method::Post,
                    url: "/b".into(),
                    headers: HashMap::new(),
                    body: None,
                    assert: vec![],
                    save_as: String::new(),
                    soft_fail: false,
                }),
            ],
        })
        .display(&mut out, 0);
        assert_eq!(count, 2);
        assert!(out.contains("parallel"));
    }

    #[test]
    fn test_collect_request_steps() {
        let steps = vec![
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
                steps: vec![Step::Request(RequestStep {
                    name: "r2".into(),
                    method: Method::Post,
                    url: "/".into(),
                    headers: HashMap::new(),
                    body: None,
                    assert: vec![],
                    save_as: String::new(),
                    soft_fail: false,
                })],
                continue_on_failure: false,
            }),
            Step::Parallel(ParallelStep {
                steps: vec![Step::Request(RequestStep {
                    name: "r3".into(),
                    method: Method::Put,
                    url: "/".into(),
                    headers: HashMap::new(),
                    body: None,
                    assert: vec![],
                    save_as: String::new(),
                    soft_fail: false,
                })],
            }),
            Step::Noop {
                description: "skip".into(),
            },
        ];
        let mut result = Vec::new();
        collect_request_steps(&steps, &mut result);
        assert_eq!(result.len(), 3);
        assert_eq!(result[0].name, "r1");
        assert_eq!(result[1].name, "r2");
        assert_eq!(result[2].name, "r3");
    }

    #[test]
    fn test_plan_request_steps() {
        let plan = TestPlan {
            name: "test".into(),
            base_url: String::new(),
            default_headers: HashMap::new(),
            setup: vec![Step::Request(RequestStep {
                name: "setup".into(),
                method: Method::Post,
                url: "/setup".into(),
                headers: HashMap::new(),
                body: None,
                assert: vec![],
                save_as: String::new(),
                soft_fail: false,
            })],
            steps: vec![Step::Request(RequestStep {
                name: "test".into(),
                method: Method::Get,
                url: "/test".into(),
                headers: HashMap::new(),
                body: None,
                assert: vec![],
                save_as: String::new(),
                soft_fail: false,
            })],
            teardown: vec![Step::Request(RequestStep {
                name: "teardown".into(),
                method: Method::Delete,
                url: "/teardown".into(),
                headers: HashMap::new(),
                body: None,
                assert: vec![],
                save_as: String::new(),
                soft_fail: false,
            })],
        };
        let steps = plan.request_steps();
        assert_eq!(steps.len(), 3);
        assert_eq!(steps[0].name, "setup");
        assert_eq!(steps[1].name, "test");
        assert_eq!(steps[2].name, "teardown");
    }

    #[test]
    fn test_plan_display_plan() {
        let plan = TestPlan {
            name: "my-plan".into(),
            base_url: "http://example.com".into(),
            default_headers: HashMap::from([("Authorization".into(), "Bearer token".into())]),
            setup: vec![],
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
            teardown: vec![],
        };
        let display = plan.display_plan();
        assert!(display.contains("my-plan"));
        assert!(display.contains("http://example.com"));
        assert!(display.contains("Authorization"));
        assert!(display.contains("Bearer token"));
        assert!(display.contains("GET"));
        assert!(display.contains("/health"));
    }

    #[test]
    fn test_run_report_display() {
        let report = RunReport {
            plan_name: "test".into(),
            total: 2,
            passed: 1,
            failed: 1,
            duration_ms: 500,
            groups: vec![TestGroupResult {
                name: "group1".into(),
                total: 2,
                passed: 1,
                failed: 1,
                results: vec![
                    TestResult {
                        name: "pass".into(),
                        passed: true,
                        status_code: 200,
                        request_method: "GET".into(),
                        request_url: "/ok".into(),
                        request_headers: HashMap::new(),
                        request_body: None,
                        response_headers: HashMap::new(),
                        response_body: None,
                        assertion_results: vec![],
                        errors: vec![],
                    },
                    TestResult {
                        name: "fail".into(),
                        passed: false,
                        status_code: 500,
                        request_method: "POST".into(),
                        request_url: "/err".into(),
                        request_headers: HashMap::new(),
                        request_body: None,
                        response_headers: HashMap::new(),
                        response_body: None,
                        assertion_results: vec![],
                        errors: vec!["server error".into()],
                    },
                ],
            }],
        };
        let display = report.to_string();
        assert!(display.contains("group1"));
        assert!(display.contains("GET"));
        assert!(display.contains("/ok"));
        assert!(display.contains("POST"));
        assert!(display.contains("/err"));
        assert!(display.contains("server error"));
        assert!(display.contains("Total: 2"));
        assert!(display.contains("Passed: 1"));
        assert!(display.contains("Failed: 1"));
        assert!(display.contains("500ms"));
    }

    #[test]
    fn test_html_escape() {
        assert_eq!(html_escape("hello"), "hello");
        assert_eq!(html_escape("a&b"), "a&amp;b");
        assert_eq!(html_escape("<tag>"), "&lt;tag&gt;");
        assert_eq!(html_escape("\"quote\""), "&quot;quote&quot;");
        assert_eq!(html_escape("'single'"), "&#39;single&#39;");
        assert_eq!(
            html_escape("a&b<c>d\"e'f"),
            "a&amp;b&lt;c&gt;d&quot;e&#39;f"
        );
    }

    #[test]
    fn test_run_report_empty_groups() {
        let report = RunReport {
            plan_name: "empty".into(),
            total: 0,
            passed: 0,
            failed: 0,
            duration_ms: 0,
            groups: vec![],
        };
        let html = report.to_html();
        assert!(html.contains("0 tests"));
        assert!(html.contains("100.0%")); // pass rate defaults to 100% when total is 0
    }

    #[test]
    fn test_test_plan_serialization_roundtrip() {
        let plan = TestPlan {
            name: "test-plan".into(),
            base_url: "http://localhost".into(),
            default_headers: HashMap::from([("Authorization".into(), "token".into())]),
            steps: vec![
                Step::Request(RequestStep {
                    name: "r1".into(),
                    method: Method::Get,
                    url: "/health".into(),
                    headers: HashMap::new(),
                    body: None,
                    assert: vec![Assertion::Status(200)],
                    save_as: String::new(),
                    soft_fail: false,
                }),
                Step::Sequence(SequenceStep {
                    name: "seq".into(),
                    steps: vec![Step::Request(RequestStep {
                        name: "r2".into(),
                        method: Method::Post,
                        url: "/data".into(),
                        headers: HashMap::new(),
                        body: Some(serde_json::json!({"key": "value"})),
                        assert: vec![],
                        save_as: "response".into(),
                        soft_fail: true,
                    })],
                    continue_on_failure: true,
                }),
                Step::Script(ScriptStep {
                    name: "script".into(),
                    language: "rhai".into(),
                    source: "print(42);".into(),
                }),
                Step::Noop {
                    description: "placeholder".into(),
                },
            ],
            setup: vec![],
            teardown: vec![],
        };

        let json = serde_json::to_string_pretty(&plan).unwrap();
        let deserialized: TestPlan = serde_json::from_str(&json).unwrap();
        assert_eq!(deserialized.name, plan.name);
        assert_eq!(deserialized.base_url, plan.base_url);
        assert_eq!(deserialized.steps.len(), plan.steps.len());
        assert_eq!(deserialized.total_tests(), plan.total_tests());
    }

    #[test]
    fn test_test_spec_serialization_roundtrip() {
        // Test leaf variants individually (AllOf with Vec is a serde tagged enum limitation)
        let specs = vec![
            test_spec::TestSpec::Data(test_spec::DataSpec::default()),
            test_spec::TestSpec::Crud(test_spec::CrudSpec::default()),
            test_spec::TestSpec::Search(test_spec::SearchSpec::default()),
            test_spec::TestSpec::Negative(test_spec::NegativeSpec::default()),
            test_spec::TestSpec::EdgeCase(test_spec::EdgeCaseSpec::default()),
            test_spec::TestSpec::Conformance(test_spec::ConformanceSpec::default()),
            test_spec::TestSpec::Security(test_spec::SecuritySpec::default()),
            test_spec::TestSpec::Performance(test_spec::PerformanceSpec::default()),
            test_spec::TestSpec::Operation(test_spec::OperationSpec::default()),
        ];

        for spec in &specs {
            let json = serde_json::to_string_pretty(spec).unwrap();
            let _deserialized: test_spec::TestSpec = serde_json::from_str(&json).unwrap();
        }
    }

    #[test]
    fn test_step_serialization_roundtrip() {
        let steps = vec![
            Step::Request(RequestStep {
                name: "r".into(),
                method: Method::Get,
                url: "/".into(),
                headers: HashMap::new(),
                body: None,
                assert: vec![],
                save_as: String::new(),
                soft_fail: false,
            }),
            Step::Sequence(SequenceStep {
                name: "s".into(),
                steps: vec![],
                continue_on_failure: false,
            }),
            Step::Parallel(ParallelStep {
                steps: vec![Step::Request(RequestStep {
                    name: "r".into(),
                    method: Method::Get,
                    url: "/".into(),
                    headers: HashMap::new(),
                    body: None,
                    assert: vec![],
                    save_as: String::new(),
                    soft_fail: false,
                })],
            }),
            Step::Script(ScriptStep {
                name: "s".into(),
                language: "js".into(),
                source: "console.log('hi')".into(),
            }),
            Step::Noop {
                description: "skip".into(),
            },
        ];

        for step in &steps {
            let json = serde_json::to_string(step).unwrap();
            let deserialized: Step = serde_json::from_str(&json).unwrap();
            assert_eq!(deserialized.count_tests(), step.count_tests());
        }
    }

    #[test]
    fn test_method_serialization_roundtrip() {
        let methods = vec![
            Method::Get,
            Method::Post,
            Method::Put,
            Method::Delete,
            Method::Patch,
            Method::Head,
            Method::Options,
        ];

        for method in &methods {
            let json = serde_json::to_string(method).unwrap();
            let deserialized: Method = serde_json::from_str(&json).unwrap();
            assert_eq!(*method, deserialized);
        }
    }

    #[test]
    fn test_run_report_serialization_roundtrip() {
        let report = RunReport {
            plan_name: "test".into(),
            total: 2,
            passed: 1,
            failed: 1,
            duration_ms: 500,
            groups: vec![TestGroupResult {
                name: "g1".into(),
                total: 2,
                passed: 1,
                failed: 1,
                results: vec![TestResult {
                    name: "t1".into(),
                    passed: true,
                    status_code: 200,
                    request_method: "GET".into(),
                    request_url: "/ok".into(),
                    request_headers: HashMap::new(),
                    request_body: None,
                    response_headers: HashMap::new(),
                    response_body: None,
                    assertion_results: vec![],
                    errors: vec![],
                }],
            }],
        };

        let json = serde_json::to_string(&report).unwrap();
        let deserialized: RunReport = serde_json::from_str(&json).unwrap();
        assert_eq!(deserialized.plan_name, report.plan_name);
        assert_eq!(deserialized.total, report.total);
        assert_eq!(deserialized.passed, report.passed);
        assert_eq!(deserialized.failed, report.failed);
        assert_eq!(deserialized.groups.len(), report.groups.len());
    }

    #[test]
    fn test_run_report_to_html() {
        let report = RunReport {
            plan_name: "my-test-plan".into(),
            total: 5,
            passed: 4,
            failed: 1,
            duration_ms: 1234,
            groups: vec![TestGroupResult {
                name: "group1".into(),
                total: 5,
                passed: 4,
                failed: 1,
                results: vec![
                    TestResult {
                        name: "test1".into(),
                        passed: true,
                        status_code: 200,
                        request_method: "GET".into(),
                        request_url: "/api/health".into(),
                        request_headers: HashMap::new(),
                        request_body: None,
                        response_headers: HashMap::new(),
                        response_body: None,
                        assertion_results: vec![],
                        errors: vec![],
                    },
                    TestResult {
                        name: "test2".into(),
                        passed: false,
                        status_code: 500,
                        request_method: "POST".into(),
                        request_url: "/api/data".into(),
                        request_headers: HashMap::new(),
                        request_body: None,
                        response_headers: HashMap::new(),
                        response_body: None,
                        assertion_results: vec![],
                        errors: vec!["Internal server error".into()],
                    },
                ],
            }],
        };
        let html = report.to_html();
        assert!(html.contains("<!DOCTYPE html>"));
        assert!(html.contains("Test Run Report"));
        assert!(html.contains("my-test-plan"));
        assert!(html.contains("80.0%")); // 4/5 = 80%
        assert!(html.contains("20.0%")); // 1/5 = 20%
        assert!(html.contains("GET"));
        assert!(html.contains("/api/health"));
        assert!(html.contains("POST"));
        assert!(html.contains("/api/data"));
        assert!(html.contains("Internal server error"));
        assert!(html.contains("</html>"));
    }

    #[test]
    fn snapshot_run_report() {
        let report = RunReport {
            plan_name: "my-test-plan".into(),
            total: 5,
            passed: 4,
            failed: 1,
            duration_ms: 1234,
            groups: vec![TestGroupResult {
                name: "group1".into(),
                total: 5,
                passed: 4,
                failed: 1,
                results: vec![
                    TestResult {
                        name: "test1".into(),
                        passed: true,
                        status_code: 200,
                        request_method: "GET".into(),
                        request_url: "/api/health".into(),
                        request_headers: HashMap::new(),
                        request_body: None,
                        response_headers: HashMap::new(),
                        response_body: None,
                        assertion_results: vec![],
                        errors: vec![],
                    },
                    TestResult {
                        name: "test2".into(),
                        passed: false,
                        status_code: 500,
                        request_method: "POST".into(),
                        request_url: "/api/data".into(),
                        request_headers: HashMap::new(),
                        request_body: None,
                        response_headers: HashMap::new(),
                        response_body: None,
                        assertion_results: vec![],
                        errors: vec!["Internal server error".into()],
                    },
                ],
            }],
        };
        insta::assert_json_snapshot!(report);
    }
}
