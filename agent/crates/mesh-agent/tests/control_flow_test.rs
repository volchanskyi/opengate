//! The server's backfill and update orders as the agent carries them out over the live
//! connection.

mod harness;

use std::time::{Duration, Instant};

use edge_tsdb::Durability;
use harness::*;
use mesh_agent_core::ml::sampler::MetricSample;
use mesh_agent_core::ml::store_sink::LocalStoreSink;
use mesh_protocol::ControlMessage;

/// Samples a second the grant allows: a full batch of 1 000 then waits 2 s.
const RATE: u32 = 500;

fn sample() -> MetricSample {
    MetricSample {
        cpu_total_percent: 50.0,
        memory_used_percent: 10.0,
        disk_used_percent: Some(20.0),
        disk_mounts_critical: Some(0),
        network_rx_bps: Some(0.0),
        network_tx_bps: Some(0.0),
        stall_cpu_some: Some(0.0),
        stall_mem_some: Some(0.0),
        stall_mem_full: Some(0.0),
        stall_io_some: Some(0.0),
        stall_io_full: Some(0.0),
        disk_await_ms: Some(0.0),
        disk_queue_depth: Some(0.0),
        processes: Vec::new(),
    }
}

fn unix_now() -> i64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .unwrap()
        .as_secs() as i64
}

/// Writes the forty minutes before now into the machine's local store, which the minute tier
/// sends as more than one batch holds.
fn seed_history(machine: &MachineBox) {
    let now = unix_now();
    let mut sink = LocalStoreSink::open(&machine.data.join("edge-tsdb"), 64 * 1024 * 1024, 1)
        .expect("open the machine's store");
    for ts in (now - 2400)..now {
        sink.record(ts, &sample(), false).expect("record");
    }
    sink.flush(Durability::Full).expect("flush");
}

/// Reads until `pick` takes a message, failing past `limit`.
async fn read_until<T>(
    conn: &mut ServerSide,
    limit: Duration,
    mut pick: impl FnMut(ControlMessage) -> Option<T>,
) -> T {
    tokio::time::timeout(limit, async {
        loop {
            if let Some(found) = pick(conn.read().await) {
                return found;
            }
        }
    })
    .await
    .expect("the agent never sent it")
}

#[tokio::test]
async fn a_granted_backfill_drains_at_the_granted_pace_and_a_deferred_one_asks_again_later() {
    let pki = Pki::new("master");
    let server = FakeServer::start(pki.clone());
    let machine = MachineBox::new();
    machine.write_identity(&pki, uuid::Uuid::new_v4());
    seed_history(&machine);
    let mut agent = machine.spawn(server.addr);
    let mut conn = server.accept().await;
    conn.expect_register().await;
    conn.acknowledge(&pki.ca_pem).await;
    let step = Duration::from_secs(10);

    read_until(&mut conn, step, |m| {
        matches!(m, ControlMessage::RequestBackfillSlot { pending_samples, .. } if pending_samples > 0)
            .then_some(())
    })
    .await;
    conn.send(ControlMessage::GrantBackfill {
        rate: RATE,
        deadline: unix_now() + 3600,
    })
    .await;
    let (tier, cursor, sent) = read_until(&mut conn, step, |m| match m {
        ControlMessage::MetricBackfillBatch {
            tier,
            samples,
            cursor,
        } => Some((tier, cursor, samples.len())),
        _ => None,
    })
    .await;
    conn.send(ControlMessage::MetricBackfillAck { tier, cursor })
        .await;
    let acked = Instant::now();
    read_until(&mut conn, step, |m| {
        matches!(m, ControlMessage::MetricBackfillBatch { .. }).then_some(())
    })
    .await;
    let pace = Duration::from_secs_f64(sent as f64 / f64::from(RATE));
    assert!(
        acked.elapsed() >= pace.mul_f64(0.8),
        "the next batch waited {:?}, where {sent} samples at {RATE} a second take {pace:?}",
        acked.elapsed()
    );

    conn.send(ControlMessage::RequestLocalHistory {
        dim: "cpu".into(),
        from_ts: unix_now() - 120,
        to_ts: unix_now(),
        max_points: 10,
    })
    .await;
    read_until(&mut conn, step, |m| match m {
        ControlMessage::LocalHistoryResponse { dim, .. } => Some(dim),
        _ => None,
    })
    .await;

    conn.send(ControlMessage::DeferBackfill { retry_after: 2 })
        .await;
    let deferred = Instant::now();
    read_until(&mut conn, step, |m| {
        matches!(m, ControlMessage::RequestBackfillSlot { .. }).then_some(())
    })
    .await;
    assert!(
        deferred.elapsed() >= Duration::from_millis(1400),
        "a deferred slot was asked for again after {:?}",
        deferred.elapsed()
    );
    agent.stop().await;
}

#[tokio::test]
async fn an_update_that_cannot_be_fetched_is_reported_failed() {
    let pki = Pki::new("master");
    let server = FakeServer::start(pki.clone());
    let machine = MachineBox::new();
    machine.write_identity(&pki, uuid::Uuid::new_v4());
    // The public key of RFC 8032's first test vector.
    std::fs::write(
        machine.data.join("update-signing-key.hex"),
        "d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a",
    )
    .unwrap();
    let mut agent = machine.spawn(server.addr);
    let mut conn = server.accept().await;
    conn.expect_register().await;
    conn.acknowledge(&pki.ca_pem).await;

    conn.send(ControlMessage::AgentUpdate {
        version: "99.0.0".into(),
        url: "https://127.0.0.1:1/mesh-agent".into(),
        sha256: "00".repeat(32),
        signature: "00".repeat(64),
    })
    .await;
    let (success, error) = read_until(&mut conn, Duration::from_secs(30), |m| match m {
        ControlMessage::AgentUpdateAck {
            version,
            success,
            error,
        } if version == "99.0.0" => Some((success, error)),
        _ => None,
    })
    .await;
    assert!(!success);
    assert!(!error.is_empty());
    agent.stop().await;
}
