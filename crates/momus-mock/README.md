# momus-mock

[![Crates.io](https://img.shields.io/crates/v/momus-mock.svg)](https://crates.io/crates/momus-mock)
[![Docs.rs](https://img.shields.io/docsrs/momus-mock)](https://docs.rs/momus-mock)

**Configurable mock HTTP server for API testing.**

## What is this?

`momus-mock` provides a lightweight, programmatic mock HTTP server built on Axum. It supports configurable routes with canned responses, request recording for verification, dynamic response generation via handler functions, and a stateful CRUD store for resource-based APIs.

## Key Features

- **Configurable Routes** — map `"METHOD /path"` to canned `MockResponse` values
- **Request Recording** — all requests are recorded and accessible for verification
- **Custom Handlers** — dynamic response generation via `MockHandler` closures
- **Stateful CRUD Store** — in-memory resource store with create, read, update, delete, and search operations
- **Random Port Binding** — auto-assigns a port when `0` is specified
- **Graceful Shutdown** — clean server teardown via `stop()`

## Usage

```rust
use momus_mock::{MockServer, MockResponse};
use std::collections::HashMap;

#[tokio::main]
async fn main() {
    let mut routes = HashMap::new();
    routes.insert(
        "GET /health".into(),
        MockResponse::json(200, serde_json::json!({"status": "ok"})),
    );
    routes.insert(
        "POST /users".into(),
        MockResponse::json(201, serde_json::json!({"id": "abc-123"})),
    );

    let server = MockServer::start(routes).await;
    println!("Mock server running at {}", server.addr);

    // Use the server...
    let recorded = server.recorded_requests();
    println!("Received {} requests", recorded.len());

    server.stop();
}
```

### Stateful CRUD Store

```rust
use momus_mock::store::{new_store, create_resource, read_resource, search_resources};

let store = new_store();
let created = create_resource(&store, "Patient", serde_json::json!({"name": "John"}));
let id = created["id"].as_str().unwrap();

let resource = read_resource(&store, "Patient", id);
let results = search_resources(&store, "Patient", &std::collections::HashMap::new());
```

---

Part of the [Momus](https://github.com/jlcoulter/momus) project — a generic API test harness with a composable assertion AST.
