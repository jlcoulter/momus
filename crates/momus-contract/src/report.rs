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

/// Field-level coverage information.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct FieldCoverage {
    /// Endpoint path.
    pub endpoint: String,
    /// HTTP method.
    pub method: String,
    /// Field path (e.g. "$.status", "$.data.health").
    pub field_path: String,
    /// Whether the field was exercised in the response.
    pub exercised: bool,
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
    /// Per-endpoint compliance percentage.
    #[serde(default)]
    pub endpoint_compliance: Vec<EndpointCompliance>,
    /// Field-level coverage analysis.
    #[serde(default)]
    pub field_coverage: Vec<FieldCoverage>,
    /// Undocumented fields detected in responses.
    #[serde(default)]
    pub undocumented_fields: Vec<String>,
    /// Wall-clock duration in seconds.
    pub duration_secs: f64,
    /// List of violations found.
    pub details: Vec<ContractViolation>,
}

/// Per-endpoint compliance breakdown.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct EndpointCompliance {
    /// Endpoint path.
    pub endpoint: String,
    /// HTTP method.
    pub method: String,
    /// Whether this endpoint passed all checks.
    pub passed: bool,
    /// Compliance percentage for this endpoint (0.0–100.0).
    pub pct: f64,
    /// Number of checks that passed.
    pub checks_passed: usize,
    /// Number of checks that failed.
    pub checks_failed: usize,
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

        // Per-endpoint breakdown
        if !self.endpoint_compliance.is_empty() {
            writeln!(f)?;
            writeln!(f, "  Per-Endpoint Breakdown:")?;
            for ec in &self.endpoint_compliance {
                let icon = if ec.passed { "✓" } else { "✗" };
                writeln!(
                    f,
                    "    {} {} {} — {:.0}% ({}/{})",
                    icon,
                    ec.method,
                    ec.endpoint,
                    ec.pct,
                    ec.checks_passed,
                    ec.checks_passed + ec.checks_failed
                )?;
            }
        }

        // Field coverage summary
        if !self.field_coverage.is_empty() {
            let exercised = self.field_coverage.iter().filter(|f| f.exercised).count();
            let total = self.field_coverage.len();
            let coverage_pct = if total > 0 {
                (exercised as f64 / total as f64) * 100.0
            } else {
                0.0
            };
            writeln!(f)?;
            writeln!(
                f,
                "  Field Coverage: {coverage_pct:.0}% ({exercised}/{total})"
            )?;
        }

        // Undocumented fields
        if !self.undocumented_fields.is_empty() {
            writeln!(f)?;
            writeln!(
                f,
                "  Undocumented Fields: {}",
                self.undocumented_fields.len()
            )?;
            for field in &self.undocumented_fields {
                writeln!(f, "    - {field}")?;
            }
        }

        // Violations
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
            endpoint_compliance: vec![EndpointCompliance {
                endpoint: "/pets/{id}".into(),
                method: "GET".into(),
                passed: false,
                pct: 50.0,
                checks_passed: 1,
                checks_failed: 1,
            }],
            field_coverage: vec![
                FieldCoverage {
                    endpoint: "/pets/{id}".into(),
                    method: "GET".into(),
                    field_path: "$.id".into(),
                    exercised: true,
                },
                FieldCoverage {
                    endpoint: "/pets/{id}".into(),
                    method: "GET".into(),
                    field_path: "$.name".into(),
                    exercised: false,
                },
            ],
            undocumented_fields: vec!["$.extra".into()],
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
        assert!(output.contains("Field Coverage"));
        assert!(output.contains("Undocumented Fields"));
        assert!(output.contains("Per-Endpoint"));
    }

    #[test]
    fn test_field_coverage_serde() {
        let fc = FieldCoverage {
            endpoint: "/health".into(),
            method: "GET".into(),
            field_path: "$.status".into(),
            exercised: true,
        };
        let json = serde_json::to_string(&fc).unwrap();
        let deserialized: FieldCoverage = serde_json::from_str(&json).unwrap();
        assert_eq!(deserialized.field_path, "$.status");
        assert!(deserialized.exercised);
    }

    #[test]
    fn test_endpoint_compliance_serde() {
        let ec = EndpointCompliance {
            endpoint: "/health".into(),
            method: "GET".into(),
            passed: true,
            pct: 100.0,
            checks_passed: 3,
            checks_failed: 0,
        };
        let json = serde_json::to_string(&ec).unwrap();
        let deserialized: EndpointCompliance = serde_json::from_str(&json).unwrap();
        assert!(deserialized.passed);
        assert_eq!(deserialized.pct, 100.0);
    }
}
