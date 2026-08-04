use anyhow::Result;
use clap::{Parser, Subcommand};
use momus::ast::*;
use momus::engine::runner;
use std::path::PathBuf;

#[derive(Parser)]
#[command(name = "momus", about = "Generic API test harness with a composable assertion AST")]
struct Cli {
    #[command(subcommand)]
    command: Commands,
}

#[derive(Subcommand)]
enum Commands {
    /// Run a test plan from a JSON file.
    Run {
        /// Path to the test plan JSON file.
        plan: PathBuf,
        /// Base URL override.
        #[arg(long)]
        base_url: Option<String>,
        /// Output directory for results.
        #[arg(long, default_value = "./output")]
        output: PathBuf,
    },
    /// Validate a test plan JSON file (parse + check structure).
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

    match cli.command {
        Commands::Run {
            plan,
            base_url,
            output,
        } => {
            let content = std::fs::read_to_string(&plan)?;
            let mut test_plan: TestPlan = serde_json::from_str(&content)?;

            // Override base URL if provided
            if let Some(url) = base_url {
                test_plan.base_url = url;
            }

            tracing::info!(
                "Running test plan '{}' with {} test(s) against {}",
                test_plan.name,
                test_plan.total_tests(),
                test_plan.base_url
            );

            let report = runner::execute_plan(&test_plan).await?;

            println!("{}", report);

            // Write results
            std::fs::create_dir_all(&output)?;
            report.write_results(&output)?;
            println!("\nResults written to: {}/results/", output.display());

            // Exit with non-zero status if any tests failed
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
                any(|| async { (axum::http::StatusCode::OK, Json(serde_json::json!({"status": "ok"}))) }),
            );
            axum::serve(listener, app).await?;
            Ok(())
        }
    }
}
