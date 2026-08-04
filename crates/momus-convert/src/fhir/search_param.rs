use serde::{Deserialize, Serialize};

/// FHIR R4 SearchParameter resource.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SearchParameter {
    #[serde(rename = "resourceType")]
    pub resource_type: String,
    pub url: String,
    pub name: String,
    pub code: String,
    #[serde(default)]
    pub base: Vec<String>,
    #[serde(rename = "type")]
    pub param_type: String,
    pub expression: Option<String>,
    pub description: Option<String>,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn deserialize_search_parameter() {
        let json = r#"{
            "resourceType": "SearchParameter",
            "url": "http://hl7.org/fhir/SearchParameter/individual-name",
            "name": "name",
            "code": "name",
            "base": ["Patient", "Practitioner"],
            "type": "string",
            "expression": "Patient.name | Practitioner.name"
        }"#;
        let sp: SearchParameter = serde_json::from_str(json).unwrap();
        assert_eq!(sp.code, "name");
        assert_eq!(sp.base, vec!["Patient", "Practitioner"]);
    }
}
