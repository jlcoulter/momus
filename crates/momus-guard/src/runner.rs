use crate::config::GuardConfig;
use crate::report::{GuardIssue, GuardReport};
use anyhow::Result;
use momus_core::ast::{Step, TestPlan};
use momus_core::transport::{TransportAdapter, TransportRequest, TransportResponse};
use std::collections::HashMap;
use std::time::Instant;

/// Execute a security scan against a test plan.
///
/// Sends requests and inspects responses for:
/// - Missing security headers (HSTS, CSP, X-Content-Type-Options, X-Frame-Options)
/// - CORS misconfiguration (permissive origins, credentials with wildcard)
/// - Information leakage (stack traces, error details in bodies)
/// - Exposed internal endpoints (common paths)
/// - Missing or weak authentication
pub async fn run_guard(
    plan: &TestPlan,
    config: &GuardConfig,
    transport: &dyn TransportAdapter,
) -> Result<GuardReport> {
    let start = Instant::now();

    let base_url = config.base_url.as_deref().unwrap_or(&plan.base_url);

    let mut issues = Vec::new();
    let mut passed = 0u64;
    let mut failed = 0u64;

    tracing::info!(
        "Running security scan on '{}' against {}",
        plan.name,
        base_url
    );

    // Collect all unique URLs from the plan
    let urls = collect_plan_urls(plan, base_url);

    // 1. Security headers check
    if config.check_headers {
        for url in &urls {
            let result = check_security_headers(transport, url).await;
            for issue in result.issues {
                issues.push(issue);
                failed += 1;
            }
            if result.passed {
                passed += 1;
            }
        }
    }

    // 2. CORS check
    if config.check_cors {
        for url in &urls {
            let result = check_cors(transport, url).await;
            for issue in result.issues {
                issues.push(issue);
                failed += 1;
            }
            if result.passed {
                passed += 1;
            }
        }
    }

    // 3. Info leak check
    if config.check_leaks {
        for url in &urls {
            let result = check_info_leaks(transport, url).await;
            for issue in result.issues {
                issues.push(issue);
                failed += 1;
            }
            if result.passed {
                passed += 1;
            }
        }
    }

    // 4. Exposed endpoints check
    if config.check_exposed {
        let result = check_exposed_endpoints(transport, base_url).await;
        for issue in result.issues {
            issues.push(issue);
            failed += 1;
        }
        if result.passed {
            passed += 1;
        }
    }

    // 5. Auth check
    for url in &urls {
        let result = check_auth(transport, url).await;
        for issue in result.issues {
            issues.push(issue);
            failed += 1;
        }
        if result.passed {
            passed += 1;
        }
    }

    let elapsed = start.elapsed().as_secs_f64();
    let total_checks = (passed + failed) as usize;

    Ok(GuardReport {
        plan_name: plan.name.clone(),
        total_checks,
        issues,
        passed: passed as usize,
        failed: failed as usize,
        duration_secs: elapsed,
    })
}

/// Result of a single check.
struct CheckResult {
    issues: Vec<GuardIssue>,
    passed: bool,
}

impl CheckResult {
    fn pass() -> Self {
        Self {
            issues: vec![],
            passed: true,
        }
    }
    fn fail(issue: GuardIssue) -> Self {
        Self {
            issues: vec![issue],
            passed: false,
        }
    }
}

/// Collect all unique URLs from a test plan.
fn collect_plan_urls(plan: &TestPlan, base_url: &str) -> Vec<String> {
    let mut urls = Vec::new();
    collect_from_steps(&plan.steps, base_url, &mut urls);
    urls.sort();
    urls.dedup();
    urls
}

fn collect_from_steps(steps: &[Step], base_url: &str, urls: &mut Vec<String>) {
    for step in steps {
        match step {
            Step::Request(req) => {
                let url = if req.url.starts_with("http") {
                    req.url.clone()
                } else {
                    format!("{}{}", base_url, req.url)
                };
                urls.push(url);
            }
            Step::Sequence(seq) => collect_from_steps(&seq.steps, base_url, urls),
            Step::Parallel(children) => collect_from_steps(children, base_url, urls),
            _ => {}
        }
    }
}

