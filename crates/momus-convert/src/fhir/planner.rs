#![allow(dead_code)]

//! FHIR test plan generator.
//!
//! Generates comprehensive conformance test plans from a CapabilityStatement.
//! Ported from fhir-autotest's planner.rs.
//!
//! Generates test cases for:
//! - CRUD interactions (read, vread, create, update, delete, patch, history)
//! - Search params (single, modifiers, prefixes, near, combo, chained)
//! - Include/revinclude tests
//! - Result param tests (_summary, _elements, _count, _sort, _has)
//! - Operation tests ($operation)
//! - Negative tests (undeclared interactions/params)
//! - Conformance tests (mustSupport field presence)

use super::capability::*;
use super::search_param::SearchParameter;
use super::test_model::*;
use std::collections::HashMap;

/// Fields that are never summary in FHIR R4.
fn summary_absent_fields() -> Vec<String> {
    vec![
        "text".to_string(),
        "contained".to_string(),
        "extension".to_string(),
        "modifierExtension".to_string(),
    ]
}

/// Build a ResponseAssertion appropriate for the test case kind.
pub fn assertion_for_kind(kind: &TestCaseKind, resource_type: &str) -> Option<ResponseAssertion> {
    match kind {
        TestCaseKind::Interaction => None,
        TestCaseKind::SearchSingle { .. } => Some(ResponseAssertion {
            bundle_type: Some("searchset".to_string()),
            min_entries: Some(0),
            ..ResponseAssertion::none()
        }),
        TestCaseKind::SearchModifier { .. } => Some(ResponseAssertion {
            bundle_type: Some("searchset".to_string()),
            min_entries: Some(0),
            ..ResponseAssertion::none()
        }),
        TestCaseKind::SearchPrefix { .. } => Some(ResponseAssertion {
            bundle_type: Some("searchset".to_string()),
            min_entries: Some(0),
            ..ResponseAssertion::none()
        }),
        TestCaseKind::SearchNear { .. } => Some(ResponseAssertion {
            bundle_type: Some("searchset".to_string()),
            min_entries: Some(0),
            ..ResponseAssertion::none()
        }),
        TestCaseKind::SearchCombo { .. } => Some(ResponseAssertion {
            bundle_type: Some("searchset".to_string()),
            min_entries: Some(0),
            ..ResponseAssertion::none()
        }),
        TestCaseKind::SearchChained { .. } => Some(ResponseAssertion {
            bundle_type: Some("searchset".to_string()),
            min_entries: Some(0),
            ..ResponseAssertion::none()
        }),
        TestCaseKind::Include { .. } => Some(ResponseAssertion {
            bundle_type: Some("searchset".to_string()),
            min_entries: Some(0),
            ..ResponseAssertion::none()
        }),
        TestCaseKind::ResultParam { param } => match param.as_str() {
            "_summary" => {
                let mut required = HashMap::new();
                required.insert(
                    resource_type.to_string(),
                    vec!["id".to_string(), "meta".to_string()],
                );
                Some(ResponseAssertion {
                    bundle_type: Some("searchset".to_string()),
                    min_entries: Some(0),
                    absent_fields: summary_absent_fields(),
                    required_fields: required,
                    ..ResponseAssertion::none()
                })
            }
            "_count" => Some(ResponseAssertion {
                bundle_type: Some("searchset".to_string()),
                max_entries: Some(1),
                ..ResponseAssertion::none()
            }),
            _ => Some(ResponseAssertion {
                bundle_type: Some("searchset".to_string()),
                min_entries: Some(0),
                ..ResponseAssertion::none()
            }),
        },
        TestCaseKind::Operation { .. } => Some(ResponseAssertion {
            response_resource_types: vec![
                "Parameters".to_string(),
                "Bundle".to_string(),
                "OperationOutcome".to_string(),
            ],
            ..ResponseAssertion::none()
        }),
        TestCaseKind::Negative { .. } => Some(ResponseAssertion {
            outcome_severity: Some("error".to_string()),
            ..ResponseAssertion::none()
        }),
        TestCaseKind::Conformance { resource_type, .. } => {
            let mut required = HashMap::new();
            required.insert(
                resource_type.clone(),
                vec!["id".to_string(), "meta".to_string()],
            );
            Some(ResponseAssertion {
                bundle_type: Some("searchset".to_string()),
                min_entries: Some(1),
                required_fields: required,
                ..ResponseAssertion::none()
            })
        }
    }
}

