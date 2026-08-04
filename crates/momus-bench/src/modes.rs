use crate::config::BenchMode;
use crate::report::BenchReport;
use anyhow::Result;
use momus_core::ast::{Method, Step, TestPlan};
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Arc;
use std::time::Instant;
use tokio::sync::Semaphore;

/// Run a benchmark in the given mode.
///
/// Dispatches to the appropriate mode-specific runner.
pub async fn run_mode(mode: &BenchMode, plan: &TestPlan, base_url: &str) -> Result<BenchReport> {
    match mode {
        BenchMode::Steady {
            concurrency,
            duration_secs,
        } => run_steady(*concurrency, *duration_secs, plan, base_url).await,
        BenchMode::MaxThroughput { .. } => {
            // TODO: implement in v0.2.0
            Ok(BenchReport {
                mode: "max_throughput".into(),
                total_requests: 0,
                duration_secs: 0.0,
                p50_ms: 0.0,
                p90_ms: 0.0,
                p95_ms: 0.0,
                p99_ms: 0.0,
                avg_ms: 0.0,
                min_ms: 0.0,
                max_ms: 0.0,
                error_count: 0,
                error_rate: 0.0,
                requests_per_sec: 0.0,
            })
        }
        BenchMode::Soak { .. } => {
            // TODO: implement in v0.2.0
            Ok(BenchReport {
                mode: "soak".into(),
                total_requests: 0,
                duration_secs: 0.0,
                p50_ms: 0.0,
                p90_ms: 0.0,
                p95_ms: 0.0,
                p99_ms: 0.0,
                avg_ms: 0.0,
                min_ms: 0.0,
                max_ms: 0.0,
                error_count: 0,
                error_rate: 0.0,
                requests_per_sec: 0.0,
            })
        }
    }
}

/// Steady mode: fixed concurrency for a fixed duration.
///
/// Spawns N concurrent workers. Each worker iterates the plan steps
/// in a loop, sending HTTP requests and recording latencies.
async fn run_steady(
    concurrency: usize,
    duration_secs: u64,
    plan: &TestPlan,
    base_url: &str,
) -> Result<BenchReport> {
    let client = reqwest::Client::builder()
        .timeout(std::time::Duration::from_secs(30))
        .build()?;

    // Collect all leaf request steps from the plan
    let steps = collect_requests(plan);
    if steps.is_empty() {
        anyhow::bail!("Test plan has no request steps to benchmark");
    }

    let steps = Arc::new(steps);
    let base_url = Arc::new(base_url.to_string());

    // Shared counters
    let total = Arc::new(AtomicU64::new(0));
    let errors = Arc::new(AtomicU64::new(0));
    let latencies: Arc<std::sync::Mutex<Vec<f64>>> = Arc::new(std::sync::Mutex::new(Vec::new()));

    let start = Instant::now();
    let duration = std::time::Duration::from_secs(duration_secs);
    let semaphore = Arc::new(Semaphore::new(concurrency));

    let mut handles = Vec::new();

    for _ in 0..concurrency {
        let client = client.clone();
        let steps = steps.clone();
        let base_url = base_url.clone();
        let total = total.clone();
        let errors = errors.clone();
        let latencies = latencies.clone();
        let semaphore = semaphore.clone();
        let start = start;

        let handle = tokio::spawn(async move {
            loop {
                // Check if time is up
                if start.elapsed() >= duration {
                    break;
                }

                let _permit = semaphore.acquire().await.unwrap();

                for step in steps.iter() {
                    // Check time again between steps
                    if start.elapsed() >= duration {
                        break;
                    }

                    let step_start = Instant::now();
                    let url = format!("{}{}", base_url, step.url);

                    let result = match step.method {
                        Method::Get => client.get(&url).send().await,
                        Method::Post => {
                            let body = step.body.clone().unwrap_or(serde_json::json!({}));
                            client.post(&url).json(&body).send().await
                        }
                        Method::Put => {
                            let body = step.body.clone().unwrap_or(serde_json::json!({}));
                            client.put(&url).json(&body).send().await
                        }
                        Method::Delete => client.delete(&url).send().await,
                        Method::Patch => {
                            let body = step.body.clone().unwrap_or(serde_json::json!({}));
                            client.patch(&url).json(&body).send().await
                        }
                        Method::Head => client.head(&url).send().await,
                        Method::Options => client.request(reqwest::Method::OPTIONS, &url).send().await,
                    };

                    let elapsed_ms = step_start.elapsed().as_secs_f64() * 1000.0;

                    total.fetch_add(1, Ordering::Relaxed);

                    match result {
                        Ok(resp) => {
                            if !resp.status().is_success() {
                                errors.fetch_add(1, Ordering::Relaxed);
                            }
                        }
                        Err(_) => {
                            errors.fetch_add(1, Ordering::Relaxed);
                        }
                    }

                    latencies.lock().unwrap().push(elapsed_ms);
                }
            }
        });

        handles.push(handle);
    }

    // Wait for all workers to finish
    for handle in handles {
        let _ = handle.await;
    }

    let elapsed = start.elapsed().as_secs_f64();
    let total_requests = total.load(Ordering::Relaxed);
    let error_count = errors.load(Ordering::Relaxed);

    // Compute statistics
    let latencies = latencies.lock().unwrap();
    let mut sorted: Vec<f64> = latencies.clone();
    sorted.sort_by(|a, b| a.partial_cmp(b).unwrap_or(std::cmp::Ordering::Equal));

    let len = sorted.len();
    let (p50, p90, p95, p99, avg, min, max) = if len > 0 {
        let p50 = percentile(&sorted, 50.0);
        let p90 = percentile(&sorted, 90.0);
        let p95 = percentile(&sorted, 95.0);
        let p99 = percentile(&sorted, 99.0);
        let avg = sorted.iter().sum::<f64>() / len as f64;
        let min = sorted[0];
        let max = sorted[len - 1];
        (p50, p90, p95, p99, avg, min, max)
    } else {
        (0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0)
    };

    Ok(BenchReport {
        mode: "steady".into(),
        total_requests: total_requests as u64,
        duration_secs: elapsed,
        p50_ms: p50,
        p90_ms: p90,
        p95_ms: p95,
        p99_ms: p99,
        avg_ms: avg,
        min_ms: min,
        max_ms: max,
        error_count: error_count as u64,
        error_rate: if total_requests > 0 {
            error_count as f64 / total_requests as f64
        } else {
            0.0
        },
        requests_per_sec: if elapsed > 0.0 {
            total_requests as f64 / elapsed
        } else {
            0.0
        },
    })
}

