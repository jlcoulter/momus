//! FHIR bulk data loader.
//!
//! Uploads NDJSON files to a FHIR server with concurrent wave-based ordering.
//! Ported from fhir-autotest's bulk_loader.rs.

use anyhow::Result;
use std::collections::{HashMap, HashSet};
use std::path::Path;
use std::sync::Arc;

/// Configuration for a FHIR write endpoint.
#[derive(Debug, Clone)]
pub enum WriteEndpoint {
    /// Repository-style endpoint with basic auth.
    Repository {
        base_url: String,
        username: String,
        password: String,
        upload_method: String,
        concurrency: usize,
    },
    /// Server-style endpoint with custom headers.
    Server {
        base_url: String,
        headers: HashMap<String, String>,
        upload_method: String,
        concurrency: usize,
    },
}

impl WriteEndpoint {
    fn base_url(&self) -> &str {
        match self {
            WriteEndpoint::Repository { base_url, .. } => base_url,
            WriteEndpoint::Server { base_url, .. } => base_url,
        }
    }

    fn upload_method(&self) -> &str {
        match self {
            WriteEndpoint::Repository { upload_method, .. } => upload_method,
            WriteEndpoint::Server { upload_method, .. } => upload_method,
        }
    }

    fn concurrency(&self) -> usize {
        match self {
            WriteEndpoint::Repository { concurrency, .. } => *concurrency,
            WriteEndpoint::Server { concurrency, .. } => *concurrency,
        }
    }
}

/// Upload NDJSON files to a FHIR server in creation order.
///
/// Reads each `{ResourceType}.ndjson` file from `data_dir`, uploads resources
/// with wave-based ordering to respect same-type dependencies, and returns
/// a map of resource type → server-assigned IDs.
pub async fn upload_ndjson_files(
    data_dir: &Path,
    creation_order: &[String],
    write_endpoint: &WriteEndpoint,
) -> Result<HashMap<String, Vec<String>>> {
    let concurrency = write_endpoint.concurrency().max(1);
    let client = reqwest::Client::builder()
        .timeout(std::time::Duration::from_secs(120))
        .build()?;

    let mut all_ids: HashMap<String, Vec<String>> = HashMap::new();

    for resource_type in creation_order {
        let file_path = data_dir.join(format!("{}.ndjson", resource_type));
        if !file_path.exists() {
            tracing::warn!("NDJSON file not found: {:?}", file_path);
            continue;
        }

        let content = std::fs::read_to_string(&file_path)?;
        let lines: Vec<String> = content.lines().map(|l| l.to_string()).collect();

        if lines.is_empty() {
            continue;
        }

        tracing::info!("Uploading {} {} resources", lines.len(), resource_type);

        let waves = order_upload_waves(resource_type, &lines);
        let mut type_ids = Vec::new();
        let mut total_errors = 0u64;

        for (wave_idx, wave) in waves.iter().enumerate() {
            let permits = Arc::new(tokio::sync::Semaphore::new(concurrency));
            let mut join_set = tokio::task::JoinSet::new();

            for line in wave {
                let client = client.clone();
                let endpoint = write_endpoint.clone();
                let line = (*line).clone();
                let rtype = resource_type.to_string();
                let permit = permits.clone().acquire_owned().await?;

                join_set.spawn(async move {
                    let _permit = permit;
                    upload_resource(&client, &endpoint, &rtype, &line).await
                });
            }

            while let Some(result) = join_set.join_next().await {
                match result? {
                    Ok(id) => type_ids.push(id),
                    Err(e) => {
                        tracing::warn!("Upload error in wave {}: {}", wave_idx, e);
                        total_errors += 1;
                    }
                }
            }
        }

        tracing::info!(
            "→ {}/{} {} created ({} errors)",
            type_ids.len(),
            lines.len(),
            resource_type,
            total_errors
        );

        all_ids.insert(resource_type.clone(), type_ids);
    }

    Ok(all_ids)
}

