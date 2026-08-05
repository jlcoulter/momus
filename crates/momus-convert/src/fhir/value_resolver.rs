//! FHIR field value extraction and search parameter resolution.
//!
//! Extracts searchable field values from generated FHIR resources and
//! resolves search parameters to concrete values for use in test URLs.
//! Ported from fhir-autotest's `generate::value_resolver` module.

use super::search_param::SearchParameter;
use serde_json::Value;
use std::collections::HashMap;

/// Extract searchable field values from a FHIR resource JSON.
/// Returns a map from "ResourceType.field_path" to the string value
/// suitable for use in search parameters.
pub fn extract_field_values(resource_type: &str, resource: &Value) -> HashMap<String, String> {
    let mut values = HashMap::new();

    if let Some(obj) = resource.as_object() {
        extract_paths(resource_type, obj, &mut values, "");
    }

    values
}

fn extract_paths(
    resource_type: &str,
    obj: &serde_json::Map<String, Value>,
    values: &mut HashMap<String, String>,
    prefix: &str,
) {
    for (key, val) in obj {
        let path = if prefix.is_empty() {
            format!("{}.{}", resource_type, key)
        } else {
            format!("{}.{}", prefix, key)
        };

        match val {
            Value::String(s) => {
                // Only index non-empty strings, skip UUIDs
                if !s.is_empty() && !s.starts_with("urn:uuid:") {
                    values.insert(path.clone(), s.clone());
                }
            }
            Value::Number(n) => {
                values.insert(path.clone(), n.to_string());
            }
            Value::Bool(b) => {
                values.insert(path.clone(), b.to_string());
            }
            Value::Object(inner) => {
                extract_paths(resource_type, inner, values, &path);
            }
            Value::Array(arr) => {
                for (i, item) in arr.iter().enumerate() {
                    let arr_path = format!("{}[{}]", path, i);
                    match item {
                        Value::String(s) => {
                            if !s.is_empty() && !s.starts_with("urn:uuid:") {
                                values.insert(arr_path.clone(), s.clone());
                            }
                        }
                        Value::Number(n) => {
                            values.insert(arr_path.clone(), n.to_string());
                        }
                        Value::Bool(b) => {
                            values.insert(arr_path.clone(), b.to_string());
                        }
                        Value::Object(inner) => {
                            extract_paths(resource_type, inner, values, &arr_path);
                        }
                        _ => {}
                    }
                }
            }
            _ => {}
        }
    }
}

/// Map a FHIR search parameter name + type to the likely field path in a resource.
/// Returns a list of possible paths to check.
pub fn search_param_to_field_paths(
    resource_type: &str,
    param_name: &str,
    param_type: &str,
) -> Vec<String> {
    match (resource_type, param_name) {
        // Common FHIR R4 search param → field mappings
        (_, "name") => vec![
            format!("{}.name", resource_type),
            format!("{}.name[0].family", resource_type),
            format!("{}.name[0].given[0]", resource_type),
        ],
        (_, "family") => vec![format!("{}.name[0].family", resource_type)],
        (_, "given") => vec![format!("{}.name[0].given[0]", resource_type)],
        (_, "identifier") => vec![format!("{}.identifier[0].value", resource_type)],
        (_, "birthdate") => vec![format!("{}.birthDate", resource_type)],
        (_, "gender") => vec![format!("{}.gender", resource_type)],
        (_, "active") => vec![format!("{}.active", resource_type)],
        (_, "status") => vec![format!("{}.status", resource_type)],
        (_, "telecom") => vec![format!("{}.telecom[0].value", resource_type)],
        (_, "phone") => vec![format!("{}.telecom[0].value", resource_type)],
        (_, "email") => vec![format!("{}.telecom[0].value", resource_type)],
        (_, "address") => vec![
            format!("{}.address[0].line[0]", resource_type),
            format!("{}.address[0].city", resource_type),
            format!("{}.address[0].postalCode", resource_type),
        ],
        (_, "city") => vec![format!("{}.address[0].city", resource_type)],
        (_, "state") => vec![format!("{}.address[0].state", resource_type)],
        (_, "postalCode") => vec![format!("{}.address[0].postalCode", resource_type)],
        (_, "country") => vec![format!("{}.address[0].country", resource_type)],
        (_, "type") => vec![format!("{}.type[0].coding[0].code", resource_type)],
        (_, "code") => vec![format!("{}.code.coding[0].code", resource_type)],
        (_, "category") => vec![format!("{}.category.coding[0].code", resource_type)],
        (_, "subject") => vec![format!("{}.subject.reference", resource_type)],
        (_, "target") => vec![format!("{}.target[0].reference", resource_type)],
        (_, "organization") => vec![format!("{}.organization.reference", resource_type)],
        (_, "partOf") => vec![format!("{}.partOf.reference", resource_type)],
        (_, "managingOrganization") => {
            vec![format!("{}.managingOrganization.reference", resource_type)]
        }
        // Special: _id
        (_, "_id") => vec![format!("{}.id", resource_type)],
        // Fallback: try common patterns based on param type
        _ => match param_type {
            "string" => vec![format!("{}.{}", resource_type, param_name)],
            "token" => vec![
                format!("{}.{}.coding[0].code", resource_type, param_name),
                format!("{}.{}.code", resource_type, param_name),
                format!("{}.{}", resource_type, param_name),
            ],
            "reference" => vec![format!("{}.{}.reference", resource_type, param_name)],
            "date" | "dateTime" => vec![format!("{}.{}", resource_type, param_name)],
            "number" => vec![format!("{}.{}", resource_type, param_name)],
            "quantity" => vec![format!("{}.{}.value", resource_type, param_name)],
            _ => vec![format!("{}.{}", resource_type, param_name)],
        },
    }
}

