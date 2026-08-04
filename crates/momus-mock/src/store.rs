/// A stateful in-memory resource store for the mock server.
///
/// Supports CRUD operations, search with query parameter filtering,
/// sorting, and pagination. Resources are stored by type in a
/// `HashMap<String, Vec<Value>>` behind an `Arc<Mutex<>>`.
use serde_json::Value;
use std::collections::HashMap;
use std::sync::{Arc, Mutex};

/// Thread-safe shared store for mock resources.
pub type MockStore = Arc<Mutex<HashMap<String, Vec<Value>>>>;

/// Create a new empty store.
pub fn new_store() -> MockStore {
    Arc::new(Mutex::new(HashMap::new()))
}

/// Create a resource (POST). Returns the created resource with an auto-generated ID.
pub fn create_resource(store: &MockStore, r#type: &str, mut body: Value) -> Value {
    let id = uuid::Uuid::new_v4().to_string();
    body["id"] = Value::String(id.clone());
    stamp_meta(&mut body);
    let mut store = store.lock().unwrap();
    store.entry(r#type.to_string()).or_default().push(body.clone());
    body
}

/// Read a resource by type and ID (GET).
pub fn read_resource(store: &MockStore, r#type: &str, id: &str) -> Option<Value> {
    let store = store.lock().unwrap();
    store.get(r#type)?.iter().find(|r| {
        r.get("id").and_then(|v| v.as_str()) == Some(id)
    }).cloned()
}

/// Update a resource by type and ID (PUT). Returns the updated resource.
pub fn update_resource(store: &MockStore, r#type: &str, id: &str, mut body: Value) -> Option<Value> {
    body["id"] = Value::String(id.to_string());
    stamp_meta(&mut body);
    let mut store = store.lock().unwrap();
    if let Some(resources) = store.get_mut(r#type) {
        if let Some(pos) = resources.iter().position(|r| {
            r.get("id").and_then(|v| v.as_str()) == Some(id)
        }) {
            resources[pos] = body.clone();
            return Some(body);
        }
    }
    None
}

/// Delete a resource by type and ID (DELETE). Returns true if found and deleted.
pub fn delete_resource(store: &MockStore, r#type: &str, id: &str) -> bool {
    let mut store = store.lock().unwrap();
    if let Some(resources) = store.get_mut(r#type) {
        if let Some(pos) = resources.iter().position(|r| {
            r.get("id").and_then(|v| v.as_str()) == Some(id)
        }) {
            resources.remove(pos);
            return true;
        }
    }
    false
}

