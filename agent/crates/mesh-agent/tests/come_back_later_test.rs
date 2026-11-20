//! A full sign-in line's "come back later": the agent hangs up at once and dials again once the
//! server's wait has passed.

mod harness;

use std::time::{Duration, Instant};

use harness::*;
use mesh_protocol::ControlMessage;

#[tokio::test]
async fn a_machine_told_to_come_back_later_dials_again_after_the_wait() {
    let pki = Pki::new("master");
    let server = FakeServer::start(pki.clone());
    let machine = MachineBox::new();
    machine.write_identity(&pki, uuid::Uuid::new_v4());
    let mut agent = machine.spawn(server.addr);

    let mut first = server.accept().await;
    first.expect_register().await;
    let told = Instant::now();
    first
        .send(ControlMessage::ComeBackLater { retry_after: 3 })
        .await;
    first.closed().await;
    assert!(
        told.elapsed() < Duration::from_secs(3),
        "the agent hangs up at once, before its wait"
    );

    let mut second = server.accept().await;
    let waited = told.elapsed();
    second.expect_register().await;
    assert!(
        waited >= Duration::from_secs(3),
        "dialled again after {waited:?}, inside the 3 s wait; log:\n{}",
        agent.log()
    );
    assert!(
        waited < Duration::from_secs(9),
        "dialled again after {waited:?}, well past the wait and its jitter"
    );
    assert!(agent
        .log()
        .contains("the server asked this machine to come back later"));
    agent.stop().await;
}
