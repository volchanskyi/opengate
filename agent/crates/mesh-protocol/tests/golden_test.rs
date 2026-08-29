//! Golden file tests for cross-language protocol compatibility.
//!
//! When GENERATE_GOLDEN=1, generates golden files to testdata/golden/.
//! Otherwise, reads and verifies existing golden files.

use mesh_protocol::*;
use serde::Serialize;
use std::path::PathBuf;

fn golden_dir() -> PathBuf {
    // OPENGATE_GOLDEN_DIR overrides the default lookup for environments where
    // the workspace tree is copied without `testdata/` (e.g. cargo-mutants).
    if let Ok(dir) = std::env::var("OPENGATE_GOLDEN_DIR") {
        return PathBuf::from(dir);
    }
    let mut path = PathBuf::from(env!("CARGO_MANIFEST_DIR"));
    path.push("../../../testdata/golden");
    path
}

fn should_generate() -> bool {
    std::env::var("GENERATE_GOLDEN").is_ok_and(|v| v == "1")
}

/// Write a golden file or verify it matches.
fn golden_check(name: &str, data: &[u8]) {
    let path = golden_dir().join(name);
    if should_generate() {
        std::fs::create_dir_all(golden_dir()).unwrap();
        std::fs::write(&path, data).unwrap();
        eprintln!("Generated golden file: {}", path.display());
    } else {
        let expected = std::fs::read(&path).unwrap_or_else(|e| {
            panic!(
                "Golden file {} not found. Run with GENERATE_GOLDEN=1 first. Error: {}",
                path.display(),
                e
            )
        });
        assert_eq!(
            data,
            &expected[..],
            "Golden file mismatch: {}",
            path.display()
        );
    }
}

