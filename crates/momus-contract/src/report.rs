use serde::{Deserialize, Serialize};

/// A single contract violation.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ContractViolation {
    /// Endpoint path.
    pub endpoint: String,
    /// HTTP method.
    pub method: String,
    /// Status code received.
    pub status: u16,
    /// Description of the violation.
    pub description: String,
    /// Severity: error, warning, info.
    pub severity: String,
}

/// Results of a contract validation run.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ContractReport {
    /// Plan name.
    pub plan_name: String,
    /// Path to the spec file.
    pub spec_path: String,
    /// Total endpoints checked.
    pub total_endpoints: usize,
    /// Endpoints that passed validation.
    pub compliant: usize,
    /// Endpoints with violations.
    pub violations: usize,
    /// Compliance percentage (0.0–100.0).
    pub compliance_pct: f64,
    /// Wall-clock duration in seconds.
    pub duration_secs: f64,
    /// List of violations found.
    pub details: Vec<ContractViolation>,
}

impl std::fmt::Display for ContractReport {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        writeln!(f, "── Contract Validation: {} ──", self.plan_name)?;
        writeln!(f, "  Spec: {}", self.spec_path)?;
        writeln!(f, "  Compliance: {:.1}%", self.compliance_pct)?;
        writeln!(
            f,
            "  Compliant: {} / {}",
            self.compliant, self.total_endpoints
        )?;
        writeln!(f, "  Violations: {}", self.violations)?;
        writeln!(f, "  Duration: {:.1}s", self.duration_secs)?;
        for v in &self.details {
            writeln!(
                f,
                "  [{}] {} {} — {}",
                v.severity, v.method, v.endpoint, v.description
            )?;
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_contract_report_display() {
        let report = ContractReport {
            plan_name: "petstore".into(),
            spec_path: "openapi.yaml".into(),
            total_endpoints: 10,
            compliant: 8,
            violations: 2,
            compliance_pct: 80.0,
            duration_secs: 5.2,
            details: vec![ContractViolation {
                endpoint: "/pets/{id}".into(),
                method: "GET".into(),
                status: 200,
                description: "missing field: 'tags'".into(),
                severity: "error".into(),
            }],
        };
        let output = report.to_string();
        assert!(output.contains("80.0%"));
        assert!(output.contains("tags"));
    }
}