/// Upload a single resource to the FHIR server.
async fn upload_resource(
    client: &reqwest::Client,
    endpoint: &WriteEndpoint,
    resource_type: &str,
    line: &str,
) -> Result<String> {
    let value: serde_json::Value = serde_json::from_str(line)?;
    let id = value
        .get("id")
        .and_then(|v| v.as_str())
        .unwrap_or("")
        .to_string();

    let url = format!(
        "{}/{}/{}",
        endpoint.base_url().trim_end_matches('/'),
        resource_type,
        id
    );

    let mut req = match endpoint.upload_method().to_uppercase().as_str() {
        "POST" => {
            // POST doesn't use client-assigned IDs — strip the id field
            let mut body = value.clone();
            if let Some(obj) = body.as_object_mut() {
                obj.remove("id");
            }
            client.post(&url).json(&body)
        }
        _ => {
            // PUT (update-as-create)
            client.put(&url).json(&value)
        }
    };

    req = add_write_auth(req, endpoint);
    let resp = req.send().await?;
    let status = resp.status();

    if status.is_success() || status.as_u16() == 201 {
        Ok(id)
    } else {
        let body = resp.text().await.unwrap_or_default();
        anyhow::bail!(
            "Failed to upload {}: HTTP {} — {}",
            url,
            status,
            body.chars().take(200).collect::<String>()
        )
    }
}

/// Partition NDJSON lines into dependency waves for same-type references.
///
/// Each wave's resources only depend on resources in earlier waves.
fn order_upload_waves<'a>(resource_type: &str, lines: &'a [String]) -> Vec<Vec<&'a String>> {
    // Parse each line to extract id and same-type references
    let mut entries: Vec<(&String, String, Vec<String>)> = Vec::new();
    let mut all_ids_in_file: HashSet<String> = HashSet::new();

    for line in lines {
        let value: serde_json::Value = match serde_json::from_str(line) {
            Ok(v) => v,
            Err(_) => continue,
        };
        let id = value
            .get("id")
            .and_then(|v| v.as_str())
            .unwrap_or("")
            .to_string();

        let mut refs = Vec::new();
        collect_same_type_refs(&value, resource_type, &mut refs);

        all_ids_in_file.insert(id.clone());
        entries.push((line, id, refs));
    }

    // Build waves
    let mut waves: Vec<Vec<&String>> = Vec::new();
    let mut placed: HashSet<&str> = HashSet::new();
    let mut remaining: Vec<usize> = (0..entries.len()).collect();

    while !remaining.is_empty() {
        let mut wave: Vec<&String> = Vec::new();
        let mut still_remaining: Vec<usize> = Vec::new();

        for &idx in &remaining {
            let (line, id, refs) = &entries[idx];
            // Check if all same-type dependencies are already placed
            let deps_met = refs
                .iter()
                .all(|r| !all_ids_in_file.contains(r) || placed.contains(r.as_str()));

            if deps_met {
                wave.push(*line);
                placed.insert(id.as_str());
            } else {
                still_remaining.push(idx);
            }
        }

        if wave.is_empty() {
            // Cycle detected — place all remaining in a final best-effort wave
            for &idx in &still_remaining {
                wave.push(entries[idx].0);
            }
            waves.push(wave);
            break;
        }

        waves.push(wave);
        remaining = still_remaining;
    }

    waves
}

/// Recursively collect same-type references from a JSON value.
fn collect_same_type_refs(value: &serde_json::Value, resource_type: &str, out: &mut Vec<String>) {
    match value {
        serde_json::Value::Object(map) => {
            for (key, val) in map {
                if key == "reference" {
                    if let Some(s) = val.as_str()
                        && s.starts_with(&format!("{}/", resource_type))
                    {
                        let id = s.trim_start_matches(&format!("{}/", resource_type));
                        if !id.is_empty() {
                            out.push(id.to_string());
                        }
                    }
                } else {
                    collect_same_type_refs(val, resource_type, out);
                }
            }
        }
        serde_json::Value::Array(arr) => {
            for item in arr {
                collect_same_type_refs(item, resource_type, out);
            }
        }
        _ => {}
    }
}

/// Add authentication to a request based on the endpoint type.
fn add_write_auth(
    req: reqwest::RequestBuilder,
    endpoint: &WriteEndpoint,
) -> reqwest::RequestBuilder {
    match endpoint {
        WriteEndpoint::Repository {
            username, password, ..
        } => req.basic_auth(username.clone(), Some(password.clone())),
        WriteEndpoint::Server { headers, .. } => {
            let mut req = req;
            for (key, value) in headers {
                req = req.header(key.as_str(), value.as_str());
            }
            req
        }
    }
}

