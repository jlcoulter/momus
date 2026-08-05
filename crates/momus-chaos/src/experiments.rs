use crate::config::ChaosExperiment;
use crate::report::ChaosReport;
use anyhow::{Context, Result};
use std::process::Command;
use std::time::Instant;

/// Run a single chaos experiment.
pub async fn run_experiment(experiment: &ChaosExperiment) -> Result<ChaosReport> {
    match experiment {
        ChaosExperiment::NetworkLatency {
            endpoint,
            delay_ms,
            duration_secs,
        } => run_network_latency(endpoint, *delay_ms, *duration_secs).await,
        ChaosExperiment::ServiceError {
            endpoint,
            status,
            duration_secs,
        } => run_service_error(endpoint, *status, *duration_secs).await,
        ChaosExperiment::ServiceDown {
            endpoint,
            duration_secs,
        } => run_service_down(endpoint, *duration_secs).await,
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

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/// Run a shell command via sudo. Returns `Ok(())` on success.
fn run_sudo(cmd: &str) -> Result<()> {
    let output = Command::new("sudo")
        .arg("sh")
        .arg("-c")
        .arg(cmd)
        .output()
        .context("sudo is not available or failed to execute")?;

    if output.status.success() {
        Ok(())
    } else {
        let stderr = String::from_utf8_lossy(&output.stderr);
        anyhow::bail!("command failed: {}", stderr.trim())
    }
}

/// Extract the TCP port from a URL string.
fn extract_port(url: &str) -> Option<u16> {
    if url.is_empty() {
        return None;
    }
    let is_https = url.starts_with("https://");
    let url = url
        .trim_start_matches("http://")
        .trim_start_matches("https://");

    let host_part = url.split('/').next()?;

    if host_part.is_empty() {
        return None;
    }

    if let Some(port_str) = host_part.split(':').nth(1) {
        port_str.parse::<u16>().ok()
    } else if is_https {
        Some(443)
    } else {
        Some(80)
    }
}

/// Detect the default network interface.
fn detect_interface() -> String {
    let output = Command::new("sh")
        .arg("-c")
        .arg("ip route get 1 2>/dev/null | grep -oP 'dev \\K\\S+'")
        .output();

    if let Ok(output) = output
        && output.status.success()
    {
        let iface = String::from_utf8_lossy(&output.stdout).trim().to_string();
        if !iface.is_empty() {
            return iface;
        }
    }
    "eth0".to_string()
}

/// Get the current Unix timestamp via `date +%s`.
fn get_unix_timestamp() -> Result<i64> {
    let output = Command::new("date")
        .arg("+%s")
        .output()
        .context("failed to run date")?;
    let s = String::from_utf8_lossy(&output.stdout).trim().to_string();
    s.parse::<i64>().context("failed to parse date output")
}

// ---------------------------------------------------------------------------
// Existing experiments (unchanged)
// ---------------------------------------------------------------------------

/// Network latency: inject artificial delay by sleeping before requests.
async fn run_network_latency(
    endpoint: &str,
    delay_ms: u64,
    duration_secs: u64,
) -> Result<ChaosReport> {
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
        if result.is_err() || result.is_ok_and(|r| !r.status().is_success()) {
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
        details: format!(
            "Injected {}ms delay for {}s, {} requests affected, {} failures",
            delay_ms, duration_secs, affected, failures
        ),
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
        details: format!(
            "Checked endpoint {} for {}s, expected status {}, {} failures out of {} requests",
            endpoint, duration_secs, status, failures, affected
        ),
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
        details: format!(
            "Checked endpoint {} for {}s, {} failures out of {} requests ({}). \
             If the service is still up, the fault was not injected.",
            endpoint,
            duration_secs,
            failures,
            affected,
            if is_down { "DOWN" } else { "UP" }
        ),
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

// ---------------------------------------------------------------------------
// New platform-specific experiments
// ---------------------------------------------------------------------------

/// Connection reset: use iptables to reject TCP traffic with tcp-reset.
///
/// Applies an iptables `REJECT --reject-with tcp-reset` rule on the target
/// port using the `statistic` module so that only a configurable percentage
/// of packets are reset. The rule is removed after the experiment duration.
async fn run_connection_reset(
    endpoint: &str,
    reset_pct: u8,
    duration_secs: u64,
) -> Result<ChaosReport> {
    let start = Instant::now();

    let port = match extract_port(endpoint) {
        Some(p) => p,
        None => {
            return Ok(ChaosReport {
                experiment: "connection_reset".into(),
                target: endpoint.into(),
                duration_secs: 0.0,
                requests_affected: 0,
                failures_during: 0,
                self_healed: true,
                details: format!(
                    "Could not extract port from endpoint '{}'. \
                     Expected format: http://host:port/path",
                    endpoint
                ),
            });
        }
    };

    // Use iptables statistic module for percentage-based rejection
    let probability = (reset_pct as f64).clamp(0.0, 100.0) / 100.0;
    let apply_cmd = format!(
        "iptables -A INPUT -p tcp --dport {} -m statistic --mode random \
         --probability {} -j REJECT --reject-with tcp-reset",
        port, probability
    );

    if let Err(e) = run_sudo(&apply_cmd) {
        return Ok(ChaosReport {
            experiment: "connection_reset".into(),
            target: endpoint.into(),
            duration_secs: 0.0,
            requests_affected: 0,
            failures_during: 0,
            self_healed: false,
            details: format!(
                "Connection reset requires iptables. Failed to apply rule: {}. \
                 Use `sudo {}` manually.",
                e, apply_cmd
            ),
        });
    }

    // Monitor the endpoint during the fault window
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

    // Remove the iptables rule
    let remove_cmd = format!(
        "iptables -D INPUT -p tcp --dport {} -m statistic --mode random \
         --probability {} -j REJECT --reject-with tcp-reset",
        port, probability
    );
    let removal_ok = run_sudo(&remove_cmd).is_ok();

    Ok(ChaosReport {
        experiment: "connection_reset".into(),
        target: endpoint.into(),
        duration_secs: start.elapsed().as_secs_f64(),
        requests_affected: affected,
        failures_during: failures,
        self_healed: removal_ok,
        details: format!(
            "Injected TCP RST on port {} ({}% reset) for {}s, \
             {} requests affected, {} failures",
            port, reset_pct, duration_secs, affected, failures
        ),
    })
}

/// Packet loss: use tc netem to add packet loss on the default interface.
///
/// Adds a `netem loss` qdisc on the detected default network interface,
/// monitors the endpoint during the fault window, then removes the qdisc.
async fn run_packet_loss(endpoint: &str, drop_pct: u8, duration_secs: u64) -> Result<ChaosReport> {
    let start = Instant::now();
    let iface = detect_interface();

    let apply_cmd = format!("tc qdisc add dev {} root netem loss {}%", iface, drop_pct);

    if let Err(e) = run_sudo(&apply_cmd) {
        return Ok(ChaosReport {
            experiment: "packet_loss".into(),
            target: endpoint.into(),
            duration_secs: 0.0,
            requests_affected: 0,
            failures_during: 0,
            self_healed: false,
            details: format!(
                "Packet loss requires tc netem. Failed to apply on interface '{}': {}. \
                 Use `sudo {}` manually.",
                iface, e, apply_cmd
            ),
        });
    }

    // Monitor the endpoint during the fault window
    let client = reqwest::Client::builder()
        .timeout(std::time::Duration::from_secs(10))
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

    // Remove the tc qdisc
    let remove_cmd = format!("tc qdisc del dev {} root", iface);
    let removal_ok = run_sudo(&remove_cmd).is_ok();

    Ok(ChaosReport {
        experiment: "packet_loss".into(),
        target: endpoint.into(),
        duration_secs: start.elapsed().as_secs_f64(),
        requests_affected: affected,
        failures_during: failures,
        self_healed: removal_ok,
        details: format!(
            "Injected {}% packet loss on interface {} for {}s, \
             {} requests affected, {} failures",
            drop_pct, iface, duration_secs, affected, failures
        ),
    })
}

/// Clock skew: adjust the system clock and restore it after the experiment.
///
/// Saves the current Unix timestamp, applies an offset via `sudo date -s`,
/// waits for the experiment duration, then attempts to restore the original
/// time using `date`, `chronyc`, `ntpdate`, or systemd service restarts.
async fn run_clock_skew(offset_secs: i64, duration_secs: u64) -> Result<ChaosReport> {
    let start = Instant::now();

    // Save the current time
    let original_time = match get_unix_timestamp() {
        Ok(t) => t,
        Err(e) => {
            return Ok(ChaosReport {
                experiment: "clock_skew".into(),
                target: format!("offset {}s", offset_secs),
                duration_secs: 0.0,
                requests_affected: 0,
                failures_during: 0,
                self_healed: true,
                details: format!(
                    "Failed to read current time: {}. Cannot apply clock skew.",
                    e
                ),
            });
        }
    };

    // Apply the skew
    let skewed_time = original_time + offset_secs;
    let apply_cmd = format!("date -s '@{}'", skewed_time);

    if let Err(e) = run_sudo(&apply_cmd) {
        return Ok(ChaosReport {
            experiment: "clock_skew".into(),
            target: format!("offset {}s", offset_secs),
            duration_secs: 0.0,
            requests_affected: 0,
            failures_during: 0,
            self_healed: false,
            details: format!(
                "Clock skew requires sudo date. Failed to set clock: {}. \
                 Use `sudo {}` manually.",
                e, apply_cmd
            ),
        });
    }

    // Wait for the duration (we can't easily make HTTP requests during clock
    // skew since TLS cert validation would fail, but we track the attempt)
    tokio::time::sleep(std::time::Duration::from_secs(duration_secs)).await;

    // Restore the clock — try multiple methods
    let restore_methods: &[&str] = &[
        &format!("date -s '@{}'", original_time),
        "chronyc makestep 2>/dev/null",
        "ntpdate -u pool.ntp.org 2>/dev/null || true",
        "systemctl restart chronyd 2>/dev/null || systemctl restart ntp 2>/dev/null || true",
    ];

    let mut restored = false;
    let mut restore_details = String::new();
    for method in restore_methods {
        if run_sudo(method).is_ok() {
            restored = true;
            restore_details = format!("restored via: {}", method);
            break;
        }
    }

    if !restored {
        restore_details = format!(
            "WARNING: could not restore clock automatically. \
             Original timestamp was {}. \
             Use `sudo date -s '@{}'` to restore manually.",
            original_time, original_time
        );
    }

    Ok(ChaosReport {
        experiment: "clock_skew".into(),
        target: format!("offset {}s", offset_secs),
        duration_secs: start.elapsed().as_secs_f64(),
        requests_affected: 0,
        failures_during: 0,
        self_healed: restored,
        details: format!(
            "Skewed clock by {}s ({} -> {}) for {}s. {}",
            offset_secs, original_time, skewed_time, duration_secs, restore_details
        ),
    })
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    use super::*;

    // -- extract_port tests --------------------------------------------------

    #[test]
    fn test_extract_port_with_port() {
        assert_eq!(extract_port("http://localhost:8080/api"), Some(8080));
        assert_eq!(extract_port("https://example.com:443/path"), Some(443));
        assert_eq!(extract_port("http://192.168.1.1:3000"), Some(3000));
    }

    #[test]
    fn test_extract_port_default_http() {
        assert_eq!(extract_port("http://example.com/api"), Some(80));
        assert_eq!(extract_port("http://localhost/test"), Some(80));
    }

    #[test]
    fn test_extract_port_default_https() {
        assert_eq!(extract_port("https://example.com/api"), Some(443));
        assert_eq!(extract_port("https://secure.example.com/path"), Some(443));
    }

    #[test]
    fn test_extract_port_no_scheme() {
        // Without a scheme, defaults to port 80
        assert_eq!(extract_port("localhost:8080"), Some(8080));
        assert_eq!(extract_port("example.com"), Some(80));
    }

    #[test]
    fn test_extract_port_empty() {
        assert_eq!(extract_port(""), None);
    }

    // -- run_sudo error path tests -------------------------------------------

    #[test]
    fn test_run_sudo_fails_on_bad_command() {
        // When sudo is not available or the command fails, run_sudo should
        // return an error.  This exercises the error-handling path that the
        // chaos experiments use when system tools are missing.
        let result = run_sudo("nonexistent-command-12345");
        assert!(result.is_err(), "run_sudo should fail for bad commands");
    }

    // -- detect_interface tests ----------------------------------------------

    #[test]
    fn test_detect_interface_returns_something() {
        // detect_interface should always return a non-empty string (either
        // the real interface or the "eth0" fallback).
        let iface = detect_interface();
        assert!(!iface.is_empty(), "Interface should not be empty");
    }

    // -- get_unix_timestamp tests --------------------------------------------

    #[test]
    fn test_get_unix_timestamp_works() {
        let ts = get_unix_timestamp();
        assert!(
            ts.is_ok(),
            "Should be able to read current time: {:?}",
            ts.err()
        );
        let ts = ts.unwrap();
        // Should be a reasonable recent timestamp (2020-01-01 = 1577836800)
        assert!(ts > 1577836800, "Timestamp {} should be after 2020", ts);
    }
}