/// Collect all leaf request steps from a test plan.
struct BenchStep {
    method: Method,
    url: String,
    body: Option<serde_json::Value>,
}

fn collect_requests(plan: &TestPlan) -> Vec<BenchStep> {
    let mut steps = Vec::new();
    collect_from_steps(&plan.steps, &mut steps);
    collect_from_steps(&plan.setup, &mut steps);
    collect_from_steps(&plan.teardown, &mut steps);
    steps
}

fn collect_from_steps(steps: &[Step], result: &mut Vec<BenchStep>) {
    for step in steps {
        match step {
            Step::Request(req) => {
                result.push(BenchStep {
                    method: req.method,
                    url: req.url.clone(),
                    body: req.body.clone(),
                });
            }
            Step::Sequence(seq) => {
                collect_from_steps(&seq.steps, result);
            }
            Step::Parallel(children) => {
                collect_from_steps(children, result);
            }
            _ => {}
        }
    }
}

/// Compute a percentile from a sorted slice.
fn percentile(sorted: &[f64], p: f64) -> f64 {
    if sorted.is_empty() {
        return 0.0;
    }
    let idx = ((p / 100.0) * (sorted.len() - 1) as f64).round() as usize;
    sorted[idx.min(sorted.len() - 1)]
}

#[cfg(test)]
mod tests {
    use super::*;
    use momus_core::ast::*;
    use std::collections::HashMap;

    #[test]
    fn test_collect_requests() {
        let plan = TestPlan {
            name: "test".into(),
            base_url: "http://localhost".into(),
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
                Step::Sequence(SequenceStep {
                    name: "seq".into(),
                    steps: vec![
                        Step::Request(RequestStep {
                            name: "r2".into(),
                            method: Method::Post,
                            url: "/users".into(),
                            headers: HashMap::new(),
                            body: Some(serde_json::json!({"name": "test"})),
                            assert: vec![],
                            save_as: String::new(),
                            soft_fail: false,
                        }),
                    ],
                    continue_on_failure: false,
                }),
            ],
            setup: vec![],
            teardown: vec![],
        };

        let steps = collect_requests(&plan);
        assert_eq!(steps.len(), 2);
        assert_eq!(steps[0].url, "/health");
        assert_eq!(steps[1].url, "/users");
    }

    #[test]
    fn test_percentile() {
        let data = vec![1.0, 2.0, 3.0, 4.0, 5.0, 6.0, 7.0, 8.0, 9.0, 10.0];
        // 50th percentile of 10 elements: index = (0.5 * 9).round() = 4.5.round() = 5 → value 6.0
        assert!((percentile(&data, 50.0) - 6.0).abs() < 0.01);
        // 90th percentile: index = (0.9 * 9).round() = 8.1.round() = 8 → value 9.0
        assert!((percentile(&data, 90.0) - 9.0).abs() < 0.01);
        // 99th percentile: index = (0.99 * 9).round() = 8.91.round() = 9 → value 10.0
        assert!((percentile(&data, 99.0) - 10.0).abs() < 0.01);
    }

    #[test]
    fn test_percentile_empty() {
        assert_eq!(percentile(&[], 50.0), 0.0);
    }
}
