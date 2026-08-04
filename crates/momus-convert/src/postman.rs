use anyhow::Result;
use momus_core::ast::TestPlan;

/// Convert a Postman Collection v2.1 to a TestPlan.
///
/// Reads a Postman collection JSON file, maps items to RequestSteps,
/// folders to SequenceSteps, and pm.* test assertions to Assertion nodes.
pub fn convert(_path: &str) -> Result<TestPlan> {
    anyhow::bail!("Postman converter not yet implemented — coming in v0.2.0")
}
