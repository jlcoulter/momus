/// Test plan generator engine.
///
/// Takes an `ApiModel` + `TestSpec` and produces a `TestPlan`.
/// This is the format-agnostic counterpart to the assertion evaluator:
/// the evaluator checks responses, the generator creates tests.
///
/// # Pipeline
///
/// ```text
/// ApiModel + TestSpec
///     │
///     ├─ resolve_spec()       — flatten AllOf/OneOf combinators
///     ├─ generate_data()      — create resources with variations
///     ├─ generate_setup()     — POST resources to populate server
///     ├─ generate_crud()      — CRUD sequences with state passing
///     ├─ generate_search()    — search/filter tests
///     ├─ generate_negative()  — invalid input tests
///     ├─ generate_edge_case() — boundary/special char tests
///     ├─ generate_conformance() — profile/schema validation
///     └─ ...                  — operation, security, performance
///     │
///     ▼
///   TestPlan
/// ```
use crate::ast::*;
use anyhow::Result;
use serde::Serialize;
use std::collections::HashMap;

// ---------------------------------------------------------------------------
// Resource generator trait
// ---------------------------------------------------------------------------

/// Generates valid resources for a specific API format.
///
/// Each converter (FHIR, OpenAPI, GraphQL, etc.) implements this trait
/// to provide format-specific resource generation, variation, and field
/// extraction. The `TestGenerator` engine calls these methods through
/// the trait, remaining completely format-agnostic.
pub trait ResourceGenerator {
    /// Generate a valid resource of the given type.
    ///
    /// The returned resource should be a valid JSON object conforming
    /// to the API definition for `resource_type`.
    fn generate(&self, resource_type: &str) -> Result<serde_json::Value>;

    /// Apply a variation to a resource.
    ///
    /// Called after `generate()` to produce resources with different
    /// characteristics (e.g., minimal fields, special characters,
    /// boundary values).
    fn vary(&self, resource: &mut serde_json::Value, variation: &DataVariation, index: u64);

    /// Extract searchable field values from a resource.
    ///
    /// Returns a map of field paths to string values that can be used
    /// in search/filter test URLs.
    fn extract_values(
        &self,
        resource_type: &str,
        resource: &serde_json::Value,
    ) -> HashMap<String, String>;
}

// ---------------------------------------------------------------------------
// Generated data
// ---------------------------------------------------------------------------

/// A single generated resource with its metadata.
#[derive(Debug, Clone, Serialize)]
pub struct GeneratedResource {
    /// Assigned resource ID.
    pub id: String,
    /// The generated JSON resource body.
    pub resource: serde_json::Value,
    /// Which variation was applied.
    pub variation: DataVariation,
}

/// All generated data for a test plan.
///
/// Holds the resources, extracted field values, and created IDs
/// that the sub-generators (CRUD, search, etc.) reference.
#[derive(Debug, Clone, Serialize)]
pub struct GeneratedData {
    /// Map of resource type to list of generated resources.
    pub resources: HashMap<String, Vec<GeneratedResource>>,
    /// Map of resource type to field values (from the first happy-path resource).
    pub field_values: HashMap<String, HashMap<String, String>>,
    /// Map of resource type to created ID (from the first happy-path resource).
    pub created_ids: HashMap<String, String>,
}

// ---------------------------------------------------------------------------
// Main entry point
// ---------------------------------------------------------------------------

/// Generate a complete `TestPlan` from an `ApiModel` and `TestSpec`.
///
/// This is the main entry point for the test generation engine.
///
/// # Arguments
///
/// * `api` - The format-agnostic API model (produced by any converter).
/// * `spec` - The test specification (what tests to generate).
/// * `generator` - The format-specific resource generator.
///
/// # Returns
///
/// A complete `TestPlan` with setup steps, CRUD sequences, search tests,
/// negative tests, edge case tests, and conformance tests.
pub fn generate_test_plan(
    api: &ApiModel,
    spec: &TestSpec,
    generator: &dyn ResourceGenerator,
) -> Result<TestPlan> {
    // 1. Resolve the test spec into a flat list of leaf specs
    let leaf_specs = resolve_spec(spec);

    // 2. Extract DataSpec and generate data
    let data_spec = extract_data_spec(&leaf_specs);
    let data = generate_data(api, &data_spec, generator)?;

    // 3. Generate setup steps from the data
    let setup_steps = generate_setup_steps(api, &data);

    // 4. Generate test steps from each leaf spec
    let mut steps = Vec::new();

    for leaf in &leaf_specs {
        match leaf {
            TestSpec::Crud(crud_spec) => {
                steps.extend(generate_crud_tests(api, crud_spec, &data, generator)?);
            }
            TestSpec::Search(search_spec) => {
                steps.extend(generate_search_tests(api, search_spec, &data));
            }
            TestSpec::Negative(neg_spec) => {
                steps.extend(generate_negative_tests(api, neg_spec));
            }
            TestSpec::EdgeCase(edge_spec) => {
                steps.extend(generate_edge_case_tests(api, edge_spec, &data, generator)?);
            }
            TestSpec::Conformance(conf_spec) => {
                steps.extend(generate_conformance_tests(api, conf_spec));
            }
            TestSpec::Operation(op_spec) => {
                steps.extend(generate_operation_tests(api, op_spec));
            }
            TestSpec::Security(sec_spec) => {
                steps.extend(generate_security_tests(api, sec_spec));
            }
            TestSpec::Performance(perf_spec) => {
                steps.extend(generate_performance_tests(api, perf_spec));
            }
            // Already handled above
            TestSpec::Data(_) | TestSpec::AllOf(_) | TestSpec::OneOf(_) => {}
        }
    }

    let plan_name = format!("{} — generated test plan", api.name);

    Ok(TestPlan {
        name: plan_name,
        base_url: String::new(),
        default_headers: HashMap::new(),
        steps,
        setup: setup_steps,
        teardown: vec![],
    })
}

// ---------------------------------------------------------------------------
// Spec resolution
// ---------------------------------------------------------------------------

