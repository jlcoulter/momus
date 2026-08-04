use super::capability::CapabilityStatement;
use super::operation::OperationDefinition;
use super::profile::StructureDefinition;
use super::search_param::SearchParameter;
use anyhow::{Context, Result};
use flate2::read::GzDecoder;
use serde_json::Value;
use std::collections::HashMap;
use std::io::Read;
use tar::Archive;

/// Parsed contents of a FHIR IG package (.tgz).
#[derive(Debug)]
pub struct IgPackage {
    pub capability_statements: Vec<CapabilityStatement>,
    pub structure_definitions: Vec<StructureDefinition>,
    pub search_parameters: Vec<SearchParameter>,
    pub operation_definitions: Vec<OperationDefinition>,
    pub raw_resources: HashMap<String, Value>,
}

/// Parse a FHIR IG package (.tgz) file.
pub fn parse_package(path: &str) -> Result<IgPackage> {
    let file =
        std::fs::File::open(path).with_context(|| format!("Failed to open IG package: {path}"))?;
    let gz = GzDecoder::new(file);
    let mut archive = Archive::new(gz);

    let mut capability_statements = Vec::new();
    let mut structure_definitions = Vec::new();
    let mut search_parameters = Vec::new();
    let mut operation_definitions = Vec::new();
    let mut raw_resources = HashMap::new();

    for entry in archive.entries()? {
        let mut entry = entry?;
        let entry_path = entry.path()?.to_path_buf();
        let path_str = entry_path.to_string_lossy();

        if !path_str.starts_with("package/") || !path_str.ends_with(".json") {
            continue;
        }

        let mut content = String::new();
        entry.read_to_string(&mut content)?;

        let json: Value = match serde_json::from_str(&content) {
            Ok(v) => v,
            Err(e) => {
                tracing::debug!("Skipping non-JSON or invalid file {}: {e}", path_str);
                continue;
            }
        };

        let resource_type = json
            .get("resourceType")
            .and_then(|v| v.as_str())
            .unwrap_or("");

        match resource_type {
            "CapabilityStatement" => {
                if let Ok(cs) = serde_json::from_value::<CapabilityStatement>(json.clone()) {
                    capability_statements.push(cs);
                }
            }
            "StructureDefinition" => {
                if let Ok(sd) = serde_json::from_value::<StructureDefinition>(json.clone()) {
                    structure_definitions.push(sd);
                }
            }
            "SearchParameter" => {
                if let Ok(sp) = serde_json::from_value::<SearchParameter>(json.clone()) {
                    search_parameters.push(sp);
                }
            }
            "OperationDefinition" => {
                if let Ok(od) = serde_json::from_value::<OperationDefinition>(json.clone()) {
                    operation_definitions.push(od);
                }
            }
            _ => {}
        }

        raw_resources.insert(path_str.to_string(), json);
    }

    tracing::info!(
        "Parsed IG package: {} CapabilityStatements, {} StructureDefinitions, {} SearchParameters, {} OperationDefinitions",
        capability_statements.len(),
        structure_definitions.len(),
        search_parameters.len(),
        operation_definitions.len(),
    );

    Ok(IgPackage {
        capability_statements,
        structure_definitions,
        search_parameters,
        operation_definitions,
        raw_resources,
    })
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::Write;

    fn create_test_tgz() -> Vec<u8> {
        let mut tar_data = Vec::new();
        {
            let mut tar = tar::Builder::new(&mut tar_data);

            let cs_json = r#"{
                "resourceType": "CapabilityStatement",
                "url": "http://example.org/CapabilityStatement/test",
                "name": "TestCS",
                "status": "active",
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

            let mut header = tar::Header::new_gnu();
            header
                .set_path("package/CapabilityStatement-test.json")
                .unwrap();
            header.set_size(cs_json.len() as u64);
            header.set_cksum();
            tar.append_data(
                &mut header,
                "package/CapabilityStatement-test.json",
                cs_json.as_bytes(),
            )
            .unwrap();

            let sd_json = r#"{
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
                        "type": [{"code": "HumanName"}]
                    }]
                }
            }"#;

            let mut header2 = tar::Header::new_gnu();
            header2
                .set_path("package/StructureDefinition-TestPatient.json")
                .unwrap();
            header2.set_size(sd_json.len() as u64);
            header2.set_cksum();
            tar.append_data(
                &mut header2,
                "package/StructureDefinition-TestPatient.json",
                sd_json.as_bytes(),
            )
            .unwrap();

            tar.finish().unwrap();
        }

        let mut gz_data = Vec::new();
        {
            let mut gz =
                flate2::write::GzEncoder::new(&mut gz_data, flate2::Compression::default());
            gz.write_all(&tar_data).unwrap();
            gz.finish().unwrap();
        }
        gz_data
    }

    #[test]
    fn parse_test_package() {
        let tgz_data = create_test_tgz();
        let temp_dir = std::env::temp_dir();
        let tgz_path = temp_dir.join("fhir_test_ig_package.tgz");
        std::fs::write(&tgz_path, &tgz_data).unwrap();

        let pkg = parse_package(tgz_path.to_str().unwrap()).unwrap();
        assert_eq!(pkg.capability_statements.len(), 1);
        assert_eq!(pkg.structure_definitions.len(), 1);
        assert_eq!(
            pkg.capability_statements[0].rest[0].resource[0].resource_type,
            "Patient"
        );
        assert_eq!(pkg.structure_definitions[0].base_type, "Patient");
        assert!(pkg.raw_resources.len() >= 2);
    }

    #[test]
    fn parse_nonexistent_file_returns_error() {
        let result = parse_package("/nonexistent/path.tgz");
        assert!(result.is_err());
    }
}
