//! FHIR-specific implementation of the `ResourceGenerator` trait.
//!
//! Uses the existing `resource_gen` module to generate profile-conformant
//! FHIR resources, and the `value_resolver` module to extract searchable
//! field values.

use super::profile::StructureDefinition;
use super::resource_gen;
use super::value_resolver;
use super::valuesets;
use anyhow::Result;
use momus_core::ast::DataVariation;
use momus_core::engine::test_generator::ResourceGenerator;
use std::collections::HashMap;

/// FHIR-specific resource generator.
///
/// Generates profile-conformant FHIR resources using the StructureDefinitions
/// from an IG package, and applies variations for comprehensive testing.
pub struct FhirResourceGenerator {
    /// All StructureDefinitions from the IG package.
    structure_definitions: Vec<StructureDefinition>,
    /// Map of ValueSet URL → system URL for code generation.
    value_set_systems: HashMap<String, String>,
    /// Index map: base_type → index in structure_definitions.
    profile_index: HashMap<String, usize>,
}

impl FhirResourceGenerator {
    /// Create a new FHIR resource generator from IG package data.
    pub fn new(
        structure_definitions: &[StructureDefinition],
        raw_resources: &HashMap<String, serde_json::Value>,
    ) -> Self {
        let value_set_systems = valuesets::build_value_set_system_map(raw_resources);
        let owned: Vec<StructureDefinition> = structure_definitions.to_vec();
        let mut profile_index = HashMap::new();
        for (i, sd) in owned.iter().enumerate() {
            profile_index.insert(sd.base_type.clone(), i);
        }

        Self {
            structure_definitions: owned,
            value_set_systems,
            profile_index,
        }
    }

    /// Find the StructureDefinition for a resource type.
    fn find_profile(&self, resource_type: &str) -> Option<&StructureDefinition> {
        self.profile_index
            .get(resource_type)
            .and_then(|&i| self.structure_definitions.get(i))
    }
}

impl ResourceGenerator for FhirResourceGenerator {
    fn generate(&self, resource_type: &str) -> Result<serde_json::Value> {
        if let Some(profile) = self.find_profile(resource_type) {
            resource_gen::generate_resource_with_value_sets(
                profile,
                &self.structure_definitions,
                &self.value_set_systems,
            )
        } else {
            // Fallback: generate a minimal resource with just the type
            Ok(serde_json::json!({
                "resourceType": resource_type,
                "meta": {
                    "profile": [format!("http://hl7.org/fhir/StructureDefinition/{}", resource_type)]
                }
            }))
        }
    }

    fn vary(&self, resource: &mut serde_json::Value, variation: &DataVariation, index: u64) {
        match variation {
            DataVariation::HappyPath | DataVariation::ToBeDeleted => {
                // No changes needed
            }
            DataVariation::Minimal => {
                // Remove optional fields by keeping only required elements.
                // Walk the resource and remove fields that are not required
                // according to the StructureDefinition.
                let rtype = resource
                    .get("resourceType")
                    .and_then(|v| v.as_str())
                    .unwrap_or("");
                if let Some(profile) = self.find_profile(rtype) {
                    remove_optional_fields(resource, profile);
                }
            }
            DataVariation::DuplicateValue { .. } => {
                // Keep the same value as the base resource (already cloned)
            }
            DataVariation::MissingField { field } => {
                // Remove the specified field path
                remove_field_by_path(resource, field);
            }
            DataVariation::SpecialChars => {
                // Add special characters to all string values
                inject_special_chars(resource);
            }
            DataVariation::Boundary { field } => {
                // Set boundary values
                if field.is_empty() {
                    // Default: set all string fields to empty
                    set_empty_strings(resource);
                } else {
                    set_field_to_boundary(resource, field, index);
                }
            }
        }
    }

    fn extract_values(
        &self,
        resource_type: &str,
        resource: &serde_json::Value,
    ) -> HashMap<String, String> {
        value_resolver::extract_field_values(resource_type, resource)
    }
}

