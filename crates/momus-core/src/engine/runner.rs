/// Execute a test plan against a target server.
use crate::ast::*;
use crate::engine::evaluator::evaluate_assertions;
use crate::engine::templates;
use crate::transport::{TransportAdapter, TransportRequest};
use anyhow::Result;
use std::collections::HashMap;
use std::time::Instant;

/// Context passed through step execution.
pub struct RunContext {
    /// Base URL for requests.
    pub base_url: String,
    /// Default headers applied to all requests.
    pub default_headers: HashMap<String, String>,
    /// Saved responses from named steps (keyed by save_as name).
    pub step_responses: HashMap<String, serde_json::Value>,
    /// Transport adapter for sending requests.
    pub transport: Box<dyn TransportAdapter>,
}

impl RunContext {
    pub fn new(
        base_url: String,
        default_headers: HashMap<String, String>,
        transport: Box<dyn TransportAdapter>,
    ) -> Self {
        Self {
            base_url,
            default_headers,
            step_responses: HashMap::new(),
            transport,
        }
    }
}

/// Execute a full test plan and return a report.
///
/// Teardown steps always run, even if setup or main steps fail.
/// The first error encountered (if any) is returned after teardown completes.
///
/// `timeout_secs` sets the per-request timeout on the HTTP client (defaults to 30).
pub async fn execute_plan(plan: &TestPlan) -> Result<RunReport> {
    execute_plan_with_timeout(plan, 30).await
}

/// Execute a test plan with a configurable per-request timeout.
pub async fn execute_plan_with_timeout(plan: &TestPlan, timeout_secs: u64) -> Result<RunReport> {
    let start = Instant::now();
    let client = reqwest::Client::builder()
        .timeout(std::time::Duration::from_secs(timeout_secs))
        .build()
        .map_err(|e| anyhow::anyhow!("Failed to build HTTP client: {e}"))?;
    let transport: Box<dyn TransportAdapter> =
        Box::new(crate::transport::HttpAdapter::with_client(client));
    let mut ctx = RunContext::new(
        plan.base_url.clone(),
        plan.default_headers.clone(),
        transport,
    );
    let mut all_results: Vec<TestGroupResult> = Vec::new();
    let mut first_error: Option<anyhow::Error> = None;

    // Run setup steps
    if !plan.setup.is_empty() {
        match execute_steps(&plan.setup, &mut ctx).await {
            Ok(setup_results) => {
                if !setup_results.is_empty() {
                    all_results.push(TestGroupResult {
                        name: "_setup".into(),
                        passed: setup_results.iter().filter(|r| r.passed).count(),
                        failed: setup_results.iter().filter(|r| !r.passed).count(),
                        total: setup_results.len(),
                        results: setup_results,
                    });
                }
            }
            Err(e) => {
                first_error = Some(e);
            }
        }
    }

    // Run main steps (only if setup didn't error)
    if first_error.is_none() && !plan.steps.is_empty() {
        match execute_steps(&plan.steps, &mut ctx).await {
            Ok(main_results) => {
                if !main_results.is_empty() {
                    all_results.push(TestGroupResult {
                        name: plan.name.clone(),
                        passed: main_results.iter().filter(|r| r.passed).count(),
                        failed: main_results.iter().filter(|r| !r.passed).count(),
                        total: main_results.len(),
                        results: main_results,
                    });
                }
            }
            Err(e) => {
                first_error = Some(e);
            }
        }
    }

    // Run teardown steps — always, even on failure
    if !plan.teardown.is_empty() {
        match execute_steps(&plan.teardown, &mut ctx).await {
            Ok(teardown_results) => {
                if !teardown_results.is_empty() {
                    all_results.push(TestGroupResult {
                        name: "_teardown".into(),
                        passed: teardown_results.iter().filter(|r| r.passed).count(),
                        failed: teardown_results.iter().filter(|r| !r.passed).count(),
                        total: teardown_results.len(),
                        results: teardown_results,
                    });
                }
            }
            Err(e) => {
                // Prefer the first error, but log the teardown error
                if first_error.is_none() {
                    first_error = Some(e);
                }
            }
        }
    }

    let total: usize = all_results.iter().map(|g| g.total).sum();
    let passed: usize = all_results.iter().map(|g| g.passed).sum();
    let failed: usize = all_results.iter().map(|g| g.failed).sum();

    let report = RunReport {
        plan_name: plan.name.clone(),
        total,
        passed,
        failed,
        groups: all_results,
        duration_ms: start.elapsed().as_millis() as u64,
    };

    // Return the first error if any, otherwise the report
    if let Some(err) = first_error {
        Err(err)
    } else {
        Ok(report)
    }
}

