use serde::{Deserialize, Serialize};

/// A single security issue found.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct GuardIssue {
    /// Endpoint or check name.
    pub endpoint: String,
    /// Category: auth, cors, leak, exposed, headers
    pub category: String,
    /// Severity: critical, high, medium, low, info
    pub severity: String,
    /// Human-readable description.
    pub description: String,
    /// Recommendation for fixing.
    pub recommendation: String,
}

/// Results of a security scan.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct GuardReport {
    /// Plan name.
    pub plan_name: String,
    /// Total checks performed.
    pub total_checks: usize,
    /// Security issues found.
    pub issues: Vec<GuardIssue>,
    /// Checks that passed.
    pub passed: usize,
    /// Checks that failed.
    pub failed: usize,
    /// Wall-clock duration in seconds.
    pub duration_secs: f64,
}

impl std::fmt::Display for GuardReport {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        writeln!(f, "── Security Scan: {} ──", self.plan_name)?;
        writeln!(
            f,
            "  Checks: {} passed, {} failed",
            self.passed, self.failed
        )?;
        writeln!(f, "  Duration: {:.1}s", self.duration_secs)?;
        if self.issues.is_empty() {
            writeln!(f, "  ✓ No issues found")?;
        } else {
            for issue in &self.issues {
                writeln!(
                    f,
                    "  [{}][{}] {} — {}",
                    issue.severity, issue.category, issue.endpoint, issue.description
                )?;
            }
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_guard_report_no_issues() {
        let report = GuardReport {
            plan_name: "test".into(),
            total_checks: 10,
            issues: vec![],
            passed: 10,
            failed: 0,
            duration_secs: 2.0,
        };
        let output = report.to_string();
        assert!(output.contains("No issues found"));
    }

    #[test]
    fn test_guard_report_with_issues() {
        let report = GuardReport {
            plan_name: "test".into(),
            total_checks: 10,
            issues: vec![GuardIssue {
                endpoint: "/api".into(),
                category: "headers".into(),
                severity: "high".into(),
                description: "Missing HSTS header".into(),
                recommendation: "Add Strict-Transport-Security header".into(),
            }],
            passed: 9,
            failed: 1,
            duration_secs: 2.0,
        };
        let output = report.to_string();
        assert!(output.contains("HSTS"));
        assert!(output.contains("high"));
    }
}
