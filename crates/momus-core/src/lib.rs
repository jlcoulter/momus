pub mod ast;
pub mod config;
pub mod deps;
pub mod engine;
pub mod junit;
pub mod leak;
pub mod transport;

/// Write a serializable report as pretty-printed JSON to `{output_dir}/{filename}`.
///
/// Creates the output directory if it doesn't exist.
pub fn write_report_json<T: serde::Serialize>(
    output_dir: &std::path::Path,
    filename: &str,
    report: &T,
) -> anyhow::Result<()> {
    std::fs::create_dir_all(output_dir)?;
    let path = output_dir.join(filename);
    let json = serde_json::to_string_pretty(report)?;
    std::fs::write(&path, json)?;
    tracing::info!("Report written to: {}", path.display());
    Ok(())
}
