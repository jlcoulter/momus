//! Integration tests for the `momus` CLI binary.
//!
//! These tests exercise the full CLI pipeline: argument parsing, plan loading,
//! execution, and output. Tests that start external servers are marked `#[ignore]`
//! to keep the common `cargo test` fast.

use assert_cmd::Command;
use predicates::prelude::*;
use std::collections::HashMap;

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/// A valid minimal test plan JSON string.
fn valid_plan_json(base_url: &str) -> String {
    format!(
        r#"{{
            "name": "integration-test",
            "base_url": "{base_url}",
            "default_headers": {{ "Accept": "application/json" }},
            "steps": [
                {{
                    "type": "request",
                    "name": "health",
                    "method": "GET",
                    "url": "/health",
                    "assert": [
                        {{ "status": 200 }},
                        {{ "content_type": "application/json" }},
                        {{ "valid_json": null }}
                    ]
                }}
            ]
        }}"#
    )
}

/// Start a mock server on a random port and return (server, base_url).
/// The mock server responds to GET /health with 200 {"status": "ok"}.
async fn start_mock_server() -> (momus_mock::MockServer, String) {
    let mut routes = HashMap::new();
    routes.insert(
        "GET /health".into(),
        momus_mock::MockResponse::json(200, serde_json::json!({"status": "ok"})),
    );
    let server = momus_mock::MockServer::start(routes).await;
    let url = server.addr.clone();
    (server, url)
}

// ---------------------------------------------------------------------------
// CLI argument parsing
// ---------------------------------------------------------------------------

#[test]
fn test_help_exits_with_0() {
    let mut cmd = Command::cargo_bin("momus").unwrap();
    cmd.arg("--help");
    cmd.assert()
        .success()
        .stdout(predicate::str::contains("Usage:"))
        .stdout(predicate::str::contains("run"))
        .stdout(predicate::str::contains("validate"))
        .stdout(predicate::str::contains("mock"))
        .stdout(predicate::str::contains("bench"))
        .stdout(predicate::str::contains("fuzz"))
        .stdout(predicate::str::contains("chaos"))
        .stdout(predicate::str::contains("convert"))
        .stdout(predicate::str::contains("contract"))
        .stdout(predicate::str::contains("guard"))
        .stdout(predicate::str::contains("diff"))
        .stdout(predicate::str::contains("init"))
        .stdout(predicate::str::contains("plan"))
        .stdout(predicate::str::contains("fhir-generate"));
}

#[test]
fn test_run_help_shows_run_flags() {
    let mut cmd = Command::cargo_bin("momus").unwrap();
    cmd.args(["run", "--help"]);
    cmd.assert()
        .success()
        .stdout(predicate::str::contains("--base-url"))
        .stdout(predicate::str::contains("--output"))
        .stdout(predicate::str::contains("--format"));
}

#[test]
fn test_version_exits_with_0() {
    let mut cmd = Command::cargo_bin("momus").unwrap();
    cmd.arg("--version");
    cmd.assert()
        .success()
        .stdout(predicate::str::contains("0.4.0"));
}

#[test]
fn test_unknown_subcommand_exits_nonzero() {
    let mut cmd = Command::cargo_bin("momus").unwrap();
    cmd.arg("nonexistent-subcommand");
    cmd.assert()
        .failure()
        .stderr(predicate::str::contains("error"));
}

// ---------------------------------------------------------------------------
// momus validate
// ---------------------------------------------------------------------------

#[test]
fn test_validate_valid_plan() {
    let mut cmd = Command::cargo_bin("momus").unwrap();
    let dir = tempfile::TempDir::new().unwrap();
    let plan_path = dir.path().join("test-plan.json");
    std::fs::write(
        &plan_path,
        r#"{
            "name": "test",
            "base_url": "http://localhost:9999",
            "steps": [
                {
                    "type": "request",
                    "name": "ping",
                    "method": "GET",
                    "url": "/ping",
                    "assert": [{ "status": 200 }]
                }
            ]
        }"#,
    )
    .unwrap();

    cmd.args(["validate", plan_path.to_str().unwrap()]);
    cmd.assert()
        .success()
        .stdout(predicate::str::contains("Valid test plan"))
        .stdout(predicate::str::contains("test"));
}