#[test]
fn golden_control_frame_agent_register() {
    let msg = ControlMessage::AgentRegister {
        capabilities: vec![AgentCapability::RemoteDesktop, AgentCapability::Terminal],
        hostname: "golden-test-host".to_string(),
        os: "linux".to_string(),
        arch: "amd64".to_string(),
        version: "0.1.0".to_string(),
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_agent_register.bin", &encoded);
}

#[test]
fn golden_control_frame_heartbeat() {
    let msg = ControlMessage::AgentHeartbeat {
        timestamp: 1700000000,
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_heartbeat.bin", &encoded);
}

#[test]
fn golden_control_frame_agent_health_summary() {
    let msg = ControlMessage::AgentHealthSummary {
        ts: 1700000100,
        tenant_id: "00000000-0000-0000-0000-000000000002".to_string(),
        node_anomaly_rate: 0.125,
        per_family_rates: vec![
            FamilyAnomalyRate {
                family: "cpu".to_string(),
                rate: 0.25,
            },
            FamilyAnomalyRate {
                family: "proc".to_string(),
                rate: 0.5,
            },
        ],
        recent_bitmask: vec![0xAA, 0x55, 0xF0],
        sampler_ver: "sysinfo-k2".to_string(),
        model_ver: "k2-baseline-v1".to_string(),
        breaches: vec![AlertBreach {
            rule_id: "disk-full".to_string(),
            metric: "disk.used".to_string(),
            value: 95.5,
        }],
        // Deliberately absent: this fixture is also the shape an agent that
        // predates coverage sends, so the Go decoder is proven against it.
        rule_coverage: Vec::new(),
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_agent_health_summary.bin", &encoded);
}

#[test]
fn golden_control_frame_agent_health_summary_coverage() {
    let msg = ControlMessage::AgentHealthSummary {
        ts: 1700000100,
        tenant_id: "00000000-0000-0000-0000-000000000002".to_string(),
        node_anomaly_rate: 0.0625,
        per_family_rates: Vec::new(),
        recent_bitmask: Vec::new(),
        sampler_ver: "sysinfo-k2".to_string(),
        model_ver: "k2-baseline-v1".to_string(),
        breaches: Vec::new(),
        rule_coverage: vec![
            RuleCoverage {
                rule_id: "disk-critical".to_string(),
                state: RuleCoverageState::Active,
            },
            RuleCoverage {
                rule_id: "io-stalled".to_string(),
                state: RuleCoverageState::Unsupported,
            },
        ],
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_agent_health_summary_coverage.bin", &encoded);
}

#[test]
fn golden_control_frame_agent_metric_window() {
    let msg = ControlMessage::AgentMetricWindow {
        ts: 1700000160,
        tenant_id: "00000000-0000-0000-0000-000000000002".to_string(),
        dims: vec![
            MetricDim {
                name: "cpu.total".to_string(),
                avg: 42.5,
            },
            MetricDim {
                name: "mem.rss".to_string(),
                avg: 2048.0,
            },
        ],
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_agent_metric_window.bin", &encoded);
}

#[test]
fn golden_control_frame_process_report() {
    let msg = ControlMessage::ProcessReport {
        ts: 1700000220,
        tenant_id: "00000000-0000-0000-0000-000000000002".to_string(),
        top_n: vec![ProcessReportEntry {
            rank: 1,
            basename: "postgres".to_string(),
            cmdline_hash: Some("sha256:abcdef".to_string()),
            pid: 4242,
            cpu: 12.5,
            mem: 3.25,
        }],
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_process_report.bin", &encoded);
}

#[test]
fn golden_control_frame_request_health_window() {
    let msg = ControlMessage::RequestHealthWindow {
        since_ts: 1700000000,
        limit: 12,
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_request_health_window.bin", &encoded);
}

#[test]
fn golden_control_frame_health_window_response() {
    let msg = ControlMessage::HealthWindowResponse {
        summaries: vec![HealthSummary {
            ts: 1700000100,
            tenant_id: "00000000-0000-0000-0000-000000000002".to_string(),
            node_anomaly_rate: 0.125,
            per_family_rates: vec![FamilyAnomalyRate {
                family: "cpu".to_string(),
                rate: 0.25,
            }],
            recent_bitmask: vec![0xAA, 0x55, 0xF0],
            sampler_ver: "sysinfo-k2".to_string(),
            model_ver: "k2-baseline-v1".to_string(),
        }],
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_health_window_response.bin", &encoded);
}

#[test]
fn golden_control_frame_relay_ready() {
    let msg = ControlMessage::RelayReady;
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_relay_ready.bin", &encoded);
}

#[test]
fn golden_desktop_frame() {
    let frame = Frame::Desktop(DesktopFrame {
        sequence: 42,
        x: 10,
        y: 20,
        width: 1920,
        height: 1080,
        encoding: FrameEncoding::Zstd,
        data: vec![0xDE, 0xAD, 0xBE, 0xEF],
    });
    let encoded = frame.encode().unwrap();
    golden_check("desktop_frame.bin", &encoded);
}

#[test]
fn golden_desktop_frame_jpeg() {
    let frame = Frame::Desktop(DesktopFrame {
        sequence: 99,
        x: 0,
        y: 0,
        width: 1920,
        height: 1080,
        encoding: FrameEncoding::Jpeg,
        data: vec![0xFF, 0xD8, 0xFF, 0xE0],
    });
    let encoded = frame.encode().unwrap();
    golden_check("desktop_frame_jpeg.bin", &encoded);
}

#[test]
fn golden_ping_pong() {
    let ping = Frame::Ping.encode().unwrap();
    let pong = Frame::Pong.encode().unwrap();
    golden_check("ping.bin", &ping);
    golden_check("pong.bin", &pong);
}

#[test]
fn golden_control_frame_switch_to_webrtc() {
    let msg = ControlMessage::SwitchToWebRTC {
        sdp_offer: "v=0\r\no=- 0 0 IN IP4 127.0.0.1\r\n".to_string(),
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_switch_to_webrtc.bin", &encoded);
}

#[test]
fn golden_control_frame_ice_candidate() {
    let msg = ControlMessage::IceCandidate {
        candidate: "candidate:1 1 UDP 2130706431 192.168.1.1 50000 typ host".to_string(),
        mid: "0".to_string(),
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_ice_candidate.bin", &encoded);
}

#[test]
fn golden_control_frame_switch_ack() {
    let msg = ControlMessage::SwitchAck;
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_switch_ack.bin", &encoded);
}

#[test]
fn golden_control_frame_agent_update_ack() {
    let msg = ControlMessage::AgentUpdateAck {
        version: "1.2.3".to_string(),
        success: true,
        error: "".to_string(),
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_agent_update_ack.bin", &encoded);
}

#[test]
fn golden_control_frame_agent_deregistered() {
    let msg = ControlMessage::AgentDeregistered {
        reason: "device deleted by administrator".to_string(),
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_agent_deregistered.bin", &encoded);
}

#[test]
fn golden_control_frame_restart_agent() {
    let msg = ControlMessage::RestartAgent {
        reason: "restart requested from web UI".to_string(),
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_restart_agent.bin", &encoded);
}

#[test]
fn golden_control_frame_request_hardware_report() {
    let msg = ControlMessage::RequestHardwareReport;
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_request_hardware_report.bin", &encoded);
}

#[test]
fn golden_control_frame_hardware_report() {
    let msg = ControlMessage::HardwareReport {
        cpu_model: "Intel Core i7-12700K".to_string(),
        cpu_cores: 12,
        ram_total_mb: 32768,
        disk_total_mb: 512000,
        disk_free_mb: 256000,
        network_interfaces: vec![NetworkInterface {
            name: "eth0".to_string(),
            mac: "00:11:22:33:44:55".to_string(),
            ipv4: vec!["192.168.1.100".to_string()],
            ipv6: vec!["fe80::1".to_string()],
        }],
        system_uuid: "4c4c4544-0037-5a10-8054-b4c04f335432".to_string(),
        amt_available: true,
        amt_version: "16.1.30.2260".to_string(),
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_hardware_report.bin", &encoded);
}

/// A host with no Management Engine: the presence flag must ride the wire as a
/// stated `false`, which is what lets the server tell "no AMT here" apart from
/// "this agent predates AMT reporting".
#[test]
fn golden_control_frame_hardware_report_no_amt() {
    let msg = ControlMessage::HardwareReport {
        cpu_model: "AMD Ryzen 9 7950X".to_string(),
        cpu_cores: 32,
        ram_total_mb: 65536,
        disk_total_mb: 1_048_576,
        disk_free_mb: 524_288,
        network_interfaces: vec![],
        system_uuid: String::new(),
        amt_available: false,
        amt_version: String::new(),
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_hardware_report_no_amt.bin", &encoded);
}

#[test]
fn golden_control_frame_hardware_report_error() {
    let msg = ControlMessage::HardwareReportError {
        error: "failed to read system info".to_string(),
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_hardware_report_error.bin", &encoded);
}

#[test]
fn golden_control_frame_request_device_logs() {
    // The on-demand log query carries a host-source selector and a structured
    // emitting-unit filter, both additive and default-empty.
    let msg = ControlMessage::RequestDeviceLogs {
        log_level: "WARN".to_string(),
        time_from: "2026-04-01T00:00:00Z".to_string(),
        time_to: "2026-04-01T23:59:59Z".to_string(),
        search: "connection".to_string(),
        log_offset: 0,
        log_limit: 100,
        source: "journald".to_string(),
        unit: "nginx.service".to_string(),
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_request_device_logs.bin", &encoded);
}

#[test]
fn golden_control_frame_agent_metric_window_host_metrics() {
    // The host-metric emitter aggregates the 1 s sampler into a 60 s
    // AgentMetricWindow over the thirteen host-resource series, five of which
    // also carry the window maximum: an average over a minute hides a stall, and
    // the maximum is what recovers it. The dim names are the shared central
    // labels from `store_sink::series_dim_name` and `series_max_dim_name`, in
    // the order a window emits them; the net dims are primary-interface
    // throughput in bytes/second, reduced the same way reconnect-backfill rolls
    // them. `disk.used_percent` is the fullest mount and `disk.mounts_critical`
    // counts the mounts at or above the critical threshold, the five stall dims
    // are the share of the minute tasks spent stalled straight from the kernel's
    // pressure accounting, and the last three are how slow the worst block
    // device was: service time per I/O, its within-minute peak, and how many
    // I/Os were outstanding on average. So this fixture carries a file server
    // whose small system volume is nearly full beside a large, mostly empty data
    // volume, during a minute whose CPU briefly pinned, whose readers spent two
    // fifths of their time waiting on the disk, and whose data device answered a
    // typical I/O in 18.5 ms while one stretch of the minute took 812. It pins
    // that naming contract for the server.
    let msg = ControlMessage::AgentMetricWindow {
        ts: 1700000260,
        tenant_id: "00000000-0000-0000-0000-000000000002".to_string(),
        dims: vec![
            MetricDim {
                name: "cpu.total".to_string(),
                avg: 42.5,
            },
            MetricDim {
                name: "cpu.total.max".to_string(),
                avg: 100.0,
            },
            MetricDim {
                name: "mem.used_percent".to_string(),
                avg: 63.0,
            },
            MetricDim {
                name: "mem.used_percent.max".to_string(),
                avg: 71.25,
            },
            MetricDim {
                name: "disk.used_percent".to_string(),
                avg: 98.0,
            },
            MetricDim {
                name: "net.rx_bps".to_string(),
                avg: 123456.0,
            },
            MetricDim {
                name: "net.rx_bps.max".to_string(),
                avg: 987654.0,
            },
            MetricDim {
                name: "net.tx_bps".to_string(),
                avg: 654321.0,
            },
            MetricDim {
                name: "net.tx_bps.max".to_string(),
                avg: 1234567.0,
            },
            MetricDim {
                name: "disk.mounts_critical".to_string(),
                avg: 1.0,
            },
            MetricDim {
                name: "stall.cpu.some".to_string(),
                avg: 3.75,
            },
            MetricDim {
                name: "stall.mem.some".to_string(),
                avg: 0.5,
            },
            MetricDim {
                name: "stall.mem.full".to_string(),
                avg: 0.25,
            },
            MetricDim {
                name: "stall.io.some".to_string(),
                avg: 41.5,
            },
            MetricDim {
                name: "stall.io.full".to_string(),
                avg: 39.0,
            },
            MetricDim {
                name: "disk.await_ms".to_string(),
                avg: 18.5,
            },
            MetricDim {
                name: "disk.await_ms.max".to_string(),
                avg: 812.0,
            },
            MetricDim {
                name: "disk.queue_depth".to_string(),
                avg: 27.25,
            },
        ],
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_agent_metric_window_host_metrics.bin", &encoded);
}

#[test]
fn golden_control_frame_device_logs_response() {
    let msg = ControlMessage::DeviceLogsResponse {
        log_entries: vec![
            LogEntry {
                timestamp: "2026-04-01T12:00:00.000000Z".to_string(),
                level: "WARN".to_string(),
                target: "mesh_agent::connection".to_string(),
                message: "slow heartbeat detected".to_string(),
            },
            LogEntry {
                timestamp: "2026-04-01T12:00:01.000000Z".to_string(),
                level: "ERROR".to_string(),
                target: "mesh_agent::connection".to_string(),
                message: "connection lost".to_string(),
            },
        ],
        total_count: 42,
        has_more: true,
        available_units: vec![
            "mesh-agent.service".to_string(),
            "nginx.service".to_string(),
            "sshd.service".to_string(),
        ],
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_device_logs_response.bin", &encoded);
}

#[test]
fn golden_control_frame_device_logs_error() {
    let msg = ControlMessage::DeviceLogsError {
        error: "log directory not found".to_string(),
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_device_logs_error.bin", &encoded);
}

// --- WS-15: offline reconnect-backfill wire contract ---

#[test]
fn golden_control_frame_request_backfill_slot() {
    let msg = ControlMessage::RequestBackfillSlot {
        pending_samples: 123456,
        oldest_ts: 1700000000,
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_request_backfill_slot.bin", &encoded);
}

#[test]
fn golden_control_frame_grant_backfill() {
    let msg = ControlMessage::GrantBackfill {
        rate: 500,
        deadline: 1700003600,
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_grant_backfill.bin", &encoded);
}

#[test]
fn golden_control_frame_defer_backfill() {
    let msg = ControlMessage::DeferBackfill { retry_after: 30 };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_defer_backfill.bin", &encoded);
}

#[test]
fn golden_control_frame_metric_backfill_batch() {
    let msg = ControlMessage::MetricBackfillBatch {
        tier: BackfillTier::Rollup1m,
        samples: vec![
            BackfillSample {
                name: "cpu.total".to_string(),
                ts: 1700000000,
                value: 42.5,
            },
            BackfillSample {
                name: "mem.rss".to_string(),
                ts: 1700000060,
                value: 2048.0,
            },
        ],
        cursor: 1700000060,
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_metric_backfill_batch.bin", &encoded);
}

#[test]
fn golden_control_frame_metric_backfill_ack() {
    let msg = ControlMessage::MetricBackfillAck {
        tier: BackfillTier::Rollup1m,
        cursor: 1700000060,
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_metric_backfill_ack.bin", &encoded);
}

#[test]
fn golden_control_frame_request_local_history() {
    let msg = ControlMessage::RequestLocalHistory {
        dim: "cpu.total".to_string(),
        from_ts: 1699990000,
        to_ts: 1700000000,
        max_points: 1000,
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_request_local_history.bin", &encoded);
}

#[test]
fn golden_control_frame_local_history_response() {
    let msg = ControlMessage::LocalHistoryResponse {
        dim: "cpu.total".to_string(),
        points: vec![
            HistoryPoint {
                ts: 1699990000,
                value: 10.0,
            },
            HistoryPoint {
                ts: 1699990001,
                value: 11.0,
            },
        ],
        truncated: true,
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_local_history_response.bin", &encoded);
}

// --- WS-16: auto-discovery report wire contract ---

#[test]
fn golden_control_frame_discovery_report() {
    let msg = ControlMessage::DiscoveryReport {
        ts: 1700000000,
        tenant_id: String::new(),
        ports: vec![
            DiscoveredPort {
                proto: "tcp".to_string(),
                port: 5432,
                process: "postgres".to_string(),
            },
            DiscoveredPort {
                proto: "udp".to_string(),
                port: 53,
                process: "systemd-resolve".to_string(),
            },
        ],
        services: vec![DiscoveredService {
            name: "nginx.service".to_string(),
            state: "running".to_string(),
        }],
        db_engines: vec![DiscoveredDbEngine {
            engine: "postgres".to_string(),
            version: "16.2".to_string(),
            port: 5432,
        }],
        containers: vec![DiscoveredContainer {
            runtime: "docker".to_string(),
            image: "redis:7".to_string(),
            name: "cache".to_string(),
            state: "running".to_string(),
        }],
        packages: vec![DiscoveredPackage {
            name: "openssl".to_string(),
            version: "3.0.13".to_string(),
        }],
        truncated: true,
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_discovery_report.bin", &encoded);
}

// --- Maintenance mode wire contract ---

#[test]
fn golden_control_frame_maintenance_applied() {
    // Agent → server applied-state report: the agent echoes the maintenance
    // state it actually reconciled to, so the server can track applied vs.
    // desired. Rust-encoded here; the Go verifier asserts struct fidelity.
    let msg = ControlMessage::MaintenanceApplied { enabled: true };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_maintenance_applied.bin", &encoded);
}

#[test]
fn golden_handshake_server_hello() {
    let msg = HandshakeMessage::ServerHello {
        nonce: [0xAA; 32],
        cert_hash: [0xBB; 48],
    };
    let encoded = msg.encode_binary();
    golden_check("handshake_server_hello.bin", &encoded);
}

#[test]
fn golden_handshake_skip_auth() {
    let msg = HandshakeMessage::SkipAuth {
        cached_cert_hash: [0xCC; 48],
    };
    let encoded = msg.encode_binary();
    golden_check("handshake_skip_auth.bin", &encoded);
}

// --- Phase A: cross-boundary ControlMessage goldens ---
//
// Every variant below is encoded by Rust and decoded by Go in production.
// The corresponding Go verifier lives in server/internal/protocol/golden_test.go
// and asserts full struct fidelity — not just err == nil.

const GOLDEN_SESSION_TOKEN: &str =
    "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff";

#[test]
fn golden_control_frame_session_accept() {
    let msg = ControlMessage::SessionAccept {
        token: session_token(),
        relay_url: "wss://relay.example.com/relay".to_string(),
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_session_accept.bin", &encoded);
}

#[test]
fn golden_control_frame_session_reject() {
    let msg = ControlMessage::SessionReject {
        token: session_token(),
        reason: "agent is busy".to_string(),
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_session_reject.bin", &encoded);
}

#[test]
fn golden_control_frame_session_request() {
    let msg = ControlMessage::SessionRequest {
        token: session_token(),
        relay_url: "wss://relay.example.com/relay".to_string(),
        permissions: Permissions {
            desktop: true,
            terminal: true,
            file_read: true,
            file_write: false,
            input: true,
        },
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_session_request.bin", &encoded);
}

#[test]
fn golden_control_frame_agent_update() {
    let msg = ControlMessage::AgentUpdate {
        version: "1.2.3".to_string(),
        url: "https://updates.example.com/agent-1.2.3-linux-amd64".to_string(),
        sha256: "a".repeat(64),
        signature: "b".repeat(128),
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_agent_update.bin", &encoded);
}

#[test]
fn golden_control_frame_file_list_request() {
    let msg = ControlMessage::FileListRequest {
        path: "/home/ivan".to_string(),
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_file_list_request.bin", &encoded);
}

#[test]
fn golden_control_frame_file_list_response() {
    let msg = ControlMessage::FileListResponse {
        path: "/home/ivan".to_string(),
        entries: vec![
            FileEntry {
                name: "documents".to_string(),
                is_dir: true,
                size: 0,
                modified: 1_700_000_000,
            },
            FileEntry {
                name: "notes.txt".to_string(),
                is_dir: false,
                size: 2048,
                modified: 1_700_000_100,
            },
        ],
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_file_list_response.bin", &encoded);
}

#[test]
fn golden_control_frame_file_list_error() {
    let msg = ControlMessage::FileListError {
        path: "/root/secret".to_string(),
        error: "permission denied".to_string(),
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_file_list_error.bin", &encoded);
}

#[test]
fn golden_control_frame_file_download_request() {
    let msg = ControlMessage::FileDownloadRequest {
        path: "/home/ivan/notes.txt".to_string(),
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_file_download_request.bin", &encoded);
}

#[test]
fn golden_control_frame_file_upload_request() {
    let msg = ControlMessage::FileUploadRequest {
        path: "/home/ivan/uploads/archive.tar".to_string(),
        total_size: 10_485_760,
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_file_upload_request.bin", &encoded);
}

#[test]
fn golden_control_frame_chat_message() {
    let msg = ControlMessage::ChatMessage {
        text: "hello from the operator".to_string(),
        sender: "operator@example.com".to_string(),
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_chat_message.bin", &encoded);
}

// --- Phase A: edge-case goldens ---

#[test]
fn golden_control_agent_register_empty_capabilities() {
    // Optional-field-absent: empty Vec should round-trip as an empty msgpack array,
    // which Go must decode as a nil/empty Capabilities slice.
    let msg = ControlMessage::AgentRegister {
        capabilities: vec![],
        hostname: "headless-ci-runner".to_string(),
        os: "linux".to_string(),
        arch: "aarch64".to_string(),
        version: "0.1.0".to_string(),
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_agent_register_empty_capabilities.bin", &encoded);
}

#[test]
fn golden_control_agent_register_utf8() {
    // Multi-byte UTF-8 (emoji + CJK) must survive round-trip bit-for-bit.
    let msg = ControlMessage::AgentRegister {
        capabilities: vec![AgentCapability::RemoteDesktop],
        hostname: "ラップトップ-🖥️-办公室".to_string(),
        os: "macos".to_string(),
        arch: "aarch64".to_string(),
        version: "0.1.0-αβγ".to_string(),
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_agent_register_utf8.bin", &encoded);
}

#[test]
fn golden_control_hardware_report_large_size() {
    // Exercise multi-byte length header and heavy payloads without bloating the repo.
    // Each interface record is ~60 bytes; 2000 of them yields a ~100-kiB payload, which
    // forces the 4-byte BE frame length to use its high bytes (0x00 0x01 ...).
    let network_interfaces: Vec<NetworkInterface> = (0..2000)
        .map(|i| NetworkInterface {
            name: format!("veth{i:04}"),
            mac: format!("02:00:00:00:{:02x}:{:02x}", (i >> 8) & 0xff, i & 0xff),
            ipv4: vec![format!("10.0.{}.{}", (i >> 8) & 0xff, i & 0xff)],
            ipv6: vec!["fe80::1".to_string()],
        })
        .collect();
    let msg = ControlMessage::HardwareReport {
        cpu_model: "AMD EPYC 7763 64-Core Processor".to_string(),
        cpu_cores: 128,
        ram_total_mb: 524_288,
        disk_total_mb: 8_388_608,
        disk_free_mb: 4_194_304,
        network_interfaces,
        system_uuid: "b1f7c2d0-9a3e-4c58-8f61-2d7a4e0b93c5".to_string(),
        amt_available: true,
        amt_version: "12.0.95.2276".to_string(),
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    assert!(
        encoded.len() > 65_536,
        "large_size golden must exceed 64 KiB to exercise BE length high bytes; got {}",
        encoded.len()
    );
    golden_check("control_hardware_report_large_size.bin", &encoded);
}

#[test]
fn golden_control_chat_message_forward_compat() {
    // Forward-compatibility: an encoder adds a new msgpack key ("future_field")
    // that the current Go decoder does not know about. Go's rmp_serde-compatible
    // msgpack library ignores unknown map keys, so this frame must still decode
    // into a valid ChatMessage with the known fields intact.
    #[derive(Serialize)]
    struct ChatMessageWithExtra<'a> {
        #[serde(rename = "type")]
        ty: &'a str,
        text: &'a str,
        sender: &'a str,
        future_field: &'a str,
        future_number: u32,
    }
    let payload = rmp_serde::to_vec_named(&ChatMessageWithExtra {
        ty: "ChatMessage",
        text: "hello from the future",
        sender: "operator@example.com",
        future_field: "reserved-for-phase-b",
        future_number: 42,
    })
    .unwrap();
    let encoded = frame_wrap(0x01, &payload);
    golden_check("control_chat_message_forward_compat.bin", &encoded);
}

#[test]
fn golden_control_unknown_future_agent_to_server() {
    // Forward-compatibility: a newer agent emits an unknown control type.
    // The Go server must decode the frame, keep the type string, and ignore it
    // at dispatch without dropping the connection.
    #[derive(Serialize)]
    struct FutureControl<'a> {
        #[serde(rename = "type")]
        ty: &'a str,
        window_id: u32,
    }
    let payload = rmp_serde::to_vec_named(&FutureControl {
        ty: "FutureTelemetryWindow",
        window_id: 7,
    })
    .unwrap();
    let encoded = frame_wrap(0x01, &payload);
    golden_check("control_unknown_future_agent_to_server.bin", &encoded);
}

#[test]
fn golden_frame_control_le_length() {
    // Negative test: frame length field encoded little-endian instead of big-endian.
    // Under BE interpretation the declared length (10 in LE = 0x0a000000 in BE =
    // ~167 MiB) exceeds MAX_FRAME_SIZE, so Go's ReadFrame must return
    // ErrFrameTooLarge rather than hang or silently truncate.
    let payload = [0xDE, 0xAD, 0xBE, 0xEF, 0xCA, 0xFE, 0x00, 0x00, 0x00, 0x00];
    let mut encoded = Vec::with_capacity(5 + payload.len());
    encoded.push(0x01); // FRAME_CONTROL
    encoded.extend_from_slice(&(payload.len() as u32).to_le_bytes());
    encoded.extend_from_slice(&payload);
    golden_check("frame_control_le_length.bin", &encoded);
}

// --- AgentAlert: the alert transport and its self-contained evidence ---
//
// Two fixtures, because an alert has two failure modes and they are unrelated.
// The full one proves the envelope carries every field and that the evidence
// blob survives as bytes; the smallest emittable one proves that an alert
// carrying almost nothing still decodes, since both encoders drop what is empty
// and the resulting three-key map is what a quiet rule actually sends.

/// Deterministic evidence at the composition the fleet ships: eight ranked
/// dimensions, three series, ten processes, twenty redacted log lines. Built
/// here rather than borrowed from the agent so the fixture depends on the wire
/// contract alone.
fn golden_evidence() -> AlertEvidence {
    AlertEvidence {
        ranked: (0..8)
            .map(|i| RankedDim {
                dim: format!("dim.{i}"),
                score: 0.9 - f64::from(i) * 0.1,
            })
            .collect(),
        series: (0..3)
            .map(|i| EvidenceSeries {
                dim: format!("dim.{i}"),
                points: (0..512)
                    .map(|p| HistoryPoint {
                        ts: 1_700_000_000 + i64::from(p),
                        value: f64::from(p) * 0.5,
                    })
                    .collect(),
            })
            .collect(),
        processes: (0..10)
            .map(|i| ProcessReportEntry {
                rank: i,
                basename: format!("proc{i}"),
                cmdline_hash: None,
                pid: 1000 + i,
                cpu: f64::from(i),
                mem: f64::from(i) * 2.0,
            })
            .collect(),
        log_samples: (0..20).map(|i| format!("log line {i}")).collect(),
        truncated: false,
    }
}

#[test]
fn golden_control_frame_agent_alert() {
    let msg = ControlMessage::AgentAlert {
        alert_id: "0f1e2d3c-4b5a-6978-8796-a5b4c3d2e1f0".to_string(),
        rule_id: "disk-latency-sustained".to_string(),
        rule_version: 3,
        severity: AlertSeverity::Critical,
        metric: "disk.await_ms".to_string(),
        value: Some(41.5),
        window_start_ts: 1_700_000_000,
        window_end_ts: 1_700_000_300,
        observed_ts: 1_700_000_305,
        backfilled: false,
        evidence_codec: EVIDENCE_CODEC.to_string(),
        evidence: golden_evidence().encode().unwrap(),
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_agent_alert.bin", &encoded);
}

#[test]
fn golden_control_frame_agent_alert_min() {
    // The smallest alert that can be emitted: severity and backfilled are always
    // stated, everything else is absent. Three keys is the whole map, and the Go
    // decoder has to accept it — reading "no severity said" as anything but a
    // decode failure is how a critical alert turns into an informational one.
    let msg = ControlMessage::AgentAlert {
        alert_id: String::new(),
        rule_id: String::new(),
        rule_version: 0,
        severity: AlertSeverity::Info,
        metric: String::new(),
        value: None,
        window_start_ts: 0,
        window_end_ts: 0,
        observed_ts: 0,
        backfilled: false,
        evidence_codec: String::new(),
        evidence: Vec::new(),
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_agent_alert_min.bin", &encoded);
}

#[test]
fn golden_alert_evidence_blob() {
    // The evidence blob on its own, so the Go side can prove it inflates with
    // stdlib compress/flate and decodes to the exact composition above. The
    // envelope fixture cannot prove that: it carries the blob as opaque bytes,
    // which is the whole point of a codec named on the message.
    golden_check("alert_evidence.bin", &golden_evidence().encode().unwrap());
}

#[test]
fn alert_evidence_round_trips_through_its_own_codec() {
    let evidence = golden_evidence();
    let blob = evidence.encode().unwrap();
    assert!(
        blob.len() <= MAX_EVIDENCE_BYTES,
        "the shipped composition must fit the cap it was sized against: {} bytes",
        blob.len()
    );
    assert_eq!(
        AlertEvidence::decode(&blob, EVIDENCE_CODEC).unwrap(),
        evidence
    );
}

#[test]
fn alert_evidence_refuses_a_codec_it_cannot_read() {
    let blob = golden_evidence().encode().unwrap();
    let err = AlertEvidence::decode(&blob, "brotli-9").unwrap_err();
    assert!(
        matches!(err, ProtocolError::UnknownEvidenceCodec(ref c) if c == "brotli-9"),
        "an unreadable codec must name itself, not decode as empty evidence: {err}"
    );
}

#[test]
fn alert_evidence_refuses_a_damaged_blob() {
    // Two ways a blob arrives damaged, and neither may produce evidence. Half a
    // blob may fail in the decompressor or in the decoder that reads what came
    // out of it — which of the two is not the contract; that nothing partial is
    // ever handed back as if it were the device's account of the event is.
    let whole = golden_evidence().encode().unwrap();

    let mut halved = whole.clone();
    halved.truncate(whole.len() / 2);
    assert!(
        AlertEvidence::decode(&halved, EVIDENCE_CODEC).is_err(),
        "a truncated blob must fail rather than yield partial evidence"
    );

    let mut flipped = whole;
    for byte in flipped.iter_mut().skip(4).take(64) {
        *byte ^= 0xFF;
    }
    assert!(
        AlertEvidence::decode(&flipped, EVIDENCE_CODEC).is_err(),
        "a corrupted blob must fail rather than yield invented evidence"
    );
}

// --- helpers ---

fn session_token() -> SessionToken {
    // SessionToken has no public constructor-from-string; we generate a token and
    // then override its contents via msgpack round-trip to keep determinism.
    let fixed: SessionTokenShim = SessionTokenShim(GOLDEN_SESSION_TOKEN.to_string());
    let bytes = rmp_serde::to_vec(&fixed).unwrap();
    rmp_serde::from_slice(&bytes).unwrap()
}

#[derive(Serialize)]
struct SessionTokenShim(String);

fn frame_wrap(frame_type: u8, payload: &[u8]) -> Vec<u8> {
    let mut buf = Vec::with_capacity(5 + payload.len());
    buf.push(frame_type);
    buf.extend_from_slice(&(payload.len() as u32).to_be_bytes());
    buf.extend_from_slice(payload);
    buf
}
