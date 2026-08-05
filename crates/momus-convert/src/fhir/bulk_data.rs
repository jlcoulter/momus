//! FHIR bulk data generator (NDJSON).
//!
//! Generates NDJSON (newline-delimited JSON) files with synthetic FHIR resources
//! for bulk data import. Ported from fhir-autotest's `generate::bulk_data` module.
//!
//! The generator:
//! - Takes a list of StructureDefinitions and a count per resource type
//! - Generates NDJSON output (one JSON object per line)
//! - Uses the existing resource generator for individual resources
//! - Handles cross-references between resources (e.g., Patient → Observation.subject)
//! - Supports a wave-based ordering (resources that depend on others are generated later)

use super::profile::StructureDefinition;
use super::resource_gen::generate_resource_with_value_sets;
use anyhow::Result;
use chrono::{Duration, Utc};
use rand::Rng;
use std::collections::HashMap;
use std::io::Write;
use std::path::Path;

/// IDs allocated during bulk generation.
/// Maps resource type → list of generated IDs.
pub type IdStore = HashMap<String, Vec<String>>;

/// FHIR data types that are not independently creatable resources.
/// Some CapabilityStatements list types like Extension or Identifier which
/// are structural types, not top-level FHIR resources.
pub const NON_RESOURCE_TYPES: &[&str] = &[
    "Extension",
    "Identifier",
    "Coding",
    "CodeableConcept",
    "Address",
    "HumanName",
    "ContactPoint",
    "Period",
    "Quantity",
    "Range",
    "Ratio",
    "Attachment",
    "Annotation",
    "Signature",
    "Timing",
];

/// Generate bulk FHIR resources as NDJSON files.
///
/// Writes one `.ndjson` file per resource type under `output_dir/data/`,
/// plus a `combined.ndjson` containing all resources in dependency order
/// (suitable for bulk import where linked items must resolve).
/// Returns an `IdStore` mapping each resource type to its generated IDs,
/// which is used to resolve cross-references during generation.
pub fn generate_bulk_data(
    counts: &HashMap<String, u64>,
    profile_urls: &HashMap<String, String>,
    profiles: &[StructureDefinition],
    value_set_systems: &HashMap<String, String>,
    output_dir: &Path,
) -> Result<IdStore> {
    use std::io::BufWriter;

    let data_dir = output_dir.join("data");
    std::fs::create_dir_all(&data_dir)?;
    let mut rng = rand::rng();

    // Determine creation order: dependent types first.
    // Organizations and Practitioners have no FHIR references, so they go first.
    // Locations reference Organizations. HealthcareServices reference Organizations
    // and Locations. PractitionerRoles reference Practitioners, Organizations,
    // and Locations.
    let order = bulk_data_creation_order(counts);

    // First pass: allocate all IDs so cross-references can be resolved.
    let mut ids: IdStore = HashMap::new();
    for resource_type in &order {
        let count = counts.get(resource_type).copied().unwrap_or(0);
        if count == 0 {
            continue;
        }
        let type_ids: Vec<String> = (0..count)
            .map(|i| format!("{}-{}", resource_type.to_lowercase(), i + 1))
            .collect();
        ids.insert(resource_type.clone(), type_ids);
    }

    // Pre-clone ID vectors for cross-referencing (avoids cloning inside hot loops).
    let org_ids = ids.get("Organization").cloned().unwrap_or_default();
    let prac_ids = ids.get("Practitioner").cloned().unwrap_or_default();
    let loc_ids = ids.get("Location").cloned().unwrap_or_default();
    let hs_ids = ids.get("HealthcareService").cloned().unwrap_or_default();
    let practitioner_role_ids = ids.get("PractitionerRole").cloned().unwrap_or_default();
    let endpoint_ids = ids.get("Endpoint").cloned().unwrap_or_default();

    // Build lookups so generation can prefer the exact profile URL from the
    // CapabilityStatement instead of an arbitrary StructureDefinition that
    // happens to share the same base type.
    let profile_by_url: HashMap<&str, &StructureDefinition> =
        profiles.iter().map(|p| (p.url.as_str(), p)).collect();
    let profile_by_base_type: HashMap<&str, &StructureDefinition> =
        profiles.iter().map(|p| (p.base_type.as_str(), p)).collect();

    // Open combined.ndjson to collect all resources in import order.
    let combined_path = data_dir.join("combined.ndjson");
    let combined_file = std::fs::File::create(&combined_path)?;
    let mut combined_writer = BufWriter::new(combined_file);

    // Second pass: generate and write resources with buffered I/O.
    for resource_type in &order {
        let count = counts.get(resource_type).copied().unwrap_or(0);
        if count == 0 {
            continue;
        }
        let type_ids = ids.get(resource_type).cloned().unwrap_or_default();
        let path = data_dir.join(format!("{resource_type}.ndjson"));
        let file = std::fs::File::create(&path)?;
        let mut writer = BufWriter::new(file);
        let mut written = 0u64;

        // Resolve the profile URL for this resource type: prefer the IG's
        // profile, fall back to the base FHIR profile.
        let profile_url = profile_urls
            .get(resource_type)
            .cloned()
            .unwrap_or_else(|| format!("http://hl7.org/fhir/StructureDefinition/{resource_type}"));

        for id in type_ids.iter() {
            let selected_profile = profile_urls
                .get(resource_type)
                .and_then(|url| profile_by_url.get(url.as_str()).copied())
                .or_else(|| profile_by_base_type.get(resource_type.as_str()).copied());

            let mut resource = if let Some(profile) = selected_profile {
                // Use profile-aware generation: generates a conformant base from
                // the StructureDefinition, then overlay cross-references.
                let mut r =
                    generate_resource_with_value_sets(profile, profiles, value_set_systems)?;
                r["id"] = serde_json::Value::String(id.clone());
                // Overlay cross-references for types that need them.
                overlay_cross_references(
                    &mut r,
                    resource_type,
                    id,
                    &org_ids,
                    &prac_ids,
                    &loc_ids,
                    &hs_ids,
                    &practitioner_role_ids,
                    &endpoint_ids,
                    &mut rng,
                );
                r
            } else {
                match resource_type.as_str() {
                    "Organization" => gen_organization(id, &mut rng),
                    "Practitioner" => gen_practitioner(id, &mut rng),
                    "PractitionerRole" => {
                        gen_practitioner_role(id, &org_ids, &prac_ids, &loc_ids, &mut rng)
                    }
                    "Location" => gen_location(id, &mut rng),
                    "HealthcareService" => gen_healthcare_service(id, &org_ids, &loc_ids, &mut rng),
                    "Endpoint" => gen_endpoint(id, &org_ids, &mut rng),
                    // Generic fallback for any resource type not explicitly handled
                    _ => gen_generic(resource_type, id, &mut rng),
                }
            };

            // When NOT using profile-aware generation (i.e. falling back to
            // the hardcoded gen_* functions), stamp the profile URL from the
            // IG package to override the hardcoded defaults.
            // When using generate_resource(), the profile URL is already set
            // correctly from the StructureDefinition, so skip the override.
            if selected_profile.is_none() {
                resource["meta"]["profile"] = serde_json::json!([profile_url]);
            }

            // Stamp a random created date within the last 12 months
            stamp_created_date(&mut resource, &mut rng);

            serde_json::to_writer(&mut writer, &resource)?;
            writeln!(writer)?;
            // Also write to combined.ndjson in import order.
            serde_json::to_writer(&mut combined_writer, &resource)?;
            writeln!(combined_writer)?;
            written += 1;
            if written.is_multiple_of(10_000) {
                // Flush progress to disk so external observers see the file growing.
                writer.flush()?;
                tracing::info!(
                    "Generated {}/{} {} resources",
                    written,
                    count,
                    resource_type
                );
            }
        }
        writer.flush()?;
        tracing::info!(
            "Wrote {} {} resources to {}",
            written,
            resource_type,
            path.display()
        );
    }

    combined_writer.flush()?;
    tracing::info!("Wrote all resources to {}", combined_path.display());

    Ok(ids)
}

/// Generate a single supplement resource for a resource type that has no bulk data count.
///
/// Used to ensure conformance must_support tests can always find a resource with the
/// expected ID pattern (`{resourcetype}-1`). Works with any FHIR IG by using the
/// profile-aware generator as the primary source and falling back to generic generation.
pub fn generate_supplement_resource(
    resource_type: &str,
    profile_urls: &HashMap<String, String>,
    profiles: &[StructureDefinition],
    value_set_systems: &HashMap<String, String>,
) -> Result<serde_json::Value> {
    let id = format!("{}-1", resource_type.to_lowercase());
    let mut rng = rand::rng();

    let profile_by_url: HashMap<&str, &StructureDefinition> =
        profiles.iter().map(|p| (p.url.as_str(), p)).collect();
    let profile_by_base_type: HashMap<&str, &StructureDefinition> =
        profiles.iter().map(|p| (p.base_type.as_str(), p)).collect();

    let profile_url = profile_urls
        .get(resource_type)
        .cloned()
        .unwrap_or_else(|| format!("http://hl7.org/fhir/StructureDefinition/{resource_type}"));

    let selected_profile = profile_urls
        .get(resource_type)
        .and_then(|url| profile_by_url.get(url.as_str()).copied())
        .or_else(|| profile_by_base_type.get(resource_type).copied());

    let mut resource = if let Some(profile) = selected_profile {
        let mut r = generate_resource_with_value_sets(profile, profiles, value_set_systems)?;
        r["id"] = serde_json::Value::String(id.clone());
        r
    } else {
        match resource_type {
            "Organization" => gen_organization(&id, &mut rng),
            "Practitioner" => gen_practitioner(&id, &mut rng),
            "PractitionerRole" => gen_practitioner_role(&id, &[], &[], &[], &mut rng),
            "Location" => gen_location(&id, &mut rng),
            "HealthcareService" => gen_healthcare_service(&id, &[], &[], &mut rng),
            "Endpoint" => gen_endpoint(&id, &[], &mut rng),
            _ => gen_generic(resource_type, &id, &mut rng),
        }
    };

    if selected_profile.is_none() {
        resource["meta"]["profile"] = serde_json::json!([profile_url]);
    }

    stamp_created_date(&mut resource, &mut rng);

    // Normalize all Reference values to use the predictable {type}-1 pattern.
    // Generated references use random UUIDs that point to non-existent resources;
    // replacing them ensures supplement resources can be uploaded without referential
    // integrity errors.
    normalize_supplement_references(&mut resource);

    Ok(resource)
}

