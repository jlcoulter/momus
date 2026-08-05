#![allow(dead_code)]

//! FHIR resource generator.
//!
//! Generates synthetic FHIR resources that conform to StructureDefinition profiles.
//! Ported from fhir-autotest's resource_generator module.
//!
//! The generation follows a 5-pass approach:
//! 1. Required fields (min > 0)
//! 2. Required slices (with discriminator pattern matching)
//! 3. Extension slices (with sub-extension support)
//! 4. MustSupport backbones
//! 5. MustSupport optional fields

use super::profile::*;
use anyhow::Result;
use std::collections::HashMap;

/// Generate a synthetic FHIR resource that conforms to a StructureDefinition profile.
pub fn generate_resource(
    profile: &StructureDefinition,
    all_profiles: &[StructureDefinition],
) -> Result<serde_json::Value> {
    generate_resource_with_value_sets(profile, all_profiles, &HashMap::new())
}

pub fn generate_resource_with_value_sets(
    profile: &StructureDefinition,
    all_profiles: &[StructureDefinition],
    value_set_systems: &HashMap<String, String>,
) -> Result<serde_json::Value> {
    let mut resource = serde_json::json!({
        "resourceType": profile.base_type
    });

    // Stamp the profile URL
    resource["meta"] = serde_json::json!({
        "profile": [profile.url]
    });

    let empty = vec![];
    let elements = match &profile.snapshot {
        Some(snapshot) => &snapshot.element,
        None => profile
            .differential
            .as_ref()
            .map(|d| &d.element)
            .unwrap_or(&empty),
    };

    // Pass 1: Required fields
    populate_required_fields(
        &mut resource,
        elements,
        &profile.base_type,
        all_profiles,
        value_set_systems,
    )?;

    // Pass 2: Required slices
    populate_required_slices(
        &mut resource,
        elements,
        &profile.base_type,
        all_profiles,
        value_set_systems,
    )?;

    // Pass 3: Extension slices
    populate_extension_slices(
        &mut resource,
        elements,
        &profile.base_type,
        all_profiles,
        value_set_systems,
    );

    // Pass 4: MustSupport backbones
    populate_must_support_backbones(
        &mut resource,
        elements,
        &profile.base_type,
        all_profiles,
        value_set_systems,
    );

    // Pass 5: MustSupport optional fields
    populate_must_support_optional_fields(
        &mut resource,
        elements,
        &profile.base_type,
        all_profiles,
        value_set_systems,
    );

    Ok(resource)
}

/// Pass 1: Populate required fields (min > 0).
fn populate_required_fields(
    resource: &mut serde_json::Value,
    elements: &[ElementDefinition],
    base_type: &str,
    all_profiles: &[StructureDefinition],
    value_set_systems: &HashMap<String, String>,
) -> Result<()> {
    for element in elements {
        if element.min.unwrap_or(0) == 0 {
            continue;
        }
        let field_path = get_field_path(&element.path, base_type);
        if field_path.is_none() || field_path.as_deref() == Some(base_type) {
            continue;
        }
        let path = field_path.unwrap();
        if path_has_value(resource, &path) {
            continue;
        }
        set_field_value(resource, &path, element, all_profiles, value_set_systems)?;
    }
    Ok(())
}

/// Pass 2: Populate required slices with discriminator pattern matching.
fn populate_required_slices(
    resource: &mut serde_json::Value,
    elements: &[ElementDefinition],
    base_type: &str,
    all_profiles: &[StructureDefinition],
    _value_set_systems: &HashMap<String, String>,
) -> Result<()> {
    // Collect all slice elements grouped by field
    let mut slice_fields: HashMap<String, Vec<&ElementDefinition>> = HashMap::new();
    for element in elements {
        if element.slice_name.is_some()
            && let Some(path) = get_field_path(&element.path, base_type)
            && path != base_type
        {
            slice_fields.entry(path).or_default().push(element);
        }
    }

    for (field_path, slices) in &slice_fields {
        // Find the discriminator path from the slicing element
        let discriminator_path: Option<String> = elements
            .iter()
            .find(|e| {
                let fpath = get_field_path(&e.path, base_type);
                fpath.as_deref() == Some(field_path) && e.slicing.is_some()
            })
            .and_then(|e| e.slicing.as_ref())
            .and_then(|s| s.discriminator.first().map(|d| d.path.clone()));

        // Check if any slice has min > 0 (required slice)
        let required_slices: Vec<&&ElementDefinition> =
            slices.iter().filter(|s| s.min.unwrap_or(0) > 0).collect();

        if required_slices.is_empty() {
            continue;
        }

        // Get or create the field value array
        let mut values: Vec<serde_json::Value> = if path_has_value(resource, field_path) {
            get_json_path(resource, field_path)
                .and_then(|v| v.as_array().cloned())
                .unwrap_or_default()
        } else {
            Vec::new()
        };

        for slice in required_slices {
            let mut value = generate_slice_value(
                slice,
                base_type,
                discriminator_path.as_deref(),
                all_profiles,
                elements,
                _value_set_systems,
            );
            // Apply identifier profile constraints if this is an Identifier slice
            if let Some(type_def) = slice.type_.first()
                && type_def.code == "Identifier"
            {
                apply_identifier_constraints(&mut value, type_def, all_profiles);
            }
            values.push(value);
        }

        set_json_path(resource, field_path, serde_json::json!(values));
    }
    Ok(())
}

