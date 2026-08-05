use anyhow::{Context, Result};
use clap::{Parser, Subcommand, ValueEnum};
use momus_core::ast::*;
use momus_core::config::MomusConfig;
use momus_core::engine::runner;
use std::path::PathBuf;

#[derive(ValueEnum, Debug, Clone)]
enum OutputFormat {
    Auto,
    Html,
    Text,
}

#[derive(ValueEnum, Debug, Clone)]
enum BenchModeCli {
    Steady,
    MaxThroughput,
    Soak,
}

#[derive(Parser)]
#[command(
    name = "momus",
    about = "Generic API test harness with a composable assertion AST",
    version,
    long_about = "Momus is a domain-agnostic test runner for HTTP APIs.\n\
                   Tests are defined as a JSON plan — a tree of steps (requests,\n\
                   sequences, parallel blocks) with composable assertions on responses.\n\n\
                   Configuration is loaded from:\n\
                   1. --config <path> (explicit)\n\
                   2. $MOMUS_CONFIG (env var)\n\
                   3. ./momus.toml\n\
                   4. ./.momus.toml\n\
                   5. ~/.config/momus/config.toml\n\n\
                   CLI flags override config file values."
)]
struct Cli {
    /// Path to config.toml (optional). Searches default locations if not set.
    #[arg(long, global = true, env = "MOMUS_CONFIG")]
    config: Option<String>,

    /// Enable verbose output (sets RUST_LOG=momus=debug).
    #[arg(short, long, global = true)]
    verbose: bool,

    /// Global request timeout in seconds (overrides config file and per-command timeouts).
    #[arg(long, global = true)]
    timeout: Option<u64>,

    #[command(subcommand)]
    command: Commands,
}