/// Execute a list of steps, collecting results.
async fn execute_steps(steps: &[Step], ctx: &mut RunContext) -> Result<Vec<TestResult>> {
    let mut results = Vec::new();

    for step in steps {
        match step {
            Step::Request(req) => {
                let result = execute_request(req, ctx).await?;
                results.push(result);
            }
            Step::Sequence(seq) => {
                let sub_results = execute_sequence(seq, ctx).await?;
                results.extend(sub_results);
            }
            Step::Parallel(par) => {
                // Execute parallel steps concurrently
                let futures: Vec<_> = par
                    .steps
                    .iter()
                    .map(|s| execute_parallel_step(s, ctx))
                    .collect();
                let sub_results: Vec<Vec<TestResult>> = futures::future::join_all(futures)
                    .await
                    .into_iter()
                    .collect::<Result<Vec<_>>>()?;
                for mut sr in sub_results {
                    results.append(&mut sr);
                }
            }
            Step::Script(script) => {
                let result = crate::engine::script::execute_script(script, &ctx.step_responses);
                results.push(result);
            }
            Step::Noop { .. } => {}
        }
    }

    Ok(results)
}

/// Execute a single parallel step (returns its results).
async fn execute_parallel_step(step: &Step, ctx: &RunContext) -> Result<Vec<TestResult>> {
    // Each parallel step gets its own context (no shared state)
    let transport: Box<dyn TransportAdapter> = Box::new(crate::transport::HttpAdapter::new());
    let mut sub_ctx = RunContext::new(ctx.base_url.clone(), ctx.default_headers.clone(), transport);
    let steps = match step {
        Step::Sequence(seq) => &seq.steps,
        other => std::slice::from_ref(other),
    };
    execute_steps(steps, &mut sub_ctx).await
}

/// Execute a single request step.
async fn execute_request(req: &RequestStep, ctx: &mut RunContext) -> Result<TestResult> {
    // Resolve templates
    let url = templates::resolve_url(&req.url, &ctx.base_url, &ctx.step_responses);

    // If the URL is relative, prepend the base URL
    let full_url = if url.starts_with('/') {
        format!("{}{}", ctx.base_url.trim_end_matches('/'), url)
    } else if url.starts_with("http://") || url.starts_with("https://") {
        url.clone()
    } else {
        format!("{}/{}", ctx.base_url.trim_end_matches('/'), url)
    };
    let mut headers = req.headers.clone();
    templates::resolve_headers(&mut headers, &ctx.step_responses);

    // Merge default headers (request headers override defaults)
    let mut all_headers = ctx.default_headers.clone();
    for (k, v) in headers {
        all_headers.insert(k, v);
    }

    // Resolve body templates
    let mut body = req.body.clone();
    if let Some(ref mut b) = body {
        templates::resolve_body(b, &ctx.step_responses);
    }

    // Build the transport request
    let method = req.method;
    let transport_request = TransportRequest {
        method,
        url: full_url.clone(),
        headers: all_headers.clone(),
        body: body.clone(),
    };

    // Execute via transport adapter
    let transport_response = match ctx.transport.send(&transport_request).await {
        Ok(resp) => resp,
        Err(e) => {
            return Ok(TestResult {
                name: req.name.clone(),
                passed: false,
                status_code: 0,
                request_method: method.to_string(),
                request_url: full_url.clone(),
                request_headers: all_headers,
                request_body: body,
                response_headers: HashMap::new(),
                response_body: None,
                assertion_results: vec![],
                errors: vec![format!("request failed: {e}")],
            });
        }
    };

    let status_code = transport_response.status_code;
    let response_headers = transport_response.headers;
    let response_body = transport_response.body;
    let response_time_ms = transport_response.elapsed_ms;

    // Evaluate assertions
    let assertion_results = evaluate_assertions(
        &req.assert,
        status_code,
        &response_headers,
        &response_body,
        response_time_ms,
    );

    let passed = assertion_results.iter().all(|a| a.passed);
    let errors: Vec<String> = assertion_results
        .iter()
        .filter(|a| !a.passed)
        .map(|a| a.message.clone().unwrap_or_default())
        .collect();

    // Save response if requested
    if !req.save_as.is_empty()
        && let Some(ref body) = response_body
    {
        ctx.step_responses.insert(req.save_as.clone(), body.clone());
    }

    Ok(TestResult {
        name: req.name.clone(),
        passed,
        status_code,
        request_method: method.to_string(),
        request_url: full_url,
        request_headers: all_headers,
        request_body: body,
        response_headers,
        response_body,
        assertion_results,
        errors,
    })
}