/// Generate a value that matches a slice's discriminator pattern.
fn generate_slice_value(
    slice: &ElementDefinition,
    _base_type: &str,
    discriminator_path: Option<&str>,
    all_profiles: &[StructureDefinition],
    elements: &[ElementDefinition],
    _value_set_systems: &HashMap<String, String>,
) -> serde_json::Value {
    // Determine the base type from the slice's own type, or fall back
    let type_code = slice
        .type_
        .first()
        .map(|t| t.code.as_str())
        .unwrap_or("string");

    // Start with a base value for the type
    let mut value = generate_type_value(type_code);

    // Apply pattern values from the slice definition
    if let Some(val) = &slice.pattern_uri
        && let Some(obj) = value.as_object_mut()
    {
        if type_code == "Identifier" {
            obj.insert("system".to_string(), serde_json::json!(val));
        } else {
            obj.insert("value".to_string(), serde_json::json!(val));
        }
    }

    if let Some(val) = &slice.pattern_code
        && let Some(obj) = value.as_object_mut()
    {
        match type_code {
            "HumanName" => {
                obj.insert("use".to_string(), serde_json::json!(val));
            }
            "Address" => {
                obj.insert("type".to_string(), serde_json::json!(val));
            }
            _ => {}
        }
    }

    if let Some(val) = &slice.pattern_string
        && let Some(obj) = value.as_object_mut()
    {
        obj.insert("value".to_string(), serde_json::json!(val));
    }

    if let Some(val) = &slice.pattern_coding
        && let Some(obj) = value.as_object_mut()
    {
        obj.insert("coding".to_string(), val.clone());
    }

    if let Some(val) = &slice.pattern_codeable_concept
        && let Some(obj) = value.as_object_mut()
    {
        obj.insert("coding".to_string(), val.clone());
    }

    // HumanName slice support: find fixed use code
    if type_code == "HumanName"
        && let Some(slice_name) = &slice.slice_name
        && let Some(use_code) = find_human_name_use(slice_name, elements)
        && let Some(obj) = value.as_object_mut()
    {
        obj.insert("use".to_string(), serde_json::json!(use_code));
    }

    // Identifier slice handling
    if type_code == "Identifier"
        && let Some(obj) = value.as_object_mut()
    {
        let profile_url = slice
            .type_
            .first()
            .and_then(|t| {
                t.profile
                    .first()
                    .or_else(|| t.target_profile.first())
                    .map(|s| s.as_str())
            })
            .unwrap_or("");

        if let Some(system) = find_identifier_system(profile_url, all_profiles)
            && (!obj.contains_key("system")
                || obj
                    .get("system")
                    .and_then(|v| v.as_str())
                    .is_some_and(is_generic_identifier_system))
        {
            obj.insert("system".to_string(), serde_json::json!(system));
        }

        // Some IG slices define Identifier.system at nested paths
        if let Some(slice_name) = &slice.slice_name
            && let Some(system) = find_slice_system(slice_name, elements)
        {
            obj.insert("system".to_string(), serde_json::json!(system));
        }

        if let Some(identifier_type) = find_identifier_type(profile_url, all_profiles)
            && !obj.contains_key("type")
        {
            obj.insert("type".to_string(), identifier_type);
        }

        // Apply discriminator path
        match discriminator_path {
            Some("system") => {
                if let Some(slice_name) = &slice.slice_name
                    && let Some(system) = find_slice_system(slice_name, elements)
                {
                    obj.insert("system".to_string(), serde_json::json!(system));
                }
            }
            Some(path) if path.starts_with("type") && !obj.contains_key("type") => {
                if let Some(identifier_type) = find_identifier_type(profile_url, all_profiles) {
                    obj.insert("type".to_string(), identifier_type);
                }
                if !obj.contains_key("type") {
                    obj.insert(
                        "type".to_string(),
                        serde_json::json!({
                            "coding": [{
                                "system": "http://terminology.hl7.org/CodeSystem/v2-0203",
                                "code": "XX"
                            }]
                        }),
                    );
                }
            }
            _ => {}
        }
    }

    value
}