#[derive(Subcommand)]
enum Commands {
    /// Run a test plan from a JSON file.
    Run {
        /// Path to the test plan JSON file (use - for stdin).
        plan: String,
        /// Base URL override.
        #[arg(long)]
        base_url: Option<String>,
        /// Output directory for results.
        #[arg(long, default_value = "./output")]
        output: PathBuf,
        /// Output format (auto-detect from --output extension, or force with this flag).
        #[arg(long, value_enum, default_value = "auto")]
        format: OutputFormat,
    },
    /// Validate a test plan JSON file.
    Validate {
        /// Path to the test plan JSON file (use - for stdin).
        plan: String,
    },
    /// Start a mock server for testing.
    Mock {
        /// Port to listen on (0 = random).
        #[arg(long, default_value = "0")]
        port: u16,
    },
    /// Load test a plan (steady, max-throughput, or soak).
    Bench {
        /// Path to the test plan JSON file.
        plan: PathBuf,
        /// Benchmark mode: steady, max-throughput, soak.
        #[arg(long, value_enum)]
        mode: Option<BenchModeCli>,
        /// Concurrency level (steady, soak).
        #[arg(long)]
        concurrency: Option<usize>,
        /// Duration in seconds (steady, soak; 0 = one-shot).
        #[arg(long)]
        duration: Option<u64>,
        /// Starting concurrency (max-throughput).
        #[arg(long)]
        min_concurrency: Option<usize>,
        /// Maximum concurrency to try (max-throughput).
        #[arg(long)]
        max_concurrency: Option<usize>,
        /// Concurrency increment per step (max-throughput).
        #[arg(long)]
        step: Option<usize>,
        /// Duration per step in seconds (max-throughput).
        #[arg(long)]
        step_duration: Option<u64>,
        /// Error rate threshold 0.0-1.0 (max-throughput).
        #[arg(long)]
        max_error_rate: Option<f64>,
        /// Latency P99 threshold in ms (max-throughput).
        #[arg(long)]
        max_p99_ms: Option<u64>,
        /// Base URL override.
        #[arg(long)]
        base_url: Option<String>,
        /// Output file for the HTML report (omit for stdout).
        #[arg(long)]
        output: Option<PathBuf>,
        /// Output format (auto-detect from --output extension, or force with this flag).
        #[arg(long, value_enum, default_value = "auto")]
        format: OutputFormat,
    },
    /// Fuzz test a plan with payload mutations.
    Fuzz {
        /// Path to the test plan JSON file.
        plan: PathBuf,
        /// Number of mutations to generate.
        #[arg(long)]
        iterations: Option<usize>,
        /// Base URL override.
        #[arg(long)]
        base_url: Option<String>,
        /// Select specific mutators by name (repeatable, empty = all).
        #[arg(long)]
        mutators: Vec<String>,
        /// Request timeout in seconds.
        #[arg(long)]
        timeout: Option<u64>,
    },
    /// Run chaos experiments against a plan.
    Chaos {
        /// Path to the test plan JSON file.
        plan: PathBuf,
        /// Base URL override.
        #[arg(long)]
        base_url: Option<String>,
        /// Chaos experiments to run (repeatable, JSON format, e.g. '{"NetworkLatency":{"endpoint":"/api","delay_ms":5000,"duration_secs":30}}').
        #[arg(long)]
        experiment: Vec<String>,
        /// Interval between experiments in seconds.
        #[arg(long)]
        interval: Option<u64>,
    },
    /// Convert an API description into a test plan.
    Convert {
        /// Input format: openapi, postman, har, curl, graphql, grpc, fhir
        #[arg(value_parser = clap::builder::PossibleValuesParser::new([
            "openapi", "postman", "har", "curl", "graphql", "grpc", "fhir",
        ]))]
        format: String,
        /// Path to the input file (or curl command string for format=curl).
        input: String,
        /// Output file for the test plan (default: <input_stem>.json in the same directory).
        #[arg(short, long)]
        output: Option<PathBuf>,
        /// Generate seed data setup steps to pre-populate the server with resources
        /// so GET/PUT/DELETE tests have data to operate on.
        #[arg(long)]
        seed_data: bool,
    },
    /// Validate API responses against an OpenAPI/GraphQL spec.
    Contract {
        /// Path to the test plan JSON file.
        plan: PathBuf,
        /// Path to the API spec file (OpenAPI YAML/JSON or GraphQL SDL).
        #[arg(long)]
        spec: String,
        /// Base URL override.
        #[arg(long)]
        base_url: Option<String>,
        /// Enable strict mode (fail on undocumented endpoints).
        #[arg(long)]
        strict: Option<bool>,
        /// Request timeout in seconds.
        #[arg(long)]
        timeout: Option<u64>,
    },
    /// Security scan a plan for common vulnerabilities.
    Guard {
        /// Path to the test plan JSON file.
        plan: PathBuf,
        /// Base URL override.
        #[arg(long)]
        base_url: Option<String>,
        /// Check for missing security headers.
        #[arg(long)]
        check_headers: Option<bool>,
        /// Check CORS configuration.
        #[arg(long)]
        check_cors: Option<bool>,
        /// Check for information leakage in error responses.
        #[arg(long)]
        check_leaks: Option<bool>,
        /// Check for exposed internal endpoints.
        #[arg(long)]
        check_exposed: Option<bool>,
        /// Request timeout in seconds.
        #[arg(long)]
        timeout: Option<u64>,
    },
    /// Diff responses between two environments.
    Diff {
        /// Path to the test plan JSON file.
        plan: PathBuf,
        /// Baseline URL (e.g. production).
        #[arg(long)]
        baseline: String,
        /// Target URL (e.g. staging).
        #[arg(long)]
        target: String,
        /// Diff response headers.
        #[arg(long)]
        diff_headers: Option<bool>,
        /// Diff response bodies.
        #[arg(long)]
        diff_bodies: Option<bool>,
        /// Diff status codes.
        #[arg(long)]
        diff_status: Option<bool>,
        /// Request timeout in seconds.
        #[arg(long)]
        timeout: Option<u64>,
    },
    /// Generate a skeleton test plan or config file.
    Init {
        /// What to generate: plan, config
        #[arg(default_value = "plan")]
        template: String,
        /// Output path (default: test-plan.json for plan, momus.toml for config).
        #[arg(short, long)]
        output: Option<PathBuf>,
    },
    /// Show a human-readable summary of a test plan.
    Plan {
        /// Path to the test plan JSON file (use - for stdin).
        plan: String,
        /// Output directory for the plan display.
        #[arg(long, default_value = "./output")]
        output: PathBuf,
    },
    /// Generate bulk FHIR test data (NDJSON) from an IG package.
    FhirGenerate {
        /// Path to the FHIR IG package (.tgz).
        package: String,
        /// Number of resources to generate per type (default: 10).
        #[arg(long, default_value = "10")]
        count: u64,
        /// Output directory for NDJSON files (default: ./fhir-data).
        #[arg(long, default_value = "./fhir-data")]
        output: PathBuf,
    },
}