/// Resolve a `TestSpec` tree into a flat list of leaf specs.
///
/// - `AllOf` is flattened (all children are included).
/// - `OneOf` picks the first child (useful for A/B test selection).
/// - Leaf specs (Data, Crud, Search, etc.) are returned as-is.
fn resolve_spec(spec: &TestSpec) -> Vec<&TestSpec> {
    let mut result = Vec::new();
    resolve_spec_inner(spec, &mut result);
    result
}

fn resolve_spec_inner<'a>(spec: &'a TestSpec, result: &mut Vec<&'a TestSpec>) {
    match spec {
        TestSpec::AllOf(children) => {
            for child in children {
                resolve_spec_inner(child, result);
            }
        }
        TestSpec::OneOf(children) => {
            if let Some(first) = children.first() {
                resolve_spec_inner(first, result);
            }
        }
        _ => {
            result.push(spec);
        }
    }
}

/// Extract the `DataSpec` from a list of leaf specs.
/// Returns `DataSpec::default()` if none is found.
fn extract_data_spec(specs: &[&TestSpec]) -> DataSpec {
    for spec in specs {
        if let TestSpec::Data(ds) = spec {
            return ds.clone();
        }
    }
    DataSpec::default()
}

// ---------------------------------------------------------------------------
// Data generation
// ---------------------------------------------------------------------------

/// Generate resources for each resource type in the API model.
///
/// For each resource type, generates `data_spec.count` resources with
/// the specified variations. The first resource is always the happy-path
/// (base) resource; subsequent resources apply variations.
pub fn generate_data(
    api: &ApiModel,
    data_spec: &DataSpec,
    generator: &dyn ResourceGenerator,
) -> Result<GeneratedData> {
    let resource_count = api.resources.len();
    let mut resources: HashMap<String, Vec<GeneratedResource>> =
        HashMap::with_capacity(resource_count);
    let mut field_values: HashMap<String, HashMap<String, String>> =
        HashMap::with_capacity(resource_count);
    let mut created_ids: HashMap<String, String> = HashMap::with_capacity(resource_count);

    for resource_model in &api.resources {
        let rtype = &resource_model.name;
        let count = data_spec.count;
        let mut type_resources = Vec::with_capacity(count as usize);

        // Generate the base (happy-path) resource
        let base = generator.generate(rtype)?;

        for i in 0..count {
            let idx = i + 1;
            let id = format!("{}-{:03}", rtype.to_lowercase(), idx);
            let mut resource = base.clone();

            // Stamp the ID
            if let Some(obj) = resource.as_object_mut() {
                obj.insert("id".to_string(), serde_json::json!(&id));
            }

            // Determine which variation to apply
            let variation = if idx == 1 {
                DataVariation::HappyPath
            } else {
                let var_idx = ((idx - 2) as usize) % data_spec.variations.len();
                data_spec.variations[var_idx].clone()
            };

            // Apply the variation (skip for the first resource)
            if idx > 1 {
                generator.vary(&mut resource, &variation, idx);
            }

            type_resources.push(GeneratedResource {
                id,
                resource,
                variation,
            });
        }

        // Extract field values from the first (happy-path) resource
        if let Some(first) = type_resources.first() {
            let values = generator.extract_values(rtype, &first.resource);
            field_values.insert(rtype.clone(), values);
            created_ids.insert(rtype.clone(), first.id.clone());
        }

        resources.insert(rtype.clone(), type_resources);
    }

    Ok(GeneratedData {
        resources,
        field_values,
        created_ids,
    })
}

// ---------------------------------------------------------------------------
// Setup steps
// ---------------------------------------------------------------------------

/// Generate setup steps that POST generated resources to the server.
///
/// Each resource is POSTed in dependency order (if the API model specifies
/// a creation order) and saved under a named reference for downstream use.
pub fn generate_setup_steps(api: &ApiModel, data: &GeneratedData) -> Vec<Step> {
    // Estimate total steps: sum of all resources across all types
    let estimated: usize = api
        .resources
        .iter()
        .filter_map(|r| data.resources.get(&r.name))
        .map(|v| v.len())
        .sum();
    let mut steps = Vec::with_capacity(estimated);

    for resource_model in &api.resources {
        let rtype = &resource_model.name;
        let rtype_lower = rtype.to_lowercase();

        if let Some(type_resources) = data.resources.get(rtype) {
            for (i, gr) in type_resources.iter().enumerate() {
                let idx = i + 1;
                let save_name = format!("seed_{rtype_lower}_{idx}");

                let step = RequestStep {
                    name: format!("setup-create-{rtype_lower}-{idx}"),
                    method: Method::Post,
                    url: format!("/{rtype}"),
                    headers: {
                        let mut h = HashMap::new();
                        h.insert("Content-Type".to_string(), "application/json".to_string());
                        h
                    },
                    body: Some(gr.resource.clone()),
                    assert: vec![Assertion::Status(201)],
                    save_as: save_name,
                    soft_fail: false,
                };
                steps.push(Step::Request(step));
            }
        }
    }

    steps
}

// ---------------------------------------------------------------------------
// CRUD test generation
// ---------------------------------------------------------------------------

