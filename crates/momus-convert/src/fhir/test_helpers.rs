//! Test helpers for FHIR converter integration tests.
//!
//! Creates a minimal FHIR IG package (.tgz) in memory for testing
//! the package parser, resource generator, and test plan generator.
//! Ported from fhir-autotest's test_helpers.rs.

use std::io::Write;

/// Create a minimal FHIR IG package (.tgz) for testing.
///
/// Contains a CapabilityStatement with Patient and Observation resources,
/// plus their StructureDefinitions and a SearchParameter.
pub fn create_test_ig_package() -> Vec<u8> {
    let cs_json = r#"{
        "resourceType": "CapabilityStatement",
        "url": "http://example.org/CapabilityStatement/TestIG",
        "name": "TestIG",
        "status": "active",
        "rest": [{
            "mode": "server",
            "resource": [{
                "type": "Patient",
                "profile": "http://hl7.org/fhir/StructureDefinition/Patient",
                "supportedProfile": ["http://example.org/StructureDefinition/TestPatient"],
                "interaction": [
                    {"code": "read"},
                    {"code": "search-type"},
                    {"code": "create"},
                    {"code": "update"},
                    {"code": "delete"}
                ],
                "searchParam": [
                    {"name": "name", "type": "string"},
                    {"name": "birthdate", "type": "date"}
                ]
            }, {
                "type": "Observation",
                "profile": "http://hl7.org/fhir/StructureDefinition/Observation",
                "supportedProfile": ["http://example.org/StructureDefinition/TestObservation"],
                "interaction": [
                    {"code": "read"},
                    {"code": "search-type"},
                    {"code": "create"}
                ],
                "searchParam": [
                    {"name": "category", "type": "token"},
                    {"name": "code", "type": "token"}
                ]
            }],
            "interaction": []
        }]
    }"#;

    let patient_sd_json = r#"{
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
                "id": "Patient.id",
                "path": "Patient.id",
                "min": 0,
                "max": "1",
                "type": [{"code": "id"}]
            }, {
                "id": "Patient.identifier",
                "path": "Patient.identifier",
                "min": 1,
                "max": "*",
                "type": [{"code": "Identifier"}],
                "mustSupport": true
            }, {
                "id": "Patient.name",
                "path": "Patient.name",
                "min": 1,
                "max": "*",
                "type": [{"code": "HumanName"}],
                "mustSupport": true
            }, {
                "id": "Patient.gender",
                "path": "Patient.gender",
                "min": 0,
                "max": "1",
                "type": [{"code": "code"}]
            }, {
                "id": "Patient.birthDate",
                "path": "Patient.birthDate",
                "min": 0,
                "max": "1",
                "type": [{"code": "date"}]
            }]
        }
    }"#;

    let observation_sd_json = r#"{
        "resourceType": "StructureDefinition",
        "url": "http://example.org/StructureDefinition/TestObservation",
        "name": "TestObservation",
        "type": "Observation",
        "kind": "resource",
        "derivation": "constraint",
        "snapshot": {
            "element": [{
                "id": "Observation",
                "path": "Observation",
                "min": 0,
                "max": "*"
            }, {
                "id": "Observation.id",
                "path": "Observation.id",
                "min": 0,
                "max": "1",
                "type": [{"code": "id"}]
            }, {
                "id": "Observation.status",
                "path": "Observation.status",
                "min": 1,
                "max": "1",
                "type": [{"code": "code"}],
                "fixedCode": "final"
            }, {
                "id": "Observation.subject",
                "path": "Observation.subject",
                "min": 1,
                "max": "1",
                "type": [{
                    "code": "Reference",
                    "targetProfile": ["http://hl7.org/fhir/StructureDefinition/Patient"]
                }],
                "mustSupport": true
            }, {
                "id": "Observation.code",
                "path": "Observation.code",
                "min": 1,
                "max": "1",
                "type": [{"code": "CodeableConcept"}]
            }, {
                "id": "Observation.valueString",
                "path": "Observation.valueString",
                "min": 0,
                "max": "1",
                "type": [{"code": "string"}]
            }]
        }
    }"#;

    let sp_json = r#"{
        "resourceType": "SearchParameter",
        "url": "http://example.org/SearchParameter/patient-name",
        "name": "name",
        "code": "name",
        "base": ["Patient"],
        "type": "string",
        "expression": "Patient.name"
    }"#;

    let mut tar_data = Vec::new();
    {
        let mut tar = tar::Builder::new(&mut tar_data);

        let files = [
            ("package/CapabilityStatement-test.json", cs_json),
            (
                "package/StructureDefinition-TestPatient.json",
                patient_sd_json,
            ),
            (
                "package/StructureDefinition-TestObservation.json",
                observation_sd_json,
            ),
            ("package/SearchParameter-patient-name.json", sp_json),
        ];

        for (path, content) in &files {
            let mut header = tar::Header::new_gnu();
            header.set_path(path).unwrap();
            header.set_size(content.len() as u64);
            header.set_cksum();
            tar.append_data(&mut header, *path, content.as_bytes())
                .unwrap();
        }

        tar.finish().unwrap();
    }

    let mut gz_data = Vec::new();
    {
        let mut gz = flate2::write::GzEncoder::new(&mut gz_data, flate2::Compression::default());
        gz.write_all(&tar_data).unwrap();
        gz.finish().unwrap();
    }

    gz_data
}
