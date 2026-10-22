//! Self-removal settles from an interruption after any of its steps, whether the process was killed
//! and restarted by its loaded unit or the power was cut and only an enabled unit starts again.

mod harness;

use harness::*;

/// What a crash after each step of self-removal leaves on disk.
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord)]
enum After {
    Marker,
    Wipe,
    Unit,
    Binary,
    Folders,
}

#[derive(Debug, Clone, Copy)]
enum Interruption {
    Killed,
    PowerCut,
}

fn installed(pki: &Pki) -> MachineBox {
    let machine = MachineBox::new();
    machine.write_identity(pki, uuid::Uuid::new_v4());
    std::fs::create_dir_all(machine.data.join("edge-tsdb")).unwrap();
    std::fs::write(machine.data.join("edge-tsdb/readings"), vec![7u8; 4096]).unwrap();
    std::fs::write(machine.data.join("retro-scans.json"), "{}").unwrap();
    std::fs::write(machine.data.join("update-signing-key.hex"), "a".repeat(64)).unwrap();
    machine
}

fn crash_after(machine: &MachineBox, step: After) {
    std::fs::write(machine.data.join("removed"), "").unwrap();
    if step >= After::Wipe {
        for entry in std::fs::read_dir(&machine.data).unwrap() {
            let path = entry.unwrap().path();
            if path.file_name().unwrap() == "removed" {
                continue;
            }
            if path.is_dir() {
                std::fs::remove_dir_all(&path).unwrap();
            } else {
                std::fs::remove_file(&path).unwrap();
            }
        }
        std::fs::remove_file(&machine.ca_path).unwrap();
    }
    if step >= After::Unit {
        std::fs::write(&machine.unit_state, "disabled\n").unwrap();
        std::fs::remove_file(&machine.unit).unwrap();
    }
    if step >= After::Binary {
        std::fs::remove_file(&machine.exe).unwrap();
    }
    if step >= After::Folders {
        std::fs::remove_file(machine.data.join("removed")).unwrap();
        std::fs::remove_dir(&machine.data).unwrap();
        std::fs::remove_dir(&machine.trust).unwrap();
    }
}

fn assert_settled(machine: &MachineBox, context: &str) {
    assert!(!machine.has_identity(), "{context}: identity files remain");
    assert!(
        !machine.data.join("edge-tsdb").exists(),
        "{context}: the local store remains"
    );
    assert!(
        !machine.ca_path.exists(),
        "{context}: the trusted master key remains"
    );
    if machine.data.exists() {
        let left: Vec<_> = std::fs::read_dir(&machine.data)
            .unwrap()
            .map(|e| e.unwrap().file_name().to_string_lossy().into_owned())
            .collect();
        assert!(
            left.iter().all(|name| name == "removed"),
            "{context}: the data folder holds {left:?}"
        );
    }
}

#[tokio::test]
async fn self_removal_settles_after_an_interruption_at_every_step() {
    let pki = Pki::new("master");
    let unreachable: std::net::SocketAddr = "127.0.0.1:9".parse().unwrap();
    for step in [
        After::Marker,
        After::Wipe,
        After::Unit,
        After::Binary,
        After::Folders,
    ] {
        for interruption in [Interruption::Killed, Interruption::PowerCut] {
            let context = format!("{interruption:?} after {step:?}");
            let machine = installed(&pki);
            crash_after(&machine, step);

            let restarts = match interruption {
                Interruption::Killed => machine.exe.exists(),
                Interruption::PowerCut => machine.exe.exists() && machine.unit_enabled(),
            };
            if restarts {
                let mut agent = machine.spawn(unreachable);
                assert_eq!(
                    agent.exit_code().await,
                    Some(0),
                    "{context}:\n{}",
                    agent.log()
                );
                assert!(!machine.data.exists(), "{context}: the data folder remains");
                assert!(!machine.exe.exists(), "{context}: the binary remains");
                assert!(!machine.unit.exists(), "{context}: the unit file remains");
            }
            assert_settled(&machine, &context);
        }
    }
}

#[tokio::test]
async fn a_kill_while_the_unit_is_being_disabled_resumes_on_restart() {
    let pki = Pki::new("master");
    let server = FakeServer::start(pki.clone());
    let machine = installed(&pki);
    std::fs::write(machine.bin.join("block-disable"), "").unwrap();

    let mut agent = machine.spawn(server.addr);
    let mut conn = server.accept().await;
    conn.expect_register().await;
    conn.close(mesh_protocol::close_code::DEVICE_DELETED);
    while !machine.trace().contains("disable") {
        tokio::time::sleep(std::time::Duration::from_millis(20)).await;
    }
    agent.kill().await;
    assert!(machine.unit_enabled(), "the kill landed after the disable");
    std::fs::remove_file(machine.bin.join("block-disable")).unwrap();

    let mut restarted = machine.spawn(server.addr);
    assert_eq!(restarted.exit_code().await, Some(0), "{}", restarted.log());
    assert!(!machine.data.exists());
    assert!(!machine.exe.exists());
    assert!(!machine.unit_enabled());
    assert_settled(&machine, "killed during disable");
}