/// Generate near/proximity search tests for location/coordinate type params.
///
/// For search parameters of type "quantity" or "location", generates test cases
/// using `:near` and `:within` modifiers with coordinate-like values.
pub fn generate_near_search_tests(
    rtype: &str,
    profile_url: &Option<String>,
    sp: &RestSearchParam,
) -> Vec<TestCase> {
    let mut tests = Vec::new();

    // Near/proximity search is applicable to quantity and location types
    let is_near_type = matches!(sp.param_type.as_str(), "quantity" | "location" | "special");
    if !is_near_type {
        return tests;
    }

    // :near modifier test — uses lat/lon or coordinate value
    tests.push(TestCase {
        name: format!("search-{}-{}-near", rtype.to_lowercase(), sp.name),
        kind: TestCaseKind::SearchNear {
            param: sp.name.clone(),
        },
        interaction: Interaction::SearchType,
        resource_type: rtype.to_string(),
        profile_url: profile_url.clone(),
        request: HttpRequest {
            method: "GET".to_string(),
            url: format!("/{}?{}:near={}", rtype, sp.name, "{lat}|{lon}|{distance}"),
            headers: HashMap::new(),
            body: None,
        },
        validation: ValidationSpec {
            expected_status: 200,
            profile_url: profile_url.clone(),
            required_elements: vec![],
            forbidden_elements: vec![],
            response_assertion: assertion_for_kind(
                &TestCaseKind::SearchNear {
                    param: sp.name.clone(),
                },
                rtype,
            ),
        },
    });

    // :within modifier test
    tests.push(TestCase {
        name: format!("search-{}-{}-within", rtype.to_lowercase(), sp.name),
        kind: TestCaseKind::SearchNear {
            param: sp.name.clone(),
        },
        interaction: Interaction::SearchType,
        resource_type: rtype.to_string(),
        profile_url: profile_url.clone(),
        request: HttpRequest {
            method: "GET".to_string(),
            url: format!("/{}?{}:within={}", rtype, sp.name, "{lat}|{lon}|{distance}"),
            headers: HashMap::new(),
            body: None,
        },
        validation: ValidationSpec {
            expected_status: 200,
            profile_url: profile_url.clone(),
            required_elements: vec![],
            forbidden_elements: vec![],
            response_assertion: assertion_for_kind(
                &TestCaseKind::SearchNear {
                    param: sp.name.clone(),
                },
                rtype,
            ),
        },
    });

    tests
}

/// Generate chained search tests for search parameters with chain expressions.
///
/// Chained search allows searching across resource references, e.g.
/// `?patient.name=John` searches for observations where the patient's name is John.
/// This function generates test cases for chained search syntax using the
/// search parameter's expression to identify chainable references.
pub fn generate_chained_search_tests(
    rtype: &str,
    profile_url: &Option<String>,
    sp: &RestSearchParam,
    search_parameters: &[SearchParameter],
) -> Vec<TestCase> {
    let mut tests = Vec::new();

    // Find the full SearchParameter definition to get the expression
    let sp_def = search_parameters
        .iter()
        .find(|s| s.code == sp.name && s.base.contains(&rtype.to_string()));

    let expression = match sp_def {
        Some(def) => def.expression.as_deref().unwrap_or(""),
        None => "",
    };

    // Check if the expression contains a reference chain (e.g., "Patient.generalPractitioner")
    // A chainable expression typically has the form "ResourceType.referenceField"
    // We look for patterns like "ResourceType.field" where field is a reference
    let chainable = !expression.is_empty()
        && (expression.contains('.') || sp.param_type == "reference" || sp.param_type == "string");

    if !chainable {
        return tests;
    }

    // Extract chain prefix from expression if available
    let chain_prefix = if let Some(dot_pos) = expression.find('.') {
        let prefix = &expression[..dot_pos];
        // Extract just the resource type part (before any space or pipe)
        prefix
            .split_whitespace()
            .next()
            .unwrap_or("")
            .trim_end_matches('|')
            .to_lowercase()
    } else {
        // Default: use the param name as a chain prefix
        sp.name.to_lowercase()
    };

    // Generate a chained search test
    // e.g., ?patient.name=John or ?subject.name=John
    let chain_param = format!("{}.{}", chain_prefix, sp.name);

    tests.push(TestCase {
        name: format!("search-{}-{}-chained", rtype.to_lowercase(), sp.name),
        kind: TestCaseKind::SearchChained {
            param: sp.name.clone(),
            chain: chain_param.clone(),
        },
        interaction: Interaction::SearchType,
        resource_type: rtype.to_string(),
        profile_url: profile_url.clone(),
        request: HttpRequest {
            method: "GET".to_string(),
            url: format!("/{}?{}={}", rtype, chain_param, "{value}"),
            headers: HashMap::new(),
            body: None,
        },
        validation: ValidationSpec {
            expected_status: 200,
            profile_url: profile_url.clone(),
            required_elements: vec![],
            forbidden_elements: vec![],
            response_assertion: assertion_for_kind(
                &TestCaseKind::SearchChained {
                    param: sp.name.clone(),
                    chain: chain_param.clone(),
                },
                rtype,
            ),
        },
    });

    tests
}

