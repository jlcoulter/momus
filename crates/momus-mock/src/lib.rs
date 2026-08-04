/// A configurable mock HTTP server for testing.
///
/// Supports:
/// - Configurable routes with canned responses
/// - Request recording for verification
/// - Dynamic response generation via handler functions
/// - Stateful CRUD store for resource-based APIs
pub mod store;
use axum::{
    Json, Router, extract::Request, http::StatusCode, response::IntoResponse, routing::any,
};
use std::collections::HashMap;
use std::sync::{Arc, Mutex};

/// Handler function type for custom mock server responses.
pub type MockHandler = Arc<dyn Fn(&RecordedRequest) -> MockResponse + Send + Sync>;

/// A recorded request.
#[derive(Debug, Clone, serde::Serialize, serde::Deserialize)]
pub struct RecordedRequest {
    pub method: String,
    pub uri: String,
    pub headers: HashMap<String, String>,
    pub body: Option<serde_json::Value>,
}

/// A canned response for a route.
#[derive(Debug, Clone)]
pub struct MockResponse {
    pub status: StatusCode,
    pub headers: HashMap<String, String>,
    pub body: serde_json::Value,
}

impl MockResponse {
    pub fn new(status: u16, body: serde_json::Value) -> Self {
        Self {
            status: StatusCode::from_u16(status).unwrap_or(StatusCode::OK),
            headers: HashMap::new(),
            body,
        }
    }

    pub fn json(status: u16, body: serde_json::Value) -> Self {
        let mut headers = HashMap::new();
        headers.insert("content-type".into(), "application/json".into());
        Self {
            status: StatusCode::from_u16(status).unwrap_or(StatusCode::OK),
            headers,
            body,
        }
    }
}

/// A mock server instance.
pub struct MockServer {
    pub addr: String,
    pub recorded: Arc<Mutex<Vec<RecordedRequest>>>,
    shutdown: tokio::sync::oneshot::Sender<()>,
}

impl MockServer {
    /// Start a mock server on a random port with the given routes.
    ///
    /// Routes are a map of `"METHOD /path"` → `MockResponse`.
    /// If no route matches, returns 404 with `{"error": "not found"}`.
    pub async fn start(routes: HashMap<String, MockResponse>) -> Self {
        Self::start_with_handler(routes, None).await
    }

    /// Start a mock server with a custom handler function.
    ///
    /// The handler receives the request and returns a response.
    /// If `None`, the default route-matching handler is used.
    pub async fn start_with_handler(
        routes: HashMap<String, MockResponse>,
        handler: Option<MockHandler>,
    ) -> Self {
        let recorded: Arc<Mutex<Vec<RecordedRequest>>> = Arc::new(Mutex::new(Vec::new()));
        let routes_arc = Arc::new(routes);
        let handler_arc = handler.map(Arc::new);

        let app = Router::new().route(
            "/{*path}",
            any({
                let recorded = recorded.clone();
                let routes = routes_arc.clone();
                let handler = handler_arc.clone();
                move |req: Request| {
                    let recorded = recorded.clone();
                    let routes = routes.clone();
                    let handler = handler.clone();
                    async move {
                        // Record the request
                        let method = req.method().to_string();
                        let uri = req.uri().to_string();
                        let headers: HashMap<String, String> = req
                            .headers()
                            .iter()
                            .map(|(k, v)| (k.to_string(), v.to_str().unwrap_or("").to_string()))
                            .collect();

                        let (_parts, body) = req.into_parts();
                        let body_bytes = http_body_util::BodyExt::collect(body)
                            .await
                            .map(|collected| collected.to_bytes())
                            .unwrap_or_default();
                        let body: Option<serde_json::Value> = if body_bytes.is_empty() {
                            None
                        } else {
                            serde_json::from_slice(&body_bytes).ok()
                        };

                        let recorded_req = RecordedRequest {
                            method: method.clone(),
                            uri: uri.clone(),
                            headers: headers.clone(),
                            body: body.clone(),
                        };

                        recorded.lock().unwrap().push(recorded_req);

                        // Determine response
                        if let Some(ref handler) = handler {
                            let resp = handler(&RecordedRequest {
                                method,
                                uri,
                                headers,
                                body,
                            });
                            let mut response = (resp.status, Json(resp.body)).into_response();
                            for (k, v) in &resp.headers {
                                response.headers_mut().insert(
                                    axum::http::HeaderName::from_bytes(k.as_bytes()).unwrap(),
                                    v.parse().unwrap(),
                                );
                            }
                            return response;
                        }

                        let path = uri.split('?').next().unwrap_or(&uri);
                        let route_key = format!("{} {}", method, path);
                        match routes.get(&route_key) {
                            Some(resp) => {
                                let mut response =
                                    (resp.status, Json(resp.body.clone())).into_response();
                                for (k, v) in &resp.headers {
                                    response.headers_mut().insert(
                                        axum::http::HeaderName::from_bytes(k.as_bytes()).unwrap(),
                                        v.parse().unwrap(),
                                    );
                                }
                                response
                            }
                            None => (
                                StatusCode::NOT_FOUND,
                                Json(serde_json::json!({"error": "not found"})),
                            )
                                .into_response(),
                        }
                    }
                }
            }),
        );

        let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr = format!("http://{}", listener.local_addr().unwrap());
        let (tx, rx) = tokio::sync::oneshot::channel::<()>();

        tokio::spawn(async move {
            let serve = axum::serve(listener, app);
            let graceful = serve.with_graceful_shutdown(async {
                rx.await.ok();
            });
            let _ = graceful.await;
        });

        // Wait for the server to be ready by making a test request
        let client = reqwest::Client::new();
        for _ in 0..20 {
            if client.get(&addr).send().await.is_ok() {
                break;
            }
            tokio::time::sleep(std::time::Duration::from_millis(50)).await;
        }
        // Clear any requests made during the readiness check
        recorded.lock().unwrap().clear();

        Self {
            addr,
            recorded,
            shutdown: tx,
        }
    }