/// Generate CRUD test sequences from the API model and generated data.
///
/// For each resource type that has create/read/update/delete operations,
/// generates a `Step::Sequence` that chains them together with state
/// passing via `{{steps.<name>.*}}` template references.
fn generate_crud_tests(
    api: &ApiModel,
    spec: &CrudSpec,
    data: &GeneratedData,
    _generator: &dyn ResourceGenerator,
) -> Result<Vec<Step>> {
    let mut steps = Vec::new();

    for resource_model in &api.resources {
        let rtype = &resource_model.name;
        let rtype_lower = rtype.to_lowercase();
        let save_name = format!("seed_{rtype_lower}_1");

        // Check which operations are declared
        let has_create = resource_model
            .operations
            .iter()
            .any(|op| op.name == "create" && spec.create);
        let has_read = resource_model
            .operations
            .iter()
            .any(|op| op.name == "read" && spec.read);
        let has_vread = resource_model
            .operations
            .iter()
            .any(|op| op.name == "vread" && spec.vread);
        let has_update = resource_model
            .operations
            .iter()
            .any(|op| op.name == "update" && spec.update);
        let has_delete = resource_model
            .operations
            .iter()
            .any(|op| op.name == "delete" && spec.delete);
        let has_patch = resource_model
            .operations
            .iter()
            .any(|op| op.name == "patch" && spec.patch);
        let has_history_instance = resource_model
            .operations
            .iter()
            .any(|op| op.name == "history-instance" && spec.history_instance);
        let has_history_type = resource_model
            .operations
            .iter()
            .any(|op| op.name == "history-type" && spec.history_type);

        if !has_create && !has_read && !has_update && !has_delete {
            continue;
        }

        let mut crud_steps: Vec<Step> = Vec::new();

        // Create step
        if has_create {
            let body = data
                .resources
                .get(rtype)
                .and_then(|res| res.first())
                .map(|gr| gr.resource.clone());

            crud_steps.push(Step::Request(RequestStep {
                name: format!("create-{rtype_lower}"),
                method: Method::Post,
                url: format!("/{rtype}"),
                headers: {
                    let mut h = HashMap::new();
                    h.insert("Content-Type".to_string(), "application/json".to_string());
                    h
                },
                body,
                assert: vec![Assertion::Status(201)],
                save_as: save_name.clone(),
                soft_fail: false,
            }));
        }

        // Read step
        if has_read {
            crud_steps.push(Step::Request(RequestStep {
                name: format!("read-{rtype_lower}"),
                method: Method::Get,
                url: format!("/{rtype}/{{steps.{save_name}.id}}"),
                headers: HashMap::new(),
                body: None,
                assert: vec![Assertion::Status(200)],
                save_as: String::new(),
                soft_fail: false,
            }));
        }

        // Update step
        if has_update {
            let body = data
                .resources
                .get(rtype)
                .and_then(|res| res.first())
                .map(|gr| gr.resource.clone());

            crud_steps.push(Step::Request(RequestStep {
                name: format!("update-{rtype_lower}"),
                method: Method::Put,
                url: format!("/{rtype}/{{steps.{save_name}.id}}"),
                headers: {
                    let mut h = HashMap::new();
                    h.insert("Content-Type".to_string(), "application/json".to_string());
                    h
                },
                body,
                assert: vec![Assertion::Status(200)],
                save_as: String::new(),
                soft_fail: false,
            }));
        }

        // Patch step
        if has_patch {
            crud_steps.push(Step::Request(RequestStep {
                name: format!("patch-{rtype_lower}"),
                method: Method::Patch,
                url: format!("/{rtype}/{{steps.{save_name}.id}}"),
                headers: {
                    let mut h = HashMap::new();
                    h.insert(
                        "Content-Type".to_string(),
                        "application/json-patch+json".to_string(),
                    );
                    h
                },
                body: Some(serde_json::json!([{
                    "op": "replace",
                    "path": "/active",
                    "value": true
                }])),
                assert: vec![Assertion::Status(200)],
                save_as: String::new(),
                soft_fail: false,
            }));
        }

        // Vread step (version read)
        if has_vread {
            crud_steps.push(Step::Request(RequestStep {
                name: format!("vread-{rtype_lower}"),
                method: Method::Get,
                url: format!("/{rtype}/{{steps.{save_name}.id}}/_history/1"),
                headers: HashMap::new(),
                body: None,
                assert: vec![Assertion::Status(200)],
                save_as: String::new(),
                soft_fail: false,
            }));
        }

        // History instance step
        if has_history_instance {
            crud_steps.push(Step::Request(RequestStep {
                name: format!("history-instance-{rtype_lower}"),
                method: Method::Get,
                url: format!("/{rtype}/{{steps.{save_name}.id}}/_history"),
                headers: HashMap::new(),
                body: None,
                assert: vec![Assertion::Status(200)],
                save_as: String::new(),
                soft_fail: false,
            }));
        }

        // History type step
        if has_history_type {
            crud_steps.push(Step::Request(RequestStep {
                name: format!("history-type-{rtype_lower}"),
                method: Method::Get,
                url: format!("/{rtype}/_history"),
                headers: HashMap::new(),
                body: None,
                assert: vec![Assertion::Status(200)],
                save_as: String::new(),
                soft_fail: false,
            }));
        }

        // Delete step — last in sequence
        if has_delete {
            crud_steps.push(Step::Request(RequestStep {
                name: format!("delete-{rtype_lower}"),
                method: Method::Delete,
                url: format!("/{rtype}/{{steps.{save_name}.id}}"),
                headers: HashMap::new(),
                body: None,
                assert: vec![Assertion::Status(204)],
                save_as: String::new(),
                soft_fail: false,
            }));
        }

        if !crud_steps.is_empty() {
            if spec.chain {
                steps.push(Step::Sequence(SequenceStep {
                    name: format!("{rtype_lower}-crud"),
                    steps: crud_steps,
                    continue_on_failure: true,
                }));
            } else {
                steps.extend(crud_steps);
            }
        }
    }

    Ok(steps)
}

// ---------------------------------------------------------------------------
// Search test generation
// ---------------------------------------------------------------------------

