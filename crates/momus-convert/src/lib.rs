use anyhow::Result;
use momus_core::ast::TestPlan;
use std::path::Path;

/// Convert an API description into a Momus test plan.
///
/// # Arguments
///
/// * `format` - The input format: "openapi", "postman", "har", "curl", "graphql", "grpc", "fhir"
/// * `input` - The input data (file path or raw string for curl)
///
/// # Errors
///
/// Returns an error if the format is unknown, the input cannot be read, or conversion fails.
pub fn convert(format: &str, input: &str) -> Result<TestPlan> {
    match format {
        #[cfg(feature = "openapi")]
        "openapi" => openapi::convert(input),
        #[cfg(feature = "postman")]
        "postman" => postman::convert(input),
        #[cfg(feature = "har")]
        "har" => har::convert(input),
        #[cfg(feature = "curl")]
        "curl" => curl::convert(input),
        #[cfg(feature = "graphql")]
        "graphql" => graphql::convert(input),
        #[cfg(feature = "grpc")]
        "grpc" => grpc::convert(input),
        #[cfg(feature = "fhir")]
        "fhir" => fhir::convert(input),
        _ => {
            let available = available_formats();
            anyhow::bail!(
                "Unknown format '{}'. Available formats: {}",
                format,
                available.join(", ")
            )
        }
    }
}

/// List all available converter formats based on enabled features.
#[allow(clippy::vec_init_then_push)]
pub fn available_formats() -> Vec<&'static str> {
    let mut formats = Vec::new();
    #[cfg(feature = "openapi")]
    formats.push("openapi");
    #[cfg(feature = "postman")]
    formats.push("postman");
    #[cfg(feature = "har")]
    formats.push("har");
    #[cfg(feature = "curl")]
    formats.push("curl");
    #[cfg(feature = "graphql")]
    formats.push("graphql");
    #[cfg(feature = "grpc")]
    formats.push("grpc");
    #[cfg(feature = "fhir")]
    formats.push("fhir");
    formats
}

/// Generate bulk FHIR test data (NDJSON) from an IG package.
///
/// Parses the IG package at `package_path`, extracts resource types and their
/// profile URLs from the CapabilityStatement, and generates NDJSON files
/// under `output_dir/data/`.
///
/// Only available when the `fhir` feature is enabled.
#[cfg(feature = "fhir")]
pub fn generate_fhir_bulk_test_data(
    package_path: &str,
    count: u64,
    output_dir: &Path,
) -> Result<()> {
    fhir::generate_bulk_test_data(package_path, count, output_dir)
}

// ---------------------------------------------------------------------------
// Feature-gated modules — each converts one format to a TestPlan
// ---------------------------------------------------------------------------

#[cfg(feature = "openapi")]
mod openapi;

#[cfg(feature = "postman")]
mod postman;

#[cfg(feature = "har")]
mod har;

#[cfg(feature = "curl")]
mod curl;

#[cfg(feature = "graphql")]
mod graphql;

#[cfg(feature = "grpc")]
mod grpc;

#[cfg(feature = "fhir")]
mod fhir;
