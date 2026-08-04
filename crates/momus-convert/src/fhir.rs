use anyhow::Result;
use momus_core::ast::TestPlan;

/// Convert a FHIR Implementation Guide package to a TestPlan.
///
/// Reads a .tgz IG package, parses CapabilityStatements, StructureDefinitions,
/// SearchParameters, and OperationDefinitions, and generates a comprehensive
/// conformance test suite.
pub fn convert(_path: &str) -> Result<TestPlan> {
    anyhow::bail!("FHIR converter not yet implemented — coming in v0.2.0")
}
