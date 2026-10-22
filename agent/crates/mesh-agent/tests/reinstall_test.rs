//! Each row of the start-up decision: what the agent does with an install note, a `removed`
//! marker, an identity, and what the server says about it.

mod harness;

use harness::*;
use mesh_protocol::{close_code, ControlMessage};

const TOKEN: &str = "join-token";

struct Scene {
    pki: std::sync::Arc<Pki>,
    server: FakeServer,
    enroll: EnrollStub,
    machine: MachineBox,
}

impl Scene {
    async fn new() -> Self {
        let pki = Pki::new("master");
        Self {
            server: FakeServer::start(pki.clone()),
            enroll: EnrollStub::start(pki.clone(), TOKEN).await,
            machine: MachineBox::new(),
            pki,
        }
    }

    fn note(&self, token: &str, fresh: bool) {
        self.machine
            .write_note(token, &self.enroll.url(), &self.pki.ca_pem, fresh);
    }

    fn known_identity(&self) -> uuid::Uuid {
        let id = uuid::Uuid::new_v4();
        self.machine.write_identity(&self.pki, id);
        id
    }

    /// The agent enrolls once, connects as the device it was given, and is acknowledged.
    async fn expect_enrolled_and_acknowledged(&self, agent: &AgentProcess) -> uuid::Uuid {
        let mut conn = self.server.accept().await;
        let issued = self.enroll.issued();
        assert_eq!(
            issued.len(),
            1,
            "enrollments: {issued:?}; log:\n{}",
            agent.log()
        );
        assert_eq!(self.machine.device_id(), Some(issued[0]));
        assert!(
            !self.machine.note_path().exists(),
            "the note outlived the enrollment"
        );
        conn.expect_register().await;
        conn.acknowledge(&self.pki.ca_pem).await;
        issued[0]
    }
}

#[tokio::test]
async fn no_identity_enrolls_with_the_notes_token() {
    let scene = Scene::new().await;
    scene.note(TOKEN, false);

    let mut agent = scene.machine.spawn(scene.server.addr);
    scene.expect_enrolled_and_acknowledged(&agent).await;
    assert_eq!(
        std::fs::read_to_string(&scene.machine.ca_path).unwrap(),
        scene.pki.ca_pem,
        "the note's master key is trusted"
    );
    agent.stop().await;
}

#[tokio::test]
async fn an_acknowledged_identity_is_kept_and_its_note_deleted_on_the_acknowledgement() {
    let scene = Scene::new().await;
    let id = scene.known_identity();
    scene.note(TOKEN, false);

    let mut agent = scene.machine.spawn(scene.server.addr);
    let mut conn = scene.server.accept().await;
    conn.expect_register().await;
    assert!(
        scene.machine.note_path().exists(),
        "the note went on the agent's own send"
    );
    conn.acknowledge(&scene.pki.ca_pem).await;
    agent.wait_for_log("install note no longer needed").await;

    assert!(!scene.machine.note_path().exists());
    assert_eq!(scene.machine.device_id(), Some(id));
    assert!(
        scene.enroll.issued().is_empty(),
        "a repair spent the join code"
    );
    agent.stop().await;
}

#[tokio::test]
async fn on_the_fast_path_the_note_survives_registration_until_the_acknowledgement() {
    let scene = Scene::new().await;
    scene.known_identity();
    scene.note(TOKEN, false);

    let mut agent = scene.machine.spawn(scene.server.addr);
    let mut first = scene.server.accept().await;
    first.expect_register().await;
    first.close(close_code::BYE);
    first.closed().await;

    let mut second = scene.server.accept().await;
    assert!(second.fast_path);
    second.expect_register().await;
    assert!(
        scene.machine.note_path().exists(),
        "registration alone consumed the note"
    );
    second.acknowledge(&scene.pki.ca_pem).await;
    agent.wait_for_log("install note no longer needed").await;
    assert!(!scene.machine.note_path().exists());
    agent.stop().await;
}

#[tokio::test]
async fn a_deleted_identity_with_a_note_enrolls_again_without_removing_itself() {
    let scene = Scene::new().await;
    let old = scene.known_identity();
    scene.note(TOKEN, false);
    std::fs::write(scene.machine.data.join("retro-scans.json"), "{}").unwrap();

    let mut agent = scene.machine.spawn(scene.server.addr);
    let mut conn = scene.server.accept().await;
    conn.expect_register().await;
    conn.send(ControlMessage::AgentDeregistered {
        reason: "deleted".into(),
    })
    .await;

    let new = scene.expect_enrolled_and_acknowledged(&agent).await;
    assert_ne!(new, old);
    assert!(
        !scene.machine.data.join("retro-scans.json").exists(),
        "the old device's data stayed"
    );
    assert!(scene.machine.exe.exists(), "the agent removed itself");
    assert!(!scene.machine.trace().contains("disable"));
    agent.stop().await;
}

#[tokio::test]
async fn an_identity_from_another_master_key_enrolls_fresh() {
    let scene = Scene::new().await;
    let other = Pki::new("another server");
    let old = uuid::Uuid::new_v4();
    scene.machine.write_identity(&other, old);
    scene.note(TOKEN, false);

    let mut agent = scene.machine.spawn(scene.server.addr);
    let new = scene.expect_enrolled_and_acknowledged(&agent).await;
    assert_ne!(new, old);
    agent.stop().await;
}