/// Walk a JSON value and replace any `"reference": "ResourceType/some-uuid"` with
/// `"reference": "ResourceType/resourcetype-1"` so supplement resources always
/// point to the predictable IDs used by other supplement resources.
/// The abstract `Resource` base type is mapped to `Organization` as a concrete fallback.
fn normalize_supplement_references(value: &mut serde_json::Value) {
    match value {
        serde_json::Value::Object(obj) => {
            if let Some(ref_val) = obj.get_mut("reference")
                && let Some(s) = ref_val.as_str()
                && let Some((rtype, _id)) = s.split_once('/')
            {
                // Map the abstract FHIR `Resource` base type to a concrete
                // type that is always present from bulk data.
                let concrete_type = if rtype == "Resource" {
                    "Organization"
                } else {
                    rtype
                };
                let new_id = format!("{}-1", concrete_type.to_lowercase());
                *ref_val = serde_json::Value::String(format!("{concrete_type}/{new_id}"));
            }
            for v in obj.values_mut() {
                normalize_supplement_references(v);
            }
        }
        serde_json::Value::Array(arr) => {
            for v in arr.iter_mut() {
                normalize_supplement_references(v);
            }
        }
        _ => {}
    }
}

/// Generate supplement resources for all resource types in `creation_order` that
/// have no entry in `bulk_counts`, write each to `{output_dir}/data/{Type}.ndjson`,
/// append all to `combined.ndjson`, and return an IdStore so callers can include
/// them in `generate_update_ndjson` and `upload_ndjson_files`.
///
/// FHIR data types (Extension, Identifier, etc.) that are not standalone resources
/// are silently skipped.
pub fn write_supplement_ndjson(
    creation_order: &[String],
    bulk_counts: &HashMap<String, u64>,
    profile_urls: &HashMap<String, String>,
    profiles: &[StructureDefinition],
    value_set_systems: &HashMap<String, String>,
    output_dir: &Path,
) -> Result<IdStore> {
    use std::io::{BufWriter, Write};

    let data_dir = output_dir.join("data");
    std::fs::create_dir_all(&data_dir)?;

    // Open combined.ndjson in append mode so supplement resources follow the bulk data.
    let combined_path = data_dir.join("combined.ndjson");
    let combined_file = std::fs::OpenOptions::new()
        .create(true)
        .append(true)
        .open(&combined_path)?;
    let mut combined_writer = BufWriter::new(combined_file);

    let mut supplement_ids: IdStore = HashMap::new();

    for resource_type in creation_order {
        let count = bulk_counts.get(resource_type).copied().unwrap_or(0);
        if count > 0 || NON_RESOURCE_TYPES.contains(&resource_type.as_str()) {
            continue;
        }

        let resource = match generate_supplement_resource(
            resource_type,
            profile_urls,
            profiles,
            value_set_systems,
        ) {
            Ok(r) => r,
            Err(e) => {
                tracing::warn!(
                    "Could not generate supplement resource for {}: {}",
                    resource_type,
                    e
                );
                continue;
            }
        };

        let id = format!("{}-1", resource_type.to_lowercase());

        // Write to per-type NDJSON file
        let type_path = data_dir.join(format!("{resource_type}.ndjson"));
        let type_file = std::fs::File::create(&type_path)?;
        let mut type_writer = BufWriter::new(type_file);
        serde_json::to_writer(&mut type_writer, &resource)?;
        writeln!(type_writer)?;
        type_writer.flush()?;

        // Append to combined.ndjson
        serde_json::to_writer(&mut combined_writer, &resource)?;
        writeln!(combined_writer)?;

        tracing::info!("Wrote supplement resource: {}/{}", resource_type, id);
        supplement_ids.insert(resource_type.clone(), vec![id]);
    }

    combined_writer.flush()?;
    Ok(supplement_ids)
}

/// Determine the creation order for bulk data generation, respecting
/// dependency relationships between resource types.
///
/// Tier 1: Root resources (no dependencies)
/// Tier 2: Depends on Organization
/// Tier 3: Depends on Organization and Endpoint
/// Tier 4: Depends on Organization, Endpoint, and Location
/// Tier 5: Depends on Practitioner, Organization, Endpoint, Location, and HealthcareService
/// Tier 6: May reference several resource pools
pub fn bulk_data_creation_order(counts: &HashMap<String, u64>) -> Vec<String> {
    let mut order = Vec::new();

    // Tier 1: root resources
    for t in &["Organization", "Practitioner"] {
        if counts.contains_key(*t) {
            order.push((*t).to_string());
        }
    }

    // Tier 2: depends on Organization
    for t in &["Endpoint"] {
        if counts.contains_key(*t) {
            order.push((*t).to_string());
        }
    }

    // Tier 3: depends on Organization and Endpoint
    for t in &["Location"] {
        if counts.contains_key(*t) {
            order.push((*t).to_string());
        }
    }

    // Tier 4: depends on Organization, Endpoint, and Location
    for t in &["HealthcareService"] {
        if counts.contains_key(*t) {
            order.push((*t).to_string());
        }
    }

    // Tier 5: depends on Practitioner, Organization, Endpoint, Location,
    // and HealthcareService
    for t in &["PractitionerRole"] {
        if counts.contains_key(*t) {
            order.push((*t).to_string());
        }
    }

    // Tier 6: may reference several resource pools and should come last.
    for t in &["Provenance"] {
        if counts.contains_key(*t) {
            order.push((*t).to_string());
        }
    }
    // Anything else not yet ordered
    for t in counts.keys() {
        if !order.contains(t) {
            order.push(t.clone());
        }
    }
    order
}

/// Generate an `update.ndjson` file containing the same resources as the
/// initial bulk data, but with 1–2 randomly updated parameters per resource.
///
/// Each resource retains its original `id` so the update file can be used
/// to test update operations (e.g. PUT /{ResourceType}/{id}) against a
/// server that already has the initial data loaded.
///
/// The update file is written to `{output_dir}/data/update.ndjson`.
pub fn generate_update_ndjson(ids: &IdStore, output_dir: &Path) -> Result<()> {
    use std::io::BufWriter;

    let data_dir = output_dir.join("data");
    let update_path = data_dir.join("update.ndjson");
    let file = std::fs::File::create(&update_path)?;
    let mut writer = BufWriter::new(file);
    let mut rng = rand::rng();
    let mut total = 0u64;

    // Process resource types in the same order as initial generation
    // so the update file has a consistent ordering.
    let order = bulk_data_creation_order(
        &ids.iter()
            .map(|(k, v)| (k.clone(), v.len() as u64))
            .collect(),
    );

    for resource_type in &order {
        if !ids.contains_key(resource_type) || ids[resource_type].is_empty() {
            continue;
        }

        // Read the original NDJSON file for this type
        let ndjson_path = data_dir.join(format!("{resource_type}.ndjson"));
        let contents = match std::fs::read_to_string(&ndjson_path) {
            Ok(c) => c,
            Err(_) => continue, // skip types that weren't written
        };

        for line in contents.lines().filter(|l| !l.is_empty()) {
            let mut resource: serde_json::Value = serde_json::from_str(line)?;
            apply_random_updates(&mut resource, resource_type, &mut rng);
            serde_json::to_writer(&mut writer, &resource)?;
            writeln!(writer)?;
            total += 1;
        }
    }

    writer.flush()?;
    tracing::info!(
        "Wrote {} updated resources to {}",
        total,
        update_path.display()
    );
    Ok(())
}

// ── Cross-reference overlay ──────────────────────────────────────────────

