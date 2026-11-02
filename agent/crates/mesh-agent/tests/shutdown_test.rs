//! The service manager's stop request ends the agent promptly, with every background task stopped.

mod harness;

use std::time::Duration;

use harness::*;

/// The unit's `TimeoutStopSec` is 15 s; the agent stops well inside it.
const STOP_LIMIT: Duration = Duration::from_secs(5);

#[tokio::test]
async fn a_stop_request_ends_the_agent_and_its_background_tasks() {
    let pki = Pki::new("master");
    let server = FakeServer::start(pki.clone());
    let machine = MachineBox::new();
    machine.write_identity(&pki, uuid::Uuid::new_v4());

    let agent = machine.spawn(server.addr);
    let mut conn = server.accept().await;
    conn.expect_register().await;
    conn.acknowledge(&pki.ca_pem).await;
    agent.wait_for_log("edge-sentinel sampler starting").await;
    agent.wait_for_log("retroactive scan job starting").await;

    agent.terminate();
    let code = agent.exited_within(STOP_LIMIT).await;
    assert_eq!(
        code,
        Some(Some(0)),
        "the agent did not exit cleanly within {STOP_LIMIT:?}; log:\n{}",
        agent.log()
    );
    assert!(
        agent.log().contains("background tasks stopped"),
        "the agent exited without stopping its background tasks; log:\n{}",
        agent.log()
    );
}

#[tokio::test]
async fn a_stop_request_while_the_server_is_unreachable_ends_the_agent() {
    let pki = Pki::new("master");
    let server = FakeServer::start(pki.clone());
    let unreachable = server.addr;
    drop(server);
    let machine = MachineBox::new();
    machine.write_identity(&pki, uuid::Uuid::new_v4());

    let agent = machine.spawn(unreachable);
    // Readiness is the earliest moment the agent promises a clean stop.
    agent
        .wait_for_log("agent ready, connecting to server")
        .await;

    agent.terminate();
    let code = agent.exited_within(STOP_LIMIT).await;
    assert_eq!(
        code,
        Some(Some(0)),
        "the agent did not exit cleanly within {STOP_LIMIT:?}; log:\n{}",
        agent.log()
    );
    assert!(agent.log().contains("background tasks stopped"));
}

/// Hangs as `docker ps` does against a stuck Docker service, naming its process first.
const HUNG_DOCKER: &str = r#"#!/usr/bin/env bash
echo $$ >"$(dirname "$0")/docker.pid"
exec sleep 600
"#;

#[tokio::test]
async fn a_stop_request_during_a_hung_inventory_command_ends_the_agent_and_the_command() {
    let pki = Pki::new("master");
    let server = FakeServer::start(pki.clone());
    let machine = MachineBox::new();
    machine.write_identity(&pki, uuid::Uuid::new_v4());
    machine.install_tool("docker", HUNG_DOCKER);

    let agent = machine.spawn(server.addr);
    let mut conn = server.accept().await;
    conn.expect_register().await;
    conn.acknowledge(&pki.ca_pem).await;
    let pid_file = machine.bin.join("docker.pid");
    let deadline = tokio::time::Instant::now() + STOP_LIMIT;
    let pid = loop {
        if let Some(pid) = std::fs::read_to_string(&pid_file)
            .ok()
            .and_then(|s| s.trim().parse::<u32>().ok())
        {
            break pid;
        }
        assert!(
            tokio::time::Instant::now() < deadline,
            "the inventory sweep never ran docker"
        );
        tokio::time::sleep(Duration::from_millis(20)).await;
    };

    agent.terminate();
    let code = agent.exited_within(STOP_LIMIT).await;
    assert_eq!(
        code,
        Some(Some(0)),
        "the agent did not exit cleanly within {STOP_LIMIT:?}; log:\n{}",
        agent.log()
    );
    assert!(
        agent.log().contains("background tasks stopped"),
        "the hung command held a background task past the stop; log:\n{}",
        agent.log()
    );
    assert!(
        !std::path::Path::new(&format!("/proc/{pid}")).exists(),
        "the hung docker outlived the agent"
    );
}
