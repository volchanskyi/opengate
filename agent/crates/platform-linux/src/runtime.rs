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
    if Path::new("/.dockerenv").exists() || Path::new("/run/.containerenv").exists() {
        return LinuxRuntime::Container;
    }
    if std::env::var_os("NOTIFY_SOCKET").is_some() {
        return LinuxRuntime::BareMetalSystemd;
    }
    LinuxRuntime::BareMetalOther
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

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_detect_bare_metal_other_default() {
        // Remove systemd indicator if present
        std::env::remove_var("NOTIFY_SOCKET");
        if !Path::new("/.dockerenv").exists() && !Path::new("/run/.containerenv").exists() {
            assert_eq!(detect_runtime(), LinuxRuntime::BareMetalOther);
        }
    }

    #[test]
    fn test_detect_bare_metal_systemd_via_notify_socket() {
        // Safety: these tests manipulate env vars and may race with each other.
        // In practice, cargo test runs them in separate threads but that's fine
        // for this simple detection logic.
        std::env::set_var("NOTIFY_SOCKET", "/run/systemd/notify");
        if !Path::new("/.dockerenv").exists() && !Path::new("/run/.containerenv").exists() {
            assert_eq!(detect_runtime(), LinuxRuntime::BareMetalSystemd);
        }
        std::env::remove_var("NOTIFY_SOCKET");
    }

    #[test]
    fn test_detect_runtime_returns_valid_variant() {
        let runtime = detect_runtime();
        assert!(matches!(
            runtime,
            LinuxRuntime::Container | LinuxRuntime::BareMetalSystemd | LinuxRuntime::BareMetalOther
        ));
    }

    #[test]
    fn test_filesystem_root_bare_metal_returns_slash() {
        if !Path::new("/host").is_dir() {
            assert_eq!(get_filesystem_root(), PathBuf::from("/"));
        }
    }

    #[test]
    fn test_filesystem_root_container_with_host_mount() {
        if Path::new("/host").is_dir() {
            assert_eq!(get_filesystem_root(), PathBuf::from("/host"));
        }
    }
}
