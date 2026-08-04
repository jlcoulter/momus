use crate::config::ChaosExperiment;
use crate::report::ChaosReport;
use anyhow::Result;

/// Run a single chaos experiment.
///
/// Dispatches to the appropriate experiment-specific runner.
pub async fn run_experiment(experiment: &ChaosExperiment) -> Result<ChaosReport> {
    match experiment {
        ChaosExperiment::NetworkLatency {
            endpoint,
            delay_ms,
            duration_secs,
        } => run_network_latency(endpoint, *delay_ms, *duration_secs).await,
        ChaosExperiment::ConnectionReset {
            endpoint,
            reset_pct,
            duration_secs,
        } => run_connection_reset(endpoint, *reset_pct, *duration_secs).await,
        ChaosExperiment::PacketLoss {
            endpoint,
            drop_pct,
            duration_secs,
        } => run_packet_loss(endpoint, *drop_pct, *duration_secs).await,
        ChaosExperiment::ServiceError {
            endpoint,
            status,
            duration_secs,
        } => run_service_error(endpoint, *status, *duration_secs).await,
        ChaosExperiment::ServiceDown {
            endpoint,
            duration_secs,
        } => run_service_down(endpoint, *duration_secs).await,
        ChaosExperiment::CpuPressure {
            cores,
            duration_secs,
        } => run_cpu_pressure(*cores, *duration_secs).await,
        ChaosExperiment::MemoryPressure { mb, duration_secs } => {
            run_memory_pressure(*mb, *duration_secs).await
        }
        ChaosExperiment::ClockSkew {
            offset_secs,
            duration_secs,
        } => run_clock_skew(*offset_secs, *duration_secs).await,
    }
}

async fn run_network_latency(
    endpoint: &str,
    delay_ms: u64,
    duration_secs: u64,
) -> Result<ChaosReport> {
    // TODO: implement in v0.2.0
    let _ = (endpoint, delay_ms, duration_secs);
    Ok(ChaosReport {
        experiment: "network_latency".into(),
        target: endpoint.into(),
        duration_secs: duration_secs as f64,
        requests_affected: 0,
        failures_during: 0,
        self_healed: true,
        details: "not yet implemented — coming in v0.2.0".into(),
    })
}

async fn run_connection_reset(
    endpoint: &str,
    reset_pct: u8,
    duration_secs: u64,
) -> Result<ChaosReport> {
    let _ = (endpoint, reset_pct, duration_secs);
    Ok(ChaosReport {
        experiment: "connection_reset".into(),
        target: endpoint.into(),
        duration_secs: duration_secs as f64,
        requests_affected: 0,
        failures_during: 0,
        self_healed: true,
        details: "not yet implemented — coming in v0.2.0".into(),
    })
}

async fn run_packet_loss(endpoint: &str, drop_pct: u8, duration_secs: u64) -> Result<ChaosReport> {
    let _ = (endpoint, drop_pct, duration_secs);
    Ok(ChaosReport {
        experiment: "packet_loss".into(),
        target: endpoint.into(),
        duration_secs: duration_secs as f64,
        requests_affected: 0,
        failures_during: 0,
        self_healed: true,
        details: "not yet implemented — coming in v0.2.0".into(),
    })
}

async fn run_service_error(endpoint: &str, status: u16, duration_secs: u64) -> Result<ChaosReport> {
    let _ = (endpoint, status, duration_secs);
    Ok(ChaosReport {
        experiment: "service_error".into(),
        target: endpoint.into(),
        duration_secs: duration_secs as f64,
        requests_affected: 0,
        failures_during: 0,
        self_healed: true,
        details: "not yet implemented — coming in v0.2.0".into(),
    })
}

async fn run_service_down(endpoint: &str, duration_secs: u64) -> Result<ChaosReport> {
    let _ = (endpoint, duration_secs);
    Ok(ChaosReport {
        experiment: "service_down".into(),
        target: endpoint.into(),
        duration_secs: duration_secs as f64,
        requests_affected: 0,
        failures_during: 0,
        self_healed: true,
        details: "not yet implemented — coming in v0.2.0".into(),
    })
}

async fn run_cpu_pressure(cores: usize, duration_secs: u64) -> Result<ChaosReport> {
    let _ = (cores, duration_secs);
    Ok(ChaosReport {
        experiment: "cpu_pressure".into(),
        target: format!("{} cores", cores),
        duration_secs: duration_secs as f64,
        requests_affected: 0,
        failures_during: 0,
        self_healed: true,
        details: "not yet implemented — coming in v0.2.0".into(),
    })
}

async fn run_memory_pressure(mb: usize, duration_secs: u64) -> Result<ChaosReport> {
    let _ = (mb, duration_secs);
    Ok(ChaosReport {
        experiment: "memory_pressure".into(),
        target: format!("{} MB", mb),
        duration_secs: duration_secs as f64,
        requests_affected: 0,
        failures_during: 0,
        self_healed: true,
        details: "not yet implemented — coming in v0.2.0".into(),
    })
}

async fn run_clock_skew(offset_secs: i64, duration_secs: u64) -> Result<ChaosReport> {
    let _ = (offset_secs, duration_secs);
    Ok(ChaosReport {
        experiment: "clock_skew".into(),
        target: format!("{}s offset", offset_secs),
        duration_secs: duration_secs as f64,
        requests_affected: 0,
        failures_during: 0,
        self_healed: true,
        details: "not yet implemented — coming in v0.2.0".into(),
    })
}
