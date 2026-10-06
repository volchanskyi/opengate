use platform_linux::runtime::{decide_runtime, detect_runtime, get_filesystem_root, LinuxRuntime};
use std::path::{Path, PathBuf};

#[test]
fn container_indicator_wins_over_systemd() {
    assert_eq!(decide_runtime(true, true), LinuxRuntime::Container);
    assert_eq!(decide_runtime(true, false), LinuxRuntime::Container);
}

#[test]
fn systemd_detected_via_notify_socket() {
    assert_eq!(decide_runtime(false, true), LinuxRuntime::BareMetalSystemd);
}

#[test]
fn falls_back_to_bare_metal_other() {
    assert_eq!(decide_runtime(false, false), LinuxRuntime::BareMetalOther);
}

#[test]
fn detect_runtime_returns_a_valid_variant() {
    assert!(matches!(
        detect_runtime(),
        LinuxRuntime::Container | LinuxRuntime::BareMetalSystemd | LinuxRuntime::BareMetalOther
    ));
}

#[test]
fn filesystem_root_matches_host_layout() {
    if Path::new("/host").is_dir() {
        assert_eq!(get_filesystem_root(), PathBuf::from("/host"));
    } else {
        assert_eq!(get_filesystem_root(), PathBuf::from("/"));
    }
}