/// Generate conformance (mustSupport) tests for a StructureDefinition profile.
///
/// For each resource type with a profile URL, generates test cases that verify
/// mustSupport fields are present in responses. Uses the FHIR response assertions
/// engine to check required field presence.
pub fn generate_conformance_tests(
    rtype: &str,
    profile_url: &Option<String>,
    supported_profile: &[String],
) -> Vec<TestCase> {
    let mut tests = Vec::new();

    // Collect all profile URLs to test
    let mut profiles = Vec::new();
    if let Some(url) = profile_url {
        profiles.push(url.clone());
    }
    for sp in supported_profile {
        profiles.push(sp.clone());
    }

    for profile_url_str in profiles {
        let test_name = format!("conformance-{}-mustsupport", rtype.to_lowercase());

        tests.push(TestCase {
            name: if profile_url_str == profile_url.as_deref().unwrap_or("") {
                test_name
            } else {
                format!(
                    "conformance-{}-mustsupport-{}",
                    rtype.to_lowercase(),
                    profile_url_str
                        .rsplit('/')
                        .next()
                        .unwrap_or(&profile_url_str)
                )
            },
            kind: TestCaseKind::Conformance {
                resource_type: rtype.to_string(),
                profile_url: profile_url_str.clone(),
            },
            interaction: Interaction::SearchType,
            resource_type: rtype.to_string(),
            profile_url: Some(profile_url_str.clone()),
            request: HttpRequest {
                method: "GET".to_string(),
                url: format!("/{}?_count=1", rtype),
                headers: HashMap::new(),
                body: None,
            },
            validation: ValidationSpec {
                expected_status: 200,
                profile_url: Some(profile_url_str.clone()),
                required_elements: vec!["id".to_string(), "meta".to_string()],
                forbidden_elements: vec![],
                response_assertion: assertion_for_kind(
                    &TestCaseKind::Conformance {
                        resource_type: rtype.to_string(),
                        profile_url: profile_url_str.clone(),
                    },
                    rtype,
                ),
            },
        });
    }

    tests
}

