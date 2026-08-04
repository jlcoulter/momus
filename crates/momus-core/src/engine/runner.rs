/// Execute a test plan against a target server.
use crate::ast::*;
use crate::engine::evaluator::evaluate_assertions;
use crate::engine::templates;
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
    /// HTTP client.
    pub client: reqwest::Client,
}

impl RunContext {
    pub fn new(base_url: String, default_headers: HashMap<String, String>) -> Self {
        Self {
            base_url,
            default_headers,
            step_responses: HashMap::new(),
            client: reqwest::Client::new(),
        }
    }
}

/// Execute a full test plan and return a report.
pub async fn execute_plan(plan: &TestPlan) -> Result<RunReport> {
    let start = Instant::now();
    let mut ctx = RunContext::new(plan.base_url.clone(), plan.default_headers.clone());
    let mut all_results: Vec<TestGroupResult> = Vec::new();

    // Run setup steps
    if !plan.setup.is_empty() {
        let setup_results = execute_steps(&plan.setup, &mut ctx).await?;
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

    // Run main steps
    let main_results = execute_steps(&plan.steps, &mut ctx).await?;
    if !main_results.is_empty() {
        all_results.push(TestGroupResult {
            name: plan.name.clone(),
            passed: main_results.iter().filter(|r| r.passed).count(),
            failed: main_results.iter().filter(|r| !r.passed).count(),
            total: main_results.len(),
            results: main_results,
        });
    }

    // Run teardown steps
    if !plan.teardown.is_empty() {
        let teardown_results = execute_steps(&plan.teardown, &mut ctx).await?;
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

    let total: usize = all_results.iter().map(|g| g.total).sum();
    let passed: usize = all_results.iter().map(|g| g.passed).sum();
    let failed: usize = all_results.iter().map(|g| g.failed).sum();

    Ok(RunReport {
        plan_name: plan.name.clone(),
        total,
        passed,
        failed,
        groups: all_results,
        duration_ms: start.elapsed().as_millis() as u64,
    })
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
            Step::Parallel(parallel_steps) => {
                // Execute parallel steps concurrently
                let futures: Vec<_> = parallel_steps
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
            Step::Script(_script) => {
                // Script steps are not yet implemented
                results.push(TestResult {
                    name: "script".into(),
                    passed: true,
                    status_code: 0,
                    request_method: String::new(),
                    request_url: String::new(),
                    request_headers: HashMap::new(),
                    request_body: None,
                    response_headers: HashMap::new(),
                    response_body: None,
                    assertion_results: vec![],
                    errors: vec!["script steps not yet implemented".into()],
                });
            }
            Step::Noop { .. } => {}
        }
    }

    Ok(results)
}

/// Execute a single parallel step (returns its results).
async fn execute_parallel_step(step: &Step, ctx: &RunContext) -> Result<Vec<TestResult>> {
    // Each parallel step gets its own context (no shared state)
    let mut sub_ctx = RunContext::new(ctx.base_url.clone(), ctx.default_headers.clone());
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

    // Build the HTTP request
    let method = req.method.to_string();
    let request_builder = match req.method {
        Method::Get => ctx.client.get(&full_url),
        Method::Post => {
            let mut rb = ctx.client.post(&full_url);
            if let Some(ref b) = body {
                rb = rb.json(b);
            }
            rb
        }
        Method::Put => {
            let mut rb = ctx.client.put(&full_url);
            if let Some(ref b) = body {
                rb = rb.json(b);
            }
            rb
        }
        Method::Delete => ctx.client.delete(&full_url),
        Method::Patch => {
            let mut rb = ctx.client.patch(&full_url);
            if let Some(ref b) = body {
                rb = rb.json(b);
            }
            rb
        }
        Method::Head => ctx.client.head(&full_url),
        Method::Options => ctx.client.request(reqwest::Method::OPTIONS, &full_url),
    };

    // Add headers
    let request_builder = request_builder.headers(
        all_headers
            .iter()
            .map(|(k, v)| {
                (
                    reqwest::header::HeaderName::from_bytes(k.as_bytes()).unwrap(),
                    reqwest::header::HeaderValue::from_str(v).unwrap(),
                )
            })
            .collect::<reqwest::header::HeaderMap>(),
    );

    // Execute
    let request_start = std::time::Instant::now();
    let response = match request_builder.send().await {
        Ok(resp) => resp,
        Err(e) => {
            return Ok(TestResult {
                name: req.name.clone(),
                passed: false,
                status_code: 0,
                request_method: method,
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
    let response_time_ms = request_start.elapsed().as_millis() as u64;

    let status_code = response.status().as_u16();
    let response_headers: HashMap<String, String> = response
        .headers()
        .iter()
        .map(|(k, v)| (k.to_string(), v.to_str().unwrap_or("").to_string()))
        .collect();
    let response_body: Option<serde_json::Value> = response.json().await.ok();

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
        request_method: method,
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
            Step::Parallel(parallel_steps) => {
                let futures: Vec<_> = parallel_steps
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
            Step::Script(_) => {
                vec![TestResult {
                    name: "script".into(),
                    passed: true,
                    status_code: 0,
                    request_method: String::new(),
                    request_url: String::new(),
                    request_headers: HashMap::new(),
                    request_body: None,
                    response_headers: HashMap::new(),
                    response_body: None,
                    assertion_results: vec![],
                    errors: vec!["script steps not yet implemented".into()],
                }]
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
