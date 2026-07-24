//! Regression guard for the always-on Edge-Sentinel collectors.
//!
//! Every agent runs the sampler, the local store, and auto-discovery
//! unconditionally — there is no opt-in. The CLI must therefore
//! *reject* the retired `--edge-*` flags (and their `OPENGATE_EDGE_*` env vars,
//! which are removed with the fields) so a stale systemd unit or deployment that
//! still passes one fails loudly at startup instead of silently ignoring it.

use std::process::Command;

/// Path to the compiled `mesh-agent` binary, provided by Cargo to this crate's
/// integration tests.
const MESH_AGENT_BIN: &str = env!("CARGO_BIN_EXE_mesh-agent");

/// Run the agent with the base required args plus `extra`, returning the exit
/// success flag and captured stderr. Argument parsing fails before any network
/// or filesystem work, so this never connects anywhere.
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
    // Each retired opt-in flag must now be an unknown argument. `--edge-store-cap-mb`
    // took a value, so it is passed in the `flag=value` form.
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
    // `--help` short-circuits parsing and prints usage to stdout with exit 0.
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
