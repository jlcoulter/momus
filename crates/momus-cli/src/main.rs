use anyhow::Result;
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

#[derive(Parser)]
#[command(
    name = "momus",
    about = "Generic API test harness with a composable assertion AST"
)]
struct Cli {
    /// Path to config.toml (optional). CLI flags override file values.
    #[arg(long, global = true)]
    config: Option<String>,

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
        /// Path to the test plan JSON file.
        plan: PathBuf,
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
        /// Concurrency level.
        #[arg(long, default_value = "10")]
        concurrency: usize,
        /// Duration in seconds (0 = one-shot).
        #[arg(long, default_value = "30")]
        duration: u64,
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
        #[arg(long, default_value = "1000")]
        iterations: usize,
        /// Base URL override.
        #[arg(long)]
        base_url: Option<String>,
    },
    /// Run chaos experiments against a plan.
    Chaos {
        /// Path to the test plan JSON file.
        plan: PathBuf,
        /// Base URL override.
        #[arg(long)]
        base_url: Option<String>,
    },
    /// Convert an API description into a test plan.
    Convert {
        /// Input format: openapi, postman, har, curl, graphql, grpc, fhir
        format: String,
        /// Path to the input file (or curl command string for format=curl).
        input: String,
        /// Output file for the test plan (default: <input_stem>.json in the same directory).
        #[arg(short, long)]
        output: Option<PathBuf>,
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
    },
    /// Security scan a plan for common vulnerabilities.
    Guard {
        /// Path to the test plan JSON file.
        plan: PathBuf,
        /// Base URL override.
        #[arg(long)]
        base_url: Option<String>,
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
    },
}

#[tokio::main]
async fn main() -> Result<()> {
    tracing_subscriber::fmt()
        .with_env_filter(
            tracing_subscriber::EnvFilter::from_default_env()
                .add_directive(tracing::Level::INFO.into()),
        )
        .init();

    let cli = Cli::parse();

    // Load optional config file — all sections default if absent.
    let cfg = cli
        .config
        .as_deref()
        .map(MomusConfig::load_optional)
        .unwrap_or_default();

    match cli.command {
        Commands::Run {
            plan,
            base_url,
            output,
            format,
        } => {
            let content = if plan == "-" {
                use std::io::Read;
                let mut buf = String::new();
                std::io::stdin().read_to_string(&mut buf)?;
                buf
            } else {
                std::fs::read_to_string(&plan)?
            };
            let mut test_plan: TestPlan = serde_json::from_str(&content)?;

            // Config precedence: CLI > [run] section > [global] section
            if let Some(url) = base_url.or(cfg.run.base_url).or(cfg.global.base_url) {
                test_plan.base_url = url;
            }

            tracing::info!(
                "Running test plan '{}' with {} test(s) against {}",
                test_plan.name,
                test_plan.total_tests(),
                test_plan.base_url
            );

            let report = runner::execute_plan(&test_plan).await?;

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
                    std::fs::create_dir_all(output.parent().unwrap_or(&output))?;
                    std::fs::write(&output, &html)?;
                    println!("HTML report written to: {}", output.display());
                } else {
                    // --format html but output is a directory — write report.html inside it
                    std::fs::create_dir_all(&output)?;
                    let html_path = output.join("report.html");
                    std::fs::write(&html_path, &html)?;
                    println!("HTML report written to: {}", html_path.display());
                }
            } else {
                println!("{}", report);
            }

            std::fs::create_dir_all(&output)?;
            report.write_results(&output)?;
            println!("\nResults written to: {}/results/", output.display());

            if report.failed > 0 {
                std::process::exit(1);
            }

            Ok(())
        }
        Commands::Validate { plan } => {
            let content = std::fs::read_to_string(&plan)?;
            let test_plan: TestPlan = serde_json::from_str(&content)?;
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
        } => {
            let plan = momus_convert::convert(&format, &input)?;
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
                    let parent = input_path
                        .parent()
                        .unwrap_or_else(|| std::path::Path::new("."));
                    parent.join(format!("{}.json", stem))
                }
            };
            std::fs::write(&path, json)?;
            println!("Test plan written to: {}", path.display());
            Ok(())
        }
        Commands::Bench {
            plan,
            concurrency,
            duration,
            base_url,
            output,
            format,
        } => {
            let content = std::fs::read_to_string(&plan)?;
            let test_plan: TestPlan = serde_json::from_str(&content)?;

            // Config precedence: CLI > [bench] section > [global] section
            let base_url = base_url.or(cfg.bench.base_url).or(cfg.global.base_url);

            let config = momus_bench::BenchConfig {
                mode: momus_bench::BenchMode::Steady {
                    concurrency,
                    duration_secs: duration,
                },
                base_url,
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
        } => {
            let content = std::fs::read_to_string(&plan)?;
            let test_plan: TestPlan = serde_json::from_str(&content)?;

            let base_url = base_url.or(cfg.fuzz.base_url).or(cfg.global.base_url);

            let config = momus_fuzz::FuzzConfig {
                iterations,
                base_url,
                ..Default::default()
            };
            let report = momus_fuzz::run_fuzz(&test_plan, &config).await?;
            println!("{}", report);
            Ok(())
        }
        Commands::Chaos { plan, base_url } => {
            let content = std::fs::read_to_string(&plan)?;
            let test_plan: TestPlan = serde_json::from_str(&content)?;

            let base_url = base_url.or(cfg.chaos.base_url).or(cfg.global.base_url);

            let config = momus_chaos::ChaosConfig {
                base_url,
                ..Default::default()
            };
            let reports = momus_chaos::run_chaos(&test_plan, &config).await?;
            for report in &reports {
                println!("{}", report);
            }
            Ok(())
        }
        Commands::Contract {
            plan,
            spec,
            base_url,
        } => {
            let content = std::fs::read_to_string(&plan)?;
            let test_plan: TestPlan = serde_json::from_str(&content)?;

            let base_url = base_url.or(cfg.contract.base_url).or(cfg.global.base_url);

            let config = momus_contract::ContractConfig {
                spec_path: spec,
                base_url,
                ..Default::default()
            };
            let report = momus_contract::run_contract(&test_plan, &config).await?;
            println!("{}", report);
            Ok(())
        }
        Commands::Guard { plan, base_url } => {
            let content = std::fs::read_to_string(&plan)?;
            let test_plan: TestPlan = serde_json::from_str(&content)?;

            let base_url = base_url.or(cfg.guard.base_url).or(cfg.global.base_url);

            let config = momus_guard::GuardConfig {
                base_url,
                ..Default::default()
            };
            let report = momus_guard::run_guard(&test_plan, &config).await?;
            println!("{}", report);
            Ok(())
        }
        Commands::Diff {
            plan,
            baseline,
            target,
        } => {
            let content = std::fs::read_to_string(&plan)?;
            let test_plan: TestPlan = serde_json::from_str(&content)?;

            let config = momus_diff::DiffConfig {
                baseline_url: baseline,
                target_url: target,
                ..Default::default()
            };
            let report = momus_diff::run_diff(&test_plan, &config).await?;
            println!("{}", report);
            Ok(())
        }
    }
}
