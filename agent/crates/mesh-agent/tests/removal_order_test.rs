//! A removal order, as a frame or close code 3, ends in self-removal; a refused fast path is
//! retried in full; only the server's acknowledgement verifies an update.

mod harness;

use harness::*;
use mesh_protocol::{close_code, ControlMessage};

fn assert_removed(machine: &MachineBox) {
    assert!(!machine.has_identity(), "identity files remain");
    assert!(
        !machine.data.join("edge-tsdb").exists(),
        "the local store remains"
    );
    assert!(!machine.data.exists(), "the data folder remains");
    assert!(!machine.ca_path.exists(), "the trusted master key remains");
    assert!(!machine.exe.exists(), "the binary remains");
    assert!(!machine.unit.exists(), "the unit file remains");
    assert!(!machine.unit_enabled(), "the unit is still enabled");
    let trace = machine.trace();
    assert!(trace.contains("daemon-reload"), "no daemon-reload: {trace}");
    assert!(
        !trace.contains("stop"),
        "the agent stopped its own service: {trace}"
    );
}

#[tokio::test]
async fn deregistered_frame_removes_the_machine() {
    let pki = Pki::new("master");
    let server = FakeServer::start(pki.clone());
    let machine = MachineBox::new();
    machine.write_identity(&pki, uuid::Uuid::new_v4());

    let mut agent = machine.spawn(server.addr);
    let mut conn = server.accept().await;
    conn.expect_register().await;
    conn.send(ControlMessage::AgentDeregistered {
        reason: "device deleted by administrator".into(),
    })
    .await;
    conn.closed().await;

    assert_eq!(agent.exit_code().await, Some(0), "log:\n{}", agent.log());
    assert!(agent.log().contains("device deregistered by server"));
    assert_removed(&machine);
}

#[tokio::test]
async fn close_code_3_without_a_frame_removes_the_machine() {
    let pki = Pki::new("master");
    let server = FakeServer::start(pki.clone());
    let machine = MachineBox::new();
    machine.write_identity(&pki, uuid::Uuid::new_v4());

    let mut agent = machine.spawn(server.addr);
    let mut conn = server.accept().await;
    conn.expect_register().await;
    conn.close(close_code::DEVICE_DELETED);

    assert_eq!(agent.exit_code().await, Some(0), "log:\n{}", agent.log());
    assert!(agent.log().contains("device deregistered by server"));
    assert_removed(&machine);
}

#[tokio::test]
async fn close_code_2_sends_the_next_connection_through_a_full_handshake() {
    let pki = Pki::new("master");
    let server = FakeServer::start(pki.clone());
    let machine = MachineBox::new();
    machine.write_identity(&pki, uuid::Uuid::new_v4());

    let mut agent = machine.spawn(server.addr);
    let mut first = server.accept().await;
    assert!(
        !first.fast_path,
        "a fresh process has no cached fingerprint"
    );
    first.expect_register().await;
    first.acknowledge(&pki.ca_pem).await;
    first.close(close_code::BYE);

    let mut second = server.accept().await;
    assert!(
        second.fast_path,
        "a reconnect with a cached fingerprint takes the fast path"
    );
    second.expect_register().await;
    second.close(close_code::HANDSHAKE_REFUSED);

    let mut third = server.accept().await;
    assert!(!third.fast_path, "a refused fast path is retried in full");
    third.expect_register().await;
    agent.stop().await;
}

#[tokio::test]
async fn an_update_is_verified_only_by_the_acknowledgement() {
    let pki = Pki::new("master");
    let server = FakeServer::start(pki.clone());
    let machine = MachineBox::new();
    machine.write_identity(&pki, uuid::Uuid::new_v4());
    let sentinel = machine.data.join(".update-pending");
    std::fs::write(&sentinel, "").unwrap();

    let mut agent = machine.spawn(server.addr);
    for refusal in [
        close_code::HANDSHAKE_REFUSED,
        close_code::CERTIFICATE_REFUSED,
    ] {
        let mut conn = server.accept().await;
        conn.expect_register().await;
        conn.close(refusal);
        conn.closed().await;
        assert!(
            sentinel.exists(),
            "a connection refused with {refusal} verified the update"
        );
    }

    let mut conn = server.accept().await;
    conn.expect_register().await;
    assert!(
        sentinel.exists(),
        "the agent's own registration verified the update"
    );
    conn.acknowledge(&pki.ca_pem).await;
    agent.wait_for_log("post-update verification passed").await;
    assert!(!sentinel.exists());
    agent.stop().await;
}

#[tokio::test]
async fn a_removal_order_never_counts_as_a_verified_update() {
    let pki = Pki::new("master");
    let server = FakeServer::start(pki.clone());
    let machine = MachineBox::new();
    machine.write_identity(&pki, uuid::Uuid::new_v4());
    std::fs::write(machine.data.join(".update-pending"), "").unwrap();

    let mut agent = machine.spawn(server.addr);
    let mut conn = server.accept().await;
    conn.expect_register().await;
    conn.close(close_code::DEVICE_DELETED);

    assert_eq!(agent.exit_code().await, Some(0));
    assert!(!agent.log().contains("post-update verification passed"));
}
