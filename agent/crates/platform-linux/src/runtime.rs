//! Linux runtime environment detection.

use std::path::{Path, PathBuf};

/// Detected Linux runtime environment.
#[derive(Debug, Clone, PartialEq, Eq)]
#[non_exhaustive]
pub enum LinuxRuntime {
    /// Running inside a Docker/Podman container.
    Container,
    /// Running on bare metal or VM with systemd.
    BareMetalSystemd,
    /// Running on bare metal or VM without systemd.
    BareMetalOther,
}

/// Detect the current Linux runtime environment.
///
/// Checks for container indicators first (`/.dockerenv`, `/run/.containerenv`),
/// then for systemd (`NOTIFY_SOCKET` env var), falling back to `BareMetalOther`.
pub fn detect_runtime() -> LinuxRuntime {
    decide_runtime(
        Path::new("/.dockerenv").exists() || Path::new("/run/.containerenv").exists(),
        std::env::var_os("NOTIFY_SOCKET").is_some(),
    )
}

/// Pure decision behind [`detect_runtime`], separated from the environment/path
/// probes so it can be unit-tested deterministically (see `tests/runtime_test.rs`).
/// Mutating `NOTIFY_SOCKET` from tests to drive the old inline cases raced every
/// other thread reading the environment — `std::env::set_var` is unsound under
/// concurrency — making those tests flaky; passing the booleans explicitly removes
/// the global state entirely.
pub fn decide_runtime(in_container: bool, has_notify_socket: bool) -> LinuxRuntime {
    if in_container {
        LinuxRuntime::Container
    } else if has_notify_socket {
        LinuxRuntime::BareMetalSystemd
    } else {
        LinuxRuntime::BareMetalOther
    }
}

/// Get the filesystem root for file operations.
///
/// In containers with a host mount at `/host`, returns `/host`.
/// Otherwise returns `/`.
pub fn get_filesystem_root() -> PathBuf {
    let host_mount = Path::new("/host");
    if host_mount.is_dir() {
        host_mount.to_path_buf()
    } else {
        PathBuf::from("/")
    }
}

// Tests live in `tests/runtime_test.rs` — `decide_runtime` is exercised purely,
// without mutating the process-global environment (which previously raced and
// flaked).