/// Pass 3: Populate extension slices with sub-extension support.
fn populate_extension_slices(
    resource: &mut serde_json::Value,
    elements: &[ElementDefinition],
    base_type: &str,
    all_profiles: &[StructureDefinition],
    _value_set_systems: &HashMap<String, String>,
) {
    // Collect extension slice elements
    let extension_slices: Vec<&ElementDefinition> = elements
        .iter()
        .filter(|e| {
            e.slice_name.is_some()
                && e.path == format!("{base_type}.extension")
                && !e.type_.is_empty()
                && e.type_[0].code == "Extension"
        })
        .collect();

    if extension_slices.is_empty() {
        return;
    }

    // Build a URL → StructureDefinition map for extension definitions
    let ext_def_map: HashMap<&str, &StructureDefinition> = all_profiles
        .iter()
        .filter(|p| p.base_type == "Extension")
        .map(|p| (p.url.as_str(), p))
        .collect();

    let mut extensions: Vec<serde_json::Value> = Vec::new();

    for slice in &extension_slices {
        // Get the profile URL from the slice's type reference
        let profile_url = slice.type_[0]
            .profile
            .first()
            .or_else(|| slice.type_[0].target_profile.first())
            .map(|s| s.split('|').next().unwrap_or(s));

        let Some(profile_url) = profile_url else {
            continue;
        };

        // Look up the extension definition
        let Some(ext_def) = ext_def_map.get(profile_url) else {
            // No definition found — create a simple extension
            let ext_url = format!(
                "http://example.org/Extension/{}",
                slice.slice_name.as_deref().unwrap_or("unknown")
            );
            extensions.push(serde_json::json!({
                "url": ext_url,
                "valueString": format!("generated-{}", slice.slice_name.as_deref().unwrap_or("unknown"))
            }));
            continue;
        };

        // Extract the fixed URL from the extension definition's snapshot
        let ext_elements = match &ext_def.snapshot {
            Some(s) => &s.element,
            None => continue,
        };

        let ext_url = ext_elements
            .iter()
            .find(|e| e.id == "Extension.url" || e.path == "Extension.url")
            .and_then(|e| e.fixed_uri.as_deref())
            .unwrap_or(profile_url);

        // Find the value type from Extension.value[x]
        let value_x_elem = ext_elements
            .iter()
            .find(|e| e.id == "Extension.value[x]" || e.path == "Extension.value[x]");

        // Determine if this is a complex extension (value[x] prohibited, uses nested extensions)
        let is_complex = value_x_elem
            .and_then(|e| e.max.as_deref())
            .map(|m| m == "0")
            .unwrap_or(false);

        let mut ext_entry = serde_json::json!({
            "url": ext_url
        });

        if is_complex {
            // Complex extension: generate nested sub-extensions
            let sub_ext_slices: Vec<&ElementDefinition> = ext_elements
                .iter()
                .filter(|e| {
                    e.slice_name.is_some()
                        && e.path == "Extension.extension"
                        && !e.type_.is_empty()
                        && e.type_[0].code == "Extension"
                })
                .collect();

            let mut sub_extensions: Vec<serde_json::Value> = Vec::new();

            for sub_slice in &sub_ext_slices {
                let min = sub_slice.min.unwrap_or(0);
                if min == 0 {
                    continue; // Optional sub-extension, skip
                }

                // Find the fixed URL for this sub-extension
                let sub_url = ext_elements
                    .iter()
                    .find(|e| e.id == format!("{}.url", sub_slice.id))
                    .and_then(|e| e.fixed_uri.as_deref())
                    .unwrap_or("");

                // Find the value[x] element for this sub-extension
                let sub_value_elem = ext_elements
                    .iter()
                    .find(|e| e.id == format!("{}.value[x]", sub_slice.id));

                let sub_value_type = sub_value_elem
                    .and_then(|e| e.type_.first())
                    .map(|t| t.code.as_str());

                let mut sub_entry = serde_json::json!({
                    "url": sub_url
                });

                if let (Some(vt), Some(_value_elem)) = (sub_value_type, sub_value_elem) {
                    let mut value = generate_type_value(vt);

                    // If the value type is CodeableConcept, check for fixed coding
                    if vt == "CodeableConcept" {
                        let fixed_coding = elements
                            .iter()
                            .find(|e| {
                                e.id == format!("{}.value[x].coding", sub_slice.id)
                                    || e.path
                                        == format!(
                                            "Extension.extension:{}",
                                            sub_slice.slice_name.as_deref().unwrap_or("")
                                        )
                            })
                            .and_then(|e| e.fixed_coding.as_ref())
                            .or_else(|| {
                                ext_elements
                                    .iter()
                                    .find(|e| e.id == format!("{}.value[x].coding", sub_slice.id))
                                    .and_then(|e| e.fixed_coding.as_ref())
                            });

                        if let Some(coding) = fixed_coding
                            && let Some(obj) = value.as_object_mut()
                        {
                            obj.insert("coding".to_string(), coding.clone());
                        }
                    }

                    let value_key = format!("value{}", capitalize_fhir_type(vt));
                    sub_entry[value_key] = value;
                }

                sub_extensions.push(sub_entry);
            }

            if !sub_extensions.is_empty() {
                ext_entry["extension"] = serde_json::json!(sub_extensions);
            }
        } else if let Some(value_elem) = value_x_elem {
            // Simple extension: generate a value
            if let Some(type_def) = value_elem.type_.first() {
                let vt = type_def.code.as_str();
                let value = generate_type_value(vt);
                let value_key = format!("value{}", capitalize_fhir_type(vt));
                ext_entry[value_key] = value;
            }
        }

        extensions.push(ext_entry);
    }

    if !extensions.is_empty() {
        resource["extension"] = serde_json::json!(extensions);
    }
}

/// Pass 4: Populate mustSupport BackboneElements with nested required fields.
fn populate_must_support_backbones(
    resource: &mut serde_json::Value,
    elements: &[ElementDefinition],
    base_type: &str,
    all_profiles: &[StructureDefinition],
    value_set_systems: &HashMap<String, String>,
) {
    for element in elements {
        if !element.must_support {
            continue;
        }
        if element.min.unwrap_or(0) > 0 {
            continue; // Already handled in pass 1
        }
        let is_backbone = element
            .type_
            .iter()
            .any(|t| t.code.contains("BackboneElement"));
        if !is_backbone {
            continue;
        }
        let field_path = get_field_path(&element.path, base_type);
        if field_path.is_none() || field_path.as_deref() == Some(base_type) {
            continue;
        }
        let path = field_path.unwrap();
        if path_has_value(resource, &path) {
            continue;
        }

        // Create a backbone with nested required fields
        let parent_path = &element.path;
        let mut backbone = serde_json::Map::new();
        populate_backbone_fields(
            &mut backbone,
            parent_path,
            elements,
            base_type,
            all_profiles,
            value_set_systems,
        );

        let max = element.max.as_deref().unwrap_or("1");
        if max != "1" || is_base_spec_repeatable(base_type, &path) {
            resource[&path] = serde_json::json!([backbone]);
        } else {
            resource[&path] = serde_json::json!(backbone);
        }
    }
}

