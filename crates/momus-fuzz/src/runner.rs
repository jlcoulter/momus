use crate::config::FuzzConfig;
use crate::mutators::{all_mutators, mutator_by_name};
use crate::report::FuzzReport;
use anyhow::Result;
use momus_core::ast::{Method, Step, TestPlan};
use momus_core::transport::{TransportAdapter, TransportRequest};
use std::sync::Arc;
use std::sync::atomic::{AtomicU64, Ordering};
use std::time::Instant;

/// Execute a fuzz run against a test plan.
///
/// Generates mutated payloads from the plan's request bodies,
/// sends them to the server, and classifies responses.
///
/// # Errors
///
/// Returns an error if the HTTP client fails to initialize.
pub async fn run_fuzz(
    plan: &TestPlan,
    config: &FuzzConfig,
    transport: Arc<dyn TransportAdapter>,
) -> Result<FuzzReport> {
    let start = Instant::now();

    let base_url = config.base_url.as_deref().unwrap_or(&plan.base_url);

    // Select mutators
    let mutators: Vec<Box<dyn crate::Mutator>> = if config.mutators.is_empty() {
        all_mutators()
    } else {
        config
            .mutators
            .iter()
            .filter_map(|name| mutator_by_name(name))
            .collect()
    };

    if mutators.is_empty() {
        anyhow::bail!("No mutators available for fuzzing");
    }

    // Collect request steps that have bodies
    let targets = collect_fuzz_targets(plan);
    if targets.is_empty() {
        anyhow::bail!("No request steps with bodies found in test plan");
    }

    tracing::info!(
        "Starting fuzz run on '{}' with {} mutator(s), {} target(s), {} iteration(s)",
        plan.name,
        mutators.len(),
        targets.len(),
        config.iterations
    );

    // Pre-compute all mutations: (target_index, mutator_name, mutated_body)
    let mut mutations: Vec<(FuzzTarget, String, serde_json::Value)> = Vec::new();
    let mut applied_mutators: Vec<String> = Vec::new();

    for i in 0..config.iterations {
        let target_idx = i % targets.len();
        let mutator_idx = (i / targets.len()) % mutators.len();
        let target = &targets[target_idx];
        let mutator = &mutators[mutator_idx];

        let seed = i as u64;
        let mutated_body = mutator.mutate(&target.body, seed);

        let name = mutator.name().to_string();
        if !applied_mutators.contains(&name) {
            applied_mutators.push(name.clone());
        }

        mutations.push((target.clone(), name, mutated_body));
    }

    let mutations = Arc::new(mutations);
    let base_url = Arc::new(base_url.to_string());

    let total = Arc::new(AtomicU64::new(0));
    let passed = Arc::new(AtomicU64::new(0));
    let rejected = Arc::new(AtomicU64::new(0));
    let errors = Arc::new(AtomicU64::new(0));
    let leaks = Arc::new(AtomicU64::new(0));

    let concurrency = 8usize.min(config.iterations);
    let semaphore = Arc::new(tokio::sync::Semaphore::new(concurrency));

    let mut handles = Vec::new();

    for chunk_idx in 0..mutations.len() {
        let transport = transport.clone();
        let mutations = mutations.clone();
        let base_url = base_url.clone();
        let total = total.clone();
        let passed = passed.clone();
        let rejected = rejected.clone();
        let errors = errors.clone();
        let leaks = leaks.clone();
        let semaphore = semaphore.clone();

        let handle = tokio::spawn(async move {
            let _permit = semaphore.acquire().await.unwrap();
            let (target, _mutator_name, mutated_body) = &mutations[chunk_idx];

            total.fetch_add(1, Ordering::Relaxed);

            let url = format!("{}{}", base_url, target.url);
            let request = TransportRequest {
                method: target.method,
                url,
                headers: std::collections::HashMap::new(),
                body: Some(mutated_body.clone()),
            };
            let result = transport.send(&request).await;

            match result {
                Ok(resp) => {
                    let status = resp.status_code;
                    if (200..=299).contains(&status) {
                        passed.fetch_add(1, Ordering::Relaxed);
                    } else if (400..=499).contains(&status) {
                        rejected.fetch_add(1, Ordering::Relaxed);
                    } else if status >= 500 {
                        errors.fetch_add(1, Ordering::Relaxed);
                    } else {
                        rejected.fetch_add(1, Ordering::Relaxed);
                    }

                    // Check for info leaks in error responses
                    if !(200..=299).contains(&status)
                        && let Ok(body) = String::from_utf8(resp.body_bytes.clone())
                        && has_info_leak(&body)
                    {
                        leaks.fetch_add(1, Ordering::Relaxed);
                    }
                }
                Err(_) => {
                    errors.fetch_add(1, Ordering::Relaxed);
                }
            }
        });

        handles.push(handle);
    }

    // Wait for all iterations
    for handle in handles {
        let _ = handle.await;
    }

    let elapsed = start.elapsed().as_secs_f64();
    let total_count = total.load(Ordering::Relaxed);
    let passed_count = passed.load(Ordering::Relaxed);
    let rejected_count = rejected.load(Ordering::Relaxed);
    let error_count = errors.load(Ordering::Relaxed);
    let leak_count = leaks.load(Ordering::Relaxed);

    Ok(FuzzReport {
        plan_name: plan.name.clone(),
        total_mutations: total_count,
        passed: passed_count,
        rejected: rejected_count,
        errors: error_count,
        leaks: leak_count,
        duration_secs: elapsed,
        mutators_applied: applied_mutators,
    })
}

