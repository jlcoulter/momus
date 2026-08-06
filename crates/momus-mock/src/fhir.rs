/// FHIR-specific mock server with CRUD + search support.
///
/// Provides a lightweight in-process FHIR R4 server that supports:
/// - Create, Read, Update, Delete operations
/// - Search with parameter filtering (string, token, reference, date)
/// - FHIR-compliant responses (Bundle, OperationOutcome)
/// - UUID-based resource IDs
///
/// # Example
///
/// ```ignore
/// use momus_mock::fhir::start_fhir_mock_server;
///
/// let addr = start_fhir_mock_server(0).await.unwrap();
/// println!("FHIR mock server at http://{}", addr);
/// ```
use axum::{
    Json, Router,
    extract::{Path, Query, State},
    http::StatusCode,
    routing::{delete, get, post, put},
};
use serde::Deserialize;
use std::collections::HashMap;
use std::net::SocketAddr;
use std::sync::{Arc, Mutex};

type FhirStore = Arc<Mutex<HashMap<String, Vec<serde_json::Value>>>>;

async fn create_resource(
    State(store): State<FhirStore>,
    Path(rtype): Path<String>,
    Json(mut body): Json<serde_json::Value>,
) -> (StatusCode, Json<serde_json::Value>) {
    let id = uuid::Uuid::new_v4().to_string();
    body["id"] = serde_json::Value::String(id.clone());
    let mut store = store.lock().unwrap();
    store.entry(rtype.clone()).or_default().push(body.clone());
    (StatusCode::CREATED, Json(body))
}

async fn read_resource(
    State(store): State<FhirStore>,
    Path((rtype, id)): Path<(String, String)>,
) -> (StatusCode, Json<serde_json::Value>) {
    let store = store.lock().unwrap();
    if let Some(resources) = store.get(&rtype) {
        if let Some(resource) = resources
            .iter()
            .find(|r| r.get("id").and_then(|v| v.as_str()) == Some(&id))
        {
            return (StatusCode::OK, Json(resource.clone()));
        }
    }
    (
        StatusCode::NOT_FOUND,
        Json(serde_json::json!({
            "resourceType": "OperationOutcome",
            "issue": [{"severity": "error", "code": "not-found", "diagnostics": format!("{}/{} not found", rtype, id)}]
        })),
    )
}

#[derive(Deserialize, Default)]
struct SearchParams {
    #[serde(default)]
    _count: Option<u32>,
    #[serde(default)]
    _summary: Option<String>,
    #[serde(flatten)]
    _rest: HashMap<String, String>,
}

async fn search_resources(
    State(store): State<FhirStore>,
    Path(rtype): Path<String>,
    Query(params): Query<SearchParams>,
) -> (StatusCode, Json<serde_json::Value>) {
    let store = store.lock().unwrap();
    let mut resources = store.get(&rtype).cloned().unwrap_or_default();

    // Filter by query parameters (skip FHIR special params starting with _)
    let filter_keys: Vec<String> = params
        ._rest
        .keys()
        .filter(|k| !k.starts_with('_'))
        .cloned()
        .collect();

    if !filter_keys.is_empty() {
        resources.retain(|r| {
            filter_keys.iter().all(|key| {
                let desired = &params._rest[key];
                match_field(r, key, desired)
            })
        });
    }

    let total = resources.len();
    if let Some(count) = params._count {
        resources.truncate(count as usize);
    }

    let entries: Vec<serde_json::Value> = resources
        .iter()
        .map(|r| {
            serde_json::json!({
                "resource": r,
                "fullUrl": format!("http://localhost/fhir/{}/{}", rtype, r["id"].as_str().unwrap_or_default())
            })
        })
        .collect();

    (
        StatusCode::OK,
        Json(serde_json::json!({
            "resourceType": "Bundle",
            "type": "searchset",
            "total": total,
            "entry": entries
        })),
    )
}

/// Try to match a search parameter against a resource field.
fn match_field(resource: &serde_json::Value, param: &str, value: &str) -> bool {
    let value_lower = value.to_lowercase();

    // Direct top-level match
    if let Some(v) = resource.get(param) {
        if json_contains(v, &value_lower) {
            return true;
        }
    }

    // Token-style: check coding.code and coding.display
    if param == "code" || param.ends_with("-code") {
        if let Some(codings) = find_all_codings(resource) {
            return codings.iter().any(|c| {
                c.get("code")
                    .or_else(|| c.get("display"))
                    .and_then(|v| v.as_str())
                    .map(|s| {
                        s.to_lowercase() == value_lower || s.to_lowercase().contains(&value_lower)
                    })
                    .unwrap_or(false)
            });
        }
    }

    // Name search: check HumanName arrays
    if param == "name" || param == "family" || param == "given" {
        if let Some(names) = resource.get("name").and_then(|n| n.as_array()) {
            for name in names {
                if let Some(family) = name.get("family").and_then(|f| f.as_str()) {
                    if family.to_lowercase().contains(&value_lower) {
                        return true;
                    }
                }
                if let Some(given) = name.get("given").and_then(|g| g.as_array()) {
                    for g in given {
                        if g.as_str()
                            .map(|s| s.to_lowercase().contains(&value_lower))
                            .unwrap_or(false)
                        {
                            return true;
                        }
                    }
                }
            }
        }
    }

    // Identifier search
    if param == "identifier" {
        if let Some(ids) = resource.get("identifier").and_then(|i| i.as_array()) {
            return ids.iter().any(|id| {
                id.get("value")
                    .and_then(|v| v.as_str())
                    .map(|s| s.to_lowercase().contains(&value_lower))
                    .unwrap_or(false)
            });
        }
    }

    false
}