/// Overlay cross-references onto a profile-generated resource.
///
/// When `generate_resource` produces a resource from a StructureDefinition,
/// it creates required fields but cannot know about the IDs of other
/// resources in the bulk data set. This function fills in cross-references
/// (practitioner, organization, location, healthcareService) that the
/// profile may require but which depend on other generated resources.
#[allow(clippy::too_many_arguments)]
fn overlay_cross_references(
    resource: &mut serde_json::Value,
    resource_type: &str,
    _id: &str,
    org_ids: &[String],
    prac_ids: &[String],
    loc_ids: &[String],
    hs_ids: &[String],
    practitioner_role_ids: &[String],
    endpoint_ids: &[String],
    rng: &mut impl Rng,
) {
    let obj = match resource.as_object_mut() {
        Some(o) => o,
        None => return,
    };

    match resource_type {
        "PractitionerRole" => {
            if !prac_ids.is_empty() {
                let ref_str = if _id == "practitionerrole-1"
                    && prac_ids.iter().any(|id| id == "practitioner-1")
                {
                    "Practitioner/practitioner-1".to_string()
                } else {
                    random_ref("Practitioner", prac_ids, rng)
                };
                obj.insert(
                    "practitioner".to_string(),
                    serde_json::json!({ "reference": ref_str }),
                );
            }
            if !org_ids.is_empty() {
                let ref_str = random_ref("Organization", org_ids, rng);
                obj.insert(
                    "organization".to_string(),
                    serde_json::json!({ "reference": ref_str }),
                );
            }
            if !loc_ids.is_empty() {
                let ref_str = random_ref("Location", loc_ids, rng);
                obj.insert(
                    "location".to_string(),
                    serde_json::json!([{ "reference": ref_str }]),
                );
            }
            if !hs_ids.is_empty() {
                let ref_str = format!("HealthcareService/{}", hs_ids[0]);
                obj.insert(
                    "healthcareService".to_string(),
                    serde_json::json!([{ "reference": ref_str }]),
                );
            }
            if !endpoint_ids.is_empty() {
                let ref_str = random_ref("Endpoint", endpoint_ids, rng);
                obj.insert(
                    "endpoint".to_string(),
                    serde_json::json!([{ "reference": ref_str }]),
                );
            }
        }
        "Location" if !org_ids.is_empty() => {
            let ref_str = if _id == "location-1" && org_ids.iter().any(|id| id == "organization-1")
            {
                "Organization/organization-1".to_string()
            } else {
                random_ref("Organization", org_ids, rng)
            };
            obj.insert(
                "managingOrganization".to_string(),
                serde_json::json!({ "reference": ref_str }),
            );
            if !endpoint_ids.is_empty() {
                let endpoint_ref =
                    if _id == "location-1" && endpoint_ids.iter().any(|id| id == "endpoint-1") {
                        "Endpoint/endpoint-1".to_string()
                    } else {
                        random_ref("Endpoint", endpoint_ids, rng)
                    };
                obj.insert(
                    "endpoint".to_string(),
                    serde_json::json!([{ "reference": endpoint_ref }]),
                );
            }
        }
        "HealthcareService" => {
            if !org_ids.is_empty() {
                let ref_str = random_ref("Organization", org_ids, rng);
                obj.insert(
                    "providedBy".to_string(),
                    serde_json::json!({ "reference": ref_str }),
                );
            }
            if !loc_ids.is_empty() {
                let ref_str = random_ref("Location", loc_ids, rng);
                obj.insert(
                    "location".to_string(),
                    serde_json::json!([{ "reference": ref_str }]),
                );
            }
            if !endpoint_ids.is_empty() {
                let ref_str = random_ref("Endpoint", endpoint_ids, rng);
                obj.insert(
                    "endpoint".to_string(),
                    serde_json::json!([{ "reference": ref_str }]),
                );
            }
            if !loc_ids.is_empty() {
                let ref_str = random_ref("Location", loc_ids, rng);
                obj.insert(
                    "coverageArea".to_string(),
                    serde_json::json!([{ "reference": ref_str }]),
                );
            } else {
                obj.remove("coverageArea");
            }
        }
        "Endpoint" if !org_ids.is_empty() => {
            let ref_str = random_ref("Organization", org_ids, rng);
            obj.insert(
                "managingOrganization".to_string(),
                serde_json::json!({ "reference": ref_str }),
            );
        }
        "Organization" if !org_ids.is_empty() => {
            const PART_OF_PROBABILITY: f64 = 0.01;
            const MUST_SUPPORT_ANCHOR: &str = "organization-1";
            const ANCHOR_PARENT: &str = "organization-2";
            if _id == MUST_SUPPORT_ANCHOR {
                if org_ids.iter().any(|id| id.as_str() == ANCHOR_PARENT) {
                    obj.insert(
                        "partOf".to_string(),
                        serde_json::json!({ "reference": format!("Organization/{ANCHOR_PARENT}") }),
                    );
                } else {
                    obj.remove("partOf");
                }
            } else if _id == ANCHOR_PARENT {
                obj.remove("partOf");
            } else {
                let self_index = org_ids.iter().position(|id| id.as_str() == _id);
                match self_index {
                    Some(idx) if idx > 0 && rng.random_bool(PART_OF_PROBABILITY) => {
                        let parent = &org_ids[rng.random_range(0..idx)];
                        obj.insert(
                            "partOf".to_string(),
                            serde_json::json!({ "reference": format!("Organization/{parent}") }),
                        );
                    }
                    _ => {
                        obj.remove("partOf");
                    }
                }
            }
        }
        "Provenance" => {
            let target_ref = provenance_target_for_id(
                _id,
                org_ids,
                prac_ids,
                loc_ids,
                hs_ids,
                practitioner_role_ids,
                endpoint_ids,
                rng,
            );

            if let Some(ref_str) = target_ref.as_deref() {
                if let Some(targets) = obj.get_mut("target").and_then(|t| t.as_array_mut()) {
                    if let Some(first) = targets.first_mut()
                        && let Some(target_obj) = first.as_object_mut()
                    {
                        target_obj.insert("reference".to_string(), serde_json::json!(ref_str));
                        ensure_target_path_extension(target_obj);
                    }
                } else {
                    obj.insert(
                        "target".to_string(),
                        serde_json::json!([{
                            "reference": ref_str,
                            "extension": [{
                                "url": "http://hl7.org/fhir/StructureDefinition/targetPath",
                                "valueString": "id"
                            }]
                        }]),
                    );
                }
            }

            obj.insert(
                "activity".to_string(),
                serde_json::json!({
                    "coding": [{
                        "system": "http://terminology.hl7.org/CodeSystem/provenance-activity-type",
                        "code": "CREATE"
                    }]
                }),
            );

            if !org_ids.is_empty() {
                obj.insert(
                    "agent".to_string(),
                    serde_json::json!([
                        {
                            "who": {
                                "reference": random_ref("Organization", org_ids, rng)
                            }
                        }
                    ]),
                );
            }

            if !org_ids.is_empty() {
                obj.insert(
                    "entity".to_string(),
                    serde_json::json!([
                        {
                            "role": "source",
                            "what": {
                                "reference": random_ref("Organization", org_ids, rng)
                            }
                        }
                    ]),
                );
            }
        }
        _ => {}
    }
}

/// Ensure a Provenance `target` object carries the mustSupport `target.extension`.
fn ensure_target_path_extension(target_obj: &mut serde_json::Map<String, serde_json::Value>) {
    if target_obj.contains_key("extension") {
        return;
    }
    target_obj.insert(
        "extension".to_string(),
        serde_json::json!([{
            "url": "http://hl7.org/fhir/StructureDefinition/targetPath",
            "valueString": "id"
        }]),
    );
}

#[allow(clippy::too_many_arguments)]
fn provenance_target_for_id(
    _provenance_id: &str,
    org_ids: &[String],
    prac_ids: &[String],
    loc_ids: &[String],
    hs_ids: &[String],
    practitioner_role_ids: &[String],
    endpoint_ids: &[String],
    rng: &mut impl Rng,
) -> Option<String> {
    let pools: Vec<(&[String], &str)> = vec![
        (org_ids, "Organization"),
        (prac_ids, "Practitioner"),
        (loc_ids, "Location"),
        (hs_ids, "HealthcareService"),
        (practitioner_role_ids, "PractitionerRole"),
        (endpoint_ids, "Endpoint"),
    ];
    let non_empty: Vec<(&[String], &str)> = pools
        .into_iter()
        .filter(|(ids, _)| !ids.is_empty())
        .collect();
    if non_empty.is_empty() {
        return None;
    }
    let idx = _provenance_id
        .rsplit('-')
        .next()
        .and_then(|s| s.parse::<usize>().ok())
        .unwrap_or(0)
        % non_empty.len();
    let (pool, rtype) = non_empty[idx];
    Some(random_ref(rtype, pool, rng))
}

fn random_ref(resource_type: &str, ids: &[String], rng: &mut impl Rng) -> String {
    if ids.is_empty() {
        format!("{resource_type}/placeholder-1")
    } else {
        let idx = rng.random_range(0..ids.len());
        format!("{}/{}", resource_type, ids[idx])
    }
}

// ── Update file generation helpers ───────────────────────────────────────

/// Apply 1–2 random mutations to a resource, keeping the same `id`.
///
/// Works generically for any FHIR resource type by walking the JSON tree
/// to find mutable leaf values (strings, numbers, booleans) and picking
/// 1–2 at random. Skips `resourceType`, `id`, `meta`, and reference fields
/// to keep the resource structurally valid.
fn apply_random_updates(
    resource: &mut serde_json::Value,
    _resource_type: &str,
    rng: &mut rand::rngs::ThreadRng,
) {
    let candidates = discover_mutable_paths(resource);
    if candidates.is_empty() {
        return;
    }

    let n_updates = rng.random_range(1..=candidates.len().min(2));
    let mut chosen_indices: Vec<usize> = (0..candidates.len()).collect();
    for i in (0..chosen_indices.len()).rev().take(n_updates) {
        let j = rng.random_range(0..=i);
        chosen_indices.swap(i, j);
    }

    let chosen: Vec<(String, MutatorFn)> = chosen_indices[..n_updates]
        .iter()
        .map(|&idx| {
            let (path, mutator) = &candidates[idx];
            (path.clone(), *mutator)
        })
        .collect();

    for (path, mutator) in &chosen {
        mutator(resource, path, rng);
    }
}

type MutatorFn = fn(&mut serde_json::Value, &str, &mut rand::rngs::ThreadRng);

/// Walk a resource JSON tree and collect paths to mutable leaf values.
fn discover_mutable_paths(resource: &serde_json::Value) -> Vec<(String, MutatorFn)> {
    let mut candidates: Vec<(String, MutatorFn)> = Vec::new();
    let mut prefix = String::new();
    walk_for_mutables(resource, &mut prefix, &mut candidates);
    candidates
}

