//! FHIR profile resolver.
//!
//! Resolves parent profile chains (baseDefinition), downloads missing profiles
//! from the FHIR registry, and merges elements from parent profiles into children.
//!
//! Ported from fhir-autotest's profile_resolver.rs.

use crate::fhir::package::IgPackage;
use crate::fhir::profile::*;
use anyhow::Result;
use std::collections::{HashMap, HashSet};

/// Resolve all parent profile chains in an IG package.
///
/// Two-pass resolution:
/// 1. Resolve `baseDefinition` chains (parent → grandparent → ...)
/// 2. Resolve profiled types referenced by `type[].profile` and `type[].target_profile`
///
/// Missing parents produce a warning but don't fail — orphan profiles stay in the list.
pub fn resolve_profiles(pkg: &mut IgPackage) -> Result<()> {
    let mut url_map: HashMap<String, usize> = HashMap::new();
    for (i, sd) in pkg.structure_definitions.iter().enumerate() {
        url_map.insert(sd.url.clone(), i);
    }

    // Pass 1: Resolve parent chains
    let mut resolved = true;
    while resolved {
        resolved = false;
        let mut to_resolve: Vec<String> = Vec::new();
        for sd in &pkg.structure_definitions {
            if let Some(base_def) = &sd.base_definition {
                let base_url = strip_version(base_def);
                if !url_map.contains_key(&base_url) {
                    to_resolve.push(base_url);
                }
            }
        }
        for base_url in to_resolve {
            match download_profile(&base_url) {
                Ok(parent) => {
                    let idx = pkg.structure_definitions.len();
                    pkg.structure_definitions.push(parent);
                    url_map.insert(base_url, idx);
                    resolved = true;
                }
                Err(e) => {
                    tracing::warn!("Could not resolve parent profile '{}': {}", base_url, e);
                }
            }
        }
    }

    // Merge parent elements into children
    let indices: Vec<usize> = (0..pkg.structure_definitions.len()).collect();
    for &i in &indices {
        let base_url = pkg.structure_definitions[i]
            .base_definition
            .clone()
            .map(|s| strip_version(&s));
        if let Some(base_url) = base_url
            && let Some(&parent_idx) = url_map.get(&base_url)
        {
            let parent = pkg.structure_definitions[parent_idx].clone();
            merge_snapshot_elements(&mut pkg.structure_definitions[i], &parent);
        }
    }

    // Pass 2: Resolve profiled types
    let mut failed_urls: HashSet<String> = HashSet::new();
    let mut resolved_any = true;
    while resolved_any {
        resolved_any = false;
        let mut referenced: Vec<String> = Vec::new();
        for sd in &pkg.structure_definitions {
            if let Some(snapshot) = &sd.snapshot {
                for elem in &snapshot.element {
                    for t in &elem.type_ {
                        for profile in &t.profile {
                            if !profile.starts_with("http://hl7.org/fhir/StructureDefinition/")
                                && !url_map.contains_key(profile)
                                && !failed_urls.contains(profile)
                            {
                                referenced.push(profile.clone());
                            }
                        }
                        for target in &t.target_profile {
                            if !target.starts_with("http://hl7.org/fhir/StructureDefinition/")
                                && !url_map.contains_key(target)
                                && !failed_urls.contains(target)
                            {
                                referenced.push(target.clone());
                            }
                        }
                    }
                }
            }
        }
        for url in referenced {
            match download_profile(&url) {
                Ok(sd) => {
                    let idx = pkg.structure_definitions.len();
                    pkg.structure_definitions.push(sd);
                    url_map.insert(url.clone(), idx);
                    resolved_any = true;
                }
                Err(e) => {
                    tracing::warn!("Could not resolve profiled type '{}': {}", url, e);
                    failed_urls.insert(url);
                }
            }
        }
    }

    Ok(())
}

/// Merge parent snapshot elements into a child profile.
///
/// Strategy: child's elements always win. Parent elements that the child
/// doesn't override are inherited. Slice elements (IDs containing `:`) are
/// kept from the child.
fn merge_snapshot_elements(child: &mut StructureDefinition, parent: &StructureDefinition) {
    let child_snapshot = match &mut child.snapshot {
        Some(s) => s,
        None => return,
    };
    let parent_snapshot = match &parent.snapshot {
        Some(s) => s,
        None => return,
    };

    let child_ids: HashSet<String> = child_snapshot
        .element
        .iter()
        .map(|e| e.id.clone())
        .collect();

    let mut to_add: Vec<ElementDefinition> = Vec::new();
    for parent_elem in &parent_snapshot.element {
        if !child_ids.contains(&parent_elem.id) {
            to_add.push(parent_elem.clone());
        }
    }

    child_snapshot.element.append(&mut to_add);
    child_snapshot.element.sort_by(|a, b| a.id.cmp(&b.id));
}

/// Strip version suffix from a FHIR URL (e.g., `http://...|4.0.1` → `http://...`).
fn strip_version(url: &str) -> String {
    url.split('|').next().unwrap_or(url).to_string()
}