/// Check if a JSON value contains a string (case-insensitive).
fn json_contains(value: &serde_json::Value, search: &str) -> bool {
    match value {
        serde_json::Value::String(s) => s.to_lowercase().contains(search),
        serde_json::Value::Bool(b) => search == b.to_string(),
        serde_json::Value::Number(n) => search == n.to_string(),
        serde_json::Value::Array(arr) => arr.iter().any(|v| json_contains(v, search)),
        serde_json::Value::Object(map) => map.values().any(|v| json_contains(v, search)),
        _ => false,
    }
}

/// Extract all Coding objects from code/CodeableConcept fields in a resource.
fn find_all_codings(resource: &serde_json::Value) -> Option<Vec<&serde_json::Value>> {
    let mut codings = Vec::new();
    for field in &["code", "type", "specialty", "role"] {
        if let Some(v) = resource.get(*field) {
            extract_codings_from_value(v, &mut codings);
        }
    }
    if codings.is_empty() {
        None
    } else {
        Some(codings)
    }
}

fn extract_codings_from_value<'a>(
    value: &'a serde_json::Value,
    codings: &mut Vec<&'a serde_json::Value>,
) {
    if let Some(coding_arr) = value.get("coding").and_then(|c| c.as_array()) {
        codings.extend(coding_arr.iter());
    } else if let Some(arr) = value.as_array() {
        for item in arr {
            extract_codings_from_value(item, codings);
        }
    }
}

async fn update_resource(
    State(store): State<FhirStore>,
    Path((rtype, id)): Path<(String, String)>,
    Json(mut body): Json<serde_json::Value>,
) -> (StatusCode, Json<serde_json::Value>) {
    body["id"] = serde_json::Value::String(id.clone());
    let mut store = store.lock().unwrap();
    let resources = store.entry(rtype.clone()).or_default();
    if let Some(idx) = resources
        .iter()
        .position(|r| r.get("id").and_then(|v| v.as_str()) == Some(&id))
    {
        resources[idx] = body.clone();
        (StatusCode::OK, Json(body))
    } else {
        // Update-as-create: resource doesn't exist yet, create it
        resources.push(body.clone());
        (StatusCode::CREATED, Json(body))
    }
}

async fn delete_resource(
    State(store): State<FhirStore>,
    Path((rtype, id)): Path<(String, String)>,
) -> (StatusCode, Json<serde_json::Value>) {
    let mut store = store.lock().unwrap();
    if let Some(resources) = store.get_mut(&rtype) {
        let before = resources.len();
        resources.retain(|r| r.get("id").and_then(|v| v.as_str()) != Some(&id));
        if resources.len() < before {
            return (
                StatusCode::OK,
                Json(serde_json::json!({
                    "resourceType": "OperationOutcome",
                    "issue": [{"severity": "information", "code": "informational", "diagnostics": "Deleted"}]
                })),
            );
        }
    }
    (
        StatusCode::NOT_FOUND,
        Json(serde_json::json!({
            "resourceType": "OperationOutcome",
            "issue": [{"severity": "error", "code": "not-found"}]
        })),
    )
}

/// Build the FHIR mock server axum Router.
/// Routes:
/// - `POST /fhir/{rtype}` — create resource
/// - `GET /fhir/{rtype}` — search resources
/// - `GET /fhir/{rtype}/{id}` — read resource
/// - `PUT /fhir/{rtype}/{id}` — update resource
/// - `DELETE /fhir/{rtype}/{id}` — delete resource
pub fn create_fhir_mock_app() -> Router {
    let store: FhirStore = Arc::new(Mutex::new(HashMap::new()));
    Router::new()
        .route("/fhir/{rtype}", post(create_resource))
        .route("/fhir/{rtype}", get(search_resources))
        .route("/fhir/{rtype}/{id}", get(read_resource))
        .route("/fhir/{rtype}/{id}", put(update_resource))
        .route("/fhir/{rtype}/{id}", delete(delete_resource))
        .with_state(store)
}