/// Search resources by type with optional query parameter filtering (GET search).
///
/// Supports:
/// - Query parameter filtering (key=value matches on string fields)
/// - `_sort` — sort by a field (prefix `-` for descending)
/// - `_count` — limit results
/// - `_summary` — return only id, meta, resourceType
/// - `_elements` — return only specified fields
pub fn search_resources(
    store: &MockStore,
    r#type: &str,
    params: &HashMap<String, String>,
) -> Vec<Value> {
    let store = store.lock().unwrap();
    let mut resources: Vec<Value> = store.get(r#type).cloned().unwrap_or_default();

    // Filter by query parameters (skip FHIR special params starting with _)
    let filter_keys: Vec<String> = params
        .keys()
        .filter(|k| !k.starts_with('_'))
        .cloned()
        .collect();

    if !filter_keys.is_empty() {
        resources.retain(|r| {
            filter_keys.iter().all(|key| {
                let param_value = params.get(key).unwrap();
                match_field(r, key, param_value)
            })
        });
    }

    // Apply _sort
    if let Some(sort_param) = params.get("_sort") {
        let desc = sort_param.starts_with('-');
        let field = if desc { &sort_param[1..] } else { sort_param.as_str() };
        resources.sort_by(|a, b| {
            let a_val = resolve_field(a, field).cloned();
            let b_val = resolve_field(b, field).cloned();
            compare_values(&a_val, &b_val)
        });
        if desc {
            resources.reverse();
        }
    }

    // Apply _count
    if let Some(count_str) = params.get("_count") {
        if let Ok(count) = count_str.parse::<usize>() {
            resources.truncate(count);
        }
    }

    // Apply _summary
    if params.get("_summary").map(|s| s == "true").unwrap_or(false) {
        resources = resources
        .into_iter()
        .map(|r| {
            let mut summary = serde_json::json!({
                "resourceType": r.get("resourceType"),
                "id": r.get("id"),
                "meta": r.get("meta")
            });
            // Only keep non-null fields
            if let Some(obj) = summary.as_object_mut() {
                obj.retain(|_, v| !v.is_null());
            }
            summary
        })
        .collect();
    }

    // Apply _elements
    if let Some(elements_str) = params.get("_elements") {
        let elements: Vec<&str> = elements_str.split(',').map(|s| s.trim()).collect();
        resources = resources
            .into_iter()
            .map(|r| {
                let mut filtered = serde_json::json!({
                    "resourceType": r.get("resourceType"),
                    "id": r.get("id"),
                });
                for elem in &elements {
                    if let Some(val) = r.get(*elem) {
                        filtered[*elem] = val.clone();
                    }
                }
                filtered
            })
            .collect();
    }

    resources
}

/// Stamp meta fields (versionId, lastUpdated) on a resource.
fn stamp_meta(body: &mut Value) {
    if body.get("meta").is_none() {
        body["meta"] = serde_json::json!({});
    }
    let meta = body.get_mut("meta").unwrap();
    if meta.get("versionId").is_none() {
        meta["versionId"] = Value::String("1".to_string());
    }
    if meta.get("lastUpdated").is_none() {
        meta["lastUpdated"] = Value::String(chrono::Utc::now().to_rfc3339());
    }
}

/// Match a field value against a query parameter value.
/// Handles top-level fields, nested fields, and token/coding patterns.
fn match_field(resource: &Value, key: &str, param_value: &str) -> bool {
    // Try exact match on top-level field
    if let Some(val) = resource.get(key) {
        if value_matches(val, param_value) {
            return true;
        }
    }

    // Try nested field (e.g., "name.family")
    if key.contains('.') {
        if let Some(val) = resolve_field(resource, key) {
            if value_matches(&val, param_value) {
                return true;
            }
        }
    }

    // Try name array (e.g., resource has "name" array with "family" fields)
    if let Some(arr) = resource.get(key).and_then(|v| v.as_array()) {
        for item in arr {
            if let Some(family) = item.get("family").and_then(|v| v.as_str()) {
                if family == param_value {
                    return true;
                }
            }
            if let Some(text) = item.get("text").and_then(|v| v.as_str()) {
                if text == param_value {
                    return true;
                }
            }
        }
    }

    // Try identifier array (e.g., resource has "identifier" with "value" fields)
    if key == "identifier" || key.ends_with(".identifier") {
        if let Some(arr) = resource.get("identifier").and_then(|v| v.as_array()) {
            for item in arr {
                if let Some(val) = item.get("value").and_then(|v| v.as_str()) {
                    if val == param_value {
                        return true;
                    }
                }
            }
        }
    }

    false
}

/// Check if a JSON value matches a string parameter value.
fn value_matches(val: &Value, param: &str) -> bool {
    match val {
        Value::String(s) => s == param,
        Value::Number(n) => n.to_string() == param,
        Value::Bool(b) => b.to_string() == param,
        _ => false,
    }
}

/// Resolve a dotted field path on a JSON value.
fn resolve_field<'a>(value: &'a Value, path: &str) -> Option<&'a Value> {
    let parts: Vec<&str> = path.split('.').collect();
    let mut current = value;
    for part in parts {
        match current {
            Value::Object(obj) => {
                current = obj.get(part)?;
            }
            Value::Array(arr) => {
                // Try first element
                current = arr.first()?;
                if let Value::Object(obj) = current {
                    current = obj.get(part)?;
                } else {
                    return None;
                }
            }
            _ => return None,
        }
    }
    Some(current)
}