/// Populate required and mustSupport sub-fields of a BackboneElement.
fn populate_backbone_fields(
    backbone: &mut serde_json::Map<String, serde_json::Value>,
    parent_path: &str,
    elements: &[ElementDefinition],
    base_type: &str,
    all_profiles: &[StructureDefinition],
    value_set_systems: &HashMap<String, String>,
) {
    // First pass: populate required children (min > 0)
    for element in elements {
        if !element.path.starts_with(&format!("{parent_path}.")) {
            continue;
        }

        let suffix = element
            .path
            .strip_prefix(&format!("{parent_path}."))
            .unwrap_or("");
        if suffix.contains('.') {
            continue; // Skip deeply nested paths in first pass
        }

        let field_name = suffix.split(':').next().unwrap_or(suffix);
        if backbone.contains_key(field_name) {
            continue;
        }

        let min = element.min.unwrap_or(0);
        if min == 0 {
            continue;
        }

        if element.type_.is_empty() {
            continue;
        }

        let type_def = &element.type_[0];
        let type_code = &type_def.code;

        if type_code == "Extension" {
            continue;
        }

        let mut value = generate_type_value(type_code);

        // For complex types, populate nested required fields
        if is_complex_type(type_code) {
            let child_path = format!("{parent_path}.{field_name}");
            populate_nested_required_fields(
                &mut value,
                &child_path,
                elements,
                all_profiles,
                value_set_systems,
            );
        }

        let max = element.max.as_deref().unwrap_or("1");
        if max != "1" || is_base_spec_repeatable(base_type, field_name) {
            backbone.insert(field_name.to_string(), serde_json::json!([value]));
        } else {
            backbone.insert(field_name.to_string(), value);
        }
    }

    // Second pass: populate mustSupport children with min=0
    for element in elements {
        if !element.path.starts_with(&format!("{parent_path}.")) {
            continue;
        }

        let suffix = element
            .path
            .strip_prefix(&format!("{parent_path}."))
            .unwrap_or("");
        if suffix.contains('.') {
            continue;
        }

        let field_name = suffix.split(':').next().unwrap_or(suffix);
        if backbone.contains_key(field_name) {
            continue;
        }

        if element.min.unwrap_or(0) != 0 {
            continue;
        }
        if !element.must_support {
            continue;
        }
        if element.type_.is_empty() {
            continue;
        }

        let type_def = &element.type_[0];
        let type_code = &type_def.code;

        if type_code == "Extension" {
            continue;
        }

        let mut value = generate_type_value(type_code);

        if is_complex_type(type_code) {
            let child_path = format!("{parent_path}.{field_name}");
            populate_nested_required_fields(
                &mut value,
                &child_path,
                elements,
                all_profiles,
                value_set_systems,
            );
        }

        let max = element.max.as_deref().unwrap_or("1");
        if max != "1" || is_base_spec_repeatable(base_type, field_name) {
            backbone.insert(field_name.to_string(), serde_json::json!([value]));
        } else {
            backbone.insert(field_name.to_string(), value);
        }
    }
}

/// Populate required sub-fields at depth 2 inside a complex type.
fn populate_nested_required_fields(
    value: &mut serde_json::Value,
    parent_path: &str,
    elements: &[ElementDefinition],
    _all_profiles: &[StructureDefinition],
    _value_set_systems: &HashMap<String, String>,
) {
    let obj = match value.as_object_mut() {
        Some(o) => o,
        None => return,
    };

    // First pass: populate required children (min > 0)
    for element in elements {
        if !element.path.starts_with(&format!("{parent_path}.")) {
            continue;
        }

        let suffix = element
            .path
            .strip_prefix(&format!("{parent_path}."))
            .unwrap_or("");
        if suffix.contains('.') {
            continue;
        }

        let field_name = suffix.split(':').next().unwrap_or(suffix);
        if obj.contains_key(field_name) {
            continue;
        }

        let min = element.min.unwrap_or(0);
        if min == 0 {
            continue;
        }

        if element.type_.is_empty() {
            continue;
        }

        let type_code = &element.type_[0].code;
        if type_code == "Extension" {
            continue;
        }

        let val = generate_type_value(type_code);
        let max = element.max.as_deref().unwrap_or("1");
        if max != "1" {
            obj.insert(field_name.to_string(), serde_json::json!([val]));
        } else {
            obj.insert(field_name.to_string(), val);
        }
    }

    // Second pass: populate mustSupport children with min=0
    for element in elements {
        if !element.path.starts_with(&format!("{parent_path}.")) {
            continue;
        }

        let suffix = element
            .path
            .strip_prefix(&format!("{parent_path}."))
            .unwrap_or("");
        if suffix.contains('.') {
            continue;
        }

        let field_name = suffix.split(':').next().unwrap_or(suffix);
        if obj.contains_key(field_name) {
            continue;
        }

        if element.min.unwrap_or(0) != 0 {
            continue;
        }
        if !element.must_support {
            continue;
        }
        if element.type_.is_empty() {
            continue;
        }

        let type_code = &element.type_[0].code;
        if type_code == "Extension" {
            continue;
        }

        let val = generate_type_value(type_code);
        let max = element.max.as_deref().unwrap_or("1");
        if max != "1" {
            obj.insert(field_name.to_string(), serde_json::json!([val]));
        } else {
            obj.insert(field_name.to_string(), val);
        }
    }
}

