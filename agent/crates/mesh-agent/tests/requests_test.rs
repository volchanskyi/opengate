//! A slow request runs beside the control loop: the loop keeps answering, a second request of the
//! same kind is answered busy, and each request is answered by its own deadline.

mod harness;

use std::time::{Duration, Instant};

use harness::*;
use mesh_protocol::{request_error, ControlMessage};

/// A `journalctl` that hangs until a `journalctl.fast` file appears beside it, and records each
/// start with its process id.
const FAKE_JOURNALCTL: &str = r#"#!/usr/bin/env bash
here="$(dirname "$0")"
printf '%s %s\n' "$$" "$*" >>"$here/journalctl.starts"
if [ -e "$here/journalctl.fast" ]; then
  case " $* " in
    *" -F "*) printf 'sshd.service\n' ;;
    *) printf '%s\n' '{"MESSAGE":"accepted key","PRIORITY":"6","__REALTIME_TIMESTAMP":"1760000000000000","_SYSTEMD_UNIT":"sshd.service"}' ;;
  esac
  exit 0
fi
exec sleep 60
"#;

fn host_logs_request() -> ControlMessage {
    ControlMessage::RequestDeviceLogs {
        log_level: String::new(),
        time_from: String::new(),
        time_to: String::new(),
        search: String::new(),
        log_offset: 0,
        log_limit: 100,
        source: "host".to_string(),
        unit: String::new(),
    }
}

/// Reads until a message `pick` accepts, returning it with how long it took.
async fn read_until<T>(
    conn: &mut ServerSide,
    mut pick: impl FnMut(ControlMessage) -> Option<T>,
) -> T {
    loop {
        if let Some(found) = pick(conn.read().await) {
            return found;
        }
    }
}

fn logs_error(msg: ControlMessage) -> Option<String> {
    match msg {
        ControlMessage::DeviceLogsError { error } => Some(error),
        _ => None,
    }
}

fn starts(machine: &MachineBox) -> Vec<(i32, String)> {
    std::fs::read_to_string(machine.bin.join("journalctl.starts"))
        .unwrap_or_default()
        .lines()
        .filter_map(|line| {
            let (pid, args) = line.split_once(' ')?;
            Some((pid.parse().ok()?, args.to_string()))
        })
        .collect()
}

fn alive(pid: i32) -> bool {
    // SAFETY: signal 0 only asks whether the process exists.
    unsafe { libc::kill(pid, 0) == 0 }
}

#[tokio::test]
async fn a_stalled_log_read_never_stalls_the_machine() {
    let pki = Pki::new("master");
    let server = FakeServer::start(pki.clone());
    let machine = MachineBox::new();
    machine.write_identity(&pki, uuid::Uuid::new_v4());
    machine.install_tool("journalctl", FAKE_JOURNALCTL);

    let mut agent = machine.spawn(server.addr);
    let mut conn = server.accept().await;
    conn.expect_register().await;
    conn.acknowledge(&pki.ca_pem).await;

    let asked = Instant::now();
    conn.send(host_logs_request()).await;
    let deadline = Instant::now() + STEP;
    while starts(&machine).is_empty() {
        assert!(Instant::now() < deadline, "journalctl never started");
        tokio::time::sleep(Duration::from_millis(20)).await;
    }

    let sent = Instant::now();
    conn.send(ControlMessage::SetMaintenanceMode { enabled: true })
        .await;
    read_until(&mut conn, |m| {
        matches!(m, ControlMessage::MaintenanceApplied { .. }).then_some(())
    })
    .await;
    assert!(
        sent.elapsed() < Duration::from_secs(1),
        "the loop answered late while logs were read"
    );

    let sent = Instant::now();
    conn.send(host_logs_request()).await;
    let second = read_until(&mut conn, logs_error).await;
    assert_eq!(second, request_error::BUSY);
    assert!(
        sent.elapsed() < Duration::from_secs(1),
        "busy came late: {:?}",
        sent.elapsed()
    );
    assert_eq!(starts(&machine).len(), 1, "a second journalctl started");

    let first = read_until(&mut conn, logs_error).await;
    assert_eq!(first, request_error::TIMED_OUT);
    let took = asked.elapsed();
    assert!(
        took >= Duration::from_secs(9) && took < Duration::from_secs(12),
        "the first request was answered after {took:?}, not at its 10 s deadline"
    );
    let (pid, _) = starts(&machine)[0].clone();
    let deadline = Instant::now() + Duration::from_secs(5);
    while alive(pid) {
        assert!(
            Instant::now() < deadline,
            "the stalled journalctl outlived its request"
        );
        tokio::time::sleep(Duration::from_millis(20)).await;
    }

    std::fs::write(machine.bin.join("journalctl.fast"), "").unwrap();
    conn.send(host_logs_request()).await;
    let (entries, units) = read_until(&mut conn, |m| match m {
        ControlMessage::DeviceLogsResponse {
            log_entries,
            available_units,
            ..
        } => Some((log_entries, available_units)),
        _ => None,
    })
    .await;
    assert_eq!(entries.len(), 1);
    assert_eq!(entries[0].message, "accepted key");
    assert_eq!(units, vec!["sshd.service".to_string()]);
    agent.stop().await;
}

#[tokio::test]
async fn a_hardware_report_never_stalls_the_machine() {
    let pki = Pki::new("master");
    let server = FakeServer::start(pki.clone());
    let machine = MachineBox::new();
    machine.write_identity(&pki, uuid::Uuid::new_v4());

    let mut agent = machine.spawn(server.addr);
    let mut conn = server.accept().await;
    conn.expect_register().await;
    conn.acknowledge(&pki.ca_pem).await;

    conn.send(ControlMessage::RequestHardwareReport).await;
    let report = read_until(&mut conn, |m| match m {
        ControlMessage::HardwareReport { cpu_cores, .. } => Some(cpu_cores),
        ControlMessage::HardwareReportError { error } => panic!("hardware report failed: {error}"),
        _ => None,
    })
    .await;
    assert!(report > 0, "the report describes this machine");
    agent.stop().await;
}