    /// Get all recorded requests.
    pub fn recorded_requests(&self) -> Vec<RecordedRequest> {
        self.recorded.lock().unwrap().clone()
    }

    /// Clear recorded requests.
    pub fn clear_recorded(&self) {
        self.recorded.lock().unwrap().clear();
    }

    /// Stop the server.
    pub fn stop(self) {
        let _ = self.shutdown.send(());
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn test_mock_server_basic() {
        let mut routes = HashMap::new();
        routes.insert(
            "GET /test".into(),
            MockResponse::json(200, serde_json::json!({"status": "ok"})),
        );

        let server = MockServer::start(routes).await;

        let client = reqwest::Client::new();
        let resp = client
            .get(format!("{}/test", server.addr))
            .send()
            .await
            .unwrap();

        assert_eq!(resp.status(), 200);
        let body: serde_json::Value = resp.json().await.unwrap();
        assert_eq!(body, serde_json::json!({"status": "ok"}));

        let recorded = server.recorded_requests();
        assert_eq!(recorded.len(), 1);
        assert_eq!(recorded[0].method, "GET");
        assert!(recorded[0].uri.contains("/test"));

        server.stop();
    }

    #[tokio::test]
    async fn test_mock_server_404() {
        let server = MockServer::start(HashMap::new()).await;

        let client = reqwest::Client::new();
        let resp = client
            .get(format!("{}/nonexistent", server.addr))
            .send()
            .await
            .unwrap();

        assert_eq!(resp.status(), 404);

        server.stop();
    }

    #[tokio::test]
    async fn test_mock_server_post() {
        let mut routes = HashMap::new();
        routes.insert(
            "POST /data".into(),
            MockResponse::json(201, serde_json::json!({"id": "abc-123"})),
        );

        let server = MockServer::start(routes).await;

        let client = reqwest::Client::new();
        let resp = client
            .post(format!("{}/data", server.addr))
            .json(&serde_json::json!({"name": "test"}))
            .send()
            .await
            .unwrap();

        assert_eq!(resp.status(), 201);

        let recorded = server.recorded_requests();
        assert_eq!(recorded.len(), 1);
        assert_eq!(recorded[0].method, "POST");
        assert_eq!(recorded[0].body, Some(serde_json::json!({"name": "test"})));

        server.stop();
    }

    #[tokio::test]
    async fn test_mock_server_custom_handler() {
        let handler = Arc::new(|req: &RecordedRequest| {
            if req.uri.contains("echo") {
                MockResponse::json(200, req.body.clone().unwrap_or(serde_json::json!({})))
            } else {
                MockResponse::json(404, serde_json::json!({"error": "not found"}))
            }
        });

        let server = MockServer::start_with_handler(HashMap::new(), Some(handler)).await;

        let client = reqwest::Client::new();
        let resp = client
            .post(format!("{}/echo", server.addr))
            .json(&serde_json::json!({"hello": "world"}))
            .send()
            .await
            .unwrap();

        assert_eq!(resp.status(), 200);
        let body: serde_json::Value = resp.json().await.unwrap();
        assert_eq!(body, serde_json::json!({"hello": "world"}));

        server.stop();
    }
}