/// Resolve a search parameter to a concrete value from the created resources.
/// Returns the first matching value found.
pub fn resolve_search_value(
    resource_type: &str,
    param_name: &str,
    param_type: &str,
    field_values: &HashMap<String, String>,
    created_ids: &HashMap<String, String>,
) -> Option<String> {
    // Special case: _id always uses the created resource ID
    if param_name == "_id" {
        return created_ids.get(resource_type).cloned();
    }

    // Special case: reference params use created IDs
    if param_type == "reference" {
        // Try to find the target resource type from common reference patterns
        if let Some(target_type) = resolve_reference_target(resource_type, param_name, None)
            && let Some(id) = created_ids.get(&target_type)
        {
            return Some(format!("{}/{}", target_type, id));
        }
        // Fall back to any created resource of matching type
        for (rt, id) in created_ids {
            if rt.to_lowercase() == param_name.to_lowercase() {
                return Some(format!("{}/{}", rt, id));
            }
        }
        return None;
    }

    // For other param types, look up from field values
    let possible_paths = search_param_to_field_paths(resource_type, param_name, param_type);
    for path in &possible_paths {
        if let Some(val) = field_values.get(path) {
            return Some(val.clone());
        }
    }

    None
}

/// Resolve a reference search param name to the likely target resource type.
///
/// First tries to infer the target from the SearchParameter's FHIRPath expression
/// (e.g., `"Patient.name | Practitioner.name"` → `"Patient"`). Falls back to a
/// hardcoded mapping of common search parameter names, then capitalizes the first
/// letter as a last resort.
pub fn resolve_reference_target(
    _resource_type: &str,
    param_name: &str,
    search_params: Option<&[SearchParameter]>,
) -> Option<String> {
    // Try SearchParameter-based inference first
    if let Some(params) = search_params
        && let Some(target) = infer_from_search_params(param_name, params)
    {
        return Some(target);
    }

    // Fallback: hardcoded mapping for common FHIR search parameter names
    Some(match param_name {
        "subject" | "patient" => "Patient".to_string(),
        "organization" | "managingOrganization" => "Organization".to_string(),
        "location" => "Location".to_string(),
        "practitioner" => "Practitioner".to_string(),
        "practitionerRole" => "PractitionerRole".to_string(),
        "endpoint" => "Endpoint".to_string(),
        "target" => "Provenance".to_string(),
        "partOf" => "Organization".to_string(),
        "encounter" => "Encounter".to_string(),
        "partof" => "Organization".to_string(),
        "device" => "Device".to_string(),
        "service" => "HealthcareService".to_string(),
        "group" => "Group".to_string(),
        "specimen" => "Specimen".to_string(),
        _ => {
            // Capitalize first letter as fallback
            let mut chars = param_name.chars();
            match chars.next() {
                None => String::new(),
                Some(c) => c.to_uppercase().chain(chars).collect(),
            }
        }
    })
}

/// Try to infer the target resource type from a SearchParameter's expression field.
/// Extracts the first resource type from the FHIRPath expression (e.g.,
/// `"Patient.name | Practitioner.name"` → `"Patient"`).
fn infer_from_search_params(param_name: &str, search_params: &[SearchParameter]) -> Option<String> {
    if let Some(sp) = search_params.iter().find(|sp| sp.code == param_name)
        && let Some(expression) = sp.expression.as_deref()
    {
        let types: Vec<&str> = expression
            .split('|')
            .filter_map(|part| {
                let part = part.trim();
                let rtype = part.split('.').next()?;
                if rtype.chars().next()?.is_uppercase() && !rtype.contains('-') {
                    Some(rtype)
                } else {
                    None
                }
            })
            .collect();
        if !types.is_empty() {
            return Some(types.first()?.to_string());
        }
    }
    None
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn test_extract_field_values_from_patient() {
        let patient = json!({
            "resourceType": "Patient",
            "name": [{
                "family": "GeneratedFamily",
                "given": ["GeneratedGiven"]
            }],
            "gender": "male",
            "birthDate": "2024-01-01",
            "identifier": [{
                "system": "http://example.org/identifier",
                "value": "generated-abc123"
            }],
            "active": true
        });

        let values = extract_field_values("Patient", &patient);
        assert!(values.contains_key("Patient.name[0].family"));
        assert_eq!(values["Patient.name[0].family"], "GeneratedFamily");
        assert!(values.contains_key("Patient.gender"));
        assert_eq!(values["Patient.gender"], "male");
        assert!(values.contains_key("Patient.birthDate"));
        assert_eq!(values["Patient.birthDate"], "2024-01-01");
    }

    #[test]
    fn test_resolve_search_value_for_id() {
        let mut created_ids = HashMap::new();
        created_ids.insert("Patient".to_string(), "patient-123".to_string());

        let result = resolve_search_value("Patient", "_id", "token", &HashMap::new(), &created_ids);
        assert_eq!(result, Some("patient-123".to_string()));
    }

    #[test]
    fn test_resolve_search_value_for_reference() {
        let mut created_ids = HashMap::new();
        created_ids.insert("Patient".to_string(), "patient-456".to_string());

        let result = resolve_search_value(
            "Observation",
            "subject",
            "reference",
            &HashMap::new(),
            &created_ids,
        );
        assert_eq!(result, Some("Patient/patient-456".to_string()));
    }

    #[test]
    fn test_search_param_to_field_paths() {
        let paths = search_param_to_field_paths("Patient", "name", "string");
        assert!(paths.contains(&"Patient.name".to_string()));
        assert!(paths.contains(&"Patient.name[0].family".to_string()));
    }
}
