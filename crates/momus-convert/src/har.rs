use anyhow::Result;
use momus_core::ast::TestPlan;

/// Convert a HAR (HTTP Archive) file to a TestPlan.
///
/// Reads a HAR JSON file, maps each log entry to a RequestStep with
/// a Status assertion matching the recorded response.
pub fn convert(_path: &str) -> Result<TestPlan> {
    anyhow::bail!("HAR converter not yet implemented — coming in v0.2.0")
}