#[test]
fn test_validate_invalid_json() {
    let mut cmd = Command::cargo_bin("momus").unwrap();
    let dir = tempfile::TempDir::new().unwrap();
    let plan_path = dir.path().join("bad-plan.json");
    std::fs::write(&plan_path, "this is not json").unwrap();

    cmd.args(["validate", plan_path.to_str().unwrap()]);
    cmd.assert()
        .failure()
        .stderr(predicate::str::contains("Failed to parse test plan"));
}

#[test]
fn test_validate_malformed_plan() {
    let mut cmd = Command::cargo_bin("momus").unwrap();
    let dir = tempfile::TempDir::new().unwrap();
    let plan_path = dir.path().join("bad-plan.json");
    // Step is missing required fields like `name`, `method`, `url`
    std::fs::write(
        &plan_path,
        r#"{
            "name": "bad",
            "base_url": "http://localhost",
            "steps": [
                { "type": "request" }
            ]
        }"#,
    )
    .unwrap();

    cmd.args(["validate", plan_path.to_str().unwrap()]);
    // The plan is valid JSON but the step is missing required fields
    cmd.assert()
        .failure()
        .stderr(predicate::str::contains("Failed to parse test plan"));
}

// ---------------------------------------------------------------------------
// momus run
// ---------------------------------------------------------------------------

#[tokio::test]
async fn test_run_health_check_plan() {
    let (_server, base_url) = start_mock_server().await;

    let mut cmd = Command::cargo_bin("momus").unwrap();
    let dir = tempfile::TempDir::new().unwrap();
    let plan_path = dir.path().join("plan.json");
    std::fs::write(&plan_path, valid_plan_json(&base_url)).unwrap();

    cmd.args(["run", plan_path.to_str().unwrap()]);
    // Use spawn_blocking so the tokio runtime can drive the mock server
    let result = tokio::task::spawn_blocking(move || cmd.assert())
        .await
        .unwrap();
    result
        .success()
        .stdout(predicate::str::contains("Passed"))
        .stdout(predicate::str::contains("health"));
}

#[tokio::test]
async fn test_run_with_base_url_override() {
    let (_server, base_url) = start_mock_server().await;

    let mut cmd = Command::cargo_bin("momus").unwrap();
    let dir = tempfile::TempDir::new().unwrap();
    // Use a different base_url in the plan; override with --base-url
    let plan_json = r#"{
            "name": "override-test",
            "base_url": "http://localhost:1",
            "steps": [
                {
                    "type": "request",
                    "name": "health",
                    "method": "GET",
                    "url": "/health",
                    "assert": [
                        { "status": 200 },
                        { "valid_json": null }
                    ]
                }
            ]
        }"#
    .to_string();
    let plan_path = dir.path().join("plan.json");
    std::fs::write(&plan_path, &plan_json).unwrap();

    cmd.args(["run", plan_path.to_str().unwrap(), "--base-url", &base_url]);
    let result = tokio::task::spawn_blocking(move || cmd.assert())
        .await
        .unwrap();
    result.success().stdout(predicate::str::contains("Passed"));
}

