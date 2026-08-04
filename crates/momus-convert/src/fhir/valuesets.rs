//! FHIR value set resolution.
//!
//! Builds maps from ValueSet and CodeSystem resources to generate
//! valid example values for bound elements.
//!
//! Ported from fhir-autotest's valuesets.rs.

use crate::fhir::profile::ElementBinding;
use std::collections::HashMap;

/// Build a map from ValueSet URL → system URL.
///
/// Scans all raw JSON resources for `resourceType == "ValueSet"`,
/// extracts the URL, then finds the system URL from `compose.include[].system`
/// or `expansion.contains[].system`.
pub fn build_value_set_system_map(raw_resources: &HashMap<String, serde_json::Value>) -> HashMap<String, String> {
    let mut map = HashMap::new();

    for (_path, resource) in raw_resources {
        let resource_type = resource
            .get("resourceType")
            .and_then(|v| v.as_str())
            .unwrap_or("");

        if resource_type != "ValueSet" {
            continue;
        }

        let url = match resource.get("url").and_then(|v| v.as_str()) {
            Some(u) => u.to_string(),
            None => continue,
        };

        if let Some(system) = extract_valueset_system(resource) {
            map.insert(url, system);
        }
    }

    map
}

/// Build a map from CodeSystem URL → (first_concept_code, optional_display).
///
/// Scans all raw JSON resources for `resourceType == "CodeSystem"`,
/// extracts the URL and the first concept's code and display.
pub fn build_code_system_first_code_map(
    raw_resources: &HashMap<String, serde_json::Value>,
) -> HashMap<String, (String, Option<String>)> {
    let mut map = HashMap::new();

    for (_path, resource) in raw_resources {
        let resource_type = resource
            .get("resourceType")
            .and_then(|v| v.as_str())
            .unwrap_or("");

        if resource_type != "CodeSystem" {
            continue;
        }

        let url = match resource.get("url").and_then(|v| v.as_str()) {
            Some(u) => u.to_string(),
            None => continue,
        };

        if let Some((code, display)) = extract_first_code(resource) {
            map.insert(url, (code, display));
        }
    }

    map
}

/// Extract the system URL from a ValueSet resource.
///
/// Prefers `compose.include[].system`, falls back to `expansion.contains[].system`.
fn extract_valueset_system(resource: &serde_json::Value) -> Option<String> {
    // Try compose.include[].system first
    if let Some(compose) = resource.get("compose") {
        if let Some(includes) = compose.get("include").and_then(|v| v.as_array()) {
            for include in includes {
                if let Some(system) = include.get("system").and_then(|v| v.as_str()) {
                    return Some(system.to_string());
                }
            }
        }
    }

    // Fall back to expansion.contains[].system
    if let Some(expansion) = resource.get("expansion") {
        if let Some(contains) = expansion.get("contains").and_then(|v| v.as_array()) {
            for item in contains {
                if let Some(system) = item.get("system").and_then(|v| v.as_str()) {
                    return Some(system.to_string());
                }
            }
        }
    }

    None
}

/// Extract the first concept code and optional display from a CodeSystem resource.
fn extract_first_code(resource: &serde_json::Value) -> Option<(String, Option<String>)> {
    let concepts = resource.get("concept").and_then(|v| v.as_array())?;

    for concept in concepts {
        let code = concept.get("code").and_then(|v| v.as_str())?.to_string();
        let display = concept.get("display").and_then(|v| v.as_str()).map(|s| s.to_string());
        return Some((code, display));
    }

    None
}

/// Look up the system URL bound to an element via its binding.
///
/// Strips version suffixes (e.g., `|4.0.1`) before lookup.
pub fn bound_system_for_element(
    binding: &Option<ElementBinding>,
    value_set_systems: &HashMap<String, String>,
) -> Option<String> {
    let binding = binding.as_ref()?;
    let value_set_url = binding.value_set.as_ref()?;

    // Strip version suffix
    let base_url = value_set_url.split('|').next().unwrap_or(value_set_url);

    value_set_systems.get(base_url).cloned()
}

/// Generate a valid code value for a given system URL.
///
/// Has hardcoded mappings for known FHIR systems.
pub fn code_value_for_system(system: &str) -> String {
    if system.ends_with("days-of-week") {
        "mon".to_string()
    } else if system.ends_with("administrative-gender") {
        "male".to_string()
    } else if system.ends_with("identifier-use") {
        "usual".to_string()
    } else if system.ends_with("name-use") {
        "official".to_string()
    } else if system.ends_with("contact-point-system") {
        "phone".to_string()
    } else if system.ends_with("contact-point-use") {
        "work".to_string()
    } else if system.ends_with("address-type") {
        "physical".to_string()
    } else if system.ends_with("address-use") {
        "home".to_string()
    } else if system.ends_with("marital-status") {
        "M".to_string()
    } else if system.ends_with("administrative-gender") {
        "male".to_string()
    } else if system.ends_with("allergy-intolerance-clinical") {
        "active".to_string()
    } else if system.ends_with("allergy-intolerance-verification") {
        "confirmed".to_string()
    } else if system.ends_with("medication-request-status") {
        "active".to_string()
    } else if system.ends_with("medication-request-intent") {
        "order".to_string()
    } else if system.ends_with("observation-status") {
        "final".to_string()
    } else if system.ends_with("condition-clinical") {
        "active".to_string()
    } else if system.ends_with("condition-verification-status") {
        "confirmed".to_string()
    } else if system.ends_with("encounter-status") {
        "in-progress".to_string()
    } else if system.ends_with("location-status") {
        "active".to_string()
    } else if system.ends_with("location-mode") {
        "instance".to_string()
    } else if system.ends_with("organization-type") {
        "prov".to_string()
    } else if system.ends_with("practitioner-role") {
        "doctor".to_string()
    } else {
        "unknown".to_string()
    }
}

