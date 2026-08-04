use serde::{Deserialize, Serialize};

/// FHIR R4 StructureDefinition resource.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct StructureDefinition {
    #[serde(rename = "resourceType")]
    pub resource_type: String,
    pub url: String,
    pub name: String,
    #[serde(rename = "type")]
    pub base_type: String,
    pub kind: String,
    pub derivation: Option<String>,
    pub snapshot: Option<Snapshot>,
    pub differential: Option<Differential>,
    #[serde(default)]
    #[serde(rename = "baseDefinition")]
    pub base_definition: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Snapshot {
    pub element: Vec<ElementDefinition>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Differential {
    pub element: Vec<ElementDefinition>,
}

#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct ElementDefinition {
    pub id: String,
    pub path: String,
    pub min: Option<u32>,
    pub max: Option<String>,
    #[serde(rename = "type", default)]
    pub type_: Vec<ElementDefinitionType>,
    #[serde(rename = "fixedString")]
    pub fixed_string: Option<String>,
    #[serde(rename = "sliceName", default)]
    pub slice_name: Option<String>,
    #[serde(default)]
    pub slicing: Option<ElementSlicing>,
    #[serde(rename = "fixedUri")]
    pub fixed_uri: Option<String>,
    #[serde(rename = "fixedCode")]
    pub fixed_code: Option<String>,
    #[serde(rename = "fixedBoolean")]
    pub fixed_boolean: Option<bool>,
    #[serde(rename = "fixedInteger")]
    pub fixed_integer: Option<i32>,
    #[serde(rename = "fixedDecimal")]
    pub fixed_decimal: Option<f64>,
    #[serde(rename = "patternString")]
    pub pattern_string: Option<String>,
    #[serde(rename = "patternUri")]
    pub pattern_uri: Option<String>,
    #[serde(rename = "patternCode")]
    pub pattern_code: Option<String>,
    #[serde(rename = "patternBoolean")]
    pub pattern_boolean: Option<bool>,
    #[serde(rename = "mustSupport", default)]
    pub must_support: bool,
    #[serde(rename = "short")]
    pub short: Option<String>,
    #[serde(rename = "definition")]
    pub definition: Option<String>,
    pub binding: Option<ElementBinding>,
    #[serde(rename = "contentReference")]
    pub content_reference: Option<String>,
    #[serde(rename = "fixedQuantity")]
    pub fixed_quantity: Option<serde_json::Value>,
    #[serde(rename = "patternQuantity")]
    pub pattern_quantity: Option<serde_json::Value>,
    #[serde(rename = "fixedCoding")]
    pub fixed_coding: Option<serde_json::Value>,
    #[serde(rename = "patternCoding")]
    pub pattern_coding: Option<serde_json::Value>,
    #[serde(rename = "fixedCodeableConcept")]
    pub fixed_codeable_concept: Option<serde_json::Value>,
    #[serde(rename = "patternCodeableConcept")]
    pub pattern_codeable_concept: Option<serde_json::Value>,
    #[serde(default)]
    pub constraint: Vec<ElementConstraint>,
    #[serde(rename = "isModifier", default)]
    pub is_modifier: bool,
    #[serde(rename = "isSummary", default)]
    pub is_summary: bool,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ElementDefinitionType {
    pub code: String,
    #[serde(rename = "targetProfile", default)]
    pub target_profile: Vec<String>,
    #[serde(default)]
    pub profile: Vec<String>,
    #[serde(rename = "versioning", default)]
    pub versioning: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ElementBinding {
    pub strength: String,
    #[serde(rename = "valueSet")]
    pub value_set: Option<String>,
    pub description: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ElementConstraint {
    pub key: String,
    pub severity: String,
    pub human: Option<String>,
    pub expression: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct ElementSlicing {
    #[serde(default)]
    pub discriminator: Vec<SlicingDiscriminator>,
    #[serde(default)]
    pub rules: Option<String>,
    #[serde(default)]
    pub description: Option<String>,
    #[serde(default)]
    pub ordered: bool,
}

#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct SlicingDiscriminator {
    #[serde(rename = "type")]
    pub discriminator_type: String,
    pub path: String,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn deserialize_structure_definition() {
        let json = r#"{
            "resourceType": "StructureDefinition",
            "url": "http://example.org/StructureDefinition/TestPatient",
            "name": "TestPatient",
            "type": "Patient",
            "kind": "resource",
            "derivation": "constraint",
            "snapshot": {
                "element": [{
                    "id": "Patient",
                    "path": "Patient",
                    "min": 0,
                    "max": "*"
                }, {
                    "id": "Patient.name",
                    "path": "Patient.name",
                    "min": 1,
                    "max": "*",
                    "type": [{"code": "HumanName"}],
                    "mustSupport": true
                }]
            }
        }"#;
        let sd: StructureDefinition = serde_json::from_str(json).unwrap();
        assert_eq!(sd.base_type, "Patient");
        assert_eq!(sd.derivation.as_deref(), Some("constraint"));
        let snapshot = sd.snapshot.unwrap();
        assert_eq!(snapshot.element.len(), 2);
        assert_eq!(snapshot.element[1].min, Some(1));
        assert_eq!(snapshot.element[1].type_.len(), 1);
        assert_eq!(snapshot.element[1].type_[0].code, "HumanName");
    }
}
