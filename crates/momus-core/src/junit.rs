use std::io::Write;

use crate::ast::{RunReport, TestGroupResult, TestResult};

/// Write a JUnit XML report for the given `RunReport` to the provided writer.
///
/// The output follows the standard JUnit XML schema consumed by Jenkins,
/// GitLab CI, Azure DevOps, and other CI systems.
pub fn write_junit_xml<W: Write>(writer: &mut W, report: &RunReport) -> std::io::Result<()> {
    writeln!(writer, r#"<?xml version="1.0" encoding="UTF-8"?>"#)?;
    writeln!(
        writer,
        r#"<testsuites name="{}" tests="{}" failures="{}" errors="0">"#,
        escape_xml(&report.plan_name),
        report.total,
        report.failed,
    )?;

    for group in &report.groups {
        write_testsuite(writer, group)?;
    }

    writeln!(writer, "</testsuites>")?;
    Ok(())
}

fn write_testsuite<W: Write>(writer: &mut W, group: &TestGroupResult) -> std::io::Result<()> {
    writeln!(
        writer,
        r#"  <testsuite name="{}" tests="{}" failures="{}" errors="0">"#,
        escape_xml(&group.name),
        group.total,
        group.failed,
    )?;

    for result in &group.results {
        write_testcase(writer, &group.name, result)?;
    }

    writeln!(writer, "  </testsuite>")?;
    Ok(())
}

fn write_testcase<W: Write>(
    writer: &mut W,
    classname: &str,
    result: &TestResult,
) -> std::io::Result<()> {
    if result.passed {
        writeln!(
            writer,
            r#"    <testcase name="{}" classname="{}"/>"#,
            escape_xml(&result.name),
            escape_xml(classname),
        )?;
    } else {
        // Collect all error messages
        let mut messages: Vec<String> = Vec::new();
        if !result.errors.is_empty() {
            messages.extend(result.errors.clone());
        }
        for ar in &result.assertion_results {
            if !ar.passed
                && let Some(msg) = &ar.message
            {
                messages.push(msg.clone());
            }
        }
        let message = if messages.is_empty() {
            format!(
                "Test failed: {} {} returned status {}",
                result.request_method, result.request_url, result.status_code
            )
        } else {
            messages.join("; ")
        };

        writeln!(
            writer,
            r#"    <testcase name="{}" classname="{}">"#,
            escape_xml(&result.name),
            escape_xml(classname),
        )?;
        writeln!(
            writer,
            r#"      <failure message="{}" type="AssertionFailure"/>"#,
            escape_xml(&message),
        )?;
        writeln!(writer, "    </testcase>")?;
    }
    Ok(())
}

/// Escape special XML characters in a string.
fn escape_xml(s: &str) -> String {
    let mut result = String::with_capacity(s.len());
    for ch in s.chars() {
        match ch {
            '&' => result.push_str("&amp;"),
            '<' => result.push_str("&lt;"),
            '>' => result.push_str("&gt;"),
            '"' => result.push_str("&quot;"),
            '\'' => result.push_str("&apos;"),
            c => result.push(c),
        }
    }
    result
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::ast::{RunReport, TestGroupResult, TestResult};
    use std::collections::HashMap;

    #[test]
    fn test_write_junit_xml_passing() {
        let report = RunReport {
            plan_name: "my-plan".into(),
            total: 1,
            passed: 1,
            failed: 0,
            duration_ms: 100,
            groups: vec![TestGroupResult {
                name: "group1".into(),
                total: 1,
                passed: 1,
                failed: 0,
                results: vec![TestResult {
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
                }],
            }],
        };

        let mut buf = Vec::new();
        write_junit_xml(&mut buf, &report).unwrap();
        let output = String::from_utf8(buf).unwrap();

        assert!(output.contains(r#"<?xml version="1.0" encoding="UTF-8"?>"#));
        assert!(
            output.contains(r#"<testsuites name="my-plan" tests="1" failures="0" errors="0">"#)
        );
        assert!(output.contains(r#"<testsuite name="group1" tests="1" failures="0" errors="0">"#));
        assert!(output.contains(r#"<testcase name="test1" classname="group1"/>"#));
        assert!(output.contains("</testsuites>"));
    }

    #[test]
    fn test_write_junit_xml_failing() {
        let report = RunReport {
            plan_name: "my-plan".into(),
            total: 1,
            passed: 0,
            failed: 1,
            duration_ms: 100,
            groups: vec![TestGroupResult {
                name: "group1".into(),
                total: 1,
                passed: 0,
                failed: 1,
                results: vec![TestResult {
                    name: "test1".into(),
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
                }],
            }],
        };

        let mut buf = Vec::new();
        write_junit_xml(&mut buf, &report).unwrap();
        let output = String::from_utf8(buf).unwrap();

        assert!(
            output.contains(r#"<testsuites name="my-plan" tests="1" failures="1" errors="0">"#)
        );
        assert!(output.contains(r#"<testsuite name="group1" tests="1" failures="1" errors="0">"#));
        assert!(output.contains(r#"<testcase name="test1" classname="group1">"#));
        assert!(
            output
                .contains(r#"<failure message="Internal server error" type="AssertionFailure"/>"#)
        );
        assert!(output.contains("</testcase>"));
    }

    #[test]
    fn test_escape_xml() {
        assert_eq!(escape_xml("hello"), "hello");
        assert_eq!(escape_xml("a & b"), "a &amp; b");
        assert_eq!(escape_xml("<tag>"), "&lt;tag&gt;");
        assert_eq!(escape_xml("\"quote\""), "&quot;quote&quot;");
        assert_eq!(escape_xml("'single'"), "&apos;single&apos;");
    }
}
