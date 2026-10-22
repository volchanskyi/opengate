//! Renewal over the live connection as the agent performs it, an expired certificate presented as
//! it is, and a connection another took over.

mod harness;

use harness::*;
use mesh_protocol::{close_code, ControlMessage};

/// Answers the agent's request with a certificate for the same device and returns it.
async fn issue_renewal(conn: &mut ServerSide, pki: &Pki, id: uuid::Uuid) -> Vec<u8> {
    conn.send(ControlMessage::RenewCertificate {
        reason: "due".into(),
    })
    .await;
    let csr = loop {
        if let ControlMessage::CertificateSigningRequest { csr } = conn.read().await {
            break csr;
        }
    };
    let pem = pem_of_csr(&csr);
    let issued = pem::parse(pki.sign_csr(&pem, id)).unwrap().into_contents();
    conn.send(ControlMessage::CertificateIssued {
        cert: issued.clone(),
        ca_bundle: pki.ca_pem.clone(),
    })
    .await;
    issued
}

fn pem_of_csr(der: &[u8]) -> String {
    pem::encode(&pem::Pem::new("CERTIFICATE REQUEST", der.to_vec()))
}

#[tokio::test]
async fn renew_then_reconnect_uses_new_cert() {
    let pki = Pki::new("master");
    let server = FakeServer::start(pki.clone());
    let machine = MachineBox::new();
    let id = uuid::Uuid::new_v4();
    machine.write_identity(&pki, id);

    let mut agent = machine.spawn(server.addr);
    let mut conn = server.accept().await;
    conn.expect_register().await;
    let issued = issue_renewal(&mut conn, &pki, id).await;
    loop {
        if let ControlMessage::CertificateInstalled = conn.read().await {
            break;
        }
    }
    assert_eq!(
        std::fs::read(machine.data.join("agent.crt")).unwrap(),
        issued
    );
    conn.close(close_code::RESTRICTED_SESSION_ENDED);

    let again = server.accept().await;
    assert_eq!(
        again.presented(),
        issued,
        "the next connection presents the renewal"
    );
    assert!(
        !again.fast_path,
        "a renewal starts the next connection with a full handshake"
    );
    assert_eq!(
        machine.device_id(),
        Some(id),
        "a renewal keeps the machine's number"
    );
    agent.stop().await;
}

#[tokio::test]
async fn expired_client_cert_is_presented() {
    let pki = Pki::new("master");
    let server = FakeServer::start_admitting_any(pki.clone());
    let machine = MachineBox::new();
    let id = uuid::Uuid::new_v4();
    let (cert, key) = pki.expired_identity(id);
    std::fs::write(machine.data.join("device_id.txt"), id.to_string()).unwrap();
    std::fs::write(machine.data.join("agent.crt"), &cert).unwrap();
    std::fs::write(machine.data.join("agent.key"), key).unwrap();
    std::fs::write(&machine.ca_path, &pki.ca_pem).unwrap();

    let mut agent = machine.spawn(server.addr);
    let mut conn = server.accept().await;
    assert_eq!(
        conn.presented(),
        cert,
        "the expired certificate is presented so it can be renewed"
    );
    conn.expect_register().await;
    agent.stop().await;
}

#[tokio::test]
async fn close_code_6_reconnects_with_backoff() {
    let pki = Pki::new("master");
    let server = FakeServer::start(pki.clone());
    let machine = MachineBox::new();
    machine.write_identity(&pki, uuid::Uuid::new_v4());

    let mut agent = machine.spawn(server.addr);
    let mut first = server.accept().await;
    first.expect_register().await;
    first.close(close_code::REPLACED_BY_NEWER_CONNECTION);

    let mut second = server.accept().await;
    second.expect_register().await;
    agent
        .wait_for_log("another connection took this machine's identity")
        .await;
    assert!(
        agent.log().contains("backing off before reconnect"),
        "a replaced connection reconnects through the flap backoff"
    );
    assert!(
        machine.has_identity(),
        "a replaced connection keeps its identity"
    );
    agent.stop().await;
}

#[tokio::test]
async fn a_ping_on_the_control_stream_is_answered_and_the_loop_continues() {
    let pki = Pki::new("master");
    let server = FakeServer::start(pki.clone());
    let machine = MachineBox::new();
    machine.write_identity(&pki, uuid::Uuid::new_v4());

    let mut agent = machine.spawn(server.addr);
    let mut conn = server.accept().await;
    conn.expect_register().await;
    conn.ping().await;
    conn.acknowledge(&pki.ca_pem).await;
    conn.send(ControlMessage::SetMaintenanceMode { enabled: true })
        .await;
    loop {
        if let ControlMessage::MaintenanceApplied { enabled } = conn.read().await {
            assert!(enabled);
            break;
        }
    }
    assert_eq!(conn.pongs, 1);
    agent.stop().await;
}