fn walk_for_mutables(
    value: &serde_json::Value,
    prefix: &mut String,
    candidates: &mut Vec<(String, MutatorFn)>,
) {
    match value {
        serde_json::Value::Object(obj) => {
            let saved_len = prefix.len();
            for (key, val) in obj {
                if key == "resourceType" || key == "id" || key == "meta" {
                    continue;
                }
                if is_reference_field(key, val) {
                    continue;
                }

                if !prefix.is_empty() {
                    prefix.push('.');
                }
                prefix.push_str(key);
                walk_for_mutables(val, prefix, candidates);
                prefix.truncate(saved_len);
            }
        }
        serde_json::Value::Array(arr) => {
            let saved_len = prefix.len();
            for (i, item) in arr.iter().enumerate() {
                if item.is_object() {
                    let idx_str = format!("[{i}]");
                    prefix.push_str(&idx_str);
                    walk_for_mutables(item, prefix, candidates);
                    prefix.truncate(saved_len);
                }
            }
        }
        serde_json::Value::String(s) => {
            if s.contains('/') && !s.starts_with("http") {
                return;
            }
            candidates.push((prefix.clone(), mutate_string));
        }
        serde_json::Value::Number(_) => {
            candidates.push((prefix.clone(), mutate_number));
        }
        serde_json::Value::Bool(_) => {
            candidates.push((prefix.clone(), mutate_bool));
        }
        serde_json::Value::Null => {}
    }
}

fn is_reference_field(key: &str, val: &serde_json::Value) -> bool {
    if key == "reference"
        && let Some(s) = val.as_str()
        && s.contains('/')
        && !s.starts_with("http")
    {
        return true;
    }
    false
}

fn get_at_path<'a>(value: &'a serde_json::Value, path: &str) -> Option<&'a serde_json::Value> {
    let parts = path.split('.');
    let mut current = value;
    for part in parts {
        if let Some(idx_str) = part.strip_suffix(']') {
            let (array_key, index_str) = idx_str.split_once('[')?;
            let idx: usize = index_str.parse().ok()?;
            current = current.get(array_key)?.get(idx)?;
        } else {
            current = current.get(part)?;
        }
    }
    Some(current)
}

fn set_at_path(value: &mut serde_json::Value, path: &str, new_val: serde_json::Value) {
    let parts: Vec<&str> = path.split('.').collect();
    let mut current = value;
    for (i, part) in parts.iter().enumerate() {
        let is_last = i == parts.len() - 1;
        if let Some(idx_str) = part.strip_suffix(']') {
            let (array_key, index_str) = idx_str.split_once('[').unwrap();
            let idx: usize = index_str.parse().unwrap();
            let arr = current
                .get_mut(array_key)
                .and_then(|v| v.as_array_mut())
                .expect("array path must exist");
            if is_last {
                arr[idx] = new_val;
                return;
            }
            current = &mut arr[idx];
        } else if is_last {
            current[part] = new_val;
            return;
        } else {
            current = current.get_mut(part).expect("path must exist");
        }
    }
}

fn mutate_string(value: &mut serde_json::Value, path: &str, _rng: &mut rand::rngs::ThreadRng) {
    let new_val = serde_json::Value::String(format!("updated-{}", path.replace('.', "-")));
    set_at_path(value, path, new_val);
}

fn mutate_number(value: &mut serde_json::Value, path: &str, rng: &mut rand::rngs::ThreadRng) {
    let current = get_at_path(value, path)
        .and_then(|v| v.as_f64())
        .unwrap_or(0.0);
    let delta = current * 0.1;
    let new_val = current + rng.random_range(-delta..=delta);
    let rounded = (new_val * 100.0).round() / 100.0;
    set_at_path(value, path, serde_json::json!(rounded));
}

fn mutate_bool(value: &mut serde_json::Value, path: &str, _rng: &mut rand::rngs::ThreadRng) {
    let current = get_at_path(value, path)
        .and_then(|v| v.as_bool())
        .unwrap_or(true);
    set_at_path(value, path, serde_json::Value::Bool(!current));
}

// ── FHIR Resource Generators ─────────────────────────────────────────────

fn gen_organization(id: &str, rng: &mut impl Rng) -> serde_json::Value {
    let org_types = [
        "prov", "dept", "team", "govt", "ins", "pay", "edu", "reli", "crs",
    ];
    let org_type = org_types[rng.random_range(0..org_types.len())];

    serde_json::json!({
        "resourceType": "Organization",
        "id": id,
        "meta": {
            "profile": ["http://hl7.org/fhir/StructureDefinition/Organization"]
        },
        "name": format!("Organization {}", &id[..id.len().min(8)]),
        "identifier": [{
            "system": "http://hl7.org/fhir/sid/us-npi",
            "value": format!("{:09}", rng.random_range(100000000..999999999))
        }],
        "type": [{
            "coding": [{
                "system": "http://terminology.hl7.org/CodeSystem/organization-type",
                "code": org_type,
                "display": match org_type {
                    "prov" => "Healthcare Provider",
                    "dept" => "Hospital Department",
                    "team" => "Organizational team",
                    "govt" => "Government",
                    "ins" => "Insurance Company",
                    "pay" => "Payer",
                    "edu" => "Educational Institute",
                    _ => "Religious Institution",
                }
            }]
        }],
        "active": true,
        "telecom": [{
            "system": "phone",
            "value": format!("{:010}", rng.random_range(1000000000..10000000000u64)),
            "use": "work"
        }],
        "address": [{
            "type": "physical",
            "line": ["123 Main St"],
            "city": "GeneratedCity",
            "state": "Gen",
            "postalCode": "0000",
            "country": "US"
        }]
    })
}

fn gen_practitioner(id: &str, rng: &mut impl Rng) -> serde_json::Value {
    let genders = ["male", "female", "other", "unknown"];
    let gender = genders[rng.random_range(0..genders.len())];
    let year: u32 = rng.random_range(1950..=2000);
    let month: u32 = rng.random_range(1..=12);
    let day: u32 = rng.random_range(1..=28);

    serde_json::json!({
        "resourceType": "Practitioner",
        "id": id,
        "meta": {
            "profile": ["http://hl7.org/fhir/StructureDefinition/Practitioner"]
        },
        "name": [{
            "family": format!("Family{}", &id[..id.len().min(4)]),
            "given": [format!("Given{}", &id[..id.len().min(4)])],
            "use": "official"
        }],
        "identifier": [{
            "system": "http://hl7.org/fhir/sid/us-npi",
            "value": format!("{:09}", rng.random_range(100000000..999999999))
        }],
        "active": true,
        "birthDate": format!("{:04}-{:02}-{:02}", year, month, day),
        "gender": gender
    })
}

fn gen_practitioner_role(
    id: &str,
    org_ids: &[String],
    prac_ids: &[String],
    loc_ids: &[String],
    rng: &mut impl Rng,
) -> serde_json::Value {
    let role_codes = [
        ("doctor", "Doctor"),
        ("nurse", "Nurse"),
        ("pharmacist", "Pharmacist"),
        ("physicaltherapist", "Physical Therapist"),
        ("socialworker", "Social Worker"),
        ("psychologist", "Psychologist"),
        ("dietitian", "Dietitian"),
        ("optometrist", "Optometrist"),
    ];
    let specialties = [
        ("394577000", "Anesthesiology"),
        ("394583001", "Dermatology"),
        ("394579002", "Cardiology"),
        ("408467006", "Emergency medicine"),
        ("394597006", "Oncology"),
        ("394580004", "General practice"),
        ("394609004", "Orthopaedics"),
        ("394612008", "Paediatrics"),
        ("394600006", "Neurology"),
        ("394585009", "Psychiatry"),
        ("394591006", "Ophthalmology"),
        ("394584008", "Respiratory"),
    ];
    let spec = specialties[rng.random_range(0..specialties.len())];
    let role = role_codes[rng.random_range(0..role_codes.len())];

    let days = ["mon", "tue", "wed", "thu", "fri", "sat", "sun"];
    let n_days = rng.random_range(2..6);
    let start_day = rng.random_range(0..days.len() - n_days + 1);
    let work_days: Vec<String> = days[start_day..start_day + n_days]
        .iter()
        .map(|d| d.to_string())
        .collect();

    let mut locations = Vec::new();
    if !loc_ids.is_empty() {
        locations.push(serde_json::json!({
            "reference": random_ref("Location", loc_ids, rng)
        }));
    }

    serde_json::json!({
        "resourceType": "PractitionerRole",
        "id": id,
        "meta": {
            "profile": ["http://hl7.org/fhir/StructureDefinition/PractitionerRole"]
        },
        "active": true,
        "practitioner": {
            "reference": random_ref("Practitioner", prac_ids, rng)
        },
        "organization": {
            "reference": random_ref("Organization", org_ids, rng)
        },
        "code": [{
            "coding": [{
                "system": "http://terminology.hl7.org/CodeSystem/v3-RoleCode",
                "code": role.0,
                "display": role.1
            }]
        }],
        "specialty": [{
            "coding": [{
                "system": "http://snomed.info/sct",
                "code": spec.0,
                "display": spec.1
            }]
        }],
        "location": locations,
        "telecom": [{
            "system": "phone",
            "value": format!("{:010}", rng.random_range(1000000000..10000000000u64)),
            "use": "work"
        }],
        "availableTime": [{
            "daysOfWeek": work_days,
            "availableStartTime": "08:00:00",
            "availableEndTime": "17:00:00"
        }]
    })
}