/// Remove optional fields from a resource, keeping only required elements.
fn remove_optional_fields(resource: &mut serde_json::Value, profile: &StructureDefinition) {
    // Collect required field paths (min > 0)
    let elements = match &profile.snapshot {
        Some(snapshot) => &snapshot.element,
        None => match &profile.differential {
            Some(diff) => &diff.element,
            None => return,
        },
    };

    let required_paths: Vec<&str> = elements
        .iter()
        .filter(|e| e.min.unwrap_or(0) > 0)
        .filter_map(|e| {
            let path = &e.path;
            // Skip the root element (e.g., "Patient")
            if !path.contains('.') {
                return None;
            }
            // Extract the field name (e.g., "Patient.name" → "name")
            path.split('.').last()
        })
        .collect();

    // Remove all fields that are not required
    if let Some(obj) = resource.as_object_mut() {
        let to_remove: Vec<String> = obj
            .keys()
            .filter(|k| {
                // Always keep resourceType, id, and meta
                !matches!(k.as_str(), "resourceType" | "id" | "meta")
                    && !required_paths.contains(&k.as_str())
            })
            .cloned()
            .collect();

        for key in to_remove {
            obj.remove(&key);
        }
    }
}

/// Remove a field from a resource by dot-separated path.
fn remove_field_by_path(resource: &mut serde_json::Value, path: &str) {
    let parts: Vec<&str> = path.split('.').collect();
    if parts.is_empty() {
        return;
    }

    if parts.len() == 1 {
        if let Some(obj) = resource.as_object_mut() {
            obj.remove(parts[0]);
        }
        return;
    }

    // Navigate to the parent and remove the last key
    let mut current = resource;
    for i in 0..parts.len() - 1 {
        let key = parts[i];
        // Handle array indices like "name[0]"
        let (field, _index) = split_array_ref(key);
        current = match current.get_mut(field) {
            Some(val) => val,
            None => return,
        };
    }

    let last_key = parts.last().unwrap();
    let (field, _index) = split_array_ref(last_key);
    if let Some(obj) = current.as_object_mut() {
        obj.remove(field);
    }
}

/// Split a field reference like "name[0]" into ("name", Some(0)).
fn split_array_ref(s: &str) -> (&str, Option<usize>) {
    if let Some(bracket) = s.find('[') {
        let field = &s[..bracket];
        let index_str = &s[bracket + 1..s.len() - 1];
        let index = index_str.parse::<usize>().ok();
        (field, index)
    } else {
        (s, None)
    }
}

/// Inject special characters into all string values in a resource.
fn inject_special_chars(resource: &mut serde_json::Value) {
    match resource {
        serde_json::Value::String(s) => {
            // Skip URLs, profile references, system URIs, and UUIDs
            if !s.starts_with("http")
                && !s.starts_with("urn:uuid:")
                && !s.starts_with("mailto:")
                && !s.starts_with("tel:")
            {
                // Inject XSS, SQL injection, unicode, and HTML patterns
                *s = format!(
                    "{}<script>alert('xss')</script>'; DROP TABLE {};--\u{0000}",
                    s,
                    s.replace(' ', "_")
                );
            }
        }
        serde_json::Value::Object(obj) => {
            for val in obj.values_mut() {
                inject_special_chars(val);
            }
        }
        serde_json::Value::Array(arr) => {
            for val in arr.iter_mut() {
                inject_special_chars(val);
            }
        }
        _ => {}
    }
}

/// Set all string values in a resource to empty strings.
fn set_empty_strings(resource: &mut serde_json::Value) {
    match resource {
        serde_json::Value::String(s) => {
            // Skip URLs, profile references, system URIs, and UUIDs
            if !s.starts_with("http")
                && !s.starts_with("urn:uuid:")
                && !s.starts_with("mailto:")
                && !s.starts_with("tel:")
            {
                s.clear();
            }
        }
        serde_json::Value::Object(obj) => {
            for val in obj.values_mut() {
                set_empty_strings(val);
            }
        }
        serde_json::Value::Array(arr) => {
            for val in arr.iter_mut() {
                set_empty_strings(val);
            }
        }
        _ => {}
    }
}

/// Set a specific field to a boundary value.
fn set_field_to_boundary(resource: &mut serde_json::Value, field: &str, _index: u64) {
    // Navigate to the field and set it to a boundary value
    let parts: Vec<&str> = field.split('.').collect();
    let mut current = resource;

    for i in 0..parts.len() - 1 {
        let key = parts[i];
        let (field_name, _index) = split_array_ref(key);
        current = match current.get_mut(field_name) {
            Some(val) => val,
            None => return,
        };
    }

    let last_key = parts.last().unwrap();
    let (field_name, _index) = split_array_ref(last_key);

    if let Some(obj) = current.as_object_mut() {
        // Set to empty string as a boundary value
        obj.insert(field_name.to_string(), serde_json::json!(""));
    }
}
