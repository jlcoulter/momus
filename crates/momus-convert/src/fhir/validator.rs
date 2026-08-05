#![allow(dead_code)]

//! FHIR profile validator.
//!
//! Validates a FHIR resource against a StructureDefinition profile.
//! Ported from fhir-autotest's validator.rs.

use super::assertions::resolve_json_path;
use super::profile::*;

/// Validate a FHIR resource against a StructureDefinition profile.
///
/// Checks:
/// - resourceType matches the profile's base type
/// - Required elements (min > 0) are present
/// - Fixed/pattern values match
pub fn validate_against_profile(
    resource: &serde_json::Value,
    profile: &StructureDefinition,
) -> Vec<String> {
    let mut errors = Vec::new();

    // Check resourceType
    if let Some(rt) = resource.get("resourceType").and_then(|v| v.as_str()) {
        if rt != profile.base_type {
            errors.push(format!(
                "resourceType is '{}', expected '{}'",
                rt, profile.base_type
            ));
        }
    } else {
        errors.push("Missing resourceType".to_string());
    }

    let elements = match &profile.snapshot {
        Some(snapshot) => &snapshot.element,
        None => match &profile.differential {
            Some(diff) => &diff.element,
            None => return errors,
        },
    };

    for element in elements {
        let field_path = match get_field_path(&element.path, &profile.base_type) {
            Some(name) => name,
            None => continue,
        };

        if field_path == profile.base_type {
            continue;
        }

        let resolved_value = resolve_json_path(resource, &field_path);

        // Check required elements
        if element.min.unwrap_or(0) > 0 && resolved_value.is_none() {
            errors.push(format!(
                "Missing required element: {} (min={})",
                element.path,
                element.min.unwrap_or(0)
            ));
        }

        // Check fixed values
        if let Some(fixed) = &element.fixed_string
            && let Some(val) = resolved_value.as_ref().and_then(|v| v.as_str())
            && val != fixed
        {
            errors.push(format!(
                "{}: expected '{}', got '{}'",
                element.path, fixed, val
            ));
        }
        if let Some(fixed) = &element.fixed_code
            && let Some(val) = resolved_value.as_ref().and_then(|v| v.as_str())
            && val != fixed
        {
            errors.push(format!(
                "{}: expected code '{}', got '{}'",
                element.path, fixed, val
            ));
        }
        if let Some(fixed) = &element.fixed_uri
            && let Some(val) = resolved_value.as_ref().and_then(|v| v.as_str())
            && val != fixed
        {
            errors.push(format!(
                "{}: expected uri '{}', got '{}'",
                element.path, fixed, val
            ));
        }
        if let Some(fixed) = &element.fixed_boolean
            && let Some(val) = resolved_value.as_ref().and_then(|v| v.as_bool())
            && val != *fixed
        {
            errors.push(format!("{}: expected {}, got {}", element.path, fixed, val));
        }
        if let Some(fixed) = &element.fixed_integer
            && let Some(val) = resolved_value.as_ref().and_then(|v| v.as_i64())
            && val != *fixed as i64
        {
            errors.push(format!("{}: expected {}, got {}", element.path, fixed, val));
        }

        // Check pattern values
        if let Some(pattern) = &element.pattern_string
            && let Some(val) = resolved_value.as_ref().and_then(|v| v.as_str())
            && val != pattern
        {
            errors.push(format!(
                "{}: pattern expected '{}', got '{}'",
                element.path, pattern, val
            ));
        }
        if let Some(pattern) = &element.pattern_code
            && let Some(val) = resolved_value.as_ref().and_then(|v| v.as_str())
            && val != pattern
        {
            errors.push(format!(
                "{}: pattern code expected '{}', got '{}'",
                element.path, pattern, val
            ));
        }
    }

    errors
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

    // Strip slice notation (e.g., "identifier:type" → "identifier")
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

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    fn test_patient_profile() -> StructureDefinition {
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
                        fixed_string: None,
                        fixed_uri: None,
                        fixed_code: None,
                        fixed_boolean: None,
                        fixed_integer: None,
                        fixed_decimal: None,
                        pattern_string: None,
                        pattern_uri: None,
                        pattern_code: None,
                        pattern_boolean: None,
                        must_support: false,
                        short: None,
                        definition: None,
                        binding: None,
                        content_reference: None,
                        fixed_quantity: None,
                        pattern_quantity: None,
                        fixed_coding: None,
                        pattern_coding: None,
                        fixed_codeable_concept: None,
                        pattern_codeable_concept: None,
                        constraint: vec![],
                        is_modifier: false,
                        is_summary: false,
                        slice_name: None,
                        slicing: None,
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
                        fixed_string: None,
                        fixed_uri: None,
                        fixed_code: None,
                        fixed_boolean: None,
                        fixed_integer: None,
                        fixed_decimal: None,
                        pattern_string: None,
                        pattern_uri: None,
                        pattern_code: None,
                        pattern_boolean: None,
                        must_support: true,
                        short: None,
                        definition: None,
                        binding: None,
                        content_reference: None,
                        fixed_quantity: None,
                        pattern_quantity: None,
                        fixed_coding: None,
                        pattern_coding: None,
                        fixed_codeable_concept: None,
                        pattern_codeable_concept: None,
                        constraint: vec![],
                        is_modifier: false,
                        is_summary: false,
                        slice_name: None,
                        slicing: None,
                    },
                ],
            }),
            differential: None,
        }
    }

    #[test]
    fn validate_valid_resource() {
        let profile = test_patient_profile();
        let resource = json!({
            "resourceType": "Patient",
            "name": [{"family": "Test", "given": ["Patient"]}]
        });
        let errors = validate_against_profile(&resource, &profile);
        assert!(errors.is_empty(), "Expected no errors, got: {errors:?}");
    }

    #[test]
    fn validate_wrong_resource_type() {
        let profile = test_patient_profile();
        let resource = json!({"resourceType": "Observation"});
        let errors = validate_against_profile(&resource, &profile);
        assert!(errors.iter().any(|e| e.contains("resourceType")));
    }

    #[test]
    fn validate_missing_required_field() {
        let profile = test_patient_profile();
        let resource = json!({"resourceType": "Patient"});
        let errors = validate_against_profile(&resource, &profile);
        assert!(
            errors
                .iter()
                .any(|e| e.contains("name") && e.contains("Missing required"))
        );
    }

    #[test]
    fn validate_fixed_value_mismatch() {
        let mut profile = test_patient_profile();
        if let Some(ref mut snapshot) = profile.snapshot {
            snapshot.element.push(ElementDefinition {
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
            });
        }
        let resource = json!({
            "resourceType": "Patient",
            "name": [{"family": "Test"}],
            "gender": "female"
        });
        let errors = validate_against_profile(&resource, &profile);
        assert!(
            errors
                .iter()
                .any(|e| e.contains("gender") && e.contains("expected code 'male'"))
        );
    }
}