fn gen_location(id: &str, rng: &mut impl Rng) -> serde_json::Value {
    let loc_types = [
        ("si", "Site"),
        ("bu", "Building"),
        ("wi", "Wing"),
        ("wa", "Ward"),
        ("lvl", "Level"),
        ("co", "Corner"),
    ];
    let phys_types = [
        ("si", "Site"),
        ("bu", "Building"),
        ("wi", "Wing"),
        ("wa", "Ward"),
        ("lvl", "Level"),
        ("co", "Corner"),
        ("ho", "House"),
        ("ca", "Room"),
        ("ve", "Vehicle"),
    ];
    let loc_type = loc_types[rng.random_range(0..loc_types.len())];
    let phys_type = phys_types[rng.random_range(0..phys_types.len())];

    let lat = 40.0 + rng.random_range(-50..50) as f64 / 1000.0;
    let lon = -74.0 + rng.random_range(-50..50) as f64 / 1000.0;

    let statuses = ["active", "suspended", "inactive"];
    let status = statuses[rng.random_range(0..2)];

    serde_json::json!({
        "resourceType": "Location",
        "id": id,
        "meta": {
            "profile": ["http://hl7.org/fhir/StructureDefinition/Location"]
        },
        "status": status,
        "name": format!("Clinic {}", &id[..id.len().min(8)]),
        "type": [{
            "coding": [{
                "system": "http://terminology.hl7.org/CodeSystem/v3-RoleCode",
                "code": loc_type.0,
                "display": loc_type.1
            }]
        }],
        "physicalType": {
            "coding": [{
                "system": "http://terminology.hl7.org/CodeSystem/location-physical-type",
                "code": phys_type.0,
                "display": phys_type.1
            }]
        },
        "position": {
            "latitude": lat,
            "longitude": lon
        },
        "address": {
            "type": "physical",
            "line": [format!("{} Main St", rng.random_range(100..9999))],
            "city": "GeneratedCity",
            "state": "Gen",
            "postalCode": "0000",
            "country": "US"
        }
    })
}

fn gen_healthcare_service(
    id: &str,
    org_ids: &[String],
    loc_ids: &[String],
    rng: &mut impl Rng,
) -> serde_json::Value {
    let svc_types = [
        ("1", "Emergency department"),
        ("2", "Hospital clinic"),
        ("3", "Hospital service"),
        ("4", "Outpatient clinic"),
        ("5", "Specialist clinic"),
        ("6", "Rehabilitation"),
        ("7", "Pharmacy"),
        ("8", "Laboratory"),
        ("9", "Imaging"),
        ("10", "Mental health"),
        ("11", "Dental"),
        ("12", "Home health"),
        ("13", "Hospice"),
        ("14", "Telehealth"),
        ("15", "Urgent care"),
    ];
    let specialties = [
        ("394577000", "Anesthesiology"),
        ("394583001", "Dermatology"),
        ("394579002", "Cardiology"),
        ("394580004", "General practice"),
        ("394597006", "Oncology"),
        ("394600006", "Neurology"),
        ("394609004", "Orthopaedics"),
        ("394612008", "Paediatrics"),
        ("394584008", "Respiratory"),
    ];

    let svc = svc_types[rng.random_range(0..svc_types.len())];
    let spec = specialties[rng.random_range(0..specialties.len())];

    let mut locations = Vec::new();
    if !loc_ids.is_empty() {
        locations.push(serde_json::json!({
            "reference": random_ref("Location", loc_ids, rng)
        }));
    }

    serde_json::json!({
        "resourceType": "HealthcareService",
        "id": id,
        "meta": {
            "profile": ["http://hl7.org/fhir/StructureDefinition/HealthcareService"]
        },
        "active": true,
        "providedBy": {
            "reference": random_ref("Organization", org_ids, rng)
        },
        "type": [{
            "coding": [{
                "system": "http://terminology.hl7.org/CodeSystem/service-type",
                "code": svc.0,
                "display": svc.1
            }]
        }],
        "specialty": [{
            "coding": [{
                "system": "http://snomed.info/sct",
                "code": spec.0,
                "display": spec.1
            }]
        }],
        "location": locations,
        "name": format!("{} Service", svc.1),
        "comment": format!("Provides {} services", svc.1.to_lowercase())
    })
}

fn gen_endpoint(id: &str, org_ids: &[String], rng: &mut impl Rng) -> serde_json::Value {
    serde_json::json!({
        "resourceType": "Endpoint",
        "id": id,
        "meta": {
            "profile": ["http://hl7.org/fhir/StructureDefinition/Endpoint"]
        },
        "status": "active",
        "connectionType": {
            "system": "http://terminology.hl7.org/CodeSystem/endpoint-connection-type",
            "code": "hl7-fhir-rest",
            "display": "HL7 FHIR REST"
        },
        "name": format!("Endpoint {}", &id[..id.len().min(8)]),
        "payloadType": [{
            "coding": [{
                "system": "http://terminology.hl7.org/CodeSystem/endpoint-payload-type",
                "code": "none",
                "display": "None"
            }]
        }],
        "address": format!("https://{}.example.org/fhir", &id[..id.len().min(8)]),
        "managingOrganization": if org_ids.is_empty() {
            serde_json::Value::Null
        } else {
            serde_json::json!({
                "reference": random_ref("Organization", org_ids, rng)
            })
        }
    })
}

/// Generic fallback generator for resource types not explicitly handled.
/// Produces a minimal resource with `resourceType`, `id`, `meta`, and `status`.
fn gen_generic(resource_type: &str, id: &str, _rng: &mut impl Rng) -> serde_json::Value {
    let mut resource = serde_json::json!({
        "resourceType": resource_type,
        "id": id,
        "meta": {
            "profile": [format!("http://hl7.org/fhir/StructureDefinition/{}", resource_type)]
        },
        "status": "active"
    });
    if matches!(
        resource_type,
        "Patient" | "Person" | "Group" | "List" | "Library"
    ) {
        resource["name"] = serde_json::json!(format!("Generated {}", resource_type));
    }
    resource
}

