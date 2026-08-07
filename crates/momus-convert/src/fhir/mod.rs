#![allow(dead_code, clippy::type_complexity)]

//! FHIR IG package to TestPlan converter.
//!
//! This module ports the fhir-autotest pipeline into momus:
//! 1. Parse IG package (.tgz) → extract FHIR resources
//! 2. Select CapabilityStatement → determine server capabilities
//! 3. Generate test plan with CRUD, search, and conformance tests

pub mod api_model;
pub mod assertions;
pub mod bulk_data;
pub mod bulk_loader;
pub mod capability;
pub mod hcpd;
pub mod operation;
pub mod package;
pub mod planner;
pub mod profile;
pub mod profile_resolver;
pub mod resource_gen;
pub mod resource_generator;
pub mod response_assertions;
pub mod search_param;
pub mod test_helpers;
pub mod test_model;
pub mod validator;
pub mod value_resolver;
pub mod valuesets;

// Re-export the FHIR resource generator for external use
pub use self::resource_generator::FhirResourceGenerator;

use anyhow::{Context, Result};
use momus_core::ast::*;
use momus_core::engine::test_generator;
use std::collections::HashMap;
use std::path::Path;

/// Convert a FHIR Implementation Guide package to a TestPlan.
///
/// Uses the format-agnostic `TestGenerator` engine:
/// 1. Parse IG package → extract FHIR resources
/// 2. Convert to `ApiModel` (format-agnostic intermediate representation)
/// 3. Create a `FhirResourceGenerator` for profile-conformant resource generation
/// 4. Apply a `TestSpec` to generate a comprehensive test plan
///
/// The generated plan includes:
/// - **Setup steps** that POST valid profile-conformant resources to populate
///   the server before any tests run.
/// - **CRUD sequences** that chain create → read → update → delete with
///   state passing via `{steps.<name>.*}` template references.
/// - **Search tests** with concrete values extracted from the generated
///   resources, so searches return results.
/// - **Negative tests** for undeclared interactions and invalid inputs.
/// - **Conformance tests** for profile validation.
pub fn convert(path: &str) -> Result<TestPlan> {
    let pkg = package::parse_package(path)?;

    // Step 1: Convert FHIR IG to format-agnostic ApiModel
    let api = api_model::package_to_api_model(&pkg);

    // Step 2: Create FHIR resource generator
    let generator = FhirResourceGenerator::new(&pkg.structure_definitions, &pkg.raw_resources);

    // Step 3: Define the test specification
    // This controls what tests are generated and with what data variations.
    let spec = TestSpec::AllOf(vec![
        TestSpec::Data(DataSpec {
            count: 3,
            variations: vec![
                DataVariation::HappyPath,
                DataVariation::Minimal,
                DataVariation::ToBeDeleted,
            ],
        }),
        TestSpec::Crud(CrudSpec {
            create: true,
            read: true,
            vread: true,
            update: true,
            delete: true,
            patch: true,
            history_instance: true,
            history_type: true,
            chain: true,
            ..CrudSpec::default()
        }),
        TestSpec::Search(SearchSpec {
            single_param: true,
            modifiers: true,
            prefixes: true,
            chained: true,
            combined_params: true,
            include: true,
            revinclude: true,
            result_params: vec![
                "_summary=true".to_string(),
                "_count=1".to_string(),
                "_elements=id".to_string(),
                "_sort=_id".to_string(),
                "_filter=name eq test".to_string(),
                "_tag=http://example.org/tag|test".to_string(),
                "_profile=http://example.org/StructureDefinition/Test".to_string(),
                "_security=http://example.org/security|test".to_string(),
                "_type=Patient".to_string(),
                "_language=en".to_string(),
                "_has:Provenance:target".to_string(),
            ],
            negative_values: vec!["NONEXISTENT".to_string()],
        }),
        TestSpec::Negative(NegativeSpec::default()),
        TestSpec::Conformance(ConformanceSpec::default()),
        TestSpec::Operation(OperationSpec::default()),
    ]);

    // Step 4: Generate the test plan using the format-agnostic engine
    let mut plan = test_generator::generate_test_plan(&api, &spec, &generator)?;

    // Step 5: Add FHIR-specific search tests (near, chained, _has)
    let fhir_extra = generate_fhir_specific_search_tests(&api, &pkg);
    plan.steps.extend(fhir_extra);

    // Customize the plan name to reflect the FHIR IG source
    let file_name = path.rsplit('/').next().unwrap_or(path);
    plan.name = format!(
        "FHIR IG: {} resources, {} tests from {}",
        api.resources.len(),
        plan.total_tests(),
        file_name,
    );

    Ok(plan)
}

