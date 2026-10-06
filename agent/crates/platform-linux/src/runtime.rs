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

/// Checks container indicators (`/.dockerenv`, `/run/.containerenv`), then `NOTIFY_SOCKET`,
/// else yields `BareMetalOther`.
pub fn detect_runtime() -> LinuxRuntime {
    decide_runtime(
        Path::new("/.dockerenv").exists() || Path::new("/run/.containerenv").exists(),
        std::env::var_os("NOTIFY_SOCKET").is_some(),
    )
}

/// Pure decision behind [`detect_runtime`]; takes the probe results as booleans so it reads
/// no process-global state.
pub fn decide_runtime(in_container: bool, has_notify_socket: bool) -> LinuxRuntime {
    if in_container {
        LinuxRuntime::Container
    } else if has_notify_socket {
        LinuxRuntime::BareMetalSystemd
    } else {
        LinuxRuntime::BareMetalOther
    }
}

/// Returns `/host` when a host mount exists there (containers), else `/`.
pub fn get_filesystem_root() -> PathBuf {
    let host_mount = Path::new("/host");
    if host_mount.is_dir() {
        host_mount.to_path_buf()
    } else {
        PathBuf::from("/")
    }
}