/// A fuzz target extracted from a test plan.
#[derive(Clone)]
struct FuzzTarget {
    method: Method,
    url: String,
    body: serde_json::Value,
}

/// Collect request steps that have bodies from a test plan.
fn collect_fuzz_targets(plan: &TestPlan) -> Vec<FuzzTarget> {
    let mut targets = Vec::new();
    collect_from_steps(&plan.steps, &mut targets);
    targets
}

fn collect_from_steps(steps: &[Step], targets: &mut Vec<FuzzTarget>) {
    for step in steps {
        match step {
            Step::Request(req) => {
                if let Some(body) = &req.body {
                    targets.push(FuzzTarget {
                        method: req.method,
                        url: req.url.clone(),
                        body: body.clone(),
                    });
                }
            }
            Step::Sequence(seq) => {
                collect_from_steps(&seq.steps, targets);
            }
            Step::Parallel(children) => {
                collect_from_steps(children, targets);
            }
            _ => {}
        }
    }
}

/// Check if a response body contains information leakage patterns.
fn has_info_leak(body: &str) -> bool {
    let leak_patterns = [
        "stack trace",
        "exception",
        "syntaxerror",
        "referenceerror",
        "typeerror",
        "sql syntax",
        "mysql_error",
        "ora-",
        "postgresql",
        "fatal:",
        "warning:",
        "fatal error",
        "internal error",
        "internal server error",
        "debug",
        "traceback",
        "com.",
        "org.",
        "java.lang",
        "system.",
        "system.io",
        "microsoft",
        "root:x:",
        "daemon:x:",
        "/etc/passwd",
        "/etc/shadow",
        "select * from",
        "insert into",
        "drop table",
        "union select",
    ];

    let lower = body.to_lowercase();
    leak_patterns.iter().any(|p| lower.contains(p))
}

#[cfg(test)]
mod tests {
    use super::*;
    use momus_core::ast::*;
    use serde_json::json;
    use std::collections::HashMap;

    #[test]
    fn test_collect_fuzz_targets() {
        let plan = TestPlan {
            name: "test".into(),
            base_url: "http://localhost".into(),
            default_headers: HashMap::new(),
            steps: vec![
                Step::Request(RequestStep {
                    name: "no_body".into(),
                    method: Method::Get,
                    url: "/health".into(),
                    headers: HashMap::new(),
                    body: None,
                    assert: vec![],
                    save_as: String::new(),
                    soft_fail: false,
                }),
                Step::Request(RequestStep {
                    name: "with_body".into(),
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

        let targets = collect_fuzz_targets(&plan);
        assert_eq!(targets.len(), 1);
        assert_eq!(targets[0].url, "/users");
    }

    #[test]
    fn test_collect_fuzz_targets_no_bodies() {
        let plan = TestPlan {
            name: "test".into(),
            base_url: "http://localhost".into(),
            default_headers: HashMap::new(),
            steps: vec![Step::Request(RequestStep {
                name: "get".into(),
                method: Method::Get,
                url: "/health".into(),
                headers: HashMap::new(),
                body: None,
                assert: vec![],
                save_as: String::new(),
                soft_fail: false,
            })],
            setup: vec![],
            teardown: vec![],
        };

        let targets = collect_fuzz_targets(&plan);
        assert!(targets.is_empty());
    }

    #[test]
    fn test_has_info_leak() {
        assert!(has_info_leak("stack trace: at main()"));
        assert!(has_info_leak("Fatal error: syntax error"));
        assert!(has_info_leak("SELECT * FROM users"));
        assert!(!has_info_leak(r#"{"status": "ok"}"#));
        assert!(!has_info_leak("hello world"));
    }

    #[test]
    fn test_has_info_leak_sql() {
        assert!(has_info_leak("SQL syntax near 'SELECT'"));
        assert!(has_info_leak("ORA-00942: table not found"));
    }

    #[test]
    fn test_has_info_leak_path() {
        assert!(has_info_leak("/etc/passwd"));
        assert!(has_info_leak("root:x:0:0:root:/root:/bin/bash"));
    }
}