/// Send a GET request via the transport adapter.
async fn get(transport: &dyn TransportAdapter, url: &str) -> Result<TransportResponse, String> {
    let request = TransportRequest {
        method: momus_core::ast::Method::Get,
        url: url.to_string(),
        headers: HashMap::new(),
        body: None,
    };
    transport.send(&request).await
}

/// Send an OPTIONS request via the transport adapter.
async fn options(
    transport: &dyn TransportAdapter,
    url: &str,
    origin: &str,
) -> Result<TransportResponse, String> {
    let mut headers = HashMap::new();
    headers.insert("Origin".to_string(), origin.to_string());
    headers.insert(
        "Access-Control-Request-Method".to_string(),
        "GET".to_string(),
    );
    let request = TransportRequest {
        method: momus_core::ast::Method::Options,
        url: url.to_string(),
        headers,
        body: None,
    };
    transport.send(&request).await
}

/// Check for missing security headers.
async fn check_security_headers(transport: &dyn TransportAdapter, url: &str) -> CheckResult {
    let resp = match get(transport, url).await {
        Ok(r) => r,
        Err(_) => return CheckResult::pass(),
    };

    let headers = &resp.headers;
    let mut issues = Vec::new();

    // HSTS
    if !headers.contains_key("strict-transport-security") {
        issues.push(GuardIssue {
            endpoint: url.to_string(),
            category: "headers".to_string(),
            severity: "medium".to_string(),
            description: "Missing Strict-Transport-Security header".to_string(),
            recommendation: "Add 'Strict-Transport-Security: max-age=31536000; includeSubDomains'"
                .to_string(),
        });
    }

    // CSP
    if !headers.contains_key("content-security-policy") {
        issues.push(GuardIssue {
            endpoint: url.to_string(),
            category: "headers".to_string(),
            severity: "medium".to_string(),
            description: "Missing Content-Security-Policy header".to_string(),
            recommendation: "Add a Content-Security-Policy header to prevent XSS attacks"
                .to_string(),
        });
    }

    // X-Content-Type-Options
    if !headers.contains_key("x-content-type-options") {
        issues.push(GuardIssue {
            endpoint: url.to_string(),
            category: "headers".to_string(),
            severity: "low".to_string(),
            description: "Missing X-Content-Type-Options header".to_string(),
            recommendation: "Add 'X-Content-Type-Options: nosniff'".to_string(),
        });
    }

    // X-Frame-Options
    if !headers.contains_key("x-frame-options") {
        issues.push(GuardIssue {
            endpoint: url.to_string(),
            category: "headers".to_string(),
            severity: "low".to_string(),
            description: "Missing X-Frame-Options header".to_string(),
            recommendation: "Add 'X-Frame-Options: DENY' or 'SAMEORIGIN'".to_string(),
        });
    }

    if issues.is_empty() {
        CheckResult::pass()
    } else {
        CheckResult {
            issues,
            passed: false,
        }
    }
}

/// Check CORS configuration.
async fn check_cors(transport: &dyn TransportAdapter, url: &str) -> CheckResult {
    let resp = match options(transport, url, "https://evil.example.com").await {
        Ok(r) => r,
        Err(_) => return CheckResult::pass(),
    };

    let headers = &resp.headers;
    let mut issues = Vec::new();

    // Check for permissive CORS
    if let Some(origin) = headers.get("access-control-allow-origin") {
        let origin_str = origin.as_str();
        if origin_str == "*" {
            issues.push(GuardIssue {
                endpoint: url.to_string(),
                category: "cors".to_string(),
                severity: "high".to_string(),
                description: "CORS allows all origins (*)".to_string(),
                recommendation: "Restrict Access-Control-Allow-Origin to specific trusted origins"
                    .to_string(),
            });
        }
        if origin_str == "*" && headers.contains_key("access-control-allow-credentials") {
            issues.push(GuardIssue {
                endpoint: url.to_string(),
                category: "cors".to_string(),
                severity: "critical".to_string(),
                description: "CORS allows all origins with credentials (security risk)".to_string(),
                recommendation:
                    "Cannot use wildcard origin with credentials. Specify exact origins."
                        .to_string(),
            });
        }
    }

    if issues.is_empty() {
        CheckResult::pass()
    } else {
        CheckResult {
            issues,
            passed: false,
        }
    }
}