#[tokio::main]
async fn main() -> Result<()> {
    let cli = Cli::parse();

    // Set up tracing: verbose flag sets RUST_LOG=momus=debug if not already set
    if cli.verbose && std::env::var("RUST_LOG").is_err() {
        // SAFETY: Setting RUST_LOG before tracing is initialized is safe — no other
        // thread is reading it yet, and we're at the start of main.
        unsafe {
            std::env::set_var("RUST_LOG", "momus=debug");
        }
    }
    tracing_subscriber::fmt()
        .with_env_filter(
            tracing_subscriber::EnvFilter::from_default_env()
                .add_directive(tracing::Level::INFO.into()),
        )
        .init();

    // Load optional config file — search default locations if not specified
    let mut cfg = load_config(cli.config.as_deref())?;

    // Apply global --timeout override if provided
    if let Some(timeout) = cli.timeout {
        cfg.global.timeout_secs = timeout;
    }

    match cli.command {
        Commands::Run {
            plan,
            base_url,
            output,
            format,
        } => {
            let content = read_plan_content(&plan)?;
            let mut test_plan: TestPlan = serde_json::from_str(&content)
                .with_context(|| format!("Failed to parse test plan '{}'", plan))?;

            // Config precedence: CLI > [run] section > [global] section
            if let Some(url) = base_url.or(cfg.run.base_url).or(cfg.global.base_url) {
                test_plan.base_url = url;
            }

            // CLI --output overrides config file output
            let output = if output.to_str() != Some("./output") {
                output
            } else {
                cfg.run.output
            };

            // Apply global headers and timeout from config
            if !cfg.global.headers.is_empty() {
                for (k, v) in &cfg.global.headers {
                    test_plan
                        .default_headers
                        .entry(k.clone())
                        .or_insert_with(|| v.clone());
                }
            }

            // Apply run-specific headers (override global)
            if !cfg.run.headers.is_empty() {
                for (k, v) in &cfg.run.headers {
                    test_plan
                        .default_headers
                        .entry(k.clone())
                        .or_insert_with(|| v.clone());
                }
            }

            // Use run-specific timeout, falling back to global
            let timeout_secs = if cfg.run.timeout_secs != 30 {
                cfg.run.timeout_secs
            } else if cfg.global.timeout_secs != 30 {
                cfg.global.timeout_secs
            } else {
                30
            };

            tracing::info!(
                "Running test plan '{}' with {} test(s) against {}",
                test_plan.name,
                test_plan.total_tests(),
                test_plan.base_url
            );

            let report = runner::execute_plan_with_timeout(&test_plan, timeout_secs).await?;

            let want_html = match format {
                OutputFormat::Html => true,
                OutputFormat::Text => false,
                OutputFormat::Auto => output
                    .extension()
                    .and_then(|e| e.to_str())
                    .map(|e| e == "html")
                    .unwrap_or(false),
            };

            if want_html {
                let html = report.to_html();
                if output.extension().and_then(|e| e.to_str()) == Some("html") {
                    // --output is a file path
                    std::fs::create_dir_all(output.parent().unwrap_or(&output))?;
                    std::fs::write(&output, &html)?;
                    println!("HTML report written to: {}", output.display());
                } else {
                    // --output is a directory — write report.html inside it
                    std::fs::create_dir_all(&output)?;
                    let html_path = output.join("report.html");
                    std::fs::write(&html_path, &html)?;
                    println!("HTML report written to: {}", html_path.display());
                }
            } else {
                println!("{}", report);
            }

            // Write per-group results to output/results/
            let results_dir = output.join("results");
            std::fs::create_dir_all(&results_dir)?;
            report.write_results(&output)?;
            println!("\nResults written to: {}/results/", output.display());

            if report.failed > 0 {
                std::process::exit(1);
            }

            Ok(())
        }
        Commands::Validate { plan } => {
            let content = read_plan_content(&plan)?;
            let test_plan: TestPlan = serde_json::from_str(&content)
                .with_context(|| format!("Failed to parse test plan '{}'", plan))?;
            println!("✓ Valid test plan: '{}'", test_plan.name);
            println!("  Total tests: {}", test_plan.total_tests());
            println!("  Steps: {}", test_plan.steps.len());
            if !test_plan.setup.is_empty() {
                println!("  Setup steps: {}", test_plan.setup.len());
            }
            if !test_plan.teardown.is_empty() {
                println!("  Teardown steps: {}", test_plan.teardown.len());
            }
            Ok(())
        }
        Commands::Mock { port } => {
            let addr = if port > 0 {
                format!("0.0.0.0:{}", port)
            } else {
                "0.0.0.0:0".into()
            };
            let listener = tokio::net::TcpListener::bind(&addr).await?;
            let local_addr = listener.local_addr()?;
            println!("Momus mock server listening on http://{}", local_addr);
            println!("All requests return 200 with 'status: ok'");
            println!("Press Ctrl+C to stop.");

            use axum::{Json, Router, routing::any};
            let app = Router::new().route(
                "/{*path}",
                any(|| async {
                    (
                        axum::http::StatusCode::OK,
                        Json(serde_json::json!({"status": "ok"})),
                    )
                }),
            );
            axum::serve(listener, app).await?;
            Ok(())
        }
        Commands::Convert {
            format,
            input,
            output,
            seed_data,
        } => {
            let plan = momus_convert::convert(&format, &input, seed_data)?;
            let json = serde_json::to_string_pretty(&plan)?;
            let path = match output {
                Some(p) => p,
                None => {
                    // Derive output filename from input (except curl, which is a raw command)
                    if format == "curl" {
                        println!("{}", json);
                        return Ok(());
                    }
                    let input_path = std::path::Path::new(&input);
                    let stem = input_path
                        .file_stem()
                        .and_then(|s| s.to_str())
                        .unwrap_or("test_plan");
                    let parent = std::path::Path::new(".");
                    parent.join(format!("{}.momus.json", stem))
                }
            };
            std::fs::write(&path, json)?;
            println!("Test plan written to: {}", path.display());
            Ok(())
        }
        Commands::Bench {
            plan,
            mode,
            concurrency,
            duration,
            min_concurrency,
            max_concurrency,
            step,
            step_duration,
            max_error_rate,
            max_p99_ms,
            base_url,
            output,
            format,
        } => {
            let content = std::fs::read_to_string(&plan)
                .with_context(|| format!("Failed to read plan file '{}'", plan.display()))?;
            let test_plan: TestPlan = serde_json::from_str(&content)
                .with_context(|| format!("Failed to parse test plan '{}'", plan.display()))?;

            // Config precedence: CLI > [bench] section > [global] section
            let base_url = base_url.or(cfg.bench.base_url).or(cfg.global.base_url);

            // Config precedence: CLI > [bench] section > hardcoded defaults
            let bench_mode = match mode.unwrap_or({
                // If no CLI mode, try config file, then default to Steady
                match &cfg.bench.mode {
                    momus_bench::BenchMode::Steady { .. } => BenchModeCli::Steady,
                    momus_bench::BenchMode::MaxThroughput { .. } => BenchModeCli::MaxThroughput,
                    momus_bench::BenchMode::Soak { .. } => BenchModeCli::Soak,
                }
            }) {
                BenchModeCli::Steady => {
                    let concurrency = concurrency
                        .or(match &cfg.bench.mode {
                            momus_bench::BenchMode::Steady { concurrency, .. } => {
                                Some(*concurrency)
                            }
                            _ => None,
                        })
                        .unwrap_or(10);
                    let duration_secs = duration
                        .or(match &cfg.bench.mode {
                            momus_bench::BenchMode::Steady { duration_secs, .. } => {
                                Some(*duration_secs)
                            }
                            _ => None,
                        })
                        .unwrap_or(30);
                    momus_bench::BenchMode::Steady {
                        concurrency,
                        duration_secs,
                    }
                }
                BenchModeCli::MaxThroughput => momus_bench::BenchMode::MaxThroughput {
                    min_concurrency: min_concurrency.unwrap_or(10),
                    max_concurrency: max_concurrency.unwrap_or(200),
                    step: step.unwrap_or(10),
                    step_duration_secs: step_duration.unwrap_or(10),
                    max_error_rate: max_error_rate.unwrap_or(0.05),
                    max_p99_ms: max_p99_ms.unwrap_or(2000),
                },
                BenchModeCli::Soak => {
                    let concurrency = concurrency
                        .or(match &cfg.bench.mode {
                            momus_bench::BenchMode::Soak { concurrency, .. } => Some(*concurrency),
                            _ => None,
                        })
                        .unwrap_or(10);
                    let duration_secs = duration
                        .or(match &cfg.bench.mode {
                            momus_bench::BenchMode::Soak { duration_secs, .. } => {
                                Some(*duration_secs)
                            }
                            _ => None,
                        })
                        .unwrap_or(30);
                    momus_bench::BenchMode::Soak {
                        concurrency,
                        duration_secs,
                    }
                }
            };

            let config = momus_bench::BenchConfig {
                mode: bench_mode,
                base_url,
                output: output.clone().unwrap_or(cfg.bench.output),
                ..Default::default()
            };
            let report = momus_bench::run_bench(&test_plan, &config).await?;

            let want_html = match format {
                OutputFormat::Html => true,
                OutputFormat::Text => false,
                OutputFormat::Auto => output
                    .as_ref()
                    .and_then(|p| p.extension())
                    .and_then(|e| e.to_str())
                    .map(|e| e == "html")
                    .unwrap_or(false),
            };

            if want_html {
                let html = report.to_html();
                if let Some(path) = &output {
                    std::fs::create_dir_all(path.parent().unwrap_or(path))?;
                    std::fs::write(path, &html)?;
                    println!("HTML report written to: {}", path.display());
                } else {
                    // stdout — just print the HTML
                    println!("{}", html);
                }
            } else {
                println!("{}", report);
            }

            Ok(())
        }
        Commands::Fuzz {
            plan,
            iterations,
            base_url,
            mutators: _mutators,
            timeout: _timeout,
        } => {
            let content = std::fs::read_to_string(&plan)
                .with_context(|| format!("Failed to read plan file '{}'", plan.display()))?;
            let test_plan: TestPlan = serde_json::from_str(&content)
                .with_context(|| format!("Failed to parse test plan '{}'", plan.display()))?;

            let base_url = base_url.or(cfg.fuzz.base_url).or(cfg.global.base_url);
            let fuzz_output = cfg.fuzz.output.clone();

            let config = momus_fuzz::FuzzConfig {
                iterations: iterations.unwrap_or(cfg.fuzz.iterations),
                base_url,
                output: fuzz_output.clone(),
                ..Default::default()
            };
            let report = momus_fuzz::run_fuzz(&test_plan, &config).await?;
            println!("{}", report);

            // Write report to output directory
            std::fs::create_dir_all(&fuzz_output)?;
            let report_json = serde_json::to_string_pretty(&report)?;
            std::fs::write(fuzz_output.join("fuzz-report.json"), &report_json)?;
            println!(
                "\nReport written to: {}/fuzz-report.json",
                fuzz_output.display()
            );
            Ok(())
        }
        Commands::Chaos {
            plan,
            base_url,
            experiment,
            interval,
        } => {
            let content = std::fs::read_to_string(&plan)
                .with_context(|| format!("Failed to read plan file '{}'", plan.display()))?;
            let test_plan: TestPlan = serde_json::from_str(&content)
                .with_context(|| format!("Failed to parse test plan '{}'", plan.display()))?;

            let base_url = base_url.or(cfg.chaos.base_url).or(cfg.global.base_url);
            let interval_secs = interval.unwrap_or(cfg.chaos.interval_secs);

            // Parse experiments from CLI JSON strings, or fall back to config
            let experiments = if experiment.is_empty() {
                cfg.chaos.experiments.clone()
            } else {
                experiment
                    .iter()
                    .map(|s| {
                        serde_json::from_str::<momus_chaos::ChaosExperiment>(s)
                            .with_context(|| format!("Failed to parse chaos experiment JSON: {s}"))
                    })
                    .collect::<Result<Vec<_>>>()?
            };
            let chaos_output = cfg.chaos.output.clone();

            let config = momus_chaos::ChaosConfig {
                experiments,
                base_url,
                interval_secs,
                timeout_secs: cfg.chaos.timeout_secs,
                output: chaos_output.clone(),
            };
            let reports = momus_chaos::run_chaos(&test_plan, &config).await?;
            for report in &reports {
                println!("{}", report);
            }

            // Write reports to output directory
            std::fs::create_dir_all(&chaos_output)?;
            let report_json = serde_json::to_string_pretty(&reports)?;
            std::fs::write(chaos_output.join("chaos-report.json"), &report_json)?;
            println!(
                "\nReport written to: {}/chaos-report.json",
                chaos_output.display()
            );
            Ok(())
        }
        Commands::Contract {
            plan,
            spec,
            base_url,
            strict,
            timeout,
        } => {
            let content = std::fs::read_to_string(&plan)
                .with_context(|| format!("Failed to read plan file '{}'", plan.display()))?;
            let test_plan: TestPlan = serde_json::from_str(&content)
                .with_context(|| format!("Failed to parse test plan '{}'", plan.display()))?;

            let base_url = base_url.or(cfg.contract.base_url).or(cfg.global.base_url);
            let strict = strict.unwrap_or(cfg.contract.strict);
            let timeout_secs = timeout
                .or(Some(cfg.contract.timeout_secs))
                .or(Some(cfg.global.timeout_secs))
                .unwrap_or(30);
            let contract_output = cfg.contract.output.clone();

            let config = momus_contract::ContractConfig {
                spec_path: spec,
                base_url,
                strict,
                timeout_secs,
                output: contract_output.clone(),
            };
            let report = momus_contract::run_contract(&test_plan, &config).await?;
            println!("{}", report);

            // Write report to output directory
            std::fs::create_dir_all(&contract_output)?;
            let report_json = serde_json::to_string_pretty(&report)?;
            std::fs::write(contract_output.join("contract-report.json"), &report_json)?;
            println!(
                "\nReport written to: {}/contract-report.json",
                contract_output.display()
            );
            Ok(())
        }
        Commands::Guard {
            plan,
            base_url,
            check_headers: _check_headers,
            check_cors: _check_cors,
            check_leaks: _check_leaks,
            check_exposed: _check_exposed,
            timeout: _timeout,
        } => {
            let content = std::fs::read_to_string(&plan)
                .with_context(|| format!("Failed to read plan file '{}'", plan.display()))?;
            let test_plan: TestPlan = serde_json::from_str(&content)
                .with_context(|| format!("Failed to parse test plan '{}'", plan.display()))?;

            let base_url = base_url.or(cfg.guard.base_url).or(cfg.global.base_url);
            let guard_output = cfg.guard.output.clone();

            let config = momus_guard::GuardConfig {
                base_url,
                output: guard_output.clone(),
                ..Default::default()
            };
            let report = momus_guard::run_guard(&test_plan, &config).await?;
            println!("{}", report);

            // Write report to output directory
            std::fs::create_dir_all(&guard_output)?;
            let report_json = serde_json::to_string_pretty(&report)?;
            std::fs::write(guard_output.join("guard-report.json"), &report_json)?;
            println!(
                "\nReport written to: {}/guard-report.json",
                guard_output.display()
            );
            Ok(())
        }
        Commands::Diff {
            plan,
            baseline,
            target,
            diff_headers,
            diff_bodies,
            diff_status,
            timeout,
        } => {
            let content = std::fs::read_to_string(&plan)
                .with_context(|| format!("Failed to read plan file '{}'", plan.display()))?;
            let test_plan: TestPlan = serde_json::from_str(&content)
                .with_context(|| format!("Failed to parse test plan '{}'", plan.display()))?;

            let timeout_secs = timeout
                .or(Some(cfg.diff.timeout_secs))
                .or(Some(cfg.global.timeout_secs))
                .unwrap_or(30);
            let diff_output = cfg.diff.output.clone();

            let config = momus_diff::DiffConfig {
                baseline_url: baseline,
                target_url: target,
                diff_headers: diff_headers.unwrap_or(cfg.diff.diff_headers),
                diff_bodies: diff_bodies.unwrap_or(cfg.diff.diff_bodies),
                diff_status: diff_status.unwrap_or(cfg.diff.diff_status),
                timeout_secs,
                output: diff_output.clone(),
            };
            let report = momus_diff::run_diff(&test_plan, &config).await?;
            println!("{}", report);

            // Write report to output directory
            std::fs::create_dir_all(&diff_output)?;
            let report_json = serde_json::to_string_pretty(&report)?;
            std::fs::write(diff_output.join("diff-report.json"), &report_json)?;
            println!(
                "\nReport written to: {}/diff-report.json",
                diff_output.display()
            );
            Ok(())
        }
        Commands::Plan { plan, output } => {
            let content = read_plan_content(&plan)?;
            let test_plan: TestPlan = serde_json::from_str(&content)
                .with_context(|| format!("Failed to parse test plan '{}'", plan))?;

            // CLI --output overrides config file output
            let output = if output.to_str() != Some("./output") {
                output
            } else {
                cfg.plan.output
            };

            let display = test_plan.display_plan();
            let output_path = output.join("plan.txt");
            std::fs::create_dir_all(&output)?;
            std::fs::write(&output_path, &display)?;
            println!("Plan written to: {}", output_path.display());
            Ok(())
        }
        Commands::Init { template, output } => {
            match template.as_str() {
                "plan" => {
                    let path = output.unwrap_or_else(|| PathBuf::from("test-plan.json"));
                    let skeleton = serde_json::json!({
                        "name": "my test plan",
                        "base_url": "http://localhost:8080",
                        "default_headers": {
                            "Accept": "application/json"
                        },
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
                    });
                    let json = serde_json::to_string_pretty(&skeleton)?;
                    std::fs::write(&path, json)?;
                    println!("✓ Skeleton test plan written to: {}", path.display());
                }
                "config" => {
                    let path = output.unwrap_or_else(|| PathBuf::from("momus.toml"));
                    let content = r#"# Momus Configuration
# CLI flags override these values.

[global]
# Base URL for all requests (overrides the plan's base_url).
# base_url = "http://localhost:8080"

# Default headers sent with every request.
# [global.headers]
# Authorization = "Bearer your-token-here"

# Request timeout in seconds (default: 30).
# timeout_secs = 60

[run]
# Output directory for results (default: ./output).
# output = "./results"

[bench]
# Output directory for results (default: ./output).
# output = "./bench-results"

# Execution mode: Steady, MaxThroughput, or Soak.
# [bench.mode]
# type = "Steady"
# concurrency = 20
# duration_secs = 60

[fuzz]
# Output directory for results (default: ./output).
# output = "./fuzz-results"

# Number of mutations to generate per input (default: 1000).
# iterations = 5000

# Select specific mutators (empty = all).
# mutators = ["null_injection", "boundary"]

# Request timeout in seconds (default: 30).
# timeout_secs = 60

[chaos]
# Output directory for results (default: ./output).
# output = "./chaos-results"

# How long to wait between experiments in seconds (default: 5).
# interval_secs = 10

[contract]
# Output directory for results (default: ./output).
# output = "./contract-results"

# Path to the API spec file (OpenAPI YAML/JSON or GraphQL SDL).
# spec_path = "./api-spec.yaml"
# strict = true

# Request timeout in seconds (default: 30).
# timeout_secs = 60

[guard]
# Output directory for results (default: ./output).
# output = "./guard-results"

# Check for missing security headers (default: true).
# check_headers = true
# check_cors = true
# check_leaks = true
# check_exposed = true

# Request timeout in seconds (default: 30).
# timeout_secs = 60

[plan]
# Output directory for the plan display (default: ./output).
# output = "./plan-output"

[diff]
# Output directory for results (default: ./output).
# output = "./diff-results"

# Baseline environment URL (e.g. production).
# baseline_url = "https://prod.example.com"
# Target environment URL (e.g. staging).
# target_url = "https://staging.example.com"

# Diff response headers (default: true).
# diff_headers = true
# Diff response bodies (default: true).
# diff_bodies = true
# Diff status codes (default: true).
# diff_status = true

# Request timeout in seconds (default: 30).
# timeout_secs = 60
"#;
                    std::fs::write(&path, content)?;
                    println!("✓ Skeleton config written to: {}", path.display());
                }
                other => {
                    anyhow::bail!("Unknown template '{}'. Available: plan, config", other);
                }
            }
            Ok(())
        }
        Commands::FhirGenerate {
            package,
            count,
            output,
        } => {
            std::fs::create_dir_all(&output)?;
            momus_convert::generate_fhir_bulk_test_data(&package, count, &output)?;
            println!(
                "✓ FHIR bulk test data written to {}/data/",
                output.display()
            );
            Ok(())
        }
    }
}

