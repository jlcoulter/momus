#![allow(dead_code, clippy::type_complexity)]

//! FHIR IG package to TestPlan converter.
//!
//! This module ports the fhir-autotest pipeline into momus:
//! 1. Parse IG package (.tgz) → extract FHIR resources
//! 2. Select CapabilityStatement → determine server capabilities
//! 3. Generate test plan with CRUD, search, and conformance tests

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
pub mod search_param;
pub mod test_helpers;
pub mod test_model;
pub mod validator;
pub mod value_resolver;
pub mod valuesets;

use anyhow::{Context, Result};
use momus_core::ast::TestPlan;
use std::collections::HashMap;

/// Convert a FHIR Implementation Guide package to a TestPlan.
///
/// Reads a .tgz IG package, parses CapabilityStatements, StructureDefinitions,
/// SearchParameters, and OperationDefinitions, and generates a comprehensive
/// conformance test suite with CRUD, search, and operation tests.
pub fn convert(path: &str) -> Result<TestPlan> {
    let pkg = package::parse_package(path)?;
    let cs = select_capability_statement(&pkg)?;

    // Generate the FHIR test plan using the planner
    let fhir_plan = planner::generate_test_plan(
        &cs,
        &pkg.search_parameters,
        Some(&pkg.operation_definitions),
        None,
        &HashMap::new(),
        &HashMap::new(),
    );

    // Convert FHIR test plan to momus TestPlan
    let plan_name = format!(
        "FHIR IG: {} groups, {} tests from {}",
        fhir_plan.test_groups.len(),
        fhir_plan.total_tests(),
        path.rsplit('/').next().unwrap_or(path)
    );

    let mut steps = Vec::new();
    for group in &fhir_plan.test_groups {
        for test in &group.tests {
            let method = match test.request.method.to_uppercase().as_str() {
                "GET" => momus_core::ast::Method::Get,
                "POST" => momus_core::ast::Method::Post,
                "PUT" => momus_core::ast::Method::Put,
                "DELETE" => momus_core::ast::Method::Delete,
                "PATCH" => momus_core::ast::Method::Patch,
                "HEAD" => momus_core::ast::Method::Head,
                "OPTIONS" => momus_core::ast::Method::Options,
                _ => momus_core::ast::Method::Get,
            };

            let mut assertions = vec![momus_core::ast::Assertion::Status(
                test.validation.expected_status,
            )];

            // Add profile validation assertion if profile URL is available
            if let Some(profile_url) = &test.validation.profile_url {
                assertions.push(momus_core::ast::Assertion::JsonPath {
                    path: "$.meta.profile[0]".to_string(),
                    predicate: momus_core::ast::JsonPredicate::Eq(serde_json::json!(profile_url)),
                });
            }

            let step = momus_core::ast::RequestStep {
                name: test.name.clone(),
                method,
                url: test.request.url.clone(),
                headers: test.request.headers.clone(),
                body: test.request.body.clone(),
                assert: assertions,
                save_as: String::new(),
                soft_fail: false,
            };
            steps.push(momus_core::ast::Step::Request(step));
        }
    }

    Ok(TestPlan {
        name: plan_name,
        base_url: String::new(),
        default_headers: std::collections::HashMap::new(),
        steps,
        setup: vec![],
        teardown: vec![],
    })
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