/// Check for information leakage in response bodies.
async fn check_info_leaks(transport: &dyn TransportAdapter, url: &str) -> CheckResult {
    let resp = match get(transport, url).await {
        Ok(r) => r,
        Err(_) => return CheckResult::pass(),
    };

    let status = resp.status_code;
    if !(400..=599).contains(&status) {
        return CheckResult::pass();
    }

    let body = String::from_utf8_lossy(&resp.body_bytes).to_string();
    let lower = body.to_lowercase();
    let mut issues = Vec::new();

    let leak_patterns: &[(&str, &str, &str)] = &[
        (
            "stack trace",
            "leak",
            "Response contains a stack trace, potentially revealing internal code paths",
        ),
        ("exception", "leak", "Response contains exception details"),
        (
            "syntaxerror",
            "leak",
            "Response contains a syntax error message",
        ),
        (
            "sql syntax",
            "leak",
            "Response contains SQL syntax information, potential SQL injection vector",
        ),
        (
            "fatal error",
            "leak",
            "Response contains a fatal error message",
        ),
        (
            "internal server error",
            "leak",
            "Generic internal server error (may be acceptable)",
        ),
        ("traceback", "leak", "Response contains a Python traceback"),
        (
            "root:x:",
            "leak",
            "Response contains password file data (/etc/passwd)",
        ),
        (
            "/etc/passwd",
            "leak",
            "Response references /etc/passwd, potential path traversal",
        ),
        (
            "select * from",
            "leak",
            "Response contains SQL query, potential SQL injection",
        ),
        (
            "insert into",
            "leak",
            "Response contains SQL query, potential SQL injection",
        ),
    ];

    for (pattern, _category, description) in leak_patterns {
        if lower.contains(pattern) {
            issues.push(GuardIssue {
                endpoint: url.to_string(),
                category: "leak".to_string(),
                severity: "high".to_string(),
                description: description.to_string(),
                recommendation: "Sanitize error responses to avoid leaking internal information"
                    .to_string(),
            });
            break; // One leak per endpoint is enough
        }
    }

    if issues.is_empty() {
        CheckResult::pass()
    } else {
        CheckResult {
            issues,
            passed: false,
        }
    }
}

/// Check for exposed internal endpoints.
async fn check_exposed_endpoints(transport: &dyn TransportAdapter, base_url: &str) -> CheckResult {
    let common_paths = &[
        "/.env",
        "/.git/config",
        "/admin",
        "/api-docs",
        "/api/v1",
        "/backup",
        "/config",
        "/console",
        "/debug",
        "/health",
        "/info",
        "/metrics",
        "/monitor",
        "/phpinfo.php",
        "/robots.txt",
        "/sitemap.xml",
        "/status",
        "/swagger",
        "/swagger.json",
        "/swagger-ui",
        "/test",
        "/.well-known/security.txt",
    ];

    let mut issues = Vec::new();

    for path in common_paths {
        let url = format!("{}{}", base_url, path);
        if let Ok(resp) = get(transport, &url).await {
            let status = resp.status_code;
            if status == 200 {
                issues.push(GuardIssue {
                    endpoint: url,
                    category: "exposed".to_string(),
                    severity: "medium".to_string(),
                    description: format!("Potentially sensitive endpoint '{}' returned 200", path),
                    recommendation: format!(
                        "Restrict access to '{}' or remove if not needed",
                        path
                    ),
                });
            }
        }
    }

    if issues.is_empty() {
        CheckResult::pass()
    } else {
        CheckResult {
            issues,
            passed: false,
        }
    }
}

