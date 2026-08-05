//! Transport adapter trait for multi-protocol support.
//!
//! Defines a generic interface for sending requests and receiving responses,
//! decoupling the engine from any specific protocol (HTTP, gRPC, etc.).

use crate::ast::Method;
use std::collections::HashMap;

/// A generic response from any transport.
#[derive(Debug, Clone)]
pub struct TransportResponse {
    /// Numeric status code (protocol-specific mapping).
    pub status_code: u16,
    /// Response headers.
    pub headers: HashMap<String, String>,
    /// Response body as JSON value (if parseable).
    pub body: Option<serde_json::Value>,
    /// Raw response body bytes.
    pub body_bytes: Vec<u8>,
    /// Elapsed time in milliseconds.
    pub elapsed_ms: u64,
}

/// A generic request to send via any transport.
#[derive(Debug, Clone)]
pub struct TransportRequest {
    /// HTTP-style method (maps to protocol-specific semantics).
    pub method: Method,
    /// Target URL / endpoint.
    pub url: String,
    /// Request headers.
    pub headers: HashMap<String, String>,
    /// Request body as JSON (if applicable).
    pub body: Option<serde_json::Value>,
}

/// Transport adapter trait.
///
/// Implement this trait to add support for new protocols (HTTP, gRPC, etc.).
#[async_trait::async_trait]
pub trait TransportAdapter: Send + Sync {
    /// Send a request and return a response.
    async fn send(&self, request: &TransportRequest) -> Result<TransportResponse, String>;
}

/// HTTP transport adapter using reqwest.
pub struct HttpAdapter {
    client: reqwest::Client,
}

impl HttpAdapter {
    /// Create a new HTTP adapter with default settings.
    pub fn new() -> Self {
        Self {
            client: reqwest::Client::new(),
        }
    }

    /// Create an HTTP adapter with a custom client.
    pub fn with_client(client: reqwest::Client) -> Self {
        Self { client }
    }
}

impl Default for HttpAdapter {
    fn default() -> Self {
        Self::new()
    }
}

#[async_trait::async_trait]
impl TransportAdapter for HttpAdapter {
    async fn send(&self, request: &TransportRequest) -> Result<TransportResponse, String> {
        let start = std::time::Instant::now();

        let mut rb = match request.method {
            Method::Get => self.client.get(&request.url),
            Method::Post => {
                let mut rb = self.client.post(&request.url);
                if let Some(ref b) = request.body {
                    rb = rb.json(b);
                }
                rb
            }
            Method::Put => {
                let mut rb = self.client.put(&request.url);
                if let Some(ref b) = request.body {
                    rb = rb.json(b);
                }
                rb
            }
            Method::Delete => self.client.delete(&request.url),
            Method::Patch => {
                let mut rb = self.client.patch(&request.url);
                if let Some(ref b) = request.body {
                    rb = rb.json(b);
                }
                rb
            }
            Method::Head => self.client.head(&request.url),
            Method::Options => self.client.request(reqwest::Method::OPTIONS, &request.url),
        };

        // Add headers
        for (k, v) in &request.headers {
            if let (Ok(name), Ok(value)) = (
                reqwest::header::HeaderName::from_bytes(k.as_bytes()),
                reqwest::header::HeaderValue::from_str(v),
            ) {
                rb = rb.header(name, value);
            }
        }

        let response = rb
            .send()
            .await
            .map_err(|e| format!("request failed: {e}"))?;
        let elapsed_ms = start.elapsed().as_millis() as u64;
        let status_code = response.status().as_u16();
        let headers: HashMap<String, String> = response
            .headers()
            .iter()
            .map(|(k, v)| (k.to_string(), v.to_str().unwrap_or("").to_string()))
            .collect();
        let body_bytes = response
            .bytes()
            .await
            .map_err(|e| format!("failed to read body: {e}"))?
            .to_vec();
        let body = serde_json::from_slice::<serde_json::Value>(&body_bytes).ok();

        Ok(TransportResponse {
            status_code,
            headers,
            body,
            body_bytes,
            elapsed_ms,
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_http_adapter_default() {
        let adapter = HttpAdapter::new();
        // Just verify it constructs without error
        let _ = adapter;
    }

    #[test]
    fn test_transport_request_construction() {
        let req = TransportRequest {
            method: Method::Get,
            url: "http://localhost:8080/health".into(),
            headers: HashMap::new(),
            body: None,
        };
        assert_eq!(req.url, "http://localhost:8080/health");
    }

    #[test]
    fn test_transport_response_construction() {
        let resp = TransportResponse {
            status_code: 200,
            headers: HashMap::new(),
            body: None,
            body_bytes: vec![],
            elapsed_ms: 42,
        };
        assert_eq!(resp.status_code, 200);
        assert_eq!(resp.elapsed_ms, 42);
    }
}