/// Execute a sequence of steps with state passing.
async fn execute_sequence(seq: &SequenceStep, ctx: &mut RunContext) -> Result<Vec<TestResult>> {
    let mut results = Vec::new();

    for step in &seq.steps {
        let sub_results = match step {
            Step::Request(req) => {
                vec![execute_request(req, ctx).await?]
            }
            Step::Sequence(sub_seq) => Box::pin(execute_sequence(sub_seq, ctx)).await?,
            Step::Parallel(par) => {
                let futures: Vec<_> = par
                    .steps
                    .iter()
                    .map(|s| execute_parallel_step(s, ctx))
                    .collect();
                futures::future::join_all(futures)
                    .await
                    .into_iter()
                    .collect::<Result<Vec<_>>>()?
                    .into_iter()
                    .flatten()
                    .collect()
            }
            Step::Script(script) => {
                vec![crate::engine::script::execute_script(
                    script,
                    &ctx.step_responses,
                )]
            }
            Step::Noop { .. } => vec![],
        };

        let all_passed = sub_results.iter().all(|r| r.passed);
        results.extend(sub_results);

        // Stop on failure unless continue_on_failure is set
        if !all_passed && !seq.continue_on_failure {
            break;
        }
    }

    Ok(results)
}

#[cfg(test)]
mod tests {
    use super::*;
    use momus_mock::MockServer;

    #[tokio::test]
    async fn test_execute_simple_request() {
        let server = MockServer::start({
            let mut routes = std::collections::HashMap::new();
            routes.insert(
                "GET /api/health".into(),
                momus_mock::MockResponse::json(200, serde_json::json!({"status": "ok"})),
            );
            routes
        })
        .await;

        let plan = TestPlan {
            name: "health check".into(),
            base_url: server.addr.clone(),
            default_headers: HashMap::new(),
            steps: vec![Step::Request(RequestStep {
                name: "health".into(),
                method: Method::Get,
                url: "/api/health".into(),
                headers: HashMap::new(),
                body: None,
                assert: vec![
                    Assertion::Status(200),
                    Assertion::json_path_eq("$.status", serde_json::json!("ok")),
                ],
                save_as: String::new(),
                soft_fail: false,
            })],
            setup: vec![],
            teardown: vec![],
        };

        let report = execute_plan(&plan).await.unwrap();
        assert_eq!(report.total, 1);
        assert_eq!(report.passed, 1);
        assert_eq!(report.failed, 0);

        server.stop();
    }

