use anyhow::Result;
use momus_core::ast::TestPlan;

/// Convert a gRPC service definition to a TestPlan.
///
/// Reads .proto files or connects to a gRPC reflection endpoint,
/// generates a test for each RPC method with valid protobuf messages.
pub fn convert(_path: &str) -> Result<TestPlan> {
    anyhow::bail!("gRPC converter not yet implemented — coming in v0.4.0")
}