/// Pass 5: Populate mustSupport optional fields (non-backbone).
fn populate_must_support_optional_fields(
    resource: &mut serde_json::Value,
    elements: &[ElementDefinition],
    base_type: &str,
    all_profiles: &[StructureDefinition],
    value_set_systems: &HashMap<String, String>,
) {
    for element in elements {
        if !element.must_support {
            continue;
        }
        if element.min.unwrap_or(0) > 0 {
            continue;
        }
        let is_backbone = element
            .type_
            .iter()
            .any(|t| t.code.contains("BackboneElement"));
        if is_backbone {
            continue;
        }
        let field_path = get_field_path(&element.path, base_type);
        if field_path.is_none() || field_path.as_deref() == Some(base_type) {
            continue;
        }
        let path = field_path.unwrap();
        if path_has_value(resource, &path) {
            continue;
        }

        // For complex types, populate nested required fields
        if let Some(type_def) = element.type_.first()
            && is_complex_type(&type_def.code)
        {
            let mut value = generate_type_value(&type_def.code);
            let child_path = format!("{base_type}.{path}");
            populate_nested_required_fields(
                &mut value,
                &child_path,
                elements,
                all_profiles,
                value_set_systems,
            );

            // Apply identifier constraints
            if type_def.code == "Identifier" {
                apply_identifier_constraints(&mut value, type_def, all_profiles);
            }

            let max = element.max.as_deref().unwrap_or("1");
            if max != "1" || is_base_spec_repeatable(base_type, &path) {
                resource[&path] = serde_json::json!([value]);
            } else {
                resource[&path] = value;
            }
        } else {
            let _ = set_field_value(resource, &path, element, all_profiles, value_set_systems);
        }
    }
}

/// Set a field value based on element type constraints.
fn set_field_value(
    resource: &mut serde_json::Value,
    path: &str,
    element: &ElementDefinition,
    all_profiles: &[StructureDefinition],
    value_set_systems: &HashMap<String, String>,
) -> Result<()> {
    // Check for fixed/pattern values first
    if let Some(val) = &element.fixed_string {
        set_json_path(resource, path, serde_json::json!(val));
        return Ok(());
    }
    if let Some(val) = &element.fixed_uri {
        set_json_path(resource, path, serde_json::json!(val));
        return Ok(());
    }
    if let Some(val) = &element.fixed_code {
        set_json_path(resource, path, serde_json::json!(val));
        return Ok(());
    }
    if let Some(val) = &element.fixed_boolean {
        set_json_path(resource, path, serde_json::json!(val));
        return Ok(());
    }
    if let Some(val) = &element.fixed_integer {
        set_json_path(resource, path, serde_json::json!(val));
        return Ok(());
    }
    if let Some(val) = &element.pattern_string {
        set_json_path(resource, path, serde_json::json!(val));
        return Ok(());
    }
    if let Some(val) = &element.pattern_uri {
        set_json_path(resource, path, serde_json::json!(val));
        return Ok(());
    }
    if let Some(val) = &element.pattern_code {
        set_json_path(resource, path, serde_json::json!(val));
        return Ok(());
    }
    if let Some(val) = &element.pattern_coding {
        set_json_path(resource, path, val.clone());
        return Ok(());
    }
    if let Some(val) = &element.pattern_codeable_concept {
        set_json_path(resource, path, val.clone());
        return Ok(());
    }

    // Generate a value based on the type
    let type_code = element
        .type_
        .first()
        .map(|t| t.code.as_str())
        .unwrap_or("string");

    let mut value = generate_type_value(type_code);

    // Apply identifier profile constraints
    if type_code == "Identifier"
        && let Some(type_def) = element.type_.first()
    {
        apply_identifier_constraints(&mut value, type_def, all_profiles);
    }

    // For complex types, populate nested required fields
    if is_complex_type(type_code) {
        let child_path = element.path.clone();
        populate_nested_required_fields(
            &mut value,
            &child_path,
            elements_for_path(element, resource),
            all_profiles,
            value_set_systems,
        );
    }

    set_json_path(resource, path, value);
    Ok(())
}

/// Apply Identifier profile constraints (system, type) to a value.
fn apply_identifier_constraints(
    value: &mut serde_json::Value,
    type_def: &ElementDefinitionType,
    all_profiles: &[StructureDefinition],
) {
    if let Some(obj) = value.as_object_mut() {
        for profile_url in type_def
            .profile
            .iter()
            .chain(type_def.target_profile.iter())
        {
            if (obj.get("system").is_none()
                || obj
                    .get("system")
                    .and_then(|v| v.as_str())
                    .is_some_and(is_generic_identifier_system))
                && let Some(system) = find_identifier_system(profile_url, all_profiles)
            {
                obj.insert("system".to_string(), serde_json::json!(system));
            }

            if !obj.contains_key("type")
                && let Some(identifier_type) = find_identifier_type(profile_url, all_profiles)
            {
                obj.insert("type".to_string(), identifier_type);
            }

            if obj.contains_key("system") && obj.contains_key("type") {
                break;
            }
        }
    }
}

