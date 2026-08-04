use serde::{Deserialize, Serialize};

/// Results of a single chaos experiment.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ChaosReport {
    /// Experiment type name.
    pub experiment: String,
    /// Target endpoint or resource.
    pub target: String,
    /// How long the fault was active in seconds.
    pub duration_secs: f64,
    /// Number of requests that hit the fault.
    pub requests_affected: u64,
    /// Number of failures observed during the fault window.
    pub failures_during: u64,
    /// Whether the system returned to normal after the fault was removed.
    pub self_healed: bool,
    /// Human-readable details.
    pub details: String,
}

impl std::fmt::Display for ChaosReport {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        let status = if self.self_healed { "✓" } else { "✗" };
        writeln!(f, "── Chaos Experiment: {} ──", self.experiment)?;
        writeln!(f, "  Target: {}", self.target)?;
        writeln!(f, "  Duration: {:.1}s", self.duration_secs)?;
        writeln!(f, "  Requests affected: {}", self.requests_affected)?;
        writeln!(f, "  Failures during fault: {}", self.failures_during)?;
        writeln!(f, "  Self-healed: {}", status)?;
        if !self.details.is_empty() {
            writeln!(f, "  Details: {}", self.details)?;
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_chaos_report_display() {
        let report = ChaosReport {
            experiment: "network_latency".into(),
            target: "/api/slow".into(),
            duration_secs: 30.0,
            requests_affected: 150,
            failures_during: 3,
            self_healed: true,
            details: "latency injected, system recovered after fault removed".into(),
        };
        let output = report.to_string();
        assert!(output.contains("network_latency"));
        assert!(output.contains("150"));
        assert!(output.contains("✓"));
    }

    #[test]
    fn test_chaos_report_not_healed() {
        let report = ChaosReport {
            experiment: "service_down".into(),
            target: "/api/db".into(),
            duration_secs: 60.0,
            requests_affected: 500,
            failures_during: 500,
            self_healed: false,
            details: "service did not recover after fault removed".into(),
        };
        let output = report.to_string();
        assert!(output.contains("✗"));
    }
}
