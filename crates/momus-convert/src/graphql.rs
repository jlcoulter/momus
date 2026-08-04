use anyhow::Result;
use momus_core::ast::TestPlan;

/// Convert a GraphQL schema (SDL or introspection) to a TestPlan.
///
/// Reads a .graphql/.gql SDL file or introspects an endpoint, generates
/// queries for each type and mutations for each mutation field.
pub fn convert(_path: &str) -> Result<TestPlan> {
    anyhow::bail!("GraphQL converter not yet implemented — coming in v0.3.0")
}