/// Find the fixed/pattern system URI for an Identifier profile.
fn find_identifier_system(
    profile_url: &str,
    all_profiles: &[StructureDefinition],
) -> Option<String> {
    let clean_url = profile_url.split('|').next().unwrap_or(profile_url);
    let profile = all_profiles.iter().find(|p| p.url == clean_url)?;

    let elements = match (&profile.snapshot, &profile.differential) {
        (Some(snapshot), _) => &snapshot.element,
        (None, Some(diff)) => &diff.element,
        _ => return None,
    };

    for el in elements {
        if el.id.ends_with(".system") || el.path.ends_with(".system") {
            if let Some(v) = &el.fixed_uri {
                return Some(v.clone());
            }
            if let Some(v) = &el.pattern_uri {
                return Some(v.clone());
            }
        }
    }
    None
}

/// Find the fixed/pattern CodeableConcept type for an Identifier profile.
fn find_identifier_type(
    profile_url: &str,
    all_profiles: &[StructureDefinition],
) -> Option<serde_json::Value> {
    let clean_url = profile_url.split('|').next().unwrap_or(profile_url);
    let profile = all_profiles.iter().find(|p| p.url == clean_url)?;

    let elements = match (&profile.snapshot, &profile.differential) {
        (Some(snapshot), _) => &snapshot.element,
        (None, Some(diff)) => &diff.element,
        _ => return None,
    };

    for el in elements {
        if el.id.ends_with(".type") || el.path.ends_with(".type") {
            if let Some(v) = &el.pattern_codeable_concept {
                return Some(v.clone());
            }
            if let Some(v) = &el.fixed_codeable_concept {
                return Some(v.clone());
            }
        }
    }
    None
}

/// Find the fixed/pattern system URI for a named slice.
fn find_slice_system(slice_name: &str, elements: &[ElementDefinition]) -> Option<String> {
    for el in elements {
        let matches_slice = el.path.contains(&format!(":{slice_name}"))
            || el.id.contains(&format!(":{slice_name}"));

        if !matches_slice {
            continue;
        }

        if el.id.ends_with(".system") || el.path.ends_with(".system") {
            if let Some(v) = &el.fixed_uri {
                return Some(v.clone());
            }
            if let Some(v) = &el.pattern_uri {
                return Some(v.clone());
            }
        }
    }
    None
}

/// Find the fixed use code for a HumanName slice.
fn find_human_name_use(slice_name: &str, elements: &[ElementDefinition]) -> Option<String> {
    for el in elements {
        let matches_slice = el.id.contains(&format!(":{slice_name}"))
            || el.path.contains(&format!(":{slice_name}"));

        if !matches_slice {
            continue;
        }

        if el.path.ends_with(".use") || el.id.ends_with(".use") {
            if let Some(v) = &el.fixed_code {
                return Some(v.clone());
            }
            if let Some(v) = &el.pattern_code {
                return Some(v.clone());
            }
        }
    }
    None
}

/// Check if a system string is a generic placeholder identifier system.
fn is_generic_identifier_system(system: &str) -> bool {
    matches!(
        system,
        "http://example.org/id" | "http://example.org/identifier" | "urn:ietf:rfc:3986"
    )
}

/// Generate a value for a FHIR type.
fn generate_type_value(type_code: &str) -> serde_json::Value {
    match type_code {
        "string" | "code" | "uri" | "oid" | "uuid" | "markdown" | "id" | "xhtml" => {
            serde_json::json!("generated-value")
        }
        "boolean" => serde_json::json!(true),
        "integer" | "unsignedInt" | "positiveInt" => serde_json::json!(0),
        "decimal" => serde_json::json!(0.0),
        "date" => serde_json::json!("2024-01-01"),
        "dateTime" | "instant" => serde_json::json!("2024-01-01T00:00:00Z"),
        "time" => serde_json::json!("00:00:00"),
        "base64Binary" => serde_json::json!(""),
        "HumanName" => {
            serde_json::json!([{"family": "GeneratedFamily", "given": ["GeneratedGiven"]}])
        }
        "Address" => {
            serde_json::json!([{"line": ["123 Generated St"], "city": "GeneratedCity", "state": "Gen", "postalCode": "0000"}])
        }
        "Identifier" => {
            serde_json::json!([{"system": "http://example.org/id", "value": "gen-001"}])
        }
        "CodeableConcept" => {
            serde_json::json!({"coding": [{"system": "http://example.org/code", "code": "gen"}], "text": "generated"})
        }
        "Coding" => serde_json::json!({"system": "http://example.org/code", "code": "gen"}),
        "ContactPoint" => {
            serde_json::json!([{"system": "phone", "value": "0400000000", "use": "mobile"}])
        }
        "Period" => serde_json::json!({"start": "2024-01-01", "end": "2024-12-31"}),
        "Quantity" => {
            serde_json::json!({"value": 1, "unit": "1", "system": "http://unitsofmeasure.org", "code": "1"})
        }
        "Range" => serde_json::json!({"low": {"value": 0}, "high": {"value": 1}}),
        "Ratio" => serde_json::json!({"numerator": {"value": 1}, "denominator": {"value": 1}}),
        "Reference" => serde_json::json!({"reference": "Unknown/placeholder"}),
        "Meta" => serde_json::json!({"versionId": "1", "lastUpdated": "2024-01-01T00:00:00Z"}),
        "Narrative" => serde_json::json!({"status": "generated", "div": "<div>generated</div>"}),
        "BackboneElement" => serde_json::json!({}),
        _ => serde_json::json!("generated-value"),
    }
}