#[tokio::test]
async fn test_run_with_output_dir() {
    let (_server, base_url) = start_mock_server().await;

    let mut cmd = Command::cargo_bin("momus").unwrap();
    let dir = tempfile::TempDir::new().unwrap();
    let plan_path = dir.path().join("plan.json");
    std::fs::write(&plan_path, valid_plan_json(&base_url)).unwrap();

    let out_dir = dir.path().join("run-output");
    cmd.args([
        "run",
        plan_path.to_str().unwrap(),
        "--output",
        out_dir.to_str().unwrap(),
    ]);
    let result = tokio::task::spawn_blocking(move || cmd.assert())
        .await
        .unwrap();
    result.success();

    // Verify output files were written
    // The CLI writes results to {output}/results/{group_name}.json
    let results_dir = out_dir.join("results");
    assert!(
        results_dir.join("integration-test.json").exists(),
        "Expected integration-test.json in {:?}. Contents: {:?}",
        results_dir,
        std::fs::read_dir(&results_dir)
            .map(|e| e.map(|e| e.unwrap().path()).collect::<Vec<_>>())
            .unwrap_or_default()
    );
}

// ---------------------------------------------------------------------------
// momus convert
// ---------------------------------------------------------------------------

#[test]
fn test_convert_curl() {
    let mut cmd = Command::cargo_bin("momus").unwrap();
    cmd.args([
        "convert",
        "curl",
        "curl -X GET https://api.example.com/health -H 'Accept: application/json'",
    ]);
    cmd.assert()
        .success()
        .stdout(predicate::str::contains("GET"))
        .stdout(predicate::str::contains("/health"));
}

#[test]
fn test_convert_curl_to_file() {
    let mut cmd = Command::cargo_bin("momus").unwrap();
    let dir = tempfile::TempDir::new().unwrap();
    let out_path = dir.path().join("converted.json");

    cmd.args([
        "convert",
        "curl",
        "curl -X POST https://api.example.com/data -d '{\"key\":\"value\"}'",
        "--output",
        out_path.to_str().unwrap(),
    ]);
    cmd.assert().success();

    // Verify the output file was created and contains valid JSON
    let content = std::fs::read_to_string(&out_path).unwrap();
    let plan: serde_json::Value = serde_json::from_str(&content).unwrap();
    assert!(plan["name"].as_str().unwrap().contains("cURL"));
}

#[test]
fn test_convert_unknown_format() {
    let mut cmd = Command::cargo_bin("momus").unwrap();
    cmd.args(["convert", "unknown-format", "input.txt"]);
    // clap's value parser rejects unknown formats before the function runs
    cmd.assert()
        .failure()
        .stderr(predicate::str::contains("invalid value"))
        .stderr(predicate::str::contains("unknown-format"));
}

// ---------------------------------------------------------------------------
// momus bench
// ---------------------------------------------------------------------------

#[tokio::test]
#[ignore = "bench tests are slow"]
async fn test_bench_steady_mode() {
    let (_server, base_url) = start_mock_server().await;

    let mut cmd = Command::cargo_bin("momus").unwrap();
    let dir = tempfile::TempDir::new().unwrap();
    let plan_path = dir.path().join("plan.json");
    std::fs::write(&plan_path, valid_plan_json(&base_url)).unwrap();

    cmd.args([
        "bench",
        plan_path.to_str().unwrap(),
        "--mode",
        "steady",
        "--concurrency",
        "1",
        "--duration",
        "1",
        "--base-url",
        &base_url,
    ]);
    let result = tokio::task::spawn_blocking(move || cmd.assert())
        .await
        .unwrap();
    result.success();
}

// ---------------------------------------------------------------------------
// momus fuzz
// ---------------------------------------------------------------------------

#[tokio::test]
#[ignore = "fuzz tests are slow"]
async fn test_fuzz_basic() {
    let (_server, base_url) = start_mock_server().await;

    let mut cmd = Command::cargo_bin("momus").unwrap();
    let dir = tempfile::TempDir::new().unwrap();
    let plan_path = dir.path().join("plan.json");
    std::fs::write(&plan_path, valid_plan_json(&base_url)).unwrap();

    cmd.args([
        "fuzz",
        plan_path.to_str().unwrap(),
        "--iterations",
        "5",
        "--base-url",
        &base_url,
    ]);
    let result = tokio::task::spawn_blocking(move || cmd.assert())
        .await
        .unwrap();
    result.success();
}