/// Stamp a random `meta.lastUpdated` date on a resource, within the last 12 months.
fn stamp_created_date(resource: &mut serde_json::Value, rng: &mut impl Rng) {
    let now = Utc::now();
    let days_ago = rng.random_range(0..365);
    let created = now - Duration::days(days_ago);
    let date_str = created.format("%Y-%m-%dT%H:%M:%S%.3fZ").to_string();
    resource["meta"]["lastUpdated"] = serde_json::Value::String(date_str);
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn generate_creates_ndjson_files() {
        let dir = tempfile::tempdir().expect("should create temp dir");
        let mut counts = HashMap::new();
        counts.insert("Organization".to_string(), 10);
        counts.insert("Practitioner".to_string(), 50);
        counts.insert("PractitionerRole".to_string(), 100);
        counts.insert("Location".to_string(), 20);
        counts.insert("HealthcareService".to_string(), 50);

        let profile_urls = HashMap::new();
        let ids = generate_bulk_data(&counts, &profile_urls, &[], &HashMap::new(), dir.path())
            .expect("should succeed");

        assert_eq!(ids.get("Organization").expect("key should exist").len(), 10);
        assert_eq!(ids.get("Practitioner").expect("key should exist").len(), 50);
        assert_eq!(
            ids.get("PractitionerRole").expect("key should exist").len(),
            100
        );
        assert_eq!(ids.get("Location").expect("key should exist").len(), 20);
        assert_eq!(
            ids.get("HealthcareService")
                .expect("key should exist")
                .len(),
            50
        );

        for (resource_type, count) in &counts {
            let path = dir
                .path()
                .join("data")
                .join(format!("{resource_type}.ndjson"));
            assert!(path.exists(), "{resource_type}.ndjson should exist");
            let contents = std::fs::read_to_string(&path).expect("should read file");
            let lines: Vec<&str> = contents.lines().filter(|l| !l.is_empty()).collect();
            assert_eq!(
                lines.len(),
                *count as usize,
                "{resource_type} should have {count} lines"
            );

            for line in &lines {
                let parsed: serde_json::Value =
                    serde_json::from_str(line).expect("should parse valid JSON");
                assert_eq!(parsed["resourceType"], *resource_type);
                assert!(
                    !parsed["id"]
                        .as_str()
                        .expect("should have a string value")
                        .is_empty()
                );
            }
        }
    }

    #[test]
    fn cross_references_are_valid() {
        let dir = tempfile::tempdir().expect("should create temp dir");
        let mut counts = HashMap::new();
        counts.insert("Organization".to_string(), 5);
        counts.insert("Practitioner".to_string(), 10);
        counts.insert("PractitionerRole".to_string(), 20);
        counts.insert("Location".to_string(), 5);
        counts.insert("HealthcareService".to_string(), 10);

        let profile_urls = HashMap::new();
        let ids = generate_bulk_data(&counts, &profile_urls, &[], &HashMap::new(), dir.path())
            .expect("should succeed");

        let pr_path = dir.path().join("data/PractitionerRole.ndjson");
        let pr_contents = std::fs::read_to_string(&pr_path).expect("should read file");
        let org_ids = ids.get("Organization").expect("key should exist");
        let prac_ids = ids.get("Practitioner").expect("key should exist");

        for line in pr_contents.lines().filter(|l| !l.is_empty()) {
            let pr: serde_json::Value =
                serde_json::from_str(line).expect("should parse valid JSON");
            let prac_ref = pr["practitioner"]["reference"]
                .as_str()
                .expect("should have a string value");
            assert!(prac_ref.starts_with("Practitioner/"));
            let prac_id = prac_ref
                .strip_prefix("Practitioner/")
                .expect("should have expected prefix");
            assert!(
                prac_ids.contains(&prac_id.to_string()),
                "Practitioner reference {prac_id} should exist"
            );

            let org_ref = pr["organization"]["reference"]
                .as_str()
                .expect("should have a string value");
            assert!(org_ref.starts_with("Organization/"));
            let org_id = org_ref
                .strip_prefix("Organization/")
                .expect("should have expected prefix");
            assert!(
                org_ids.contains(&org_id.to_string()),
                "Organization reference {org_id} should exist"
            );
        }

        let hs_path = dir.path().join("data/HealthcareService.ndjson");
        let hs_contents = std::fs::read_to_string(&hs_path).expect("should read file");
        for line in hs_contents.lines().filter(|l| !l.is_empty()) {
            let hs: serde_json::Value =
                serde_json::from_str(line).expect("should parse valid JSON");
            let org_ref = hs["providedBy"]["reference"]
                .as_str()
                .expect("should have a string value");
            assert!(org_ref.starts_with("Organization/"));
        }
    }

    #[test]
    fn location_has_coordinates() {
        let dir = tempfile::tempdir().expect("should create temp dir");
        let mut counts = HashMap::new();
        counts.insert("Location".to_string(), 100);

        generate_bulk_data(&counts, &HashMap::new(), &[], &HashMap::new(), dir.path())
            .expect("should succeed");

        let loc_path = dir.path().join("data/Location.ndjson");
        let contents = std::fs::read_to_string(&loc_path).expect("should read file");
        for line in contents.lines().filter(|l| !l.is_empty()) {
            let loc: serde_json::Value =
                serde_json::from_str(line).expect("should parse valid JSON");
            let lat = loc["position"]["latitude"]
                .as_f64()
                .expect("should have a float value");
            let lon = loc["position"]["longitude"]
                .as_f64()
                .expect("should have a float value");
            assert!(
                (20.0..=60.0).contains(&lat),
                "Latitude {lat} should be in US range"
            );
            assert!(
                (-130.0..=-60.0).contains(&lon),
                "Longitude {lon} should be in US range"
            );
        }
    }

    #[test]
    fn creation_order_respects_dependencies() {
        let mut counts = HashMap::new();
        counts.insert("PractitionerRole".to_string(), 10);
        counts.insert("Organization".to_string(), 5);
        counts.insert("Endpoint".to_string(), 5);
        counts.insert("Location".to_string(), 5);

        let order = bulk_data_creation_order(&counts);

        let org_idx = order
            .iter()
            .position(|t| t == "Organization")
            .expect("type should be in creation order");
        let endpoint_idx = order
            .iter()
            .position(|t| t == "Endpoint")
            .expect("type should be in creation order");
        let loc_idx = order
            .iter()
            .position(|t| t == "Location")
            .expect("type should be in creation order");
        let pr_idx = order
            .iter()
            .position(|t| t == "PractitionerRole")
            .expect("type should be in creation order");
        assert!(
            org_idx < pr_idx,
            "Organization should come before PractitionerRole"
        );
        assert!(
            endpoint_idx < loc_idx,
            "Endpoint should come before Location"
        );
        assert!(
            loc_idx < pr_idx,
            "Location should come before PractitionerRole"
        );
    }

    #[test]
    fn generic_fallback_works() {
        let dir = tempfile::tempdir().expect("should create temp dir");
        let mut counts = HashMap::new();
        counts.insert("Patient".to_string(), 5);

        let ids = generate_bulk_data(&counts, &HashMap::new(), &[], &HashMap::new(), dir.path())
            .expect("should succeed");
        assert_eq!(ids.get("Patient").expect("key should exist").len(), 5);

        let path = dir.path().join("data/Patient.ndjson");
        let contents = std::fs::read_to_string(&path).expect("should read file");
        let first_line = contents
            .lines()
            .next()
            .expect("should have at least one line");
        let patient: serde_json::Value =
            serde_json::from_str(first_line).expect("should parse valid JSON");
        assert_eq!(patient["resourceType"], "Patient");
        assert_eq!(patient["status"], "active");
    }

    #[test]
    fn profile_urls_override_meta_profile() {
        let dir = tempfile::tempdir().expect("should create temp dir");
        let mut counts = HashMap::new();
        counts.insert("Organization".to_string(), 3);

        let mut profile_urls = HashMap::new();
        profile_urls.insert(
            "Organization".to_string(),
            "http://example.org/fhir/StructureDefinition/MyOrg".to_string(),
        );

        let ids = generate_bulk_data(&counts, &profile_urls, &[], &HashMap::new(), dir.path())
            .expect("should succeed");
        assert_eq!(ids.get("Organization").expect("key should exist").len(), 3);

        let path = dir.path().join("data/Organization.ndjson");
        let contents = std::fs::read_to_string(&path).expect("should read file");
        for line in contents.lines().filter(|l| !l.is_empty()) {
            let org: serde_json::Value =
                serde_json::from_str(line).expect("should parse valid JSON");
            let profiles = org["meta"]["profile"]
                .as_array()
                .expect("should be an array");
            assert_eq!(
                profiles[0].as_str().expect("should have a string value"),
                "http://example.org/fhir/StructureDefinition/MyOrg",
                "meta.profile should use the IG profile URL"
            );
        }
    }

    #[test]
    fn profile_urls_fallback_to_base_fhir() {
        let dir = tempfile::tempdir().expect("should create temp dir");
        let mut counts = HashMap::new();
        counts.insert("Organization".to_string(), 2);

        let profile_urls = HashMap::new();
        let ids = generate_bulk_data(&counts, &profile_urls, &[], &HashMap::new(), dir.path())
            .expect("should succeed");
        assert_eq!(ids.get("Organization").expect("key should exist").len(), 2);

        let path = dir.path().join("data/Organization.ndjson");
        let contents = std::fs::read_to_string(&path).expect("should read file");
        for line in contents.lines().filter(|l| !l.is_empty()) {
            let org: serde_json::Value =
                serde_json::from_str(line).expect("should parse valid JSON");
            let profiles = org["meta"]["profile"]
                .as_array()
                .expect("should be an array");
            assert_eq!(
                profiles[0].as_str().expect("should have a string value"),
                "http://hl7.org/fhir/StructureDefinition/Organization",
                "meta.profile should fall back to base FHIR profile"
            );
        }
    }

    #[test]
    fn profile_aware_generation_uses_structure_definition() {
        let dir = tempfile::tempdir().expect("should create temp dir");
        let mut counts = HashMap::new();
        counts.insert("Patient".to_string(), 2);

        let profile = StructureDefinition {
            resource_type: "StructureDefinition".to_string(),
            url: "http://example.org/fhir/StructureDefinition/MyPatient".to_string(),
            name: "MyPatient".to_string(),
            base_type: "Patient".to_string(),
            kind: "resource".to_string(),
            derivation: Some("constraint".to_string()),
            snapshot: None,
            differential: None,
            base_definition: Some("http://hl7.org/fhir/StructureDefinition/Patient".to_string()),
        };

        let ids = generate_bulk_data(
            &counts,
            &HashMap::new(),
            &[profile],
            &HashMap::new(),
            dir.path(),
        )
        .expect("should succeed");
        assert_eq!(ids.get("Patient").expect("key should exist").len(), 2);

        let path = dir.path().join("data/Patient.ndjson");
        let contents = std::fs::read_to_string(&path).expect("should read file");
        for line in contents.lines().filter(|l| !l.is_empty()) {
            let patient: serde_json::Value =
                serde_json::from_str(line).expect("should parse valid JSON");
            assert_eq!(patient["resourceType"], "Patient");
            let profiles = patient["meta"]["profile"]
                .as_array()
                .expect("should be an array");
            assert_eq!(
                profiles[0].as_str().expect("should have a string value"),
                "http://example.org/fhir/StructureDefinition/MyPatient",
                "Profile-aware generation should use the StructureDefinition URL"
            );
        }
    }

    #[test]
    fn provenance_overlay_uses_existing_ids() {
        let mut provenance = serde_json::json!({
            "resourceType": "Provenance",
            "id": "provenance-1",
            "target": [{ "reference": "Organization/random-uuid" }],
            "agent": [{ "who": { "reference": "Organization/random-uuid" } }],
            "entity": [{ "role": "source", "what": { "reference": "Resource/random-uuid" } }]
        });

        let org_ids = vec!["organization-1".to_string(), "organization-2".to_string()];
        let prac_ids = vec!["practitioner-1".to_string()];
        let mut rng = rand::rng();

        overlay_cross_references(
            &mut provenance,
            "Provenance",
            "provenance-1",
            &org_ids,
            &prac_ids,
            &[],
            &[],
            &[],
            &[],
            &mut rng,
        );

        let target_ref = provenance["target"][0]["reference"]
            .as_str()
            .expect("should have a string value");
        let agent_ref = provenance["agent"][0]["who"]["reference"]
            .as_str()
            .expect("should have a string value");
        let entity_ref = provenance["entity"][0]["what"]["reference"]
            .as_str()
            .expect("should succeed");

        assert_eq!(
            target_ref, "Practitioner/practitioner-1",
            "target should reference a non-Organization type for _revinclude coverage"
        );
        assert!(
            agent_ref == "Organization/organization-1"
                || agent_ref == "Organization/organization-2",
            "agent.who should reference an existing Organization ID"
        );
        assert!(
            ["Organization/organization-1", "Organization/organization-2",].contains(&entity_ref),
            "entity.what should reference an Organization ID"
        );
    }

    #[test]
    fn provenance_overlay_populates_target_extension() {
        let mut provenance = serde_json::json!({
            "resourceType": "Provenance",
            "id": "provenance-1",
            "target": [{ "reference": "Organization/placeholder" }],
        });

        let org_ids = vec!["organization-1".to_string()];
        let mut rng = rand::rng();

        overlay_cross_references(
            &mut provenance,
            "Provenance",
            "provenance-1",
            &org_ids,
            &[],
            &[],
            &[],
            &[],
            &[],
            &mut rng,
        );

        let ext = &provenance["target"][0]["extension"];
        assert!(ext.is_array(), "target.extension should be populated");
        assert_eq!(
            ext[0]["url"].as_str(),
            Some("http://hl7.org/fhir/StructureDefinition/targetPath"),
            "target.extension should be the standard targetPath extension"
        );
        assert!(
            ext[0]["valueString"].is_string(),
            "targetPath extension should carry a valueString"
        );
    }

    #[test]
    fn organization_overlay_anchor_has_partof() {
        let org_ids = vec![
            "organization-1".to_string(),
            "organization-2".to_string(),
            "organization-3".to_string(),
        ];
        let mut rng = rand::rng();

        let mut anchor =
            serde_json::json!({ "resourceType": "Organization", "id": "organization-1" });
        overlay_cross_references(
            &mut anchor,
            "Organization",
            "organization-1",
            &org_ids,
            &[],
            &[],
            &[],
            &[],
            &[],
            &mut rng,
        );
        assert_eq!(
            anchor["partOf"]["reference"].as_str(),
            Some("Organization/organization-2"),
            "organization-1 must reference organization-2 via partOf"
        );

        let mut parent = serde_json::json!({ "resourceType": "Organization", "id": "organization-2", "partOf": { "reference": "Organization/organization-1" } });
        overlay_cross_references(
            &mut parent,
            "Organization",
            "organization-2",
            &org_ids,
            &[],
            &[],
            &[],
            &[],
            &[],
            &mut rng,
        );
        assert!(
            parent.get("partOf").is_none(),
            "organization-2 must be a root (no partOf) to prevent a cycle"
        );
    }

    #[test]
    fn overlay_adds_endpoint_links_for_include_tests() {
        let mut location = serde_json::json!({ "resourceType": "Location", "id": "location-1" });
        let mut healthcare_service =
            serde_json::json!({ "resourceType": "HealthcareService", "id": "healthcareservice-1" });
        let mut practitioner_role =
            serde_json::json!({ "resourceType": "PractitionerRole", "id": "practitionerrole-1" });

        let org_ids = vec!["organization-1".to_string()];
        let prac_ids = vec!["practitioner-1".to_string()];
        let loc_ids = vec!["location-1".to_string()];
        let hs_ids = vec!["healthcareservice-1".to_string()];
        let endpoint_ids = vec!["endpoint-1".to_string()];
        let mut rng = rand::rng();

        overlay_cross_references(
            &mut location,
            "Location",
            "location-1",
            &org_ids,
            &prac_ids,
            &loc_ids,
            &hs_ids,
            &[],
            &endpoint_ids,
            &mut rng,
        );
        overlay_cross_references(
            &mut healthcare_service,
            "HealthcareService",
            "healthcareservice-1",
            &org_ids,
            &prac_ids,
            &loc_ids,
            &hs_ids,
            &[],
            &endpoint_ids,
            &mut rng,
        );
        overlay_cross_references(
            &mut practitioner_role,
            "PractitionerRole",
            "practitionerrole-1",
            &org_ids,
            &prac_ids,
            &loc_ids,
            &hs_ids,
            &[],
            &endpoint_ids,
            &mut rng,
        );

        assert_eq!(
            location["endpoint"][0]["reference"]
                .as_str()
                .expect("should have a string value"),
            "Endpoint/endpoint-1"
        );
        assert_eq!(
            healthcare_service["endpoint"][0]["reference"]
                .as_str()
                .expect("should succeed"),
            "Endpoint/endpoint-1"
        );
        assert_eq!(
            practitioner_role["endpoint"][0]["reference"]
                .as_str()
                .expect("should succeed"),
            "Endpoint/endpoint-1"
        );
    }

    #[test]
    fn location_one_links_to_organization_one_when_present() {
        let mut location = serde_json::json!({ "resourceType": "Location", "id": "location-1" });
        let org_ids = vec!["organization-1".to_string(), "organization-2".to_string()];
        let mut rng = rand::rng();

        overlay_cross_references(
            &mut location,
            "Location",
            "location-1",
            &org_ids,
            &[],
            &[],
            &[],
            &[],
            &[],
            &mut rng,
        );

        assert_eq!(
            location["managingOrganization"]["reference"]
                .as_str()
                .expect("should succeed"),
            "Organization/organization-1"
        );
    }

    #[test]
    fn provenance_overlay_seeds_id_one_targets_for_revinclude_coverage() {
        let org_ids = vec!["organization-1".to_string()];
        let prac_ids = vec!["practitioner-1".to_string()];
        let loc_ids = vec!["location-1".to_string()];
        let hs_ids = vec!["healthcareservice-1".to_string()];
        let practitioner_role_ids = vec!["practitionerrole-1".to_string()];
        let mut rng = rand::rng();

        let mut p1 = serde_json::json!({ "resourceType": "Provenance", "id": "provenance-1" });
        let mut p2 = serde_json::json!({ "resourceType": "Provenance", "id": "provenance-2" });

        for p in [&mut p1, &mut p2] {
            let id = p["id"]
                .as_str()
                .expect("should have a string value")
                .to_string();
            overlay_cross_references(
                p,
                "Provenance",
                &id,
                &org_ids,
                &prac_ids,
                &loc_ids,
                &hs_ids,
                &practitioner_role_ids,
                &[],
                &mut rng,
            );
        }

        assert_eq!(
            p1["target"][0]["reference"]
                .as_str()
                .expect("should have a string value"),
            "Practitioner/practitioner-1"
        );
        assert_eq!(
            p2["target"][0]["reference"]
                .as_str()
                .expect("should have a string value"),
            "Location/location-1"
        );
    }

    #[test]
    fn supplement_resource_creates_valid_fhir_json() {
        let resource =
            generate_supplement_resource("Organization", &HashMap::new(), &[], &HashMap::new())
                .expect("should succeed");

        assert_eq!(resource["resourceType"], "Organization");
        assert_eq!(resource["id"], "organization-1");
        assert!(resource["meta"]["profile"].as_array().is_some());
        assert!(resource["meta"]["lastUpdated"].as_str().is_some());
    }

    #[test]
    fn supplement_resource_uses_profile_url_when_provided() {
        let mut profile_urls = HashMap::new();
        profile_urls.insert(
            "Organization".to_string(),
            "http://example.org/fhir/StructureDefinition/MyOrg".to_string(),
        );

        let resource =
            generate_supplement_resource("Organization", &profile_urls, &[], &HashMap::new())
                .expect("should succeed");

        let profiles = resource["meta"]["profile"]
            .as_array()
            .expect("should be an array");
        assert_eq!(
            profiles[0].as_str().expect("should have a string value"),
            "http://example.org/fhir/StructureDefinition/MyOrg"
        );
    }

    #[test]
    fn supplement_resource_normalizes_references() {
        let resource =
            generate_supplement_resource("PractitionerRole", &HashMap::new(), &[], &HashMap::new())
                .expect("should succeed");

        let practitioner_ref = resource["practitioner"]["reference"]
            .as_str()
            .expect("should have a string value");
        assert_eq!(practitioner_ref, "Practitioner/practitioner-1");

        let organization_ref = resource["organization"]["reference"]
            .as_str()
            .expect("should have a string value");
        assert_eq!(organization_ref, "Organization/organization-1");
    }

    #[test]
    fn supplement_resource_handles_unknown_type() {
        let resource =
            generate_supplement_resource("UnknownType", &HashMap::new(), &[], &HashMap::new())
                .expect("should succeed");

        assert_eq!(resource["resourceType"], "UnknownType");
        assert_eq!(resource["id"], "unknowntype-1");
        assert_eq!(resource["status"], "active");
    }

    #[test]
    fn write_supplement_creates_files_for_uncovered_types() {
        let dir = tempfile::tempdir().expect("should create temp dir");

        let mut bulk_counts = HashMap::new();
        bulk_counts.insert("Organization".to_string(), 5);

        let creation_order = bulk_data_creation_order(&bulk_counts);

        let supplement_ids = write_supplement_ndjson(
            &creation_order,
            &bulk_counts,
            &HashMap::new(),
            &[],
            &HashMap::new(),
            dir.path(),
        )
        .expect("should succeed");

        assert!(
            !supplement_ids.contains_key("Organization"),
            "Organization has bulk count, should not be in supplement"
        );

        for (resource_type, ids) in &supplement_ids {
            let path = dir
                .path()
                .join("data")
                .join(format!("{resource_type}.ndjson"));
            assert!(path.exists(), "{resource_type}.ndjson should exist");
            let contents = std::fs::read_to_string(&path).expect("should read file");
            let lines: Vec<&str> = contents.lines().filter(|l| !l.is_empty()).collect();
            assert_eq!(lines.len(), 1, "{resource_type} should have 1 line");
            assert_eq!(ids.len(), 1, "{resource_type} should have 1 ID");

            let parsed: serde_json::Value =
                serde_json::from_str(lines[0]).expect("should parse valid JSON");
            assert_eq!(parsed["resourceType"], *resource_type);
            assert_eq!(parsed["id"], format!("{}-1", resource_type.to_lowercase()));
        }
    }

    #[test]
    fn write_supplement_skips_non_resource_types() {
        let dir = tempfile::tempdir().expect("should create temp dir");

        let mut bulk_counts = HashMap::new();
        bulk_counts.insert("Organization".to_string(), 5);

        let mut creation_order = bulk_data_creation_order(&bulk_counts);
        creation_order.push("Extension".to_string());

        let supplement_ids = write_supplement_ndjson(
            &creation_order,
            &bulk_counts,
            &HashMap::new(),
            &[],
            &HashMap::new(),
            dir.path(),
        )
        .expect("should succeed");

        assert!(
            !supplement_ids.contains_key("Extension"),
            "Extension is a non-resource type and should be skipped"
        );
    }

    #[test]
    fn write_supplement_appends_to_combined_ndjson() {
        let dir = tempfile::tempdir().expect("should create temp dir");

        let mut bulk_counts = HashMap::new();
        bulk_counts.insert("Organization".to_string(), 2);

        let mut creation_order = bulk_data_creation_order(&bulk_counts);
        creation_order.push("Patient".to_string());

        generate_bulk_data(
            &bulk_counts,
            &HashMap::new(),
            &[],
            &HashMap::new(),
            dir.path(),
        )
        .expect("should succeed");

        write_supplement_ndjson(
            &creation_order,
            &bulk_counts,
            &HashMap::new(),
            &[],
            &HashMap::new(),
            dir.path(),
        )
        .expect("should succeed");

        let combined_path = dir.path().join("data/combined.ndjson");
        let contents = std::fs::read_to_string(&combined_path).expect("should read file");
        let lines: Vec<&str> = contents.lines().filter(|l| !l.is_empty()).collect();

        assert!(
            lines.len() > 2,
            "combined.ndjson should have bulk + supplement resources"
        );
    }

    #[test]
    fn update_ndjson_creates_file_with_same_count() {
        let dir = tempfile::tempdir().expect("should create temp dir");

        let mut counts = HashMap::new();
        counts.insert("Organization".to_string(), 5);
        counts.insert("Practitioner".to_string(), 10);

        let ids = generate_bulk_data(&counts, &HashMap::new(), &[], &HashMap::new(), dir.path())
            .expect("should succeed");

        generate_update_ndjson(&ids, dir.path()).expect("should generate bulk data");

        let update_path = dir.path().join("data/update.ndjson");
        assert!(update_path.exists(), "update.ndjson should exist");

        let contents = std::fs::read_to_string(&update_path).expect("should read file");
        let lines: Vec<&str> = contents.lines().filter(|l| !l.is_empty()).collect();

        let total_bulk: usize = counts.values().sum::<u64>() as usize;
        assert_eq!(
            lines.len(),
            total_bulk,
            "update.ndjson should have {total_bulk} lines"
        );
    }

    #[test]
    fn update_ndjson_resources_differ_from_originals() {
        let dir = tempfile::tempdir().expect("should create temp dir");

        let mut counts = HashMap::new();
        counts.insert("Organization".to_string(), 3);

        let ids = generate_bulk_data(&counts, &HashMap::new(), &[], &HashMap::new(), dir.path())
            .expect("should succeed");

        let orig_path = dir.path().join("data/Organization.ndjson");
        let orig_contents = std::fs::read_to_string(&orig_path).expect("should read file");
        let orig_lines: Vec<&str> = orig_contents.lines().filter(|l| !l.is_empty()).collect();

        generate_update_ndjson(&ids, dir.path()).expect("should generate bulk data");

        let update_path = dir.path().join("data/update.ndjson");
        let update_contents = std::fs::read_to_string(&update_path).expect("should read file");
        let update_lines: Vec<&str> = update_contents.lines().filter(|l| !l.is_empty()).collect();

        assert_eq!(orig_lines.len(), update_lines.len());

        for (orig_line, update_line) in orig_lines.iter().zip(update_lines.iter()) {
            let orig: serde_json::Value =
                serde_json::from_str(orig_line).expect("should parse valid JSON");
            let updated: serde_json::Value =
                serde_json::from_str(update_line).expect("should parse valid JSON");

            assert_eq!(orig["id"], updated["id"]);
            assert_ne!(
                orig, updated,
                "Updated resource should differ from original"
            );
        }
    }

    #[test]
    fn update_ndjson_preserves_resource_type_and_id() {
        let dir = tempfile::tempdir().expect("should create temp dir");

        let mut counts = HashMap::new();
        counts.insert("Organization".to_string(), 2);
        counts.insert("Practitioner".to_string(), 2);

        let ids = generate_bulk_data(&counts, &HashMap::new(), &[], &HashMap::new(), dir.path())
            .expect("should succeed");

        generate_update_ndjson(&ids, dir.path()).expect("should generate bulk data");

        let update_path = dir.path().join("data/update.ndjson");
        let contents = std::fs::read_to_string(&update_path).expect("should read file");

        for line in contents.lines().filter(|l| !l.is_empty()) {
            let resource: serde_json::Value =
                serde_json::from_str(line).expect("should parse valid JSON");
            let rtype = resource["resourceType"]
                .as_str()
                .expect("should have a string value");
            let id = resource["id"].as_str().expect("should have a string value");

            assert!(!rtype.is_empty());
            assert!(!id.is_empty());
            assert!(id.starts_with(&rtype.to_lowercase()));
        }
    }

    #[test]
    fn update_ndjson_handles_empty_ids() {
        let dir = tempfile::tempdir().expect("should create temp dir");
        let ids = IdStore::new();

        std::fs::create_dir_all(dir.path().join("data")).expect("should create directory");

        generate_update_ndjson(&ids, dir.path()).expect("should generate bulk data");

        let update_path = dir.path().join("data/update.ndjson");
        assert!(update_path.exists(), "update.ndjson should exist");
        let contents = std::fs::read_to_string(&update_path).expect("should read file");
        assert!(contents.trim().is_empty(), "update.ndjson should be empty");
    }

    #[test]
    fn bulk_data_handles_empty_counts() {
        let dir = tempfile::tempdir().expect("should create temp dir");
        let counts = HashMap::new();

        let ids = generate_bulk_data(&counts, &HashMap::new(), &[], &HashMap::new(), dir.path())
            .expect("should succeed");

        assert!(
            ids.is_empty(),
            "No resources should be generated for empty counts"
        );
    }

    #[test]
    fn bulk_data_creates_combined_ndjson() {
        let dir = tempfile::tempdir().expect("should create temp dir");
        let mut counts = HashMap::new();
        counts.insert("Organization".to_string(), 3);
        counts.insert("Practitioner".to_string(), 2);

        generate_bulk_data(&counts, &HashMap::new(), &[], &HashMap::new(), dir.path())
            .expect("should succeed");

        let combined_path = dir.path().join("data/combined.ndjson");
        assert!(combined_path.exists(), "combined.ndjson should exist");

        let contents = std::fs::read_to_string(&combined_path).expect("should read file");
        let lines: Vec<&str> = contents.lines().filter(|l| !l.is_empty()).collect();
        let total: usize = counts.values().sum::<u64>() as usize;
        assert_eq!(
            lines.len(),
            total,
            "combined.ndjson should have all resources"
        );
    }

    #[test]
    fn bulk_data_stamps_created_date() {
        let dir = tempfile::tempdir().expect("should create temp dir");
        let mut counts = HashMap::new();
        counts.insert("Organization".to_string(), 5);

        generate_bulk_data(&counts, &HashMap::new(), &[], &HashMap::new(), dir.path())
            .expect("should succeed");

        let path = dir.path().join("data/Organization.ndjson");
        let contents = std::fs::read_to_string(&path).expect("should read file");

        for line in contents.lines().filter(|l| !l.is_empty()) {
            let org: serde_json::Value =
                serde_json::from_str(line).expect("should parse valid JSON");
            let last_updated = org["meta"]["lastUpdated"]
                .as_str()
                .expect("should have a string value");
            assert!(!last_updated.is_empty(), "meta.lastUpdated should be set");
            assert!(
                last_updated.contains('T'),
                "meta.lastUpdated should be an ISO timestamp, got: {last_updated}"
            );
        }
    }

    #[test]
    fn bulk_data_creation_order_includes_all_types() {
        let mut counts = HashMap::new();
        counts.insert("Organization".to_string(), 5);
        counts.insert("Practitioner".to_string(), 5);
        counts.insert("Endpoint".to_string(), 5);
        counts.insert("Location".to_string(), 5);
        counts.insert("HealthcareService".to_string(), 5);
        counts.insert("PractitionerRole".to_string(), 5);
        counts.insert("Provenance".to_string(), 5);
        counts.insert("Patient".to_string(), 5);

        let order = bulk_data_creation_order(&counts);

        for t in counts.keys() {
            assert!(order.contains(t), "{t} should be in creation order");
        }

        let org_idx = order
            .iter()
            .position(|t| t == "Organization")
            .expect("type should be in creation order");
        let prac_idx = order
            .iter()
            .position(|t| t == "Practitioner")
            .expect("type should be in creation order");
        let endpoint_idx = order
            .iter()
            .position(|t| t == "Endpoint")
            .expect("type should be in creation order");
        let loc_idx = order
            .iter()
            .position(|t| t == "Location")
            .expect("type should be in creation order");
        let hs_idx = order
            .iter()
            .position(|t| t == "HealthcareService")
            .expect("type should be in creation order");
        let pr_idx = order
            .iter()
            .position(|t| t == "PractitionerRole")
            .expect("type should be in creation order");
        let prov_idx = order
            .iter()
            .position(|t| t == "Provenance")
            .expect("type should be in creation order");

        assert!(org_idx < endpoint_idx, "Organization before Endpoint");
        assert!(endpoint_idx < loc_idx, "Endpoint before Location");
        assert!(loc_idx < hs_idx, "Location before HealthcareService");
        assert!(hs_idx < pr_idx, "HealthcareService before PractitionerRole");
        assert!(prac_idx < pr_idx, "Practitioner before PractitionerRole");
        assert!(pr_idx < prov_idx, "PractitionerRole before Provenance");
    }
}