/// Generate FHIR-specific search tests that are not covered by the
/// format-agnostic TestGenerator engine.
///
/// These include:
/// - **Near/proximity** search for location/quantity params (`:near`, `:within`)
/// - **Chained** search across resource references (`param.reference:chain`)
/// - **_has** reverse chaining search
fn generate_fhir_specific_search_tests(api: &ApiModel, pkg: &package::IgPackage) -> Vec<Step> {
    let mut steps = Vec::new();

    for resource_model in &api.resources {
        let rtype = &resource_model.name;
        let rtype_lower = rtype.to_lowercase();

        let has_search = resource_model
            .operations
            .iter()
            .any(|op| op.name == "search-type");

        if !has_search {
            continue;
        }

        for sp in &resource_model.search_params {
            // --- Near/proximity search tests ---
            if matches!(sp.param_type.as_str(), "quantity" | "location" | "special") {
                // :near modifier
                steps.push(Step::Request(RequestStep {
                    name: format!("search-{}-{}-near", rtype_lower, sp.name),
                    method: Method::Get,
                    url: format!("/{}?{}:near=40.0|-74.0|10.0", rtype, sp.name),
                    headers: HashMap::new(),
                    body: None,
                    assert: vec![Assertion::Status(200)],
                    save_as: String::new(),
                    soft_fail: false,
                }));

                // :within modifier
                steps.push(Step::Request(RequestStep {
                    name: format!("search-{}-{}-within", rtype_lower, sp.name),
                    method: Method::Get,
                    url: format!("/{}?{}:within=40.0|-74.0|10.0", rtype, sp.name),
                    headers: HashMap::new(),
                    body: None,
                    assert: vec![Assertion::Status(200)],
                    save_as: String::new(),
                    soft_fail: false,
                }));
            }

            // --- Chained search tests ---
            if sp.param_type == "reference" || sp.param_type == "string" {
                // Find the SearchParameter definition to get the expression
                let expression = pkg
                    .search_parameters
                    .iter()
                    .find(|s| s.code == sp.name && s.base.contains(&rtype.to_string()))
                    .and_then(|s| s.expression.as_deref())
                    .unwrap_or("");

                let chainable = !expression.is_empty() && expression.contains('.');

                if chainable {
                    // Extract chain prefix from expression
                    let chain_prefix = expression
                        .split('.')
                        .next()
                        .unwrap_or("")
                        .split_whitespace()
                        .next()
                        .unwrap_or("")
                        .trim_end_matches('|');

                    if !chain_prefix.is_empty() {
                        let chain_param = format!("{}.{}", chain_prefix, sp.name);
                        steps.push(Step::Request(RequestStep {
                            name: format!("search-{}-{}-chained", rtype_lower, sp.name),
                            method: Method::Get,
                            url: format!("/{rtype}?{chain_param}={{value}}"),
                            headers: HashMap::new(),
                            body: None,
                            assert: vec![Assertion::Status(200)],
                            save_as: String::new(),
                            soft_fail: false,
                        }));
                    }
                }
            }

            // --- _has (reverse chaining) tests ---
            if sp.param_type == "reference" {
                // Find resource types that reference this type
                for other_resource in &api.resources {
                    for other_sp in &other_resource.search_params {
                        if other_sp.param_type == "reference" {
                            // Check if this reference param could point to our type
                            let target_type = capitalize_first(&other_sp.name);
                            if target_type == *rtype {
                                steps.push(Step::Request(RequestStep {
                                    name: format!(
                                        "search-{}-has-{}-{}",
                                        rtype_lower, other_resource.name, other_sp.name
                                    ),
                                    method: Method::Get,
                                    url: format!(
                                        "/{}?_has:{}:{}=test",
                                        rtype, other_resource.name, other_sp.name
                                    ),
                                    headers: HashMap::new(),
                                    body: None,
                                    assert: vec![Assertion::Status(200)],
                                    save_as: String::new(),
                                    soft_fail: false,
                                }));
                            }
                        }
                    }
                }
            }
        }
    }

    steps
}

fn capitalize_first(s: &str) -> String {
    let mut chars = s.chars();
    match chars.next() {
        Some(c) => c.to_uppercase().to_string() + chars.as_str(),
        None => String::new(),
    }
}

/// Generate seed data setup steps from a FHIR IG package.
///
/// Generates valid profile-conformant FHIR resources and returns them as
/// POST request steps that populate the server before tests run.
pub fn generate_seed_data(path: &str) -> Result<Vec<Step>> {
    let pkg = package::parse_package(path)?;
    let api = api_model::package_to_api_model(&pkg);
    let generator = FhirResourceGenerator::new(&pkg.structure_definitions, &pkg.raw_resources);

    // Generate 3 resources per type with variations
    let data_spec = DataSpec {
        count: 3,
        variations: vec![
            DataVariation::HappyPath,
            DataVariation::Minimal,
            DataVariation::ToBeDeleted,
        ],
    };

    let data = test_generator::generate_data(&api, &data_spec, &generator)?;
    let steps = test_generator::generate_setup_steps(&api, &data);

    Ok(steps)
}