/// Capitalize the first letter of a FHIR type code.
fn capitalize_fhir_type(type_code: &str) -> String {
    let mut chars = type_code.chars();
    match chars.next() {
        None => String::new(),
        Some(first) => first.to_uppercase().collect::<String>() + chars.as_str(),
    }
}

/// Returns true for FHIR complex types that can have sub-fields.
fn is_complex_type(type_code: &str) -> bool {
    matches!(
        type_code,
        "Identifier"
            | "HumanName"
            | "Address"
            | "ContactPoint"
            | "CodeableConcept"
            | "Coding"
            | "Quantity"
            | "Reference"
            | "Period"
            | "Attachment"
            | "Annotation"
            | "Range"
            | "Ratio"
            | "Timing"
            | "SampledData"
            | "BackboneElement"
    )
}

/// Returns true for fields that are 0..* in the FHIR R4 base spec.
fn is_base_spec_repeatable(resource_type: &str, field_name: &str) -> bool {
    // Fields that are always 0..* regardless of resource type
    if matches!(
        field_name,
        "identifier"
            | "telecom"
            | "extension"
            | "contained"
            | "contact"
            | "qualification"
            | "location"
            | "healthcareService"
            | "endpoint"
            | "alias"
            | "type"
            | "specialty"
            | "availableTime"
            | "notAvailable"
            | "communication"
            | "category"
            | "language"
            | "referralMethod"
            | "practiceSetting"
            | "coverageArea"
            | "serviceType"
            | "eligibility"
            | "program"
            | "characteristic"
            | "annotation"
            | "note"
            | "photo"
            | "review"
            | "usage"
            | "coverage"
            | "plan"
            | "guarantor"
            | "network"
            | "resource"
            | "entry"
            | "link"
            | "outcome"
            | "issue"
            | "coding"
            | "given"
            | "line"
    ) {
        return true;
    }

    // Fields that are 0..* only for specific resource types
    match (resource_type, field_name) {
        ("Patient" | "Person" | "Practitioner" | "RelatedPerson", "name") => true,
        ("Organization" | "HealthcareService" | "Location", "name") => false,
        (
            "Organization" | "Practitioner" | "Patient" | "Person" | "RelatedPerson"
            | "PractitionerRole",
            "address",
        ) => true,
        ("Location", "address") => false,
        ("PractitionerRole", "code") => true,
        ("HealthcareService", "code") => true,
        ("Provenance", "target") => true,
        ("Provenance", "agent") => true,
        _ => false,
    }
}

/// Set a value at a dotted JSON path.
fn set_json_path(resource: &mut serde_json::Value, path: &str, value: serde_json::Value) {
    let parts: Vec<&str> = path.split('.').collect();
    let mut current = resource;
    for (i, part) in parts.iter().enumerate() {
        if i == parts.len() - 1 {
            current[part] = value.clone();
        } else {
            if !current.get(*part).is_some_and(|v| v.is_object()) {
                current[part] = serde_json::json!({});
            }
            current = current.get_mut(part).unwrap();
        }
    }
}

/// Get a value at a dotted JSON path.
fn get_json_path<'a>(resource: &'a serde_json::Value, path: &str) -> Option<&'a serde_json::Value> {
    let parts: Vec<&str> = path.split('.').collect();
    let mut current = resource;
    for part in &parts {
        current = current.get(*part)?;
    }
    Some(current)
}

/// Check if a dotted JSON path has a value.
fn path_has_value(resource: &serde_json::Value, path: &str) -> bool {
    let parts: Vec<&str> = path.split('.').collect();
    let mut current = resource;
    for part in &parts {
        match current.get(*part) {
            Some(v) => current = v,
            None => return false,
        }
    }
    true
}

/// Extract the field path from a FHIR path like "Patient.name.family" → "name.family".
fn get_field_path(path: &str, resource_type: &str) -> Option<String> {
    if !path.starts_with(resource_type) {
        return None;
    }
    let remainder = path.strip_prefix(resource_type)?;
    if remainder.is_empty() {
        return Some(resource_type.to_string());
    }
    if !remainder.starts_with('.') {
        return None;
    }
    let field_part = remainder.strip_prefix('.')?;
    let field_name = if let Some((first, rest)) = field_part.split_once('.') {
        let first_clean = first.split(':').next().unwrap_or(first);
        format!("{first_clean}.{rest}")
    } else {
        field_part
            .split(':')
            .next()
            .unwrap_or(field_part)
            .to_string()
    };
    Some(field_name)
}

