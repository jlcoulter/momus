use serde::{Deserialize, Serialize};

/// FHIR R4 OperationDefinition resource.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct OperationDefinition {
    #[serde(rename = "resourceType")]
    pub resource_type: String,
    pub url: String,
    pub name: String,
    pub code: String,
    pub system: Option<bool>,
    #[serde(rename = "type")]
    pub type_: Option<bool>,
    pub instance: Option<bool>,
    #[serde(default)]
    pub parameter: Vec<OperationParameter>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct OperationParameter {
    pub name: String,
    #[serde(rename = "use")]
    pub use_: Option<String>,
    pub min: Option<u32>,
    pub max: Option<String>,
    #[serde(rename = "type")]
    pub param_type: Option<String>,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn deserialize_operation_definition() {
        let json = r#"{
            "resourceType": "OperationDefinition",
            "url": "http://hl7.org/fhir/OperationDefinition/Patient-everything",
            "name": "everything",
            "code": "everything",
            "system": false,
            "type": false,
            "instance": true,
            "parameter": [{
                "name": "start",
                "use": "in",
                "min": 0,
                "max": "1",
                "type": "date"
            }]
        }"#;
        let od: OperationDefinition = serde_json::from_str(json).unwrap();
        assert_eq!(od.code, "everything");
        assert_eq!(od.parameter.len(), 1);
    }
}