/// Start the FHIR mock server and return the address it's bound to.
///
/// The server runs in a background tokio task.
///
/// # Arguments
///
/// * `port` - Port to bind to (0 = random available port)
pub async fn start_fhir_mock_server(port: u16) -> anyhow::Result<SocketAddr> {
    let addr = std::net::Ipv4Addr::LOCALHOST;
    let listener = tokio::net::TcpListener::bind((addr, port)).await?;
    let bound_addr = listener.local_addr()?;

    let app = create_fhir_mock_app();
    tokio::spawn(async move {
        axum::serve(listener, app).await.unwrap();
    });

    // Give the server a moment to start accepting connections
    tokio::time::sleep(std::time::Duration::from_millis(50)).await;

    Ok(bound_addr)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn test_create_and_read_resource() {
        let addr = start_fhir_mock_server(0).await.unwrap();
        let client = reqwest::Client::new();
        let base = format!("http://{}", addr);

        // Create a Patient
        let resp = client
            .post(format!("{}/fhir/Patient", base))
            .json(&serde_json::json!({"resourceType": "Patient", "name": [{"family": "Smith"}]}))
            .send()
            .await
            .unwrap();
        assert_eq!(resp.status(), 201);
        let created: serde_json::Value = resp.json().await.unwrap();
        let id = created["id"].as_str().unwrap().to_string();

        // Read the Patient
        let resp = client
            .get(format!("{}/fhir/Patient/{}", base, id))
            .send()
            .await
            .unwrap();
        assert_eq!(resp.status(), 200);
        let read: serde_json::Value = resp.json().await.unwrap();
        assert_eq!(read["id"].as_str().unwrap(), &id);
        assert_eq!(read["name"][0]["family"].as_str().unwrap(), "Smith");
    }

    #[tokio::test]
    async fn test_search_resources() {
        let addr = start_fhir_mock_server(0).await.unwrap();
        let client = reqwest::Client::new();
        let base = format!("http://{}", addr);

        // Create two Patients
        client
            .post(format!("{}/fhir/Patient", base))
            .json(&serde_json::json!({"resourceType": "Patient", "name": [{"family": "Smith"}]}))
            .send()
            .await
            .unwrap();
        client
            .post(format!("{}/fhir/Patient", base))
            .json(&serde_json::json!({"resourceType": "Patient", "name": [{"family": "Jones"}]}))
            .send()
            .await
            .unwrap();

        // Search by family name
        let resp = client
            .get(format!("{}/fhir/Patient?family=Smith", base))
            .send()
            .await
            .unwrap();
        assert_eq!(resp.status(), 200);
        let bundle: serde_json::Value = resp.json().await.unwrap();
        assert_eq!(bundle["resourceType"].as_str().unwrap(), "Bundle");
        assert_eq!(bundle["total"].as_i64().unwrap(), 1);
    }

    #[tokio::test]
    async fn test_update_resource() {
        let addr = start_fhir_mock_server(0).await.unwrap();
        let client = reqwest::Client::new();
        let base = format!("http://{}", addr);

        // Create
        let resp = client
            .post(format!("{}/fhir/Patient", base))
            .json(&serde_json::json!({"resourceType": "Patient", "name": [{"family": "Smith"}]}))
            .send()
            .await
            .unwrap();
        let created: serde_json::Value = resp.json().await.unwrap();
        let id = created["id"].as_str().unwrap().to_string();

        // Update
        let resp = client
            .put(format!("{}/fhir/Patient/{}", base, id))
            .json(&serde_json::json!({"resourceType": "Patient", "name": [{"family": "Jones"}]}))
            .send()
            .await
            .unwrap();
        assert_eq!(resp.status(), 200);

        // Verify
        let resp = client
            .get(format!("{}/fhir/Patient/{}", base, id))
            .send()
            .await
            .unwrap();
        let read: serde_json::Value = resp.json().await.unwrap();
        assert_eq!(read["name"][0]["family"].as_str().unwrap(), "Jones");
    }

    #[tokio::test]
    async fn test_delete_resource() {
        let addr = start_fhir_mock_server(0).await.unwrap();
        let client = reqwest::Client::new();
        let base = format!("http://{}", addr);

        // Create
        let resp = client
            .post(format!("{}/fhir/Patient", base))
            .json(&serde_json::json!({"resourceType": "Patient", "name": [{"family": "Smith"}]}))
            .send()
            .await
            .unwrap();
        let created: serde_json::Value = resp.json().await.unwrap();
        let id = created["id"].as_str().unwrap().to_string();

        // Delete
        let resp = client
            .delete(format!("{}/fhir/Patient/{}", base, id))
            .send()
            .await
            .unwrap();
        assert_eq!(resp.status(), 200);

        // Verify deleted
        let resp = client
            .get(format!("{}/fhir/Patient/{}", base, id))
            .send()
            .await
            .unwrap();
        assert_eq!(resp.status(), 404);
    }
}
