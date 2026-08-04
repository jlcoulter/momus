use serde::{Deserialize, Serialize};

/// A single diff between baseline and target responses.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DiffEntry {
    /// Endpoint path.
    pub endpoint: String,
    /// HTTP method.
    pub method: String,
    /// Type of change: added, removed, modified
    pub change_type: String,
    /// The field path that changed (e.g. "$.data.user.name").
    pub field: String,
    /// Value in the baseline.
    pub baseline: Option<serde_json::Value>,
    /// Value in the target.
    pub target: Option<serde_json::Value>,
}

/// Results of a diff run.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DiffReport {
    /// Plan name.
    pub plan_name: String,
    /// Baseline URL.
    pub baseline_url: String,
    /// Target URL.
    pub target_url: String,
    /// Total endpoints compared.
    pub total_endpoints: usize,
    /// Endpoints with identical responses.
    pub identical: usize,
    /// Endpoints with differences.
    pub different: usize,
    /// Fields present in target but not baseline.
    pub fields_added: usize,
    /// Fields present in baseline but not target.
    pub fields_removed: usize,
    /// Fields with different values.
    pub fields_modified: usize,
    /// Wall-clock duration in seconds.
    pub duration_secs: f64,
    /// List of diffs found.
    pub diffs: Vec<DiffEntry>,
}

impl std::fmt::Display for DiffReport {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        writeln!(f, "── Diff Report: {} ──", self.plan_name)?;
        writeln!(f, "  Baseline: {}", self.baseline_url)?;
        writeln!(f, "  Target:   {}", self.target_url)?;
        writeln!(
            f,
            "  Endpoints: {} identical, {} different",
            self.identical, self.different
        )?;
        writeln!(
            f,
            "  Fields: {} added, {} removed, {} modified",
            self.fields_added, self.fields_removed, self.fields_modified
        )?;
        writeln!(f, "  Duration: {:.1}s", self.duration_secs)?;
        for d in &self.diffs {
            writeln!(
                f,
                "  [{}] {} {} — {}: {:?} → {:?}",
                d.change_type, d.method, d.endpoint, d.field, d.baseline, d.target
            )?;
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_diff_report_display() {
        let report = DiffReport {
            plan_name: "migration".into(),
            baseline_url: "https://api-v1.example.com".into(),
            target_url: "https://api-v2.example.com".into(),
            total_endpoints: 5,
            identical: 3,
            different: 2,
            fields_added: 3,
            fields_removed: 1,
            fields_modified: 2,
            duration_secs: 10.0,
            diffs: vec![DiffEntry {
                endpoint: "/users".into(),
                method: "GET".into(),
                change_type: "added".into(),
                field: "$.data[0].email".into(),
                baseline: None,
                target: Some(serde_json::json!("test@example.com")),
            }],
        };
        let output = report.to_string();
        assert!(output.contains("3 added"));
        assert!(output.contains("api-v1"));
    }
}