// ---------------------------------------------------------------------------
// momus guard
// ---------------------------------------------------------------------------

#[tokio::test]
#[ignore = "guard tests are slow"]
async fn test_guard_basic() {
    let (_server, base_url) = start_mock_server().await;

    let mut cmd = Command::cargo_bin("momus").unwrap();
    let dir = tempfile::TempDir::new().unwrap();
    let plan_path = dir.path().join("plan.json");
    std::fs::write(&plan_path, valid_plan_json(&base_url)).unwrap();

    cmd.args([
        "guard",
        plan_path.to_str().unwrap(),
        "--base-url",
        &base_url,
    ]);
    let result = tokio::task::spawn_blocking(move || cmd.assert())
        .await
        .unwrap();
    result.success();
}

// ---------------------------------------------------------------------------
// momus diff
// ---------------------------------------------------------------------------

#[tokio::test]
#[ignore = "diff tests are slow"]
async fn test_diff_basic() {
    let (_server1, base_url1) = start_mock_server().await;
    let (_server2, base_url2) = start_mock_server().await;

    let mut cmd = Command::cargo_bin("momus").unwrap();
    let dir = tempfile::TempDir::new().unwrap();
    let plan_path = dir.path().join("plan.json");
    // Use a plan with a base_url that will be overridden by --baseline and --target
    std::fs::write(
        &plan_path,
        r#"{
            "name": "diff-test",
            "base_url": "http://localhost:1",
            "steps": [
                {
                    "type": "request",
                    "name": "health",
                    "method": "GET",
                    "url": "/health",
                    "assert": [{ "status": 200 }]
                }
            ]
        }"#,
    )
    .unwrap();

    cmd.args([
        "diff",
        plan_path.to_str().unwrap(),
        "--baseline",
        &base_url1,
        "--target",
        &base_url2,
    ]);
    let result = tokio::task::spawn_blocking(move || cmd.assert())
        .await
        .unwrap();
    result.success();
}

// ---------------------------------------------------------------------------
// momus init
// ---------------------------------------------------------------------------

#[test]
fn test_init_plan() {
    let mut cmd = Command::cargo_bin("momus").unwrap();
    let dir = tempfile::TempDir::new().unwrap();
    let out_path = dir.path().join("test-plan.json");

    cmd.args(["init", "plan", "--output", out_path.to_str().unwrap()]);
    cmd.assert().success();

    // Verify the output file was created and contains valid JSON
    let content = std::fs::read_to_string(&out_path).unwrap();
    let plan: serde_json::Value = serde_json::from_str(&content).unwrap();
    assert!(plan["name"].is_string());
    assert!(plan["steps"].is_array());
}

// ---------------------------------------------------------------------------
// momus plan (display a plan)
// ---------------------------------------------------------------------------

#[test]
fn test_plan_display() {
    let mut cmd = Command::cargo_bin("momus").unwrap();
    let dir = tempfile::TempDir::new().unwrap();
    let plan_path = dir.path().join("plan.json");
    std::fs::write(
        &plan_path,
        r#"{
            "name": "display-test",
            "base_url": "http://localhost:9999",
            "steps": [
                {
                    "type": "request",
                    "name": "ping",
                    "method": "GET",
                    "url": "/ping",
                    "assert": [{ "status": 200 }]
                }
            ]
        }"#,
    )
    .unwrap();

    cmd.args(["plan", plan_path.to_str().unwrap()]);
    cmd.assert().success();

    // The plan command writes to ./output/plan.txt by default
    let output_path = std::path::Path::new("./output/plan.txt");
    if output_path.exists() {
        let content = std::fs::read_to_string(output_path).unwrap();
        assert!(
            content.contains("display-test"),
            "Expected plan name in output"
        );
        assert!(content.contains("ping"), "Expected step name in output");
    }
}
