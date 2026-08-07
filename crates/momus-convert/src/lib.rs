use anyhow::Result;
use momus_core::ast::TestPlan;
#[cfg(feature = "fhir")]
use std::path::Path;

/// Convert an API description into a Momus test plan.
///
/// # Arguments
///
/// * `format` - The input format: "openapi", "postman", "har", "curl", "graphql", "grpc", "fhir"
/// * `input` - The input data (file path or raw string for curl)
/// * `seed_data` - If true, generate seed data setup steps that pre-populate the server
///   with resources so GET/PUT/DELETE tests have data to operate on
///
/// # Errors
///
/// Returns an error if the format is unknown, the input cannot be read, or conversion fails.
pub fn convert(format: &str, input: &str, seed_data: bool) -> Result<TestPlan> {
    let mut plan = match format {
        #[cfg(feature = "openapi")]
        "openapi" => openapi::convert(input)?,
        #[cfg(feature = "postman")]
        "postman" => postman::convert(input)?,
        #[cfg(feature = "har")]
        "har" => har::convert(input)?,
        #[cfg(feature = "curl")]
        "curl" => curl::convert(input)?,
        #[cfg(feature = "graphql")]
        "graphql" => graphql::convert(input)?,
        #[cfg(feature = "grpc")]
        "grpc" => grpc::convert(input)?,
        #[cfg(feature = "fhir")]
        "fhir" => fhir::convert(input)?,
        _ => {
            let available = available_formats();
            anyhow::bail!(
                "Unknown format '{}'. Available formats: {}",
                format,
                available.join(", ")
            )
        }
    };

    if seed_data {
        let seed_steps = match format {
            #[cfg(feature = "openapi")]
            "openapi" => openapi::generate_seed_data(input)?,
            #[cfg(feature = "postman")]
            "postman" => postman::generate_seed_data(input)?,
            #[cfg(feature = "har")]
            "har" => har::generate_seed_data(input)?,
            #[cfg(feature = "curl")]
            "curl" => curl::generate_seed_data(input)?,
            #[cfg(feature = "graphql")]
            "graphql" => graphql::generate_seed_data(input)?,
            #[cfg(feature = "grpc")]
            "grpc" => grpc::generate_seed_data(input)?,
            #[cfg(feature = "fhir")]
            "fhir" => fhir::generate_seed_data(input)?,
            _ => vec![],
        };
        plan.setup = seed_steps;
    }

    Ok(plan)
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

/// Validate a JSON resource against a profile from an IG package.
///
/// Parses the IG package, finds the matching profile (by explicit URL or
/// auto-detected by resource type), and validates the resource against it.
///
/// Only available when the `fhir` feature is enabled.
#[cfg(feature = "fhir")]
pub fn fhir_validate_resource(
    package_path: &str,
    resource_path: &str,
    profile_url: Option<&str>,
) -> Result<()> {
    fhir::validate_resource(package_path, resource_path, profile_url)
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
