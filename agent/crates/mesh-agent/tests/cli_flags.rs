//! The CLI rejects every `--edge-*` opt-in flag, so a stale unit that passes one fails at startup.

use std::process::Command;

/// Path to the compiled `mesh-agent` binary, provided by Cargo to this crate's
/// integration tests.
const MESH_AGENT_BIN: &str = env!("CARGO_BIN_EXE_mesh-agent");

fn run_with(extra: &[&str]) -> (bool, String) {
    let mut args = vec![
        "--server-addr",
        "127.0.0.1:9090",
        "--server-ca",
        "/tmp/mesh-agent-cli-flags-test-ca.pem",
    ];
    args.extend_from_slice(extra);
    let output = Command::new(MESH_AGENT_BIN)
        .args(&args)
        .output()
        .expect("run mesh-agent binary");
    (
        output.status.success(),
        String::from_utf8_lossy(&output.stderr).into_owned(),
    )
}

#[test]
fn retired_edge_flags_are_rejected() {
    for flag in [
        "--edge-sentinel",
        "--edge-store",
        "--edge-store-cap-mb=256",
        "--edge-log-readers",
        "--edge-discovery",
    ] {
        let (ok, stderr) = run_with(&[flag]);
        assert!(
            !ok,
            "expected `{flag}` to be rejected, but the agent accepted it"
        );
        assert!(
            stderr.contains("unexpected argument"),
            "expected an unknown-argument error for `{flag}`, got stderr: {stderr}"
        );
    }
}

#[test]
fn help_does_not_list_retired_flags() {
    let output = Command::new(MESH_AGENT_BIN)
        .arg("--help")
        .output()
        .expect("run mesh-agent --help");
    assert!(output.status.success(), "`--help` should exit 0");
    let stdout = String::from_utf8_lossy(&output.stdout);
    for token in [
        "--edge-sentinel",
        "--edge-store",
        "--edge-store-cap-mb",
        "--edge-log-readers",
        "--edge-discovery",
    ] {
        assert!(
            !stdout.contains(token),
            "retired flag `{token}` still appears in --help output:\n{stdout}"
        );
    }
}
