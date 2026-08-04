#![allow(dead_code, clippy::type_complexity)]

//! FHIR IG package to TestPlan converter.
//!
//! This module ports the fhir-autotest pipeline into momus:
//! 1. Parse IG package (.tgz) → extract FHIR resources
//! 2. Select CapabilityStatement → determine server capabilities
//! 3. Generate test plan with CRUD, search, and conformance tests
//!
//! Some types are defined here but not yet consumed by the converter
//! (the full test plan generator is ported in a follow-up).

pub mod capability;
pub mod operation;
pub mod package;
pub mod profile;
pub mod search_param;
pub mod test_model;

use anyhow::{Context, Result};
use momus_core::ast::TestPlan;

/// Convert a FHIR Implementation Guide package to a TestPlan.
pub fn convert(path: &str) -> Result<TestPlan> {
    let pkg = package::parse_package(path)?;
    let cs = select_capability_statement(&pkg)?;

    let resource_types: Vec<String> = cs.rest.iter()
        .flat_map(|r| r.resource.iter())
        .map(|r| r.resource_type.clone())
        .collect();

    let plan_name = format!(
        "FHIR IG: {} resources from {}",
        resource_types.len(),
        path.rsplit('/').next().unwrap_or(path)
    );

    let mut steps = Vec::new();
    for rtype in &resource_types {
        let step = momus_core::ast::RequestStep {
            name: format!("read_{}", rtype.to_lowercase()),
            method: momus_core::ast::Method::Get,
            url: format!("/{}", rtype),
            headers: std::collections::HashMap::new(),
            body: None,
            assert: vec![momus_core::ast::Assertion::Status(200)],
            save_as: String::new(),
            soft_fail: false,
        };
        steps.push(momus_core::ast::Step::Request(step));
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

fn select_capability_statement(pkg: &package::IgPackage) -> Result<capability::CapabilityStatement> {
    pkg.capability_statements
        .iter()
        .find(|cs| {
            cs.rest.iter().any(|r| r.mode == "server" && !r.resource.is_empty())
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
