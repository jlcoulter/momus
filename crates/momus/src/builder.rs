//! Programmatic builder for constructing test plans.
//!
//! # Example
//!
//! ```rust
//! use momus::builder::*;
//! use momus_core::ast::Assertion;
//!
//! let plan = TestPlanBuilder::new("health check")
//!     .base_url("http://localhost:8080")
//!     .step(
//!         request("health")
//!             .get("/health")
//!             .assert(Assertion::Status(200))
//!             .assert(Assertion::valid_json())
//!             .build(),
//!     )
//!     .build();
//!
//! assert_eq!(plan.total_tests(), 1);
//! ```

use momus_core::ast::*;
use std::collections::HashMap;

/// Builder for constructing a `TestPlan`.
#[derive(Debug, Clone)]
pub struct TestPlanBuilder {
    name: String,
    base_url: String,
    default_headers: HashMap<String, String>,
    steps: Vec<Step>,
    setup: Vec<Step>,
    teardown: Vec<Step>,
}

impl TestPlanBuilder {
    /// Start building a new test plan with the given name.
    pub fn new(name: impl Into<String>) -> Self {
        Self {
            name: name.into(),
            base_url: String::new(),
            default_headers: HashMap::new(),
            steps: Vec::new(),
            setup: Vec::new(),
            teardown: Vec::new(),
        }
    }

    /// Set the base URL for all requests.
    pub fn base_url(mut self, url: impl Into<String>) -> Self {
        self.base_url = url.into();
        self
    }

    /// Add a default header applied to every request.
    pub fn default_header(mut self, key: impl Into<String>, value: impl Into<String>) -> Self {
        self.default_headers.insert(key.into(), value.into());
        self
    }

    /// Add a step to the plan.
    pub fn step(mut self, step: Step) -> Self {
        self.steps.push(step);
        self
    }

    /// Add a setup step (runs before all tests).
    pub fn setup(mut self, step: Step) -> Self {
        self.setup.push(step);
        self
    }

    /// Add a teardown step (runs after all tests).
    pub fn teardown(mut self, step: Step) -> Self {
        self.teardown.push(step);
        self
    }

    /// Consume the builder and produce a `TestPlan`.
    pub fn build(self) -> TestPlan {
        TestPlan {
            name: self.name,
            base_url: self.base_url,
            default_headers: self.default_headers,
            steps: self.steps,
            setup: self.setup,
            teardown: self.teardown,
        }
    }
}

/// Create a request step builder.
pub fn request(name: impl Into<String>) -> RequestStepBuilder {
    RequestStepBuilder {
        name: name.into(),
        method: Method::Get,
        url: String::new(),
        headers: HashMap::new(),
        body: None,
        assert: Vec::new(),
        save_as: String::new(),
        soft_fail: false,
    }
}

/// Builder for constructing a `RequestStep`.
#[derive(Debug, Clone)]
pub struct RequestStepBuilder {
    name: String,
    method: Method,
    url: String,
    headers: HashMap<String, String>,
    body: Option<serde_json::Value>,
    assert: Vec<Assertion>,
    save_as: String,
    soft_fail: bool,
}

impl RequestStepBuilder {
    /// Set the HTTP method to GET.
    pub fn get(mut self, url: impl Into<String>) -> Self {
        self.method = Method::Get;
        self.url = url.into();
        self
    }

    /// Set the HTTP method to POST.
    pub fn post(mut self, url: impl Into<String>) -> Self {
        self.method = Method::Post;
        self.url = url.into();
        self
    }

    /// Set the HTTP method to PUT.
    pub fn put(mut self, url: impl Into<String>) -> Self {
        self.method = Method::Put;
        self.url = url.into();
        self
    }

    /// Set the HTTP method to DELETE.
    pub fn delete(mut self, url: impl Into<String>) -> Self {
        self.method = Method::Delete;
        self.url = url.into();
        self
    }

    /// Set the HTTP method to PATCH.
    pub fn patch(mut self, url: impl Into<String>) -> Self {
        self.method = Method::Patch;
        self.url = url.into();
        self
    }

    /// Set the request body as JSON.
    pub fn body(mut self, body: serde_json::Value) -> Self {
        self.body = Some(body);
        self
    }

    /// Add a header.
    pub fn header(mut self, key: impl Into<String>, value: impl Into<String>) -> Self {
        self.headers.insert(key.into(), value.into());
        self
    }

    /// Add an assertion.
    pub fn assert(mut self, assertion: Assertion) -> Self {
        self.assert.push(assertion);
        self
    }