#[tokio::test]
async fn a_fresh_note_enrolls_fresh_and_a_restart_enrolls_no_second_time() {
    let scene = Scene::new().await;
    let old = scene.known_identity();
    scene.note(TOKEN, true);

    let mut agent = scene.machine.spawn(scene.server.addr);
    let new = scene.expect_enrolled_and_acknowledged(&agent).await;
    assert_ne!(new, old);
    agent.kill().await;

    let mut again = scene.machine.spawn(scene.server.addr);
    let mut conn = scene.server.accept().await;
    conn.expect_register().await;
    assert_eq!(scene.enroll.issued().len(), 1, "the restart enrolled again");
    assert_eq!(scene.machine.device_id(), Some(new));
    again.stop().await;
}

#[tokio::test]
async fn a_refused_certificate_with_a_note_enrolls_fresh() {
    let scene = Scene::new().await;
    let old = scene.known_identity();
    scene.note(TOKEN, false);

    let mut agent = scene.machine.spawn(scene.server.addr);
    let mut conn = scene.server.accept().await;
    conn.expect_register().await;
    conn.close(close_code::CERTIFICATE_REFUSED);

    let new = scene.expect_enrolled_and_acknowledged(&agent).await;
    assert_ne!(new, old);
    agent.stop().await;
}

#[tokio::test]
async fn a_refused_certificate_without_a_note_keeps_retrying_with_its_identity() {
    let scene = Scene::new().await;
    let id = scene.known_identity();

    let mut agent = scene.machine.spawn(scene.server.addr);
    for _ in 0..2 {
        let mut conn = scene.server.accept().await;
        conn.expect_register().await;
        conn.close(close_code::CERTIFICATE_REFUSED);
    }
    scene.server.accept().await;
    assert!(agent.log().contains("certificate refused by the server"));
    assert_eq!(scene.machine.device_id(), Some(id));
    assert!(scene.enroll.issued().is_empty());
    agent.stop().await;
}

#[tokio::test]
async fn an_untrusted_server_without_a_note_keeps_retrying() {
    let scene = Scene::new().await;
    let id = scene.known_identity();
    let other = Pki::new("a master key this machine never trusted");
    std::fs::write(&scene.machine.ca_path, &other.ca_pem).unwrap();

    let mut agent = scene.machine.spawn(scene.server.addr);
    for _ in 0..2 {
        assert!(scene.server.accept_attempt().await.is_err());
    }
    agent
        .wait_for_log("the server's master key is unknown")
        .await;
    assert_eq!(scene.machine.device_id(), Some(id));
    agent.stop().await;
}

#[tokio::test]
async fn a_refused_token_keeps_the_note_and_exits_non_zero() {
    let scene = Scene::new().await;
    scene.note("refused-token", false);

    let mut agent = scene.machine.spawn(scene.server.addr);
    let code = agent.exit_code().await;
    assert!(matches!(code, Some(c) if c != 0), "exit {code:?}");
    assert!(
        agent.log().contains("enrollment token expired"),
        "log:\n{}",
        agent.log()
    );
    assert!(scene.machine.note_path().exists());
    assert!(!scene.machine.has_identity());
}

#[tokio::test]
async fn a_reply_whose_number_differs_from_its_certificate_is_refused_and_nothing_saved() {
    let scene = Scene::new().await;
    scene.note("mismatch-token", false);

    let mut agent = scene.machine.spawn(scene.server.addr);
    let code = agent.exit_code().await;
    assert!(matches!(code, Some(c) if c != 0), "exit {code:?}");
    assert!(!scene.machine.has_identity());
    assert!(!scene.machine.ca_path.exists());
    assert!(scene.machine.note_path().exists());
}

#[tokio::test]
async fn a_removed_marker_without_a_note_finishes_the_removal_offline() {
    let scene = Scene::new().await;
    scene.known_identity();
    std::fs::write(scene.machine.data.join("removed"), "").unwrap();

    let mut agent = scene.machine.spawn("127.0.0.1:9".parse().unwrap());
    assert_eq!(agent.exit_code().await, Some(0), "log:\n{}", agent.log());
    assert!(!scene.machine.data.exists());
    assert!(!scene.machine.exe.exists());
}

#[derive(Debug, Clone, Copy)]
enum Order {
    Frame,
    CloseCode,
}

#[tokio::test]
async fn every_combination_of_note_marker_and_order_ends_in_its_row() {
    for marker in [false, true] {
        for order in [Order::Frame, Order::CloseCode] {
            let scene = Scene::new().await;
            let old = scene.known_identity();
            scene.note(TOKEN, false);
            if marker {
                std::fs::write(scene.machine.data.join("removed"), "").unwrap();
            }

            let mut agent = scene.machine.spawn(scene.server.addr);
            let mut conn = scene.server.accept().await;
            assert!(
                !scene.machine.data.join("removed").exists(),
                "marker {marker}"
            );
            conn.expect_register().await;
            assert!(
                scene.machine.note_path().exists(),
                "{order:?}: the note went early"
            );
            match order {
                Order::Frame => {
                    conn.send(ControlMessage::AgentDeregistered {
                        reason: String::new(),
                    })
                    .await
                }
                Order::CloseCode => conn.close(close_code::DEVICE_DELETED),
            }
            let new = scene.expect_enrolled_and_acknowledged(&agent).await;
            assert_ne!(new, old, "marker {marker}, {order:?}");
            assert!(
                scene.machine.exe.exists(),
                "marker {marker}, {order:?}: removed itself"
            );
            agent.stop().await;
        }
    }
}

#[tokio::test]
async fn a_removed_marker_with_a_note_and_no_identity_enrolls() {
    let scene = Scene::new().await;
    std::fs::write(scene.machine.data.join("removed"), "").unwrap();
    scene.note(TOKEN, false);

    let mut agent = scene.machine.spawn(scene.server.addr);
    scene.expect_enrolled_and_acknowledged(&agent).await;
    assert!(!scene.machine.data.join("removed").exists());
    agent.stop().await;
}
