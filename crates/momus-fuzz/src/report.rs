use serde::{Deserialize, Serialize};

/// Results of a fuzz run.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct FuzzReport {
    /// Plan name.
    pub plan_name: String,
    /// Total mutations generated and sent.
    pub total_mutations: u64,
    /// Mutations that passed (server accepted, no error).
    pub passed: u64,
    /// Mutations that were rejected (4xx).
    pub rejected: u64,
    /// Mutations that caused server errors (5xx).
    pub errors: u64,
    /// Mutations that leaked information (stack traces, SQL errors, etc.).
    pub leaks: u64,
    /// Wall-clock duration in seconds.
    pub duration_secs: f64,
    /// Names of mutators that were applied.
    pub mutators_applied: Vec<String>,
}

impl std::fmt::Display for FuzzReport {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        writeln!(f, "── Fuzz Report: {} ──", self.plan_name)?;
        writeln!(f, "  Total mutations: {}", self.total_mutations)?;
        writeln!(f, "  Passed: {}", self.passed)?;
        writeln!(f, "  Rejected: {}", self.rejected)?;
        writeln!(f, "  Errors: {}", self.errors)?;
        writeln!(f, "  Leaks detected: {}", self.leaks)?;
        writeln!(f, "  Duration: {:.1}s", self.duration_secs)?;
        if !self.mutators_applied.is_empty() {
            writeln!(f, "  Mutators: {}", self.mutators_applied.join(", "))?;
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_fuzz_report_display() {
        let report = FuzzReport {
            plan_name: "test".into(),
            total_mutations: 500,
            passed: 400,
            rejected: 95,
            errors: 5,
            leaks: 0,
            duration_secs: 12.5,
            mutators_applied: vec!["boundary".into(), "encoding".into()],
        };
        let output = report.to_string();
        assert!(output.contains("500"));
        assert!(output.contains("boundary"));
    }
}
