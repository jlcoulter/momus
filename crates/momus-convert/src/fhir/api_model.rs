//! Convert a FHIR IG package to the format-agnostic `ApiModel`.
//!
//! This is the bridge between FHIR-specific concepts (CapabilityStatement,
//! StructureDefinition, SearchParameter) and the generic `ApiModel` that
//! the `TestGenerator` engine consumes.

use super::capability::*;
use super::package::IgPackage;
use momus_core::ast::*;

/// Convert a parsed FHIR IG package to an `ApiModel`.
///
/// Extracts resource types, operations, search parameters, and profile
/// URLs from the CapabilityStatement and maps them to the format-agnostic
/// `ApiModel` types.
pub fn package_to_api_model(pkg: &IgPackage) -> ApiModel {
    let cs = select_capability_statement(pkg);

    let mut resources = Vec::new();

    for rest in &cs.rest {
        for resource in &rest.resource {
            let rtype = &resource.resource_type;

            // Build operations from declared interactions
            let mut operations = Vec::new();
            for interaction in &resource.interaction {
                let (method, path) = match interaction.code.as_str() {
                    "read" => ("GET", format!("/{rtype}/{{id}}")),
                    "vread" => ("GET", format!("/{rtype}/{{id}}/_history/{{vid}}")),
                    "update" => ("PUT", format!("/{rtype}/{{id}}")),
                    "patch" => ("PATCH", format!("/{rtype}/{{id}}")),
                    "delete" => ("DELETE", format!("/{rtype}/{{id}}")),
                    "create" => ("POST", format!("/{rtype}")),
                    "search-type" => ("GET", format!("/{rtype}")),
                    "history-instance" => ("GET", format!("/{rtype}/{{id}}/_history")),
                    "history-type" => ("GET", format!("/{rtype}/_history")),
                    other => {
                        // Custom operation
                        ("POST", format!("/{rtype}/${other}"))
                    }
                };

                let expected_status = match interaction.code.as_str() {
                    "create" => 201,
                    "delete" => 204,
                    _ => 200,
                };

                operations.push(OperationModel {
                    name: interaction.code.clone(),
                    method: method.to_string(),
                    path,
                    request_body: if matches!(
                        interaction.code.as_str(),
                        "create" | "update" | "patch"
                    ) {
                        Some(BodyModel {
                            content_type: "application/json".to_string(),
                            schema: None,
                            required_fields: vec![],
                        })
                    } else {
                        None
                    },
                    responses: vec![ResponseModel {
                        status_code: expected_status,
                        content_type: Some("application/json".to_string()),
                        schema: None,
                    }],
                });
            }

            // Build search parameters
            let mut search_params = Vec::new();
            for sp in &resource.search_param {
                let (modifiers, prefixes) = applicable_modifiers_and_prefixes(&sp.param_type);
                search_params.push(SearchParamModel {
                    name: sp.name.clone(),
                    param_type: sp.param_type.clone(),
                    modifiers,
                    prefixes,
                });
            }

            resources.push(ResourceModel {
                name: rtype.clone(),
                profile_url: resource.profile.clone(),
                operations,
                search_params,
                search_include: resource.search_include.clone(),
                search_revinclude: resource.search_revinclude.clone(),
                supported_profiles: resource.supported_profile.clone(),
            });
        }
    }

    let name = cs.name.clone().unwrap_or_else(|| "FHIR IG".to_string());

    ApiModel { name, resources }
}

/// Select the best CapabilityStatement from the package.
fn select_capability_statement(pkg: &IgPackage) -> CapabilityStatement {
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
        .unwrap_or_else(|| CapabilityStatement {
            resource_type: "CapabilityStatement".to_string(),
            url: None,
            name: Some("Unknown".to_string()),
            status: None,
            rest: vec![],
        })
}

/// Return applicable search modifiers and prefixes for a parameter type.
fn applicable_modifiers_and_prefixes(param_type: &str) -> (Vec<String>, Vec<String>) {
    let modifiers = match param_type {
        "string" => vec!["exact".into(), "contains".into()],
        "token" => vec![
            "exact".into(),
            "contains".into(),
            "text".into(),
            "not".into(),
            "above".into(),
            "below".into(),
            "in".into(),
            "not-in".into(),
            "of-type".into(),
        ],
        "reference" => vec!["above".into(), "below".into(), "identifier".into()],
        "uri" => vec!["above".into(), "below".into()],
        _ => vec!["missing".into()],
    };

    let prefixes = match param_type {
        "number" | "quantity" => vec![
            "eq".into(),
            "ne".into(),
            "gt".into(),
            "lt".into(),
            "ge".into(),
            "le".into(),
        ],
        "date" | "dateTime" => vec![
            "eq".into(),
            "ne".into(),
            "gt".into(),
            "lt".into(),
            "ge".into(),
            "le".into(),
            "sa".into(),
            "eb".into(),
            "ap".into(),
        ],
        _ => vec![],
    };

    // Always include :missing
    let mut all_modifiers = vec!["missing".into()];
    all_modifiers.extend(modifiers);

    (all_modifiers, prefixes)
}