/// Generate a comprehensive test plan from a CapabilityStatement.
pub fn generate_test_plan(
    cs: &CapabilityStatement,
    _search_parameters: &[SearchParameter],
    _operation_definitions: Option<&[super::operation::OperationDefinition]>,
    _profile_urls: Option<&HashMap<String, String>>,
    field_values: &HashMap<String, HashMap<String, String>>,
    _created_ids: &HashMap<String, String>,
) -> FhirTestPlan {
    let mut groups = Vec::new();

    for rest in &cs.rest {
        for resource in &rest.resource {
            let mut tests = Vec::new();
            let rtype = &resource.resource_type;
            let profile_url = resource.profile.clone();

            // Get field values for this resource type
            let values = field_values.get(rtype);

            // --- CRUD tests ---
            for interaction in &resource.interaction {
                match interaction.code.as_str() {
                    "read" => {
                        let id = values
                            .and_then(|v| v.get("id"))
                            .cloned()
                            .unwrap_or_else(|| "{id}".to_string());
                        tests.push(TestCase {
                            name: format!("read-{}", rtype.to_lowercase()),
                            kind: TestCaseKind::Interaction,
                            interaction: Interaction::Read,
                            resource_type: rtype.to_string(),
                            profile_url: profile_url.clone(),
                            request: HttpRequest {
                                method: "GET".to_string(),
                                url: format!("/{}/{}", rtype, id),
                                headers: HashMap::new(),
                                body: None,
                            },
                            validation: ValidationSpec {
                                expected_status: 200,
                                profile_url: profile_url.clone(),
                                required_elements: vec![],
                                forbidden_elements: vec![],
                                response_assertion: None,
                            },
                        });
                    }
                    "vread" => {
                        tests.push(TestCase {
                            name: format!("vread-{}", rtype.to_lowercase()),
                            kind: TestCaseKind::Interaction,
                            interaction: Interaction::Vread,
                            resource_type: rtype.to_string(),
                            profile_url: profile_url.clone(),
                            request: HttpRequest {
                                method: "GET".to_string(),
                                url: format!("/{}/{}/_history/1", rtype, "{id}"),
                                headers: HashMap::new(),
                                body: None,
                            },
                            validation: ValidationSpec {
                                expected_status: 200,
                                profile_url: profile_url.clone(),
                                required_elements: vec![],
                                forbidden_elements: vec![],
                                response_assertion: None,
                            },
                        });
                    }
                    "create" => {
                        tests.push(TestCase {
                            name: format!("create-{}", rtype.to_lowercase()),
                            kind: TestCaseKind::Interaction,
                            interaction: Interaction::Create,
                            resource_type: rtype.to_string(),
                            profile_url: profile_url.clone(),
                            request: HttpRequest {
                                method: "POST".to_string(),
                                url: format!("/{}", rtype),
                                headers: {
                                    let mut h = HashMap::new();
                                    h.insert(
                                        "Content-Type".to_string(),
                                        "application/json".to_string(),
                                    );
                                    h
                                },
                                body: Some(serde_json::json!({"resourceType": rtype})),
                            },
                            validation: ValidationSpec {
                                expected_status: 201,
                                profile_url: profile_url.clone(),
                                required_elements: vec![],
                                forbidden_elements: vec![],
                                response_assertion: None,
                            },
                        });
                    }
                    "update" => {
                        tests.push(TestCase {
                            name: format!("update-{}", rtype.to_lowercase()),
                            kind: TestCaseKind::Interaction,
                            interaction: Interaction::Update,
                            resource_type: rtype.to_string(),
                            profile_url: profile_url.clone(),
                            request: HttpRequest {
                                method: "PUT".to_string(),
                                url: format!("/{}/{}", rtype, "{id}"),
                                headers: {
                                    let mut h = HashMap::new();
                                    h.insert(
                                        "Content-Type".to_string(),
                                        "application/json".to_string(),
                                    );
                                    h
                                },
                                body: Some(serde_json::json!({"resourceType": rtype})),
                            },
                            validation: ValidationSpec {
                                expected_status: 200,
                                profile_url: profile_url.clone(),
                                required_elements: vec![],
                                forbidden_elements: vec![],
                                response_assertion: None,
                            },
                        });
                    }
                    "delete" => {
                        tests.push(TestCase {
                            name: format!("delete-{}", rtype.to_lowercase()),
                            kind: TestCaseKind::Interaction,
                            interaction: Interaction::Delete,
                            resource_type: rtype.to_string(),
                            profile_url: profile_url.clone(),
                            request: HttpRequest {
                                method: "DELETE".to_string(),
                                url: format!("/{}/{}", rtype, "{id}"),
                                headers: HashMap::new(),
                                body: None,
                            },
                            validation: ValidationSpec {
                                expected_status: 204,
                                profile_url: profile_url.clone(),
                                required_elements: vec![],
                                forbidden_elements: vec![],
                                response_assertion: None,
                            },
                        });
                    }
                    "patch" => {
                        tests.push(TestCase {
                            name: format!("patch-{}", rtype.to_lowercase()),
                            kind: TestCaseKind::Interaction,
                            interaction: Interaction::Patch,
                            resource_type: rtype.to_string(),
                            profile_url: profile_url.clone(),
                            request: HttpRequest {
                                method: "PATCH".to_string(),
                                url: format!("/{}/{}", rtype, "{id}"),
                                headers: {
                                    let mut h = HashMap::new();
                                    h.insert("Content-Type".to_string(), "application/json-patch+json".to_string());
                                    h
                                },
                                body: Some(serde_json::json!([{"op": "replace", "path": "/active", "value": true}])),
                            },
                            validation: ValidationSpec {
                                expected_status: 200,
                                profile_url: profile_url.clone(),
                                required_elements: vec![],
                                forbidden_elements: vec![],
                                response_assertion: None,
                            },
                        });
                    }
                    "history-instance" => {
                        tests.push(TestCase {
                            name: format!("history-instance-{}", rtype.to_lowercase()),
                            kind: TestCaseKind::Interaction,
                            interaction: Interaction::HistoryInstance,
                            resource_type: rtype.to_string(),
                            profile_url: profile_url.clone(),
                            request: HttpRequest {
                                method: "GET".to_string(),
                                url: format!("/{}/{}/_history", rtype, "{id}"),
                                headers: HashMap::new(),
                                body: None,
                            },
                            validation: ValidationSpec {
                                expected_status: 200,
                                profile_url: profile_url.clone(),
                                required_elements: vec![],
                                forbidden_elements: vec![],
                                response_assertion: Some(ResponseAssertion {
                                    bundle_type: Some("history".to_string()),
                                    min_entries: Some(0),
                                    ..ResponseAssertion::none()
                                }),
                            },
                        });
                    }
                    "history-type" => {
                        tests.push(TestCase {
                            name: format!("history-type-{}", rtype.to_lowercase()),
                            kind: TestCaseKind::Interaction,
                            interaction: Interaction::HistoryType,
                            resource_type: rtype.to_string(),
                            profile_url: profile_url.clone(),
                            request: HttpRequest {
                                method: "GET".to_string(),
                                url: format!("/{}/_history", rtype),
                                headers: HashMap::new(),
                                body: None,
                            },
                            validation: ValidationSpec {
                                expected_status: 200,
                                profile_url: profile_url.clone(),
                                required_elements: vec![],
                                forbidden_elements: vec![],
                                response_assertion: Some(ResponseAssertion {
                                    bundle_type: Some("history".to_string()),
                                    min_entries: Some(0),
                                    ..ResponseAssertion::none()
                                }),
                            },
                        });
                    }
                    _ => {}
                }
            }

            // --- Search tests ---
            if resource.interaction.iter().any(|i| i.code == "search-type") {
                for sp in &resource.search_param {
                    // Single search param test
                    tests.push(TestCase {
                        name: format!("search-{}-{}", rtype.to_lowercase(), sp.name),
                        kind: TestCaseKind::SearchSingle {
                            param_name: sp.name.clone(),
                            param_type: sp.param_type.clone(),
                        },
                        interaction: Interaction::SearchType,
                        resource_type: rtype.to_string(),
                        profile_url: profile_url.clone(),
                        request: HttpRequest {
                            method: "GET".to_string(),
                            url: format!("/{}?{}={}", rtype, sp.name, "{value}"),
                            headers: HashMap::new(),
                            body: None,
                        },
                        validation: ValidationSpec {
                            expected_status: 200,
                            profile_url: profile_url.clone(),
                            required_elements: vec![],
                            forbidden_elements: vec![],
                            response_assertion: assertion_for_kind(
                                &TestCaseKind::SearchSingle {
                                    param_name: sp.name.clone(),
                                    param_type: sp.param_type.clone(),
                                },
                                rtype,
                            ),
                        },
                    });

                    // Modifier tests for applicable param types
                    for modifier in SearchModifier::applicable_to(&sp.param_type) {
                        tests.push(TestCase {
                            name: format!(
                                "search-{}-{}-{:?}",
                                rtype.to_lowercase(),
                                sp.name,
                                modifier
                            ),
                            kind: TestCaseKind::SearchModifier {
                                param_name: sp.name.clone(),
                                modifier: modifier.clone(),
                            },
                            interaction: Interaction::SearchType,
                            resource_type: rtype.to_string(),
                            profile_url: profile_url.clone(),
                            request: HttpRequest {
                                method: "GET".to_string(),
                                url: format!(
                                    "/{}?{}:{}={}",
                                    rtype,
                                    sp.name,
                                    modifier.suffix(),
                                    "{value}"
                                ),
                                headers: HashMap::new(),
                                body: None,
                            },
                            validation: ValidationSpec {
                                expected_status: 200,
                                profile_url: profile_url.clone(),
                                required_elements: vec![],
                                forbidden_elements: vec![],
                                response_assertion: assertion_for_kind(
                                    &TestCaseKind::SearchModifier {
                                        param_name: sp.name.clone(),
                                        modifier: modifier.clone(),
                                    },
                                    rtype,
                                ),
                            },
                        });
                    }

                    // Prefix tests for number/date/quantity
                    for prefix in SearchPrefix::applicable_to(&sp.param_type) {
                        tests.push(TestCase {
                            name: format!(
                                "search-{}-{}-{:?}",
                                rtype.to_lowercase(),
                                sp.name,
                                prefix
                            ),
                            kind: TestCaseKind::SearchPrefix {
                                param_name: sp.name.clone(),
                                prefix: prefix.clone(),
                            },
                            interaction: Interaction::SearchType,
                            resource_type: rtype.to_string(),
                            profile_url: profile_url.clone(),
                            request: HttpRequest {
                                method: "GET".to_string(),
                                url: format!(
                                    "/{}?{}={}{}",
                                    rtype,
                                    sp.name,
                                    prefix.prefix_str(),
                                    "{value}"
                                ),
                                headers: HashMap::new(),
                                body: None,
                            },
                            validation: ValidationSpec {
                                expected_status: 200,
                                profile_url: profile_url.clone(),
                                required_elements: vec![],
                                forbidden_elements: vec![],
                                response_assertion: assertion_for_kind(
                                    &TestCaseKind::SearchPrefix {
                                        param_name: sp.name.clone(),
                                        prefix: prefix.clone(),
                                    },
                                    rtype,
                                ),
                            },
                        });
                    }

                    // Near/proximity search tests for location/coordinate type params
                    tests.extend(generate_near_search_tests(rtype, &profile_url, sp));

                    // Chained search tests for reference params with chain expressions
                    tests.extend(generate_chained_search_tests(
                        rtype,
                        &profile_url,
                        sp,
                        _search_parameters,
                    ));
                }

                // Combinatorial search (2-param combos)
                if resource.search_param.len() >= 2 {
                    for i in 0..resource.search_param.len().min(3) {
                        for j in (i + 1)..resource.search_param.len().min(4) {
                            let p1 = &resource.search_param[i];
                            let p2 = &resource.search_param[j];
                            tests.push(TestCase {
                                name: format!(
                                    "search-{}-{}-{}-combo",
                                    rtype.to_lowercase(),
                                    p1.name,
                                    p2.name
                                ),
                                kind: TestCaseKind::SearchCombo {
                                    params: vec![p1.name.clone(), p2.name.clone()],
                                },
                                interaction: Interaction::SearchType,
                                resource_type: rtype.to_string(),
                                profile_url: profile_url.clone(),
                                request: HttpRequest {
                                    method: "GET".to_string(),
                                    url: format!(
                                        "/{}?{}={}&{}={}",
                                        rtype, p1.name, "{value1}", p2.name, "{value2}"
                                    ),
                                    headers: HashMap::new(),
                                    body: None,
                                },
                                validation: ValidationSpec {
                                    expected_status: 200,
                                    profile_url: profile_url.clone(),
                                    required_elements: vec![],
                                    forbidden_elements: vec![],
                                    response_assertion: assertion_for_kind(
                                        &TestCaseKind::SearchCombo {
                                            params: vec![p1.name.clone(), p2.name.clone()],
                                        },
                                        rtype,
                                    ),
                                },
                            });
                        }
                    }
                }

                // _include tests
                for include in &resource.search_include {
                    tests.push(TestCase {
                        name: format!("search-{}-include-{}", rtype.to_lowercase(), include),
                        kind: TestCaseKind::Include {
                            param: include.clone(),
                            revinclude: false,
                        },
                        interaction: Interaction::SearchType,
                        resource_type: rtype.to_string(),
                        profile_url: profile_url.clone(),
                        request: HttpRequest {
                            method: "GET".to_string(),
                            url: format!("/{}?_include={}", rtype, include),
                            headers: HashMap::new(),
                            body: None,
                        },
                        validation: ValidationSpec {
                            expected_status: 200,
                            profile_url: profile_url.clone(),
                            required_elements: vec![],
                            forbidden_elements: vec![],
                            response_assertion: assertion_for_kind(
                                &TestCaseKind::Include {
                                    param: include.clone(),
                                    revinclude: false,
                                },
                                rtype,
                            ),
                        },
                    });
                }

                // _revinclude tests
                for revinclude in &resource.search_revinclude {
                    tests.push(TestCase {
                        name: format!("search-{}-revinclude-{}", rtype.to_lowercase(), revinclude),
                        kind: TestCaseKind::Include {
                            param: revinclude.clone(),
                            revinclude: true,
                        },
                        interaction: Interaction::SearchType,
                        resource_type: rtype.to_string(),
                        profile_url: profile_url.clone(),
                        request: HttpRequest {
                            method: "GET".to_string(),
                            url: format!("/{}?_revinclude={}", rtype, revinclude),
                            headers: HashMap::new(),
                            body: None,
                        },
                        validation: ValidationSpec {
                            expected_status: 200,
                            profile_url: profile_url.clone(),
                            required_elements: vec![],
                            forbidden_elements: vec![],
                            response_assertion: assertion_for_kind(
                                &TestCaseKind::Include {
                                    param: revinclude.clone(),
                                    revinclude: true,
                                },
                                rtype,
                            ),
                        },
                    });
                }

                // Result param tests
                for result_param in &["_summary=true", "_count=1", "_elements=id", "_sort=_id"] {
                    let param_name = result_param.split('=').next().unwrap_or(result_param);
                    tests.push(TestCase {
                        name: format!(
                            "search-{}-{}",
                            rtype.to_lowercase(),
                            result_param.replace('=', "_")
                        ),
                        kind: TestCaseKind::ResultParam {
                            param: param_name.to_string(),
                        },
                        interaction: Interaction::SearchType,
                        resource_type: rtype.to_string(),
                        profile_url: profile_url.clone(),
                        request: HttpRequest {
                            method: "GET".to_string(),
                            url: format!("/{}?{}", rtype, result_param),
                            headers: HashMap::new(),
                            body: None,
                        },
                        validation: ValidationSpec {
                            expected_status: 200,
                            profile_url: profile_url.clone(),
                            required_elements: vec![],
                            forbidden_elements: vec![],
                            response_assertion: assertion_for_kind(
                                &TestCaseKind::ResultParam {
                                    param: param_name.to_string(),
                                },
                                rtype,
                            ),
                        },
                    });
                }
            }

            // --- Operation tests ---
            for op in &resource.operation {
                tests.push(TestCase {
                    name: format!("op-{}-{}", rtype.to_lowercase(), op.name),
                    kind: TestCaseKind::Operation {
                        code: op.name.clone(),
                    },
                    interaction: Interaction::Operation(op.name.clone()),
                    resource_type: rtype.to_string(),
                    profile_url: profile_url.clone(),
                    request: HttpRequest {
                        method: "GET".to_string(),
                        url: format!("/{}/${}", rtype, op.name),
                        headers: HashMap::new(),
                        body: None,
                    },
                    validation: ValidationSpec {
                        expected_status: 200,
                        profile_url: profile_url.clone(),
                        required_elements: vec![],
                        forbidden_elements: vec![],
                        response_assertion: assertion_for_kind(
                            &TestCaseKind::Operation {
                                code: op.name.clone(),
                            },
                            rtype,
                        ),
                    },
                });
            }

            // --- Negative tests ---
            // Test undeclared interaction
            let undeclared = ["read", "search-type", "create", "update", "delete"]
                .iter()
                .find(|code| !resource.interaction.iter().any(|i| i.code == **code));
            if let Some(undeclared_code) = undeclared {
                tests.push(TestCase {
                    name: format!(
                        "negative-{}-undeclared-{}",
                        rtype.to_lowercase(),
                        undeclared_code
                    ),
                    kind: TestCaseKind::Negative {
                        description: format!("Undeclared interaction: {}", undeclared_code),
                    },
                    interaction: Interaction::from_code(undeclared_code).unwrap(),
                    resource_type: rtype.to_string(),
                    profile_url: profile_url.clone(),
                    request: HttpRequest {
                        method: match *undeclared_code {
                            "read" | "search-type" => "GET".to_string(),
                            "create" => "POST".to_string(),
                            "update" => "PUT".to_string(),
                            "delete" => "DELETE".to_string(),
                            _ => "GET".to_string(),
                        },
                        url: match *undeclared_code {
                            "read" => format!("/{}/nonexistent", rtype),
                            "search-type" => format!("/{}?name=test", rtype),
                            "create" => format!("/{}", rtype),
                            "update" => format!("/{}/nonexistent", rtype),
                            "delete" => format!("/{}/nonexistent", rtype),
                            _ => format!("/{}", rtype),
                        },
                        headers: HashMap::new(),
                        body: if *undeclared_code == "create" {
                            Some(serde_json::json!({"resourceType": rtype}))
                        } else {
                            None
                        },
                    },
                    validation: ValidationSpec {
                        expected_status: 0, // sentinel: accept non-2xx or 200+Bundle
                        profile_url: None,
                        required_elements: vec![],
                        forbidden_elements: vec![],
                        response_assertion: assertion_for_kind(
                            &TestCaseKind::Negative {
                                description: format!("Undeclared interaction: {}", undeclared_code),
                            },
                            rtype,
                        ),
                    },
                });
            }

            // --- Conformance (mustSupport) tests ---
            tests.extend(generate_conformance_tests(
                rtype,
                &profile_url,
                &resource.supported_profile,
            ));

            if !tests.is_empty() {
                groups.push(TestGroup {
                    resource_type: rtype.to_string(),
                    profile_url,
                    tests,
                });
            }
        }
    }

    FhirTestPlan {
        name: cs
            .name
            .clone()
            .unwrap_or_else(|| "FHIR Test Plan".to_string()),
        ig_url: cs.url.clone(),
        test_groups: groups,
        creation_order: vec![],
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn test_capability_statement() -> CapabilityStatement {
        CapabilityStatement {
            resource_type: "CapabilityStatement".to_string(),
            url: Some("http://example.org/CapabilityStatement/test".to_string()),
            name: Some("TestCS".to_string()),
            status: Some("active".to_string()),
            rest: vec![Rest {
                mode: "server".to_string(),
                resource: vec![RestResource {
                    resource_type: "Patient".to_string(),
                    profile: Some("http://example.org/StructureDefinition/TestPatient".to_string()),
                    supported_profile: vec![],
                    interaction: vec![
                        RestInteraction {
                            code: "read".to_string(),
                        },
                        RestInteraction {
                            code: "search-type".to_string(),
                        },
                        RestInteraction {
                            code: "create".to_string(),
                        },
                    ],
                    search_param: vec![
                        RestSearchParam {
                            name: "name".to_string(),
                            param_type: "string".to_string(),
                            definition: None,
                            documentation: None,
                        },
                        RestSearchParam {
                            name: "birthdate".to_string(),
                            param_type: "date".to_string(),
                            definition: None,
                            documentation: None,
                        },
                    ],
                    operation: vec![],
                    read_history: None,
                    update_create: None,
                    conditional_create: None,
                    conditional_read: None,
                    conditional_update: None,
                    conditional_delete: None,
                    search_include: vec![],
                    search_revinclude: vec![],
                }],
                interaction: vec![],
                operation: vec![],
            }],
        }
    }

    #[test]
    fn generate_plan_with_crud() {
        let cs = test_capability_statement();
        let plan = generate_test_plan(&cs, &[], None, None, &HashMap::new(), &HashMap::new());
        assert!(!plan.test_groups.is_empty());
        let group = &plan.test_groups[0];
        assert_eq!(group.resource_type, "Patient");

        // Should have read, search-type, create tests
        let test_names: Vec<&str> = group.tests.iter().map(|t| t.name.as_str()).collect();
        assert!(test_names.contains(&"read-patient"));
        assert!(test_names.contains(&"create-patient"));
        assert!(test_names.contains(&"search-patient-name"));
    }

    #[test]
    fn generate_plan_with_search_params() {
        let cs = test_capability_statement();
        let plan = generate_test_plan(&cs, &[], None, None, &HashMap::new(), &HashMap::new());
        let group = &plan.test_groups[0];

        let search_tests: Vec<&TestCase> = group
            .tests
            .iter()
            .filter(|t| matches!(t.kind, TestCaseKind::SearchSingle { .. }))
            .collect();
        assert!(search_tests.len() >= 2); // name + birthdate
    }

    #[test]
    fn generate_plan_with_modifiers() {
        let cs = test_capability_statement();
        let plan = generate_test_plan(&cs, &[], None, None, &HashMap::new(), &HashMap::new());
        let group = &plan.test_groups[0];

        let modifier_tests: Vec<&TestCase> = group
            .tests
            .iter()
            .filter(|t| matches!(t.kind, TestCaseKind::SearchModifier { .. }))
            .collect();
        // String params get :exact and :contains modifiers
        assert!(!modifier_tests.is_empty());
    }

    #[test]
    fn generate_plan_with_negative_tests() {
        let cs = test_capability_statement();
        let plan = generate_test_plan(&cs, &[], None, None, &HashMap::new(), &HashMap::new());
        let group = &plan.test_groups[0];

        let negative_tests: Vec<&TestCase> = group
            .tests
            .iter()
            .filter(|t| matches!(t.kind, TestCaseKind::Negative { .. }))
            .collect();
        // Should have at least one negative test for an undeclared interaction
        assert!(!negative_tests.is_empty());
    }

    #[test]
    fn assertion_for_kind_returns_correct_assertions() {
        let search_assertion = assertion_for_kind(
            &TestCaseKind::SearchSingle {
                param_name: "name".to_string(),
                param_type: "string".to_string(),
            },
            "Patient",
        );
        assert!(search_assertion.is_some());
        assert_eq!(
            search_assertion.unwrap().bundle_type,
            Some("searchset".to_string())
        );

        let interaction_assertion = assertion_for_kind(&TestCaseKind::Interaction, "Patient");
        assert!(interaction_assertion.is_none());
    }

    #[test]
    fn generate_near_search_tests_for_location_type() {
        let sp = RestSearchParam {
            name: "near".to_string(),
            param_type: "location".to_string(),
            definition: None,
            documentation: None,
        };
        let tests = generate_near_search_tests("Location", &None, &sp);
        assert_eq!(tests.len(), 2, "should generate :near and :within tests");
        assert!(tests[0].name.contains("near"));
        assert!(tests[1].name.contains("within"));
        assert_eq!(
            tests[0].kind,
            TestCaseKind::SearchNear {
                param: "near".to_string()
            }
        );
    }

    #[test]
    fn generate_near_search_tests_skips_non_location() {
        let sp = RestSearchParam {
            name: "name".to_string(),
            param_type: "string".to_string(),
            definition: None,
            documentation: None,
        };
        let tests = generate_near_search_tests("Patient", &None, &sp);
        assert!(
            tests.is_empty(),
            "string params should not generate near tests"
        );
    }

    #[test]
    fn generate_chained_search_tests_with_expression() {
        let sp = RestSearchParam {
            name: "name".to_string(),
            param_type: "string".to_string(),
            definition: None,
            documentation: None,
        };
        let search_params = vec![SearchParameter {
            resource_type: "SearchParameter".to_string(),
            url: "http://example.org/SearchParameter/Patient-name".to_string(),
            name: "name".to_string(),
            code: "name".to_string(),
            base: vec!["Patient".to_string()],
            param_type: "string".to_string(),
            expression: Some("Patient.name".to_string()),
            description: None,
        }];
        let tests = generate_chained_search_tests("Patient", &None, &sp, &search_params);
        assert_eq!(tests.len(), 1, "should generate one chained search test");
        assert!(tests[0].name.contains("chained"));
        if let TestCaseKind::SearchChained { param, chain } = &tests[0].kind {
            assert_eq!(param, "name");
            assert!(chain.contains('.'));
        } else {
            panic!("Expected SearchChained variant");
        }
    }

    #[test]
    fn generate_chained_search_tests_skips_without_expression() {
        let sp = RestSearchParam {
            name: "name".to_string(),
            param_type: "string".to_string(),
            definition: None,
            documentation: None,
        };
        let tests = generate_chained_search_tests("Patient", &None, &sp, &[]);
        assert!(
            tests.is_empty(),
            "no expression should yield no chained tests"
        );
    }

    #[test]
    fn generate_conformance_tests_with_profile() {
        let tests = generate_conformance_tests(
            "Patient",
            &Some("http://example.org/StructureDefinition/TestPatient".to_string()),
            &[],
        );
        assert_eq!(tests.len(), 1, "should generate one conformance test");
        assert!(tests[0].name.contains("mustsupport"));
        assert_eq!(
            tests[0].kind,
            TestCaseKind::Conformance {
                resource_type: "Patient".to_string(),
                profile_url: "http://example.org/StructureDefinition/TestPatient".to_string(),
            }
        );
        assert_eq!(tests[0].validation.required_elements, vec!["id", "meta"]);
    }

    #[test]
    fn generate_conformance_tests_with_supported_profiles() {
        let tests = generate_conformance_tests(
            "Patient",
            &None,
            &["http://example.org/StructureDefinition/SupportedPatient".to_string()],
        );
        assert_eq!(
            tests.len(),
            1,
            "should generate one conformance test per supported profile"
        );
        assert!(tests[0].name.contains("mustsupport"));
    }

    #[test]
    fn generate_plan_includes_near_chained_conformance() {
        let mut cs = test_capability_statement();
        // Add a location-type search param to trigger near tests
        cs.rest[0].resource[0].search_param.push(RestSearchParam {
            name: "position".to_string(),
            param_type: "location".to_string(),
            definition: None,
            documentation: None,
        });
        // Add a supported profile to trigger conformance tests
        cs.rest[0].resource[0].supported_profile =
            vec!["http://example.org/StructureDefinition/SupportedPatient".to_string()];

        let search_params = vec![SearchParameter {
            resource_type: "SearchParameter".to_string(),
            url: "http://example.org/SearchParameter/Patient-name".to_string(),
            name: "name".to_string(),
            code: "name".to_string(),
            base: vec!["Patient".to_string()],
            param_type: "string".to_string(),
            expression: Some("Patient.name".to_string()),
            description: None,
        }];

        let plan = generate_test_plan(
            &cs,
            &search_params,
            None,
            None,
            &HashMap::new(),
            &HashMap::new(),
        );
        let group = &plan.test_groups[0];

        let near_tests: Vec<&TestCase> = group
            .tests
            .iter()
            .filter(|t| matches!(t.kind, TestCaseKind::SearchNear { .. }))
            .collect();
        assert!(!near_tests.is_empty(), "should have near search tests");

        let chained_tests: Vec<&TestCase> = group
            .tests
            .iter()
            .filter(|t| matches!(t.kind, TestCaseKind::SearchChained { .. }))
            .collect();
        assert!(
            !chained_tests.is_empty(),
            "should have chained search tests"
        );

        let conformance_tests: Vec<&TestCase> = group
            .tests
            .iter()
            .filter(|t| matches!(t.kind, TestCaseKind::Conformance { .. }))
            .collect();
        assert!(
            !conformance_tests.is_empty(),
            "should have conformance tests"
        );
    }
}
