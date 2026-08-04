#![allow(dead_code)]

//! FHIR resource generator.
//!
//! Generates synthetic FHIR resources that conform to StructureDefinition profiles.
//! Ported from fhir-autotest's resource_generator module.
//!
//! The generation follows a 5-pass approach:
//! 1. Required fields (min > 0)
//! 2. Required slices
//! 3. Extension slices
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
    populate_required_fields(&mut resource, elements, &profile.base_type, all_profiles, value_set_systems)?;

    // Pass 2: Required slices
    populate_required_slices(&mut resource, elements, &profile.base_type, all_profiles, value_set_systems)?;

    // Pass 3: Extension slices
    populate_extension_slices(&mut resource, elements, &profile.base_type, all_profiles, value_set_systems);

    // Pass 4: MustSupport backbones
    populate_must_support_backbones(&mut resource, elements, &profile.base_type, all_profiles, value_set_systems);

    // Pass 5: MustSupport optional fields
    populate_must_support_optional_fields(&mut resource, elements, &profile.base_type, all_profiles, value_set_systems);

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
        set_field_value(resource, &path, &element, all_profiles, value_set_systems)?;
    }
    Ok(())
}

/// Pass 2: Populate required slices.
fn populate_required_slices(
    resource: &mut serde_json::Value,
    elements: &[ElementDefinition],
    base_type: &str,
    all_profiles: &[StructureDefinition],
    value_set_systems: &HashMap<String, String>,
) -> Result<()> {
    for element in elements {
        if element.slice_name.is_none() {
            continue;
        }
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

/// Pass 3: Populate extension slices.
fn populate_extension_slices(
    resource: &mut serde_json::Value,
    elements: &[ElementDefinition],
    base_type: &str,
    _all_profiles: &[StructureDefinition],
    _value_set_systems: &HashMap<String, String>,
) {
    for element in elements {
        if !element.path.ends_with(".extension") {
            continue;
        }
        if element.slice_name.is_none() {
            continue;
        }
        let field_path = get_field_path(&element.path, base_type);
        if field_path.is_none() {
            continue;
        }
        let path = field_path.unwrap();
        if path_has_value(resource, &path) {
            continue;
        }
        // Create a simple extension with the slice name
        let ext = serde_json::json!({
            "url": format!("http://example.org/Extension/{}", element.slice_name.as_ref().unwrap()),
            "valueString": format!("generated-{}", element.slice_name.as_ref().unwrap())
        });
        set_json_path(resource, &path, ext);
    }
}

/// Pass 4: Populate mustSupport BackboneElements.
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
        let is_backbone = element.type_.iter().any(|t| t.code.contains("BackboneElement"));
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
        // Create a minimal backbone element
        let _ = set_field_value(resource, &path, element, all_profiles, value_set_systems);
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
        let is_backbone = element.type_.iter().any(|t| t.code.contains("BackboneElement"));
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
        let _ = set_field_value(resource, &path, element, all_profiles, value_set_systems);
    }
}

/// Set a field value based on element type constraints.
fn set_field_value(
    resource: &mut serde_json::Value,
    path: &str,
    element: &ElementDefinition,
    _all_profiles: &[StructureDefinition],
    _value_set_systems: &HashMap<String, String>,
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

    // Generate a value based on the type
    let type_code = element.type_.first().map(|t| t.code.as_str()).unwrap_or("string");
    let value = generate_type_value(type_code);
    set_json_path(resource, path, value);
    Ok(())
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
        "HumanName" => serde_json::json!([{"family": "GeneratedFamily", "given": ["GeneratedGiven"]}]),
        "Address" => serde_json::json!([{"line": ["123 Generated St"], "city": "GeneratedCity", "state": "Gen", "postalCode": "0000"}]),
        "Identifier" => serde_json::json!([{"system": "http://example.org/id", "value": "gen-001"}]),
        "CodeableConcept" => serde_json::json!({"coding": [{"system": "http://example.org/code", "code": "gen"}], "text": "generated"}),
        "Coding" => serde_json::json!({"system": "http://example.org/code", "code": "gen"}),
        "ContactPoint" => serde_json::json!([{"system": "phone", "value": "0400000000", "use": "mobile"}]),
        "Period" => serde_json::json!({"start": "2024-01-01", "end": "2024-12-31"}),
        "Quantity" => serde_json::json!({"value": 1, "unit": "1", "system": "http://unitsofmeasure.org", "code": "1"}),
        "Range" => serde_json::json!({"low": {"value": 0}, "high": {"value": 1}}),
        "Ratio" => serde_json::json!({"numerator": {"value": 1}, "denominator": {"value": 1}}),
        "Reference" => serde_json::json!({"reference": "Unknown/placeholder"}),
        "Meta" => serde_json::json!({"versionId": "1", "lastUpdated": "2024-01-01T00:00:00Z"}),
        "Narrative" => serde_json::json!({"status": "generated", "div": "<div>generated</div>"}),
        _ => serde_json::json!("generated-value"),
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
        format!("{}.{}", first_clean, rest)
    } else {
        field_part.split(':').next().unwrap_or(field_part).to_string()
    };
    Some(field_name)
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
                        min: Some(0), max: Some("*".to_string()),
                        type_: vec![],
                        ..Default::default()
                    },
                    ElementDefinition {
                        id: "Patient.name".to_string(),
                        path: "Patient.name".to_string(),
                        min: Some(1), max: Some("*".to_string()),
                        type_: vec![ElementDefinitionType {
                            code: "HumanName".to_string(),
                            target_profile: vec![], profile: vec![], versioning: None,
                        }],
                        ..Default::default()
                    },
                    ElementDefinition {
                        id: "Patient.gender".to_string(),
                        path: "Patient.gender".to_string(),
                        min: Some(1), max: Some("1".to_string()),
                        type_: vec![ElementDefinitionType {
                            code: "code".to_string(),
                            target_profile: vec![], profile: vec![], versioning: None,
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
        assert!(resource["meta"]["profile"][0].as_str().unwrap().contains("TestPatient"));
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
}
