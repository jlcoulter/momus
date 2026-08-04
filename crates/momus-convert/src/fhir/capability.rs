use serde::{Deserialize, Serialize};

/// FHIR R4 CapabilityStatement resource.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CapabilityStatement {
    #[serde(rename = "resourceType")]
    pub resource_type: String,
    pub url: Option<String>,
    pub name: Option<String>,
    pub status: Option<String>,
    pub rest: Vec<Rest>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Rest {
    pub mode: String,
    #[serde(default)]
    pub resource: Vec<RestResource>,
    #[serde(default)]
    pub interaction: Vec<RestInteraction>,
    #[serde(default)]
    pub operation: Vec<RestOperation>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RestResource {
    #[serde(rename = "type")]
    pub resource_type: String,
    pub profile: Option<String>,
    #[serde(rename = "supportedProfile", default)]
    pub supported_profile: Vec<String>,
    #[serde(default)]
    pub interaction: Vec<RestInteraction>,
    #[serde(rename = "searchParam", default)]
    pub search_param: Vec<RestSearchParam>,
    #[serde(default)]
    pub operation: Vec<RestOperation>,
    #[serde(rename = "readHistory", default)]
    pub read_history: Option<bool>,
    #[serde(rename = "updateCreate", default)]
    pub update_create: Option<bool>,
    #[serde(rename = "conditionalCreate", default)]
    pub conditional_create: Option<bool>,
    #[serde(rename = "conditionalRead", default)]
    pub conditional_read: Option<String>,
    #[serde(rename = "conditionalUpdate", default)]
    pub conditional_update: Option<bool>,
    #[serde(rename = "conditionalDelete", default)]
    pub conditional_delete: Option<String>,
    #[serde(rename = "searchInclude", default)]
    pub search_include: Vec<String>,
    #[serde(rename = "searchRevInclude", default)]
    pub search_revinclude: Vec<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RestInteraction {
    pub code: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RestSearchParam {
    pub name: String,
    #[serde(rename = "type")]
    pub param_type: String,
    pub definition: Option<String>,
    pub documentation: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RestOperation {
    pub name: String,
    pub definition: Option<String>,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn deserialize_minimal_capability_statement() {
        let json = r#"{
            "resourceType": "CapabilityStatement",
            "status": "active",
            "name": "test",
            "rest": [{
                "mode": "server",
                "resource": [{
                    "type": "Patient",
                    "interaction": [{"code": "read"}, {"code": "search-type"}],
                    "searchParam": [{"name": "name", "type": "string"}]
                }],
                "interaction": []
            }]
        }"#;
        let cs: CapabilityStatement = serde_json::from_str(json).unwrap();
        assert_eq!(cs.rest[0].resource[0].resource_type, "Patient");
        assert_eq!(cs.rest[0].resource[0].interaction.len(), 2);
        assert_eq!(cs.rest[0].resource[0].search_param[0].name, "name");
    }
}