/// Delete all uploaded resources in reverse creation order.
pub async fn delete_all_resources(
    ids: &HashMap<String, Vec<String>>,
    creation_order: &[String],
    write_endpoint: &WriteEndpoint,
) -> Result<()> {
    let concurrency = write_endpoint.concurrency().max(1);
    let client = reqwest::Client::builder()
        .timeout(std::time::Duration::from_secs(30))
        .build()?;

    // Reverse order so dependents are deleted before their dependencies
    for resource_type in creation_order.iter().rev() {
        let type_ids = match ids.get(resource_type) {
            Some(v) => v,
            None => continue,
        };

        if type_ids.is_empty() {
            continue;
        }

        tracing::info!("Deleting {} {} resources", type_ids.len(), resource_type);

        let permits = Arc::new(tokio::sync::Semaphore::new(concurrency));
        let mut join_set = tokio::task::JoinSet::new();

        for id in type_ids {
            let client = client.clone();
            let endpoint = write_endpoint.clone();
            let rtype = resource_type.to_string();
            let rid = id.clone();
            let permit = permits.clone().acquire_owned().await?;

            join_set.spawn(async move {
                let _permit = permit;
                let url = format!(
                    "{}/{}/{}",
                    endpoint.base_url().trim_end_matches('/'),
                    rtype,
                    rid
                );
                let req = add_write_auth(client.delete(&url), &endpoint);
                let result = req.send().await;
                match result {
                    Ok(resp) => {
                        let status = resp.status();
                        if status.is_success() || status.as_u16() == 404 {
                            Ok(())
                        } else {
                            let body = resp.text().await.unwrap_or_default();
                            anyhow::bail!(
                                "Failed to delete {}: HTTP {} — {}",
                                url,
                                status,
                                body.chars().take(200).collect::<String>()
                            )
                        }
                    }
                    Err(e) => anyhow::bail!("Failed to delete {}: {}", url, e),
                }
            });
        }

        while let Some(result) = join_set.join_next().await {
            if let Err(e) = result? {
                tracing::warn!("Delete error: {}", e);
            }
        }
    }

    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_order_upload_waves_no_deps() {
        let lines = vec![
            r#"{"id": "a", "resourceType": "Organization"}"#.to_string(),
            r#"{"id": "b", "resourceType": "Organization"}"#.to_string(),
        ];
        let waves = order_upload_waves("Organization", &lines);
        assert_eq!(waves.len(), 1);
        assert_eq!(waves[0].len(), 2);
    }

    #[test]
    fn test_order_upload_waves_with_deps() {
        let lines = vec![
            r#"{"id": "c", "resourceType": "Organization", "partOf": {"reference": "Organization/b"}}"#.to_string(),
            r#"{"id": "b", "resourceType": "Organization", "partOf": {"reference": "Organization/a"}}"#.to_string(),
            r#"{"id": "a", "resourceType": "Organization"}"#.to_string(),
        ];
        let waves = order_upload_waves("Organization", &lines);
        // Wave 0: a (no deps), Wave 1: b (depends on a), Wave 2: c (depends on b)
        assert!(
            waves.len() >= 3,
            "Expected at least 3 waves, got {}",
            waves.len()
        );
    }

    #[test]
    fn test_order_upload_waves_self_ref() {
        let lines = vec![
            r#"{"id": "a", "resourceType": "Organization", "partOf": {"reference": "Organization/a"}}"#.to_string(),
        ];
        let waves = order_upload_waves("Organization", &lines);
        assert_eq!(waves.len(), 1);
        assert_eq!(waves[0].len(), 1);
    }

    #[test]
    fn test_order_upload_waves_cycle() {
        let lines = vec![
            r#"{"id": "a", "resourceType": "Organization", "partOf": {"reference": "Organization/b"}}"#.to_string(),
            r#"{"id": "b", "resourceType": "Organization", "partOf": {"reference": "Organization/a"}}"#.to_string(),
        ];
        let waves = order_upload_waves("Organization", &lines);
        // Both should end up in the final wave (cycle)
        assert_eq!(waves.len(), 1);
        assert_eq!(waves[0].len(), 2);
    }

    #[test]
    fn test_collect_same_type_refs() {
        let value = serde_json::json!({
            "resourceType": "Organization",
            "partOf": {"reference": "Organization/parent-id"}
        });
        let mut refs = Vec::new();
        collect_same_type_refs(&value, "Organization", &mut refs);
        assert_eq!(refs, vec!["parent-id"]);
    }

    #[test]
    fn test_collect_same_type_refs_ignores_other_types() {
        let value = serde_json::json!({
            "subject": {"reference": "Patient/123"}
        });
        let mut refs = Vec::new();
        collect_same_type_refs(&value, "Organization", &mut refs);
        assert!(refs.is_empty());
    }

    #[test]
    fn test_collect_same_type_refs_nested() {
        let value = serde_json::json!({
            "contained": [{
                "partOf": {"reference": "Organization/nested-id"}
            }]
        });
        let mut refs = Vec::new();
        collect_same_type_refs(&value, "Organization", &mut refs);
        assert_eq!(refs, vec!["nested-id"]);
    }
}