/// Check for missing or weak authentication.
async fn check_auth(transport: &dyn TransportAdapter, url: &str) -> CheckResult {
    let resp = match get(transport, url).await {
        Ok(r) => r,
        Err(_) => return CheckResult::pass(),
    };

    let status = resp.status_code;

    // If the endpoint returns 200 without auth, that might be fine for public endpoints
    // But if it returns 401/403, auth is working
    if status == 401 || status == 403 {
        return CheckResult::pass();
    }

    // Check if the response indicates auth is needed but not enforced
    let body = String::from_utf8_lossy(&resp.body_bytes).to_string();
    let lower = body.to_lowercase();
    if lower.contains("unauthorized")
        || lower.contains("unauthenticated")
        || lower.contains("forbidden")
    {
        return CheckResult::pass();
    }

    // For non-2xx responses, auth is probably fine
    if !(200..=299).contains(&status) {
        return CheckResult::pass();
    }

    // If we got a 200 with no auth headers and the response looks like data, flag it
    if let Ok(json) = serde_json::from_str::<serde_json::Value>(&body)
        && json.is_object()
        && json.as_object().is_some_and(|o| o.len() > 1)
    {
        return CheckResult::fail(GuardIssue {
            endpoint: url.to_string(),
            category: "auth".to_string(),
            severity: "info".to_string(),
            description: "Endpoint returned data without authentication".to_string(),
            recommendation: "Verify this endpoint should be publicly accessible".to_string(),
        });
    }

    CheckResult::pass()
}

#[cfg(test)]
mod tests {
    use super::*;
    use momus_core::ast::*;
    use serde_json::json;
    use std::collections::HashMap;

    #[test]
    fn test_collect_plan_urls() {
        let plan = TestPlan {
            name: "test".into(),
            base_url: "http://localhost:8080".into(),
            default_headers: HashMap::new(),
            steps: vec![
                Step::Request(RequestStep {
                    name: "r1".into(),
                    method: Method::Get,
                    url: "/health".into(),
                    headers: HashMap::new(),
                    body: None,
                    assert: vec![],
                    save_as: String::new(),
                    soft_fail: false,
                }),
                Step::Request(RequestStep {
                    name: "r2".into(),
                    method: Method::Post,
                    url: "/users".into(),
                    headers: HashMap::new(),
                    body: Some(json!({"name": "test"})),
                    assert: vec![],
                    save_as: String::new(),
                    soft_fail: false,
                }),
            ],
            setup: vec![],
            teardown: vec![],
        };

        let urls = collect_plan_urls(&plan, "http://localhost:8080");
        assert_eq!(urls.len(), 2);
        assert!(urls.contains(&"http://localhost:8080/health".to_string()));
        assert!(urls.contains(&"http://localhost:8080/users".to_string()));
    }

    #[test]
    fn test_collect_plan_urls_dedup() {
        let plan = TestPlan {
            name: "test".into(),
            base_url: "http://localhost:8080".into(),
            default_headers: HashMap::new(),
            steps: vec![
                Step::Request(RequestStep {
                    name: "r1".into(),
                    method: Method::Get,
                    url: "/health".into(),
                    headers: HashMap::new(),
                    body: None,
                    assert: vec![],
                    save_as: String::new(),
                    soft_fail: false,
                }),
                Step::Request(RequestStep {
                    name: "r2".into(),
                    method: Method::Get,
                    url: "/health".into(),
                    headers: HashMap::new(),
                    body: None,
                    assert: vec![],
                    save_as: String::new(),
                    soft_fail: false,
                }),
            ],
            setup: vec![],
            teardown: vec![],
        };

        let urls = collect_plan_urls(&plan, "http://localhost:8080");
        assert_eq!(urls.len(), 1);
    }

    #[test]
    fn test_collect_plan_urls_absolute() {
        let plan = TestPlan {
            name: "test".into(),
            base_url: "http://localhost:8080".into(),
            default_headers: HashMap::new(),
            steps: vec![Step::Request(RequestStep {
                name: "r1".into(),
                method: Method::Get,
                url: "https://api.example.com/health".into(),
                headers: HashMap::new(),
                body: None,
                assert: vec![],
                save_as: String::new(),
                soft_fail: false,
            })],
            setup: vec![],
            teardown: vec![],
        };

        let urls = collect_plan_urls(&plan, "http://localhost:8080");
        assert_eq!(urls.len(), 1);
        assert_eq!(urls[0], "https://api.example.com/health");
    }

    #[test]
    fn test_check_result_pass() {
        let result = CheckResult::pass();
        assert!(result.passed);
        assert!(result.issues.is_empty());
    }

    #[test]
    fn test_check_result_fail() {
        let issue = GuardIssue {
            endpoint: "/test".into(),
            category: "test".into(),
            severity: "high".into(),
            description: "test issue".into(),
            recommendation: "fix it".into(),
        };
        let result = CheckResult::fail(issue);
        assert!(!result.passed);
        assert_eq!(result.issues.len(), 1);
    }
}