    #[tokio::test]
    async fn test_execute_sequence() {
        let server = MockServer::start({
            let mut routes = std::collections::HashMap::new();
            routes.insert(
                "POST /api/items".into(),
                momus_mock::MockResponse::json(201, serde_json::json!({"id": "item-001"})),
            );
            routes.insert(
                "GET /api/items/item-001".into(),
                momus_mock::MockResponse::json(
                    200,
                    serde_json::json!({"id": "item-001", "name": "test"}),
                ),
            );
            routes
        })
        .await;

        let plan = TestPlan {
            name: "create then read".into(),
            base_url: server.addr.clone(),
            default_headers: HashMap::new(),
            steps: vec![Step::Sequence(SequenceStep {
                name: "crud".into(),
                steps: vec![
                    Step::Request(RequestStep {
                        name: "create".into(),
                        method: Method::Post,
                        url: "/api/items".into(),
                        headers: HashMap::new(),
                        body: Some(serde_json::json!({"name": "test"})),
                        assert: vec![Assertion::Status(201)],
                        save_as: "create_item".into(),
                        soft_fail: false,
                    }),
                    Step::Request(RequestStep {
                        name: "read".into(),
                        method: Method::Get,
                        url: "/api/items/{steps.create_item.id}".into(),
                        headers: HashMap::new(),
                        body: None,
                        assert: vec![
                            Assertion::Status(200),
                            Assertion::json_path_eq("$.name", serde_json::json!("test")),
                        ],
                        save_as: String::new(),
                        soft_fail: false,
                    }),
                ],
                continue_on_failure: false,
            })],
            setup: vec![],
            teardown: vec![],
        };

        let report = execute_plan(&plan).await.unwrap();
        assert_eq!(report.total, 2);
        assert_eq!(report.passed, 2);
        assert_eq!(report.failed, 0);

        server.stop();
    }

    #[tokio::test]
    async fn test_execute_failing_assertion() {
        let server = MockServer::start({
            let mut routes = std::collections::HashMap::new();
            routes.insert(
                "GET /api/data".into(),
                momus_mock::MockResponse::json(200, serde_json::json!({"value": 42})),
            );
            routes
        })
        .await;

        let plan = TestPlan {
            name: "fail test".into(),
            base_url: server.addr.clone(),
            default_headers: HashMap::new(),
            steps: vec![Step::Request(RequestStep {
                name: "check".into(),
                method: Method::Get,
                url: "/api/data".into(),
                headers: HashMap::new(),
                body: None,
                assert: vec![
                    Assertion::Status(200),
                    Assertion::json_path_eq("$.value", serde_json::json!(99)),
                ],
                save_as: String::new(),
                soft_fail: false,
            })],
            setup: vec![],
            teardown: vec![],
        };

        let report = execute_plan(&plan).await.unwrap();
        assert_eq!(report.total, 1);
        assert_eq!(report.passed, 0);
        assert_eq!(report.failed, 1);

        server.stop();
    }

    #[tokio::test]
    async fn test_execute_with_setup_and_teardown() {
        let server = MockServer::start({
            let mut routes = std::collections::HashMap::new();
            routes.insert(
                "POST /api/setup".into(),
                momus_mock::MockResponse::json(200, serde_json::json!({"status": "ready"})),
            );
            routes.insert(
                "GET /api/test".into(),
                momus_mock::MockResponse::json(200, serde_json::json!({"result": "pass"})),
            );
            routes.insert(
                "POST /api/teardown".into(),
                momus_mock::MockResponse::json(200, serde_json::json!({"status": "cleaned"})),
            );
            routes
        })
        .await;

        let plan = TestPlan {
            name: "with setup/teardown".into(),
            base_url: server.addr.clone(),
            default_headers: HashMap::new(),
            setup: vec![Step::Request(RequestStep {
                name: "setup".into(),
                method: Method::Post,
                url: "/api/setup".into(),
                headers: HashMap::new(),
                body: None,
                assert: vec![Assertion::Status(200)],
                save_as: String::new(),
                soft_fail: false,
            })],
            steps: vec![Step::Request(RequestStep {
                name: "test".into(),
                method: Method::Get,
                url: "/api/test".into(),
                headers: HashMap::new(),
                body: None,
                assert: vec![Assertion::Status(200)],
                save_as: String::new(),
                soft_fail: false,
            })],
            teardown: vec![Step::Request(RequestStep {
                name: "teardown".into(),
                method: Method::Post,
                url: "/api/teardown".into(),
                headers: HashMap::new(),
                body: None,
                assert: vec![Assertion::Status(200)],
                save_as: String::new(),
                soft_fail: false,
            })],
        };

        let report = execute_plan(&plan).await.unwrap();
        assert_eq!(report.total, 3);
        assert_eq!(report.passed, 3);
        assert_eq!(report.failed, 0);

        server.stop();
    }
}