/// Download a StructureDefinition from the FHIR registry.
///
/// Tries multiple sources in order:
/// 1. `https://packages.fhir.org/StructureDefinition/{name}`
/// 2. `https://hl7.org/fhir/{name}.profile.json`
/// 3. `https://hl7.org/fhir/StructureDefinition/{name}`
fn download_profile(url: &str) -> Result<StructureDefinition> {
    let name = url
        .split('/')
        .next_back()
        .ok_or_else(|| anyhow::anyhow!("Invalid profile URL: {}", url))?;

    let sources = vec![
        format!("https://packages.fhir.org/StructureDefinition/{}", name),
        format!("https://hl7.org/fhir/{}.profile.json", name),
        format!("https://hl7.org/fhir/StructureDefinition/{}", name),
    ];

    let client = reqwest::blocking::Client::builder()
        .timeout(std::time::Duration::from_secs(10))
        .build()?;

    for source in &sources {
        let resp = client
            .get(source)
            .header("Accept", "application/fhir+json")
            .send()?;

        if resp.status().is_success() {
            let content_type = resp
                .headers()
                .get("content-type")
                .and_then(|v| v.to_str().ok())
                .unwrap_or("");
            if content_type.contains("json") || content_type.contains("json") {
                let sd: StructureDefinition = resp.json()?;
                return Ok(sd);
            }
        }
    }

    anyhow::bail!("Could not download profile '{}' from any source", name)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_strip_version() {
        assert_eq!(
            strip_version("http://example.org/StructureDefinition/Patient"),
            "http://example.org/StructureDefinition/Patient"
        );
        assert_eq!(
            strip_version("http://example.org/StructureDefinition/Patient|4.0.1"),
            "http://example.org/StructureDefinition/Patient"
        );
    }

    #[test]
    fn test_merge_snapshot_elements() {
        let mut child = StructureDefinition {
            resource_type: "StructureDefinition".into(),
            url: "http://example.org/StructureDefinition/Child".into(),
            name: "Child".into(),
            base_type: "Patient".into(),
            kind: "resource".into(),
            derivation: Some("constraint".into()),
            base_definition: Some("http://example.org/StructureDefinition/Parent".into()),
            snapshot: Some(Snapshot {
                element: vec![ElementDefinition {
                    id: "Patient.name".into(),
                    path: "Patient.name".into(),
                    min: Some(1),
                    max: Some("1".into()),
                    ..Default::default()
                }],
            }),
            differential: None,
        };

        let parent = StructureDefinition {
            resource_type: "StructureDefinition".into(),
            url: "http://example.org/StructureDefinition/Parent".into(),
            name: "Parent".into(),
            base_type: "Patient".into(),
            kind: "resource".into(),
            derivation: None,
            base_definition: None,
            snapshot: Some(Snapshot {
                element: vec![
                    ElementDefinition {
                        id: "Patient".into(),
                        path: "Patient".into(),
                        min: Some(0),
                        max: Some("*".into()),
                        ..Default::default()
                    },
                    ElementDefinition {
                        id: "Patient.name".into(),
                        path: "Patient.name".into(),
                        min: Some(0),
                        max: Some("*".into()),
                        ..Default::default()
                    },
                    ElementDefinition {
                        id: "Patient.birthDate".into(),
                        path: "Patient.birthDate".into(),
                        min: Some(0),
                        max: Some("1".into()),
                        ..Default::default()
                    },
                ],
            }),
            differential: None,
        };

        merge_snapshot_elements(&mut child, &parent);

        let snapshot = child.snapshot.unwrap();
        // Should have 3 elements: Patient, Patient.birthDate (inherited), Patient.name (child wins)
        assert_eq!(snapshot.element.len(), 3);
        // Child's Patient.name should keep min=1 (child wins)
        assert_eq!(snapshot.element[2].id, "Patient.name");
        assert_eq!(snapshot.element[2].min, Some(1));
        // Patient.birthDate should be inherited from parent
        assert!(snapshot.element.iter().any(|e| e.id == "Patient.birthDate"));
    }

    #[test]
    fn test_resolve_profiles_no_parents() {
        let mut pkg = IgPackage {
            structure_definitions: vec![StructureDefinition {
                resource_type: "StructureDefinition".into(),
                url: "http://example.org/StructureDefinition/TestPatient".into(),
                name: "TestPatient".into(),
                base_type: "Patient".into(),
                kind: "resource".into(),
                derivation: None,
                base_definition: None,
                snapshot: Some(Snapshot {
                    element: vec![ElementDefinition {
                        id: "Patient".into(),
                        path: "Patient".into(),
                        min: Some(0),
                        max: Some("*".into()),
                        ..Default::default()
                    }],
                }),
                differential: None,
            }],
            capability_statements: vec![],
            search_parameters: vec![],
            operation_definitions: vec![],
            raw_resources: HashMap::new(),
        };

        // Should not error — profiles with no parents are left as-is
        resolve_profiles(&mut pkg).unwrap();
        assert_eq!(pkg.structure_definitions.len(), 1);
    }
}