/// Get the full elements list (needed for nested field population).
/// This is a helper that returns the elements from the resource's profile context.
/// In practice, the caller passes elements directly.
fn elements_for_path<'a>(
    _element: &ElementDefinition,
    _resource: &serde_json::Value,
) -> &'a [ElementDefinition] {
    // This is a placeholder — the actual elements are passed by the caller.
    // The function exists to maintain the same interface as fhir-autotest.
    &[]
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    fn test_profile() -> StructureDefinition {
        StructureDefinition {
            resource_type: "StructureDefinition".to_string(),
            url: "http://example.org/TestPatient".to_string(),
            base_type: "Patient".to_string(),
            name: "TestPatient".to_string(),
            kind: "resource".to_string(),
            derivation: Some("constraint".to_string()),
            base_definition: None,
            snapshot: Some(Snapshot {
                element: vec![
                    ElementDefinition {
                        id: "Patient".to_string(),
                        path: "Patient".to_string(),
                        min: Some(0),
                        max: Some("*".to_string()),
                        type_: vec![],
                        ..Default::default()
                    },
                    ElementDefinition {
                        id: "Patient.name".to_string(),
                        path: "Patient.name".to_string(),
                        min: Some(1),
                        max: Some("*".to_string()),
                        type_: vec![ElementDefinitionType {
                            code: "HumanName".to_string(),
                            target_profile: vec![],
                            profile: vec![],
                            versioning: None,
                        }],
                        ..Default::default()
                    },
                    ElementDefinition {
                        id: "Patient.gender".to_string(),
                        path: "Patient.gender".to_string(),
                        min: Some(1),
                        max: Some("1".to_string()),
                        type_: vec![ElementDefinitionType {
                            code: "code".to_string(),
                            target_profile: vec![],
                            profile: vec![],
                            versioning: None,
                        }],
                        fixed_code: Some("male".to_string()),
                        ..Default::default()
                    },
                ],
            }),
            differential: None,
        }
    }

    #[test]
    fn generate_basic_resource() {
        let profile = test_profile();
        let resource = generate_resource(&profile, &[]).unwrap();
        assert_eq!(resource["resourceType"], "Patient");
        assert!(resource.get("name").is_some());
        assert_eq!(resource["gender"], "male");
        assert!(
            resource["meta"]["profile"][0]
                .as_str()
                .unwrap()
                .contains("TestPatient")
        );
    }

    #[test]
    fn generate_without_snapshot() {
        let profile = StructureDefinition {
            snapshot: None,
            ..test_profile()
        };
        let result = generate_resource(&profile, &[]);
        assert!(result.is_ok());
    }

    #[test]
    fn test_set_json_path() {
        let mut resource = json!({});
        set_json_path(&mut resource, "name.family", json!("Smith"));
        assert_eq!(resource["name"]["family"], "Smith");
    }

    #[test]
    fn test_path_has_value() {
        let resource = json!({"name": {"family": "Smith"}});
        assert!(path_has_value(&resource, "name.family"));
        assert!(!path_has_value(&resource, "name.given"));
    }

    #[test]
    fn test_generate_type_value() {
        assert_eq!(generate_type_value("string"), json!("generated-value"));
        assert_eq!(generate_type_value("boolean"), json!(true));
        assert_eq!(generate_type_value("date"), json!("2024-01-01"));
        assert!(generate_type_value("HumanName").is_array());
        assert!(generate_type_value("Address").is_array());
    }

    #[test]
    fn test_is_base_spec_repeatable() {
        assert!(is_base_spec_repeatable("Patient", "identifier"));
        assert!(is_base_spec_repeatable("Patient", "name"));
        assert!(!is_base_spec_repeatable("Organization", "name"));
        assert!(is_base_spec_repeatable("Practitioner", "name"));
        assert!(is_base_spec_repeatable("Patient", "address"));
        assert!(!is_base_spec_repeatable("Location", "address"));
    }

    #[test]
    fn test_is_complex_type() {
        assert!(is_complex_type("Identifier"));
        assert!(is_complex_type("HumanName"));
        assert!(is_complex_type("CodeableConcept"));
        assert!(is_complex_type("BackboneElement"));
        assert!(!is_complex_type("string"));
        assert!(!is_complex_type("boolean"));
    }

    #[test]
    fn test_capitalize_fhir_type() {
        assert_eq!(capitalize_fhir_type("string"), "String");
        assert_eq!(capitalize_fhir_type("boolean"), "Boolean");
        assert_eq!(capitalize_fhir_type("Period"), "Period");
    }

    #[test]
    fn test_is_generic_identifier_system() {
        assert!(is_generic_identifier_system("http://example.org/id"));
        assert!(is_generic_identifier_system("urn:ietf:rfc:3986"));
        assert!(!is_generic_identifier_system(
            "http://ns.electronichealth.net.au/id/hpio"
        ));
    }

    #[test]
    fn snapshot_generated_resource() {
        let profile = test_profile();
        let resource = generate_resource(&profile, &[]).unwrap();
        insta::assert_json_snapshot!(resource);
    }

    #[test]
    fn test_get_field_path() {
        assert_eq!(
            get_field_path("Patient.name", "Patient"),
            Some("name".to_string())
        );
        assert_eq!(
            get_field_path("Patient.name.family", "Patient"),
            Some("name.family".to_string())
        );
        assert_eq!(
            get_field_path("Patient.name:official", "Patient"),
            Some("name".to_string())
        );
        assert_eq!(
            get_field_path("Observation.valueString", "Observation"),
            Some("valueString".to_string())
        );
        assert_eq!(
            get_field_path("Patient", "Patient"),
            Some("Patient".to_string())
        );
        assert!(get_field_path("Organization.name", "Patient").is_none());
    }
}