/// Read plan content from a file path or stdin.
fn read_plan_content(plan: &str) -> Result<String> {
    if plan == "-" {
        use std::io::Read;
        let mut buf = String::new();
        std::io::stdin().read_to_string(&mut buf)?;
        Ok(buf)
    } else {
        std::fs::read_to_string(plan)
            .with_context(|| format!("Failed to read plan file '{}'", plan))
    }
}

/// Load config from explicit path, env var, or search default locations.
fn load_config(explicit: Option<&str>) -> Result<MomusConfig> {
    let path = match explicit {
        Some(p) => Some(p.to_string()),
        None => {
            // Search default locations
            let candidates = [
                "momus.toml",
                ".momus.toml",
                &dirs::config_dir()
                    .map(|d| d.join("momus").join("config.toml"))
                    .map(|p| p.to_string_lossy().to_string())
                    .unwrap_or_default(),
            ];
            candidates
                .iter()
                .find(|p| !p.is_empty() && std::path::Path::new(p).exists())
                .map(|s| s.to_string())
        }
    };

    match path {
        Some(p) => {
            tracing::debug!("Loading config from: {}", p);
            MomusConfig::load(&p).with_context(|| format!("Failed to parse config file '{}'", p))
        }
        None => Ok(MomusConfig::default()),
    }
}
