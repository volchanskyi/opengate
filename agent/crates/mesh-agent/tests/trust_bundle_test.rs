//! A trust bundle the server sends: saved when it changes and verifies the server the agent is
//! talking to, and refused otherwise.

mod harness;

use harness::*;

#[tokio::test]
async fn a_changed_bundle_that_verifies_this_server_is_saved_and_any_other_is_refused() {
    let pki = Pki::new("master");
    let next = Pki::new("next");
    let server = FakeServer::start(pki.clone());
    let machine = MachineBox::new();
    machine.write_identity(&pki, uuid::Uuid::new_v4());
    let mut agent = machine.spawn(server.addr);
    let mut conn = server.accept().await;
    conn.expect_register().await;

    conn.acknowledge(&pki.ca_pem).await;
    conn.acknowledge("").await;
    conn.acknowledge(&next.ca_pem).await;
    agent
        .wait_for_log("ignoring a trust bundle that does not verify this server")
        .await;
    assert_eq!(
        std::fs::read_to_string(&machine.ca_path).unwrap(),
        pki.ca_pem,
        "a bundle that does not verify this server leaves the trusted one"
    );

    let rotated = format!("{}{}", pki.ca_pem, next.ca_pem);
    conn.acknowledge(&rotated).await;
    agent.wait_for_log("trust bundle updated").await;
    assert_eq!(std::fs::read_to_string(&machine.ca_path).unwrap(), rotated);
    let log = agent.log();
    assert_eq!(
        log.matches("ignoring a trust bundle").count(),
        1,
        "the held bundle and an empty one are neither refused nor saved:\n{log}"
    );
    assert_eq!(log.matches("trust bundle updated").count(), 1, "{log}");
    agent.stop().await;
}
