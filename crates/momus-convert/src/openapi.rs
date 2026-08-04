use anyhow::Result;
use momus_core::ast::TestPlan;

/// Convert an OpenAPI 3.x spec to a TestPlan.
///
/// Reads a YAML or JSON OpenAPI spec file, walks paths and schemas,
/// and generates CRUD sequences with response assertions.
pub fn convert(_path: &str) -> Result<TestPlan> {
    anyhow::bail!("OpenAPI converter not yet implemented — coming in v0.2.0")
}