fn select_capability_statement(
    pkg: &package::IgPackage,
) -> Result<capability::CapabilityStatement> {
    pkg.capability_statements
        .iter()
        .find(|cs| {
            cs.rest
                .iter()
                .any(|r| r.mode == "server" && !r.resource.is_empty())
        })
        .or_else(|| {
            pkg.capability_statements
                .iter()
                .find(|cs| cs.rest.iter().any(|r| !r.resource.is_empty()))
        })
        .or(pkg.capability_statements.first())
        .cloned()
        .context("No CapabilityStatement found in IG package")
}

/// Generate bulk FHIR test data (NDJSON) from an IG package.
///
/// Parses the IG package at `package_path`, extracts resource types and their
/// profile URLs from the CapabilityStatement, and generates NDJSON files
/// under `output_dir/data/` — one per resource type, plus `combined.ndjson`
/// and `update.ndjson`.
///
/// # Arguments
///
/// * `package_path` - Path to the IG package `.tgz` file
/// * `count` - Number of resources to generate per type (default: 10)
/// * `output_dir` - Directory to write NDJSON files into
pub fn generate_bulk_test_data(package_path: &str, count: u64, output_dir: &Path) -> Result<()> {
    let pkg = package::parse_package(package_path)?;
    let cs = select_capability_statement(&pkg)?;

    // Build counts and profile_urls from the CapabilityStatement
    let mut counts = HashMap::new();
    let mut profile_urls = HashMap::new();
    for rest in &cs.rest {
        for resource in &rest.resource {
            let rtype = &resource.resource_type;
            counts.insert(rtype.clone(), count);
            if let Some(profile) = &resource.profile {
                profile_urls.insert(rtype.clone(), profile.clone());
            }
        }
    }

    if counts.is_empty() {
        anyhow::bail!("No resource types found in CapabilityStatement");
    }

    // Build value_set_systems from raw resources
    let value_set_systems = valuesets::build_value_set_system_map(&pkg.raw_resources);

    tracing::info!(
        "Generating bulk data for {} resource types ({} each) into {}",
        counts.len(),
        count,
        output_dir.display()
    );

    // Generate initial bulk data
    let ids = bulk_data::generate_bulk_data(
        &counts,
        &profile_urls,
        &pkg.structure_definitions,
        &value_set_systems,
        output_dir,
    )?;

    // Generate supplement resources for types in creation order that have no bulk count
    let creation_order = bulk_data::bulk_data_creation_order(&counts);
    let supplement_ids = bulk_data::write_supplement_ndjson(
        &creation_order,
        &counts,
        &profile_urls,
        &pkg.structure_definitions,
        &value_set_systems,
        output_dir,
    )?;

    // Merge supplement IDs into the main ID store for update generation
    let mut all_ids = ids;
    for (rtype, type_ids) in supplement_ids {
        all_ids.entry(rtype).or_default().extend(type_ids);
    }

    // Generate update.ndjson
    bulk_data::generate_update_ndjson(&all_ids, output_dir)?;

    tracing::info!("Bulk test data written to {}/data/", output_dir.display());

    Ok(())
}

/// Validate a JSON resource against a profile from an IG package.
///
/// Parses the IG package, finds the matching profile (by explicit URL or
/// auto-detected by resource type), and validates the resource against it.
pub fn validate_resource(
    package_path: &str,
    resource_path: &str,
    profile_url: Option<&str>,
) -> anyhow::Result<()> {
    let pkg = package::parse_package(package_path)?;
    let resource_content = std::fs::read_to_string(resource_path)?;
    let resource: serde_json::Value = serde_json::from_str(&resource_content)?;

    let resource_type = resource
        .get("resourceType")
        .and_then(|v| v.as_str())
        .ok_or_else(|| anyhow::anyhow!("Resource JSON missing 'resourceType' field"))?;

    // Find the profile
    let profile = if let Some(url) = profile_url {
        pkg.structure_definitions
            .iter()
            .find(|sd| sd.url == url)
            .ok_or_else(|| anyhow::anyhow!("Profile '{}' not found in IG package", url))?
    } else {
        // Auto-detect by resource type
        pkg.structure_definitions
            .iter()
            .find(|sd| sd.base_type == resource_type)
            .ok_or_else(|| {
                anyhow::anyhow!(
                    "No profile found for resource type '{}'. Specify --profile explicitly.",
                    resource_type
                )
            })?
    };

    let errors = validator::validate_against_profile(&resource, profile);

    if errors.is_empty() {
        println!(
            "Validation passed for {} against {}",
            resource_type, profile.url
        );
    } else {
        println!(
            "Validation failed for {} against {}:",
            resource_type, profile.url
        );
        for err in &errors {
            println!("  - {}", err);
        }
        anyhow::bail!("Validation failed with {} error(s)", errors.len());
    }

    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::fhir::test_helpers;

    #[test]
    fn snapshot_fhir_test_plan() {
        let pkg_bytes = test_helpers::create_test_ig_package();
        let dir = tempfile::TempDir::new().unwrap();
        let path = dir.path().join("test.json");
        std::fs::write(&path, &pkg_bytes).unwrap();

        let plan = convert(path.to_str().unwrap()).unwrap();
        insta::assert_json_snapshot!(plan);
    }
}