/// Generate a CodeableConcept value for a bound element.
pub fn codeable_concept_for_system(system: &str) -> serde_json::Value {
    let code = code_value_for_system(system);
    serde_json::json!({
        "coding": [{
            "system": system,
            "code": code
        }],
        "text": code
    })
}

/// Generate a Coding value for a bound element.
pub fn coding_for_system(system: &str) -> serde_json::Value {
    let code = code_value_for_system(system);
    serde_json::json!({
        "system": system,
        "code": code
    })
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn test_build_value_set_system_map() {
        let mut resources = HashMap::new();
        resources.insert(
            "ValueSet-days-of-week.json".to_string(),
            json!({
                "resourceType": "ValueSet",
                "url": "http://hl7.org/fhir/ValueSet/days-of-week",
                "compose": {
                    "include": [{
                        "system": "http://hl7.org/fhir/days-of-week"
                    }]
                }
            }),
        );

        let map = build_value_set_system_map(&resources);
        assert_eq!(map.len(), 1);
        assert_eq!(
            map.get("http://hl7.org/fhir/ValueSet/days-of-week"),
            Some(&"http://hl7.org/fhir/days-of-week".to_string())
        );
    }

    #[test]
    fn test_build_code_system_first_code_map() {
        let mut resources = HashMap::new();
        resources.insert(
            "CodeSystem-administrative-gender.json".to_string(),
            json!({
                "resourceType": "CodeSystem",
                "url": "http://hl7.org/fhir/administrative-gender",
                "concept": [
                    {"code": "male", "display": "Male"},
                    {"code": "female", "display": "Female"}
                ]
            }),
        );

        let map = build_code_system_first_code_map(&resources);
        assert_eq!(map.len(), 1);
        let (code, display) = map.get("http://hl7.org/fhir/administrative-gender").unwrap();
        assert_eq!(code, "male");
        assert_eq!(display.as_deref(), Some("Male"));
    }

    #[test]
    fn test_bound_system_for_element() {
        let mut value_set_systems = HashMap::new();
        value_set_systems.insert(
            "http://hl7.org/fhir/ValueSet/days-of-week".to_string(),
            "http://hl7.org/fhir/days-of-week".to_string(),
        );

        let binding = Some(ElementBinding {
            strength: "required".to_string(),
            value_set: Some("http://hl7.org/fhir/ValueSet/days-of-week".to_string()),
            description: None,
        });

        let system = bound_system_for_element(&binding, &value_set_systems);
        assert_eq!(system, Some("http://hl7.org/fhir/days-of-week".to_string()));
    }

    #[test]
    fn test_bound_system_strips_version() {
        let mut value_set_systems = HashMap::new();
        value_set_systems.insert(
            "http://hl7.org/fhir/ValueSet/days-of-week".to_string(),
            "http://hl7.org/fhir/days-of-week".to_string(),
        );

        let binding = Some(ElementBinding {
            strength: "required".to_string(),
            value_set: Some("http://hl7.org/fhir/ValueSet/days-of-week|4.0.1".to_string()),
            description: None,
        });

        let system = bound_system_for_element(&binding, &value_set_systems);
        assert_eq!(system, Some("http://hl7.org/fhir/days-of-week".to_string()));
    }

    #[test]
    fn test_code_value_for_system() {
        assert_eq!(code_value_for_system("http://hl7.org/fhir/days-of-week"), "mon");
        assert_eq!(code_value_for_system("http://hl7.org/fhir/administrative-gender"), "male");
        assert_eq!(code_value_for_system("http://unknown.system"), "unknown");
    }

    #[test]
    fn test_codeable_concept_for_system() {
        let cc = codeable_concept_for_system("http://hl7.org/fhir/administrative-gender");
        assert_eq!(cc["coding"][0]["system"], "http://hl7.org/fhir/administrative-gender");
        assert_eq!(cc["coding"][0]["code"], "male");
    }

    #[test]
    fn test_extract_valueset_system_compose() {
        let resource = json!({
            "resourceType": "ValueSet",
            "url": "http://example.org/ValueSet/test",
            "compose": {
                "include": [{"system": "http://example.org/CodeSystem/test"}]
            }
        });
        assert_eq!(
            extract_valueset_system(&resource),
            Some("http://example.org/CodeSystem/test".to_string())
        );
    }

    #[test]
    fn test_extract_valueset_system_expansion() {
        let resource = json!({
            "resourceType": "ValueSet",
            "url": "http://example.org/ValueSet/test",
            "expansion": {
                "contains": [{"system": "http://example.org/CodeSystem/test"}]
            }
        });
        assert_eq!(
            extract_valueset_system(&resource),
            Some("http://example.org/CodeSystem/test".to_string())
        );
    }
}