    /// Save the response under this name for template references.
    pub fn save_as(mut self, name: impl Into<String>) -> Self {
        self.save_as = name.into();
        self
    }

    /// Mark this step as soft-fail (does not abort sequence on failure).
    pub fn soft_fail(mut self) -> Self {
        self.soft_fail = true;
        self
    }

    /// Consume the builder and produce a `Step::Request`.
    pub fn build(self) -> Step {
        Step::Request(RequestStep {
            name: self.name,
            method: self.method,
            url: self.url,
            headers: self.headers,
            body: self.body,
            assert: self.assert,
            save_as: self.save_as,
            soft_fail: self.soft_fail,
        })
    }
}

/// Create a sequence step.
pub fn sequence(name: impl Into<String>) -> SequenceStepBuilder {
    SequenceStepBuilder {
        name: name.into(),
        steps: Vec::new(),
        continue_on_failure: false,
    }
}

/// Builder for constructing a `SequenceStep`.
#[derive(Debug, Clone)]
pub struct SequenceStepBuilder {
    name: String,
    steps: Vec<Step>,
    continue_on_failure: bool,
}

impl SequenceStepBuilder {
    /// Add a sub-step to the sequence.
    pub fn step(mut self, step: Step) -> Self {
        self.steps.push(step);
        self
    }

    /// Continue executing remaining steps even if one fails.
    pub fn continue_on_failure(mut self) -> Self {
        self.continue_on_failure = true;
        self
    }

    /// Consume the builder and produce a `Step::Sequence`.
    pub fn build(self) -> Step {
        Step::Sequence(SequenceStep {
            name: self.name,
            steps: self.steps,
            continue_on_failure: self.continue_on_failure,
        })
    }
}

/// Create a parallel step group.
pub fn parallel(steps: Vec<Step>) -> Step {
    Step::Parallel(ParallelStep { steps })
}

/// Create a no-op step (placeholder / disabled test).
pub fn noop(description: impl Into<String>) -> Step {
    Step::Noop {
        description: description.into(),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_builder_simple_plan() {
        let plan = TestPlanBuilder::new("test")
            .base_url("http://localhost:8080")
            .default_header("Accept", "application/json")
            .step(
                request("health")
                    .get("/health")
                    .assert(Assertion::Status(200))
                    .assert(Assertion::valid_json())
                    .build(),
            )
            .build();

        assert_eq!(plan.name, "test");
        assert_eq!(plan.base_url, "http://localhost:8080");
        assert_eq!(plan.default_headers.len(), 1);
        assert_eq!(plan.total_tests(), 1);
    }

    #[test]
    fn test_builder_sequence() {
        let plan = TestPlanBuilder::new("crud")
            .base_url("http://localhost:8080")
            .step(
                sequence("items")
                    .step(
                        request("create")
                            .post("/items")
                            .body(serde_json::json!({"name": "test"}))
                            .assert(Assertion::Status(201))
                            .save_as("created")
                            .build(),
                    )
                    .step(
                        request("read")
                            .get("/items/{steps.created.id}")
                            .assert(Assertion::Status(200))
                            .build(),
                    )
                    .continue_on_failure()
                    .build(),
            )
            .build();

        assert_eq!(plan.total_tests(), 2);
    }

    #[test]
    fn test_builder_parallel() {
        let plan = TestPlanBuilder::new("parallel")
            .base_url("http://localhost:8080")
            .step(parallel(vec![
                request("a")
                    .get("/a")
                    .assert(Assertion::Status(200))
                    .build(),
                request("b")
                    .get("/b")
                    .assert(Assertion::Status(200))
                    .build(),
            ]))
            .build();

        assert_eq!(plan.total_tests(), 2);
    }

    #[test]
    fn test_builder_setup_teardown() {
        let plan = TestPlanBuilder::new("with-setup")
            .base_url("http://localhost:8080")
            .setup(
                request("init")
                    .post("/init")
                    .assert(Assertion::Status(200))
                    .build(),
            )
            .step(
                request("test")
                    .get("/test")
                    .assert(Assertion::Status(200))
                    .build(),
            )
            .teardown(
                request("cleanup")
                    .delete("/cleanup")
                    .assert(Assertion::Status(204))
                    .build(),
            )
            .build();

        assert_eq!(plan.setup.len(), 1);
        assert_eq!(plan.steps.len(), 1);
        assert_eq!(plan.teardown.len(), 1);
    }
}