/// Compare two optional JSON values for sorting.
fn compare_values(a: &Option<Value>, b: &Option<Value>) -> std::cmp::Ordering {
    match (a, b) {
        (None, None) => std::cmp::Ordering::Equal,
        (None, Some(_)) => std::cmp::Ordering::Less,
        (Some(_), None) => std::cmp::Ordering::Greater,
        (Some(a_val), Some(b_val)) => {
            if let (Some(a_str), Some(b_str)) = (a_val.as_str(), b_val.as_str()) {
                a_str.cmp(b_str)
            } else if let (Some(a_num), Some(b_num)) = (a_val.as_f64(), b_val.as_f64()) {
                a_num.partial_cmp(&b_num).unwrap_or(std::cmp::Ordering::Equal)
            } else {
                std::cmp::Ordering::Equal
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn test_create_and_read() {
        let store = new_store();
        let created = create_resource(&store, "Patient", json!({"name": "John"}));
        let id = created["id"].as_str().unwrap().to_string();

        let read = read_resource(&store, "Patient", &id).unwrap();
        assert_eq!(read["name"], "John");
        assert_eq!(read["id"], id);
    }

    #[test]
    fn test_read_not_found() {
        let store = new_store();
        assert!(read_resource(&store, "Patient", "nonexistent").is_none());
    }

    #[test]
    fn test_update() {
        let store = new_store();
        let created = create_resource(&store, "Patient", json!({"name": "John"}));
        let id = created["id"].as_str().unwrap().to_string();

        let updated = update_resource(&store, "Patient", &id, json!({"name": "Jane"})).unwrap();
        assert_eq!(updated["name"], "Jane");

        let read = read_resource(&store, "Patient", &id).unwrap();
        assert_eq!(read["name"], "Jane");
    }

    #[test]
    fn test_update_not_found() {
        let store = new_store();
        assert!(update_resource(&store, "Patient", "nonexistent", json!({"name": "Jane"})).is_none());
    }

    #[test]
    fn test_delete() {
        let store = new_store();
        let created = create_resource(&store, "Patient", json!({"name": "John"}));
        let id = created["id"].as_str().unwrap().to_string();

        assert!(delete_resource(&store, "Patient", &id));
        assert!(read_resource(&store, "Patient", &id).is_none());
    }

    #[test]
    fn test_delete_not_found() {
        let store = new_store();
        assert!(!delete_resource(&store, "Patient", "nonexistent"));
    }

    #[test]
    fn test_search_no_params() {
        let store = new_store();
        create_resource(&store, "Patient", json!({"name": "John"}));
        create_resource(&store, "Patient", json!({"name": "Jane"}));

        let results = search_resources(&store, "Patient", &HashMap::new());
        assert_eq!(results.len(), 2);
    }

    #[test]
    fn test_search_with_filter() {
        let store = new_store();
        create_resource(&store, "Patient", json!({"name": "John", "gender": "male"}));
        create_resource(&store, "Patient", json!({"name": "Jane", "gender": "female"}));

        let mut params = HashMap::new();
        params.insert("gender".to_string(), "male".to_string());
        let results = search_resources(&store, "Patient", &params);
        assert_eq!(results.len(), 1);
        assert_eq!(results[0]["name"], "John");
    }

    #[test]
    fn test_search_with_sort() {
        let store = new_store();
        create_resource(&store, "Patient", json!({"name": "Charlie"}));
        create_resource(&store, "Patient", json!({"name": "Alice"}));
        create_resource(&store, "Patient", json!({"name": "Bob"}));

        let mut params = HashMap::new();
        params.insert("_sort".to_string(), "name".to_string());
        let results = search_resources(&store, "Patient", &params);
        assert_eq!(results.len(), 3);
        assert_eq!(results[0]["name"], "Alice");
        assert_eq!(results[1]["name"], "Bob");
        assert_eq!(results[2]["name"], "Charlie");
    }

    #[test]
    fn test_search_with_sort_desc() {
        let store = new_store();
        create_resource(&store, "Patient", json!({"name": "Alice"}));
        create_resource(&store, "Patient", json!({"name": "Bob"}));

        let mut params = HashMap::new();
        params.insert("_sort".to_string(), "-name".to_string());
        let results = search_resources(&store, "Patient", &params);
        assert_eq!(results[0]["name"], "Bob");
        assert_eq!(results[1]["name"], "Alice");
    }

    #[test]
    fn test_search_with_count() {
        let store = new_store();
        create_resource(&store, "Patient", json!({"name": "A"}));
        create_resource(&store, "Patient", json!({"name": "B"}));
        create_resource(&store, "Patient", json!({"name": "C"}));

        let mut params = HashMap::new();
        params.insert("_count".to_string(), "2".to_string());
        let results = search_resources(&store, "Patient", &params);
        assert_eq!(results.len(), 2);
    }

    #[test]
    fn test_search_empty_type() {
        let store = new_store();
        let results = search_resources(&store, "Nonexistent", &HashMap::new());
        assert!(results.is_empty());
    }

    #[test]
    fn test_stamp_meta() {
        let mut resource = json!({"resourceType": "Patient"});
        stamp_meta(&mut resource);
        assert!(resource["meta"]["versionId"].is_string());
        assert!(resource["meta"]["lastUpdated"].is_string());
    }

    #[test]
    fn test_create_assigns_id() {
        let store = new_store();
        let created = create_resource(&store, "Patient", json!({"name": "Test"}));
        assert!(created["id"].as_str().unwrap().len() > 0);
    }

    #[test]
    fn test_multiple_types() {
        let store = new_store();
        create_resource(&store, "Patient", json!({"name": "John"}));
        create_resource(&store, "Observation", json!({"value": 42}));

        assert_eq!(search_resources(&store, "Patient", &HashMap::new()).len(), 1);
        assert_eq!(search_resources(&store, "Observation", &HashMap::new()).len(), 1);
    }
}
