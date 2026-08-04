use crate::config::ChaosExperiment;
use crate::report::ChaosReport;
use anyhow::Result;
use std::time::Instant;

/// Run a single chaos experiment.
pub async fn run_experiment(experiment: &ChaosExperiment) -> Result<ChaosReport> {
    match experiment {
        ChaosExperiment::NetworkLatency { endpoint, delay_ms, duration_secs } => {
            run_network_latency(endpoint, *delay_ms, *duration_secs).await
        }
        ChaosExperiment::ServiceError { endpoint, status, duration_secs } => {
            run_service_error(endpoint, *status, *duration_secs).await
        }
        ChaosExperiment::ServiceDown { endpoint, duration_secs } => {
            run_service_down(endpoint, *duration_secs).await
        }
        // Platform-specific experiments: document what's needed
        ChaosExperiment::ConnectionReset { .. } => Ok(ChaosReport {
            experiment: "connection_reset".into(),
            target: "requires iptables".into(),
            duration_secs: 0.0,
            requests_affected: 0,
            failures_during: 0,
            self_healed: true,
            details: "Connection reset requires iptables rules. Use `sudo iptables -A INPUT ... -j REJECT --reject-with tcp-reset`".into(),
        }),
        ChaosExperiment::PacketLoss { .. } => Ok(ChaosReport {
            experiment: "packet_loss".into(),
            target: "requires tc netem".into(),
            duration_secs: 0.0,
            requests_affected: 0,
            failures_during: 0,
            self_healed: true,
            details: "Packet loss requires tc netem. Use `sudo tc qdisc add dev eth0 root netem loss 10%`".into(),
        }),
        ChaosExperiment::CpuPressure { cores, duration_secs } => {
            run_cpu_pressure(*cores, *duration_secs).await
        }
        ChaosExperiment::MemoryPressure { mb, duration_secs } => {
            run_memory_pressure(*mb, *duration_secs).await
        }
        ChaosExperiment::ClockSkew { .. } => Ok(ChaosReport {
            experiment: "clock_skew".into(),
            target: "requires faketime/libfaketime".into(),
            duration_secs: 0.0,
            requests_affected: 0,
            failures_during: 0,
            self_healed: true,
            details: "Clock skew requires faketime. Use `faketime '2024-01-01 00:00:00' command`".into(),
        }),
    }
}

/// Network latency: inject artificial delay by sleeping before requests.
async fn run_network_latency(endpoint: &str, delay_ms: u64, duration_secs: u64) -> Result<ChaosReport> {
    let start = Instant::now();
    let client = reqwest::Client::builder()
        .timeout(std::time::Duration::from_secs(duration_secs + 10))
        .build()?;

    let mut failures = 0u64;
    let mut affected = 0u64;

    while start.elapsed().as_secs() < duration_secs {
        // Sleep to simulate latency
        tokio::time::sleep(std::time::Duration::from_millis(delay_ms)).await;

        // Send a request to the target
        let result = client.get(endpoint).send().await;
        affected += 1;
        if result.is_err() || result.map_or(false, |r| !r.status().is_success()) {
            failures += 1;
        }
    }

    Ok(ChaosReport {
        experiment: "network_latency".into(),
        target: endpoint.into(),
        duration_secs: start.elapsed().as_secs_f64(),
        requests_affected: affected,
        failures_during: failures,
        self_healed: true,
        details: format!("Injected {}ms delay for {}s, {} requests affected, {} failures", delay_ms, duration_secs, affected, failures),
    })
}

/// Service error: verify the endpoint returns errors (simulated by checking status).
async fn run_service_error(endpoint: &str, status: u16, duration_secs: u64) -> Result<ChaosReport> {
    let start = Instant::now();
    let client = reqwest::Client::builder()
        .timeout(std::time::Duration::from_secs(10))
        .build()?;

    let mut failures = 0u64;
    let mut affected = 0u64;

    while start.elapsed().as_secs() < duration_secs {
        let result = client.get(endpoint).send().await;
        affected += 1;
        match result {
            Ok(resp) => {
                if resp.status().as_u16() == status || resp.status().is_server_error() {
                    failures += 1;
                }
            }
            Err(_) => {
                failures += 1;
            }
        }
        tokio::time::sleep(std::time::Duration::from_millis(500)).await;
    }

    Ok(ChaosReport {
        experiment: "service_error".into(),
        target: endpoint.into(),
        duration_secs: start.elapsed().as_secs_f64(),
        requests_affected: affected,
        failures_during: failures,
        self_healed: true,
        details: format!("Checked endpoint {} for {}s, expected status {}, {} failures out of {} requests", endpoint, duration_secs, status, failures, affected),
    })
}

/// Service down: verify the endpoint becomes unreachable.
async fn run_service_down(endpoint: &str, duration_secs: u64) -> Result<ChaosReport> {
    let start = Instant::now();
    let client = reqwest::Client::builder()
        .timeout(std::time::Duration::from_secs(5))
        .build()?;

    let mut failures = 0u64;
    let mut affected = 0u64;

    while start.elapsed().as_secs() < duration_secs {
        let result = client.get(endpoint).send().await;
        affected += 1;
        if result.is_err() {
            failures += 1;
        }
        tokio::time::sleep(std::time::Duration::from_millis(500)).await;
    }

    let is_down = failures == affected;
    Ok(ChaosReport {
        experiment: "service_down".into(),
        target: endpoint.into(),
        duration_secs: start.elapsed().as_secs_f64(),
        requests_affected: affected,
        failures_during: failures,
        self_healed: !is_down,
        details: format!("Checked endpoint {} for {}s, {} failures out of {} requests ({}). If the service is still up, the fault was not injected.", endpoint, duration_secs, failures, affected, if is_down { "DOWN" } else { "UP" }),
    })
}

/// CPU pressure: spawn busy-looping threads.
async fn run_cpu_pressure(cores: usize, duration_secs: u64) -> Result<ChaosReport> {
    let start = Instant::now();
    let mut handles = Vec::new();

    for _ in 0..cores {
        handles.push(tokio::task::spawn_blocking(move || {
            let end = Instant::now() + std::time::Duration::from_secs(duration_secs);
            while Instant::now() < end {
                // Busy loop — consume CPU
                std::hint::spin_loop();
            }
        }));
    }

    for handle in handles {
        let _ = handle.await;
    }

    Ok(ChaosReport {
        experiment: "cpu_pressure".into(),
        target: format!("{} cores", cores),
        duration_secs: start.elapsed().as_secs_f64(),
        requests_affected: 0,
        failures_during: 0,
        self_healed: true,
        details: format!("Saturated {} CPU core(s) for {}s", cores, duration_secs),
    })
}

/// Memory pressure: allocate and hold memory.
async fn run_memory_pressure(mb: usize, duration_secs: u64) -> Result<ChaosReport> {
    let start = Instant::now();
    let bytes = mb * 1024 * 1024;

    // Allocate a vector of the requested size
    let mut memory: Vec<u8> = Vec::with_capacity(bytes);
    // Fill with data to force actual allocation
    memory.resize(bytes, 0xFF);

    // Hold the memory for the duration
    tokio::time::sleep(std::time::Duration::from_secs(duration_secs)).await;

    // Drop it (memory is freed)
    drop(memory);

    Ok(ChaosReport {
        experiment: "memory_pressure".into(),
        target: format!("{} MB", mb),
        duration_secs: start.elapsed().as_secs_f64(),
        requests_affected: 0,
        failures_during: 0,
        self_healed: true,
        details: format!("Allocated and held {} MB for {}s", mb, duration_secs),
    })
}
