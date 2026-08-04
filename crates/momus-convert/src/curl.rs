use anyhow::Result;
use momus_core::ast::TestPlan;

/// Convert a cURL command to a TestPlan.
///
/// Parses a curl command string and produces a single RequestStep
/// with method, URL, headers, and body extracted from the flags.
pub fn convert(_command: &str) -> Result<TestPlan> {
    anyhow::bail!("cURL converter not yet implemented — coming in v0.2.0")
}