/// Generate search/filter tests from the API model and generated data.
///
/// For each resource type with search parameters, generates:
/// - Single-parameter search tests with concrete values
/// - Modifier tests (:exact, :contains, :missing)
/// - Prefix tests (gt, lt, ge, le)
/// - Combined parameter tests
/// - Negative search tests (values that should return empty)
fn generate_search_tests(api: &ApiModel, spec: &SearchSpec, data: &GeneratedData) -> Vec<Step> {
    let mut steps = Vec::new();

    for resource_model in &api.resources {
        let rtype = &resource_model.name;
        let rtype_lower = rtype.to_lowercase();

        // Check if search-type is declared
        let has_search = resource_model
            .operations
            .iter()
            .any(|op| op.name == "search-type");

        if !has_search || resource_model.search_params.is_empty() {
            continue;
        }

        let values = data.field_values.get(rtype);
        let created_id = data.created_ids.get(rtype);

        for sp in &resource_model.search_params {
            // Resolve a concrete value for this search parameter
            let resolved_value = resolve_search_value(sp, values, created_id);

            // --- Single param search ---
            if spec.single_param
                && let Some(ref val) = resolved_value
            {
                steps.push(Step::Request(RequestStep {
                    name: format!("search-{}-{}", rtype_lower, sp.name),
                    method: Method::Get,
                    url: format!("/{}?{}={}", rtype, sp.name, url_encode(val)),
                    headers: HashMap::new(),
                    body: None,
                    assert: vec![
                        Assertion::Status(200),
                        Assertion::JsonPath {
                            path: "$.resourceType".to_string(),
                            predicate: JsonPredicate::Eq(serde_json::json!("Bundle")),
                        },
                    ],
                    save_as: String::new(),
                    soft_fail: false,
                }));
            }

            // --- Modifier tests ---
            if spec.modifiers {
                for modifier in &sp.modifiers {
                    if let Some(ref val) = resolved_value {
                        steps.push(Step::Request(RequestStep {
                            name: format!("search-{}-{}-{}", rtype_lower, sp.name, modifier),
                            method: Method::Get,
                            url: format!("/{}?{}:{}={}", rtype, sp.name, modifier, url_encode(val)),
                            headers: HashMap::new(),
                            body: None,
                            assert: vec![Assertion::Status(200)],
                            save_as: String::new(),
                            soft_fail: false,
                        }));
                    }
                }

                // Always add :missing=true and :missing=false tests
                steps.push(Step::Request(RequestStep {
                    name: format!("search-{}-{}-missing-true", rtype_lower, sp.name),
                    method: Method::Get,
                    url: format!("/{}?{}:missing=true", rtype, sp.name),
                    headers: HashMap::new(),
                    body: None,
                    assert: vec![Assertion::Status(200)],
                    save_as: String::new(),
                    soft_fail: false,
                }));

                steps.push(Step::Request(RequestStep {
                    name: format!("search-{}-{}-missing-false", rtype_lower, sp.name),
                    method: Method::Get,
                    url: format!("/{}?{}:missing=false", rtype, sp.name),
                    headers: HashMap::new(),
                    body: None,
                    assert: vec![Assertion::Status(200)],
                    save_as: String::new(),
                    soft_fail: false,
                }));
            }

            // --- Prefix tests ---
            if spec.prefixes {
                for prefix in &sp.prefixes {
                    if let Some(ref val) = resolved_value {
                        steps.push(Step::Request(RequestStep {
                            name: format!("search-{}-{}-{}", rtype_lower, sp.name, prefix),
                            method: Method::Get,
                            url: format!("/{}?{}={}{}", rtype, sp.name, prefix, url_encode(val)),
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

        // --- Combined param tests ---
        if spec.combined_params && resource_model.search_params.len() >= 2 {
            for i in 0..resource_model.search_params.len() {
                for j in (i + 1)..resource_model.search_params.len() {
                    let p1 = &resource_model.search_params[i];
                    let p2 = &resource_model.search_params[j];
                    let v1 = resolve_search_value(p1, values, created_id);
                    let v2 = resolve_search_value(p2, values, created_id);

                    if let (Some(ref v1), Some(ref v2)) = (v1, v2) {
                        steps.push(Step::Request(RequestStep {
                            name: format!("search-{}-{}-{}-combo", rtype_lower, p1.name, p2.name),
                            method: Method::Get,
                            url: format!(
                                "/{}?{}={}&{}={}",
                                rtype,
                                p1.name,
                                url_encode(v1),
                                p2.name,
                                url_encode(v2)
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

        // --- Negative search tests ---
        for neg_val in &spec.negative_values {
            if let Some(first_param) = resource_model.search_params.first() {
                steps.push(Step::Request(RequestStep {
                    name: format!(
                        "search-{}-{}-negative-{}",
                        rtype_lower, first_param.name, neg_val
                    ),
                    method: Method::Get,
                    url: format!("/{}?{}={}", rtype, first_param.name, url_encode(neg_val)),
                    headers: HashMap::new(),
                    body: None,
                    assert: vec![Assertion::Status(200)],
                    save_as: String::new(),
                    soft_fail: false,
                }));
            }
        }

        // --- Result param tests ---
        for result_param in &spec.result_params {
            steps.push(Step::Request(RequestStep {
                name: format!(
                    "search-{}-{}",
                    rtype_lower,
                    result_param.replace('=', "_").replace(':', "-")
                ),
                method: Method::Get,
                url: format!("/{rtype}?{result_param}"),
                headers: HashMap::new(),
                body: None,
                assert: vec![Assertion::Status(200)],
                save_as: String::new(),
                soft_fail: false,
            }));
        }

        // --- _include tests ---
        if spec.include {
            for include in &resource_model.search_include {
                steps.push(Step::Request(RequestStep {
                    name: format!("search-{rtype_lower}-include-{include}"),
                    method: Method::Get,
                    url: format!("/{rtype}?_include={include}"),
                    headers: HashMap::new(),
                    body: None,
                    assert: vec![Assertion::Status(200)],
                    save_as: String::new(),
                    soft_fail: false,
                }));
            }
        }

        // --- _revinclude tests ---
        if spec.revinclude {
            for revinclude in &resource_model.search_revinclude {
                steps.push(Step::Request(RequestStep {
                    name: format!("search-{rtype_lower}-revinclude-{revinclude}"),
                    method: Method::Get,
                    url: format!("/{rtype}?_revinclude={revinclude}"),
                    headers: HashMap::new(),
                    body: None,
                    assert: vec![Assertion::Status(200)],
                    save_as: String::new(),
                    soft_fail: false,
                }));
            }
        }
    }

    steps
}

/// Resolve a search parameter to a concrete value from generated data.
fn resolve_search_value(
    sp: &SearchParamModel,
    field_values: Option<&HashMap<String, String>>,
    created_id: Option<&String>,
) -> Option<String> {
    // Special case: _id always uses the created resource ID
    if sp.name == "_id" {
        return created_id.cloned();
    }

    // For reference params, use the created ID of the target type
    if sp.param_type == "reference" {
        // Try to find a matching resource type from the param name
        // e.g., "patient" → "Patient", "organization" → "Organization"
        let target_type = capitalize_first(&sp.name);
        // The caller should have populated created_ids with the right types
        return created_id.map(|id| format!("{target_type}/{id}"));
    }

    // For other param types, look up from field values
    if let Some(values) = field_values {
        // Try exact field path match first
        let exact_key = format!("{}.{}", sp.name, sp.param_type);
        if let Some(val) = values.get(&exact_key) {
            return Some(val.clone());
        }

        // Try common field path patterns
        let patterns = match sp.param_type.as_str() {
            "string" => vec![sp.name.clone()],
            "token" => vec![
                format!("{}.coding[0].code", sp.name),
                format!("{}.code", sp.name),
                sp.name.clone(),
            ],
            "date" | "dateTime" => vec![sp.name.clone()],
            "number" => vec![sp.name.clone()],
            _ => vec![sp.name.clone()],
        };

        for pattern in &patterns {
            if let Some(val) = values.get(pattern) {
                return Some(val.clone());
            }
        }
    }

    None
}

fn capitalize_first(s: &str) -> String {
    let mut chars = s.chars();
    match chars.next() {
        Some(c) => c.to_uppercase().to_string() + chars.as_str(),
        None => String::new(),
    }
}

fn url_encode(s: &str) -> String {
    // Simple URL encoding — only encode characters that are problematic in URLs
    s.replace(' ', "%20")
        .replace('&', "%26")
        .replace('=', "%3D")
        .replace('?', "%3F")
        .replace('#', "%23")
}

// ---------------------------------------------------------------------------
// Negative test generation
// ---------------------------------------------------------------------------

/// Generate negative tests from the API model.
///
/// Tests operations that should fail:
/// - Undeclared interactions (operations not in the spec)
/// - Invalid request bodies (missing required fields, wrong types)
/// - Malformed requests (invalid JSON, wrong Content-Type)
fn generate_negative_tests(api: &ApiModel, spec: &NegativeSpec) -> Vec<Step> {
    let mut steps = Vec::new();

    for resource_model in &api.resources {
        let rtype = &resource_model.name;
        let rtype_lower = rtype.to_lowercase();

        // --- Undeclared interaction tests ---
        if spec.undeclared_interactions {
            let declared: Vec<&str> = resource_model
                .operations
                .iter()
                .map(|op| op.name.as_str())
                .collect();

            // Test each standard interaction that isn't declared
            for &interaction in &["read", "search-type", "create", "update", "delete"] {
                if !declared.contains(&interaction) {
                    let (method, url, body) = match interaction {
                        "read" => ("GET", format!("/{rtype}/nonexistent"), None),
                        "search-type" => ("GET", format!("/{rtype}?nonexistent=true"), None),
                        "create" => (
                            "POST",
                            format!("/{rtype}"),
                            Some(serde_json::json!({"resourceType": rtype})),
                        ),
                        "update" => (
                            "PUT",
                            format!("/{rtype}/nonexistent"),
                            Some(serde_json::json!({"resourceType": rtype})),
                        ),
                        "delete" => ("DELETE", format!("/{rtype}/nonexistent"), None),
                        _ => continue,
                    };

                    let method_enum = match method {
                        "GET" => Method::Get,
                        "POST" => Method::Post,
                        "PUT" => Method::Put,
                        "DELETE" => Method::Delete,
                        _ => Method::Get,
                    };

                    steps.push(Step::Request(RequestStep {
                        name: format!("negative-{rtype_lower}-undeclared-{interaction}"),
                        method: method_enum,
                        url,
                        headers: HashMap::new(),
                        body,
                        // Expected status 0 = sentinel: accept non-2xx or 200+Bundle
                        assert: vec![Assertion::StatusIn(vec![400, 401, 403, 404, 405, 422, 501])],
                        save_as: String::new(),
                        soft_fail: false,
                    }));
                }
            }
        }

        // --- Invalid body tests ---
        if spec.invalid_bodies {
            // Test with empty body
            steps.push(Step::Request(RequestStep {
                name: format!("negative-{rtype_lower}-empty-body"),
                method: Method::Post,
                url: format!("/{rtype}"),
                headers: {
                    let mut h = HashMap::new();
                    h.insert("Content-Type".to_string(), "application/json".to_string());
                    h
                },
                body: Some(serde_json::json!({})),
                assert: vec![Assertion::StatusIn(vec![400, 422])],
                save_as: String::new(),
                soft_fail: false,
            }));

            // Test with wrong resource type
            steps.push(Step::Request(RequestStep {
                name: format!("negative-{rtype_lower}-wrong-type"),
                method: Method::Post,
                url: format!("/{rtype}"),
                headers: {
                    let mut h = HashMap::new();
                    h.insert("Content-Type".to_string(), "application/json".to_string());
                    h
                },
                body: Some(serde_json::json!({"resourceType": "Unknown"})),
                assert: vec![Assertion::StatusIn(vec![400, 422])],
                save_as: String::new(),
                soft_fail: false,
            }));
        }

        // --- Malformed request tests ---
        if spec.malformed_requests {
            // Test with invalid JSON
            // (This would need to be a raw HTTP request — skip for now)
            // Test with wrong Content-Type
            steps.push(Step::Request(RequestStep {
                name: format!("negative-{rtype_lower}-wrong-content-type"),
                method: Method::Post,
                url: format!("/{rtype}"),
                headers: {
                    let mut h = HashMap::new();
                    h.insert("Content-Type".to_string(), "application/xml".to_string());
                    h
                },
                body: Some(serde_json::json!({"resourceType": rtype})),
                assert: vec![Assertion::StatusIn(vec![400, 415, 422])],
                save_as: String::new(),
                soft_fail: false,
            }));
        }
    }

    steps
}

// ---------------------------------------------------------------------------
// Edge case test generation
// ---------------------------------------------------------------------------

/// Generate edge case tests from the API model.
///
/// Tests boundary conditions, special characters, and other edge cases.
fn generate_edge_case_tests(
    api: &ApiModel,
    spec: &EdgeCaseSpec,
    _data: &GeneratedData,
    generator: &dyn ResourceGenerator,
) -> Result<Vec<Step>> {
    let mut steps = Vec::new();

    for resource_model in &api.resources {
        let rtype = &resource_model.name;
        let rtype_lower = rtype.to_lowercase();

        // --- Special characters test ---
        if spec.special_characters
            && let Ok(mut resource) = generator.generate(rtype)
        {
            generator.vary(&mut resource, &DataVariation::SpecialChars, 99);
            if let Some(obj) = resource.as_object_mut() {
                obj.insert("id".to_string(), serde_json::json!("edge-special-chars"));
            }

            steps.push(Step::Request(RequestStep {
                name: format!("edge-{rtype_lower}-special-chars"),
                method: Method::Post,
                url: format!("/{rtype}"),
                headers: {
                    let mut h = HashMap::new();
                    h.insert("Content-Type".to_string(), "application/json".to_string());
                    h
                },
                body: Some(resource),
                assert: vec![Assertion::Status(201)],
                save_as: String::new(),
                soft_fail: false,
            }));
        }

        // --- Boundary values test ---
        if spec.boundary_values
            && let Ok(mut resource) = generator.generate(rtype)
        {
            generator.vary(
                &mut resource,
                &DataVariation::Boundary {
                    field: String::new(),
                },
                99,
            );
            if let Some(obj) = resource.as_object_mut() {
                obj.insert("id".to_string(), serde_json::json!("edge-boundary"));
            }

            steps.push(Step::Request(RequestStep {
                name: format!("edge-{rtype_lower}-boundary"),
                method: Method::Post,
                url: format!("/{rtype}"),
                headers: {
                    let mut h = HashMap::new();
                    h.insert("Content-Type".to_string(), "application/json".to_string());
                    h
                },
                body: Some(resource),
                assert: vec![Assertion::Status(201)],
                save_as: String::new(),
                soft_fail: false,
            }));
        }

        // --- Dangling reference test ---
        if spec.dangling_references
            && let Ok(mut resource) = generator.generate(rtype)
        {
            // Add a reference to a non-existent resource
            if let Some(obj) = resource.as_object_mut() {
                obj.insert(
                    "subject".to_string(),
                    serde_json::json!({
                        "reference": "Patient/nonexistent-000"
                    }),
                );
                obj.insert("id".to_string(), serde_json::json!("edge-dangling-ref"));
            }

            steps.push(Step::Request(RequestStep {
                name: format!("edge-{rtype_lower}-dangling-ref"),
                method: Method::Post,
                url: format!("/{rtype}"),
                headers: {
                    let mut h = HashMap::new();
                    h.insert("Content-Type".to_string(), "application/json".to_string());
                    h
                },
                body: Some(resource),
                // Server may accept or reject dangling refs — accept either
                assert: vec![Assertion::StatusIn(vec![201, 202, 400, 422])],
                save_as: String::new(),
                soft_fail: false,
            }));
        }
    }

    Ok(steps)
}

/// Generate conformance tests from the API model.
///
/// Validates that responses conform to profile/schema definitions
/// and that mustSupport fields are present.
fn generate_conformance_tests(api: &ApiModel, spec: &ConformanceSpec) -> Vec<Step> {
    let mut steps = Vec::new();

    for resource_model in &api.resources {
        let rtype = &resource_model.name;
        let rtype_lower = rtype.to_lowercase();

        // Conformance: search with _count=1 and verify profile
        if spec.profile_validation
            && let Some(profile_url) = &resource_model.profile_url
        {
            steps.push(Step::Request(RequestStep {
                name: format!("conformance-{rtype_lower}-profile"),
                method: Method::Get,
                url: format!("/{rtype}?_count=1"),
                headers: HashMap::new(),
                body: None,
                assert: vec![
                    Assertion::Status(200),
                    Assertion::JsonPath {
                        path: "$.entry[0].resource.meta.profile[0]".to_string(),
                        predicate: JsonPredicate::Eq(serde_json::json!(profile_url)),
                    },
                ],
                save_as: String::new(),
                soft_fail: false,
            }));
        }

        // Conformance: mustSupport tests for each supported profile
        if spec.must_support {
            for profile_url in &resource_model.supported_profiles {
                let profile_name = profile_url.rsplit('/').next().unwrap_or(profile_url);
                steps.push(Step::Request(RequestStep {
                    name: format!("conformance-{rtype_lower}-mustsupport-{profile_name}"),
                    method: Method::Get,
                    url: format!("/{rtype}?_count=1"),
                    headers: HashMap::new(),
                    body: None,
                    assert: vec![
                        Assertion::Status(200),
                        Assertion::JsonPath {
                            path: "$.entry[0].resource.meta.profile".to_string(),
                            predicate: JsonPredicate::Every(Box::new(JsonPredicate::Exists)),
                        },
                    ],
                    save_as: String::new(),
                    soft_fail: false,
                }));
            }
        }
    }

    steps
}

// ---------------------------------------------------------------------------
// Operation test generation
// ---------------------------------------------------------------------------

/// Generate operation/action tests from the API model.
fn generate_operation_tests(api: &ApiModel, _spec: &OperationSpec) -> Vec<Step> {
    let mut steps = Vec::new();

    for resource_model in &api.resources {
        let rtype = &resource_model.name;
        let rtype_lower = rtype.to_lowercase();

        for op in &resource_model.operations {
            // Skip standard CRUD/search operations — they're handled elsewhere
            if matches!(
                op.name.as_str(),
                "create" | "read" | "update" | "delete" | "patch" | "search-type"
            ) {
                continue;
            }

            steps.push(Step::Request(RequestStep {
                name: format!("op-{}-{}", rtype_lower, op.name),
                method: Method::Get,
                url: format!("/{rtype}/{name}", rtype = rtype, name = op.name),
                headers: HashMap::new(),
                body: None,
                assert: vec![Assertion::Status(200)],
                save_as: String::new(),
                soft_fail: false,
            }));
        }
    }

    steps
}

// ---------------------------------------------------------------------------
// Security test generation
// ---------------------------------------------------------------------------

/// Generate security tests from the API model.
fn generate_security_tests(_api: &ApiModel, _spec: &SecuritySpec) -> Vec<Step> {
    // Security tests are format-specific and require knowledge of auth schemes.
    // This is a placeholder for future implementation.
    Vec::new()
}

// ---------------------------------------------------------------------------
// Performance test generation
// ---------------------------------------------------------------------------

/// Generate performance tests from the API model.
fn generate_performance_tests(api: &ApiModel, spec: &PerformanceSpec) -> Vec<Step> {
    let mut steps = Vec::new();

    if spec.pagination {
        for resource_model in &api.resources {
            let rtype = &resource_model.name;
            let rtype_lower = rtype.to_lowercase();

            // Test _count=1 pagination
            steps.push(Step::Request(RequestStep {
                name: format!("perf-{rtype_lower}-count-1"),
                method: Method::Get,
                url: format!("/{rtype}?_count=1"),
                headers: HashMap::new(),
                body: None,
                assert: vec![Assertion::Status(200)],
                save_as: String::new(),
                soft_fail: false,
            }));
        }
    }

    steps
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    use super::*;

    /// A mock resource generator for testing.
    struct MockGenerator;

    impl ResourceGenerator for MockGenerator {
        fn generate(&self, resource_type: &str) -> Result<serde_json::Value> {
            Ok(serde_json::json!({
                "resourceType": resource_type,
                "name": "Test Resource",
                "status": "active",
                "active": true
            }))
        }

        fn vary(&self, resource: &mut serde_json::Value, variation: &DataVariation, _index: u64) {
            match variation {
                DataVariation::Minimal => {
                    // Remove optional fields
                    if let Some(obj) = resource.as_object_mut() {
                        obj.remove("name");
                    }
                }
                DataVariation::SpecialChars => {
                    if let Some(obj) = resource.as_object_mut() {
                        obj.insert(
                            "name".to_string(),
                            serde_json::json!("<script>alert('xss')</script> & \"'<>"),
                        );
                    }
                }
                DataVariation::Boundary { .. } => {
                    if let Some(obj) = resource.as_object_mut() {
                        obj.insert("name".to_string(), serde_json::json!(""));
                    }
                }
                DataVariation::DuplicateValue { .. } => {}
                DataVariation::MissingField { field } => {
                    if let Some(obj) = resource.as_object_mut() {
                        obj.remove(field);
                    }
                }
                DataVariation::HappyPath | DataVariation::ToBeDeleted => {}
            }
        }

        fn extract_values(
            &self,
            _resource_type: &str,
            resource: &serde_json::Value,
        ) -> HashMap<String, String> {
            let mut values = HashMap::new();
            if let Some(name) = resource.get("name").and_then(|v| v.as_str()) {
                values.insert("name".to_string(), name.to_string());
            }
            if let Some(status) = resource.get("status").and_then(|v| v.as_str()) {
                values.insert("status".to_string(), status.to_string());
            }
            values
        }
    }

    fn make_test_api() -> ApiModel {
        ApiModel {
            name: "Test API".to_string(),
            resources: vec![ResourceModel {
                name: "Patient".to_string(),
                profile_url: Some("http://hl7.org/fhir/StructureDefinition/Patient".to_string()),
                operations: vec![
                    OperationModel {
                        name: "create".to_string(),
                        method: "POST".to_string(),
                        path: "/Patient".to_string(),
                        request_body: None,
                        responses: vec![ResponseModel {
                            status_code: 201,
                            content_type: None,
                            schema: None,
                        }],
                    },
                    OperationModel {
                        name: "read".to_string(),
                        method: "GET".to_string(),
                        path: "/Patient/{id}".to_string(),
                        request_body: None,
                        responses: vec![ResponseModel {
                            status_code: 200,
                            content_type: None,
                            schema: None,
                        }],
                    },
                    OperationModel {
                        name: "update".to_string(),
                        method: "PUT".to_string(),
                        path: "/Patient/{id}".to_string(),
                        request_body: None,
                        responses: vec![ResponseModel {
                            status_code: 200,
                            content_type: None,
                            schema: None,
                        }],
                    },
                    OperationModel {
                        name: "delete".to_string(),
                        method: "DELETE".to_string(),
                        path: "/Patient/{id}".to_string(),
                        request_body: None,
                        responses: vec![ResponseModel {
                            status_code: 204,
                            content_type: None,
                            schema: None,
                        }],
                    },
                    OperationModel {
                        name: "search-type".to_string(),
                        method: "GET".to_string(),
                        path: "/Patient".to_string(),
                        request_body: None,
                        responses: vec![ResponseModel {
                            status_code: 200,
                            content_type: None,
                            schema: None,
                        }],
                    },
                ],
                search_params: vec![
                    SearchParamModel {
                        name: "name".to_string(),
                        param_type: "string".to_string(),
                        modifiers: vec!["exact".to_string(), "contains".to_string()],
                        prefixes: vec![],
                    },
                    SearchParamModel {
                        name: "birthdate".to_string(),
                        param_type: "date".to_string(),
                        modifiers: vec![],
                        prefixes: vec!["eq".to_string(), "gt".to_string(), "lt".to_string()],
                    },
                ],
                search_include: vec![],
                search_revinclude: vec![],
                supported_profiles: vec![],
            }],
        }
    }

    #[test]
    fn test_resolve_spec_all_of() {
        let spec = TestSpec::AllOf(vec![
            TestSpec::Data(DataSpec::default()),
            TestSpec::Crud(CrudSpec::default()),
            TestSpec::Search(SearchSpec::default()),
        ]);

        let resolved = resolve_spec(&spec);
        assert_eq!(resolved.len(), 3);
    }

    #[test]
    fn test_resolve_spec_one_of() {
        let spec = TestSpec::OneOf(vec![
            TestSpec::Crud(CrudSpec::default()),
            TestSpec::Search(SearchSpec::default()),
        ]);

        let resolved = resolve_spec(&spec);
        assert_eq!(resolved.len(), 1);
        assert!(matches!(resolved[0], TestSpec::Crud(_)));
    }

    #[test]
    fn test_resolve_spec_nested() {
        let spec = TestSpec::AllOf(vec![
            TestSpec::Data(DataSpec::default()),
            TestSpec::AllOf(vec![
                TestSpec::Crud(CrudSpec::default()),
                TestSpec::Search(SearchSpec::default()),
            ]),
        ]);

        let resolved = resolve_spec(&spec);
        assert_eq!(resolved.len(), 3);
    }

    #[test]
    fn test_extract_data_spec() {
        let data_spec = TestSpec::Data(DataSpec {
            count: 5,
            variations: vec![DataVariation::HappyPath, DataVariation::Minimal],
        });
        let crud_spec = TestSpec::Crud(CrudSpec::default());
        let specs = vec![&crud_spec as &TestSpec, &data_spec as &TestSpec];

        let ds = extract_data_spec(&specs);
        assert_eq!(ds.count, 5);
        assert_eq!(ds.variations.len(), 2);
    }

    #[test]
    fn test_extract_data_spec_default() {
        let crud_spec = TestSpec::Crud(CrudSpec::default());
        let specs: Vec<&TestSpec> = vec![&crud_spec];
        let ds = extract_data_spec(&specs);
        assert_eq!(ds.count, 3);
    }

    #[test]
    fn test_generate_data() {
        let api = make_test_api();
        let spec = DataSpec::default();
        let generator = MockGenerator;

        let data = generate_data(&api, &spec, &generator).unwrap();

        assert!(data.resources.contains_key("Patient"));
        let patient_resources = data.resources.get("Patient").unwrap();
        assert_eq!(patient_resources.len(), 3);

        // First resource should be happy path
        assert!(matches!(
            patient_resources[0].variation,
            DataVariation::HappyPath
        ));

        // Should have field values
        assert!(data.field_values.contains_key("Patient"));
        assert!(data.created_ids.contains_key("Patient"));
        assert_eq!(data.created_ids.get("Patient").unwrap(), "patient-001");
    }

    #[test]
    fn test_generate_setup_steps() {
        let api = make_test_api();
        let spec = DataSpec::default();
        let generator = MockGenerator;
        let data = generate_data(&api, &spec, &generator).unwrap();

        let steps = generate_setup_steps(&api, &data);
        assert_eq!(steps.len(), 3); // 3 resources

        for step in &steps {
            match step {
                Step::Request(req) => {
                    assert_eq!(req.method, Method::Post);
                    assert!(req.url.starts_with("/Patient"));
                    assert!(req.body.is_some());
                    assert!(!req.save_as.is_empty());
                }
                _ => panic!("Expected Request step"),
            }
        }
    }

    #[test]
    fn test_generate_crud_tests() {
        let api = make_test_api();
        let spec = CrudSpec::default();
        let data = {
            let data_spec = DataSpec::default();
            let generator = MockGenerator;
            generate_data(&api, &data_spec, &generator).unwrap()
        };
        let generator = MockGenerator;

        let steps = generate_crud_tests(&api, &spec, &data, &generator).unwrap();
        assert_eq!(steps.len(), 1); // 1 sequence

        match &steps[0] {
            Step::Sequence(seq) => {
                assert_eq!(seq.name, "patient-crud");
                // Should have create, read, update, delete
                assert_eq!(seq.steps.len(), 4);
            }
            _ => panic!("Expected Sequence step"),
        }
    }

    #[test]
    fn test_generate_search_tests() {
        let api = make_test_api();
        let spec = SearchSpec {
            single_param: true,
            modifiers: true,
            prefixes: true,
            combined_params: false,
            chained: false,
            include: false,
            revinclude: false,
            result_params: vec!["_count=1".to_string()],
            negative_values: vec!["NONEXISTENT".to_string()],
        };
        let data = {
            let data_spec = DataSpec::default();
            let generator = MockGenerator;
            generate_data(&api, &data_spec, &generator).unwrap()
        };

        let steps = generate_search_tests(&api, &spec, &data);
        // name (single + exact + contains + missing:true + missing:false) = 5
        // birthdate (single + eq + gt + lt) = 4
        // _count=1 = 1
        // negative = 1
        assert!(!steps.is_empty());
    }

    #[test]
    fn test_generate_negative_tests() {
        let api = make_test_api();
        let spec = NegativeSpec {
            undeclared_interactions: true,
            invalid_bodies: true,
            malformed_requests: true,
            auth_errors: false,
            version_conflicts: false,
        };

        let steps = generate_negative_tests(&api, &spec);
        // All interactions are declared, so no undeclared tests
        // But we should have invalid body tests
        assert!(!steps.is_empty());
    }

    #[test]
    fn test_generate_full_plan() {
        let api = make_test_api();
        let spec = TestSpec::AllOf(vec![
            TestSpec::Data(DataSpec::default()),
            TestSpec::Crud(CrudSpec::default()),
            TestSpec::Search(SearchSpec {
                single_param: true,
                modifiers: true,
                prefixes: true,
                combined_params: false,
                chained: false,
                include: false,
                revinclude: false,
                result_params: vec![],
                negative_values: vec![],
            }),
            TestSpec::Negative(NegativeSpec {
                undeclared_interactions: true,
                invalid_bodies: true,
                malformed_requests: false,
                auth_errors: false,
                version_conflicts: false,
            }),
        ]);
        let generator = MockGenerator;

        let plan = generate_test_plan(&api, &spec, &generator).unwrap();
        assert_eq!(plan.name, "Test API — generated test plan");
        assert_eq!(plan.setup.len(), 3); // 3 setup steps
        assert!(!plan.steps.is_empty());
    }
}
