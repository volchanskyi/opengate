//! Linux platform implementations for OpenGate agent.
//!
//! Provides runtime detection and systemd service lifecycle for Linux hosts
//! and containers. Linux agents support Terminal and FileManager only.

pub mod runtime;
pub mod service;

pub use mesh_agent_core::{NullServiceLifecycle, ServiceLifecycle};
pub use runtime::{detect_runtime, get_filesystem_root, LinuxRuntime};
pub use service::SystemdLifecycle;

/// Create a service lifecycle notifier for the current environment.
///
/// Returns [`SystemdLifecycle`] if `NOTIFY_SOCKET` is set,
/// otherwise returns [`NullServiceLifecycle`].
pub fn create_service_lifecycle() -> Box<dyn ServiceLifecycle> {
    if std::env::var_os("NOTIFY_SOCKET").is_some() {
        Box::new(SystemdLifecycle::new())
    } else {
        Box::new(NullServiceLifecycle)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_create_service_lifecycle_without_systemd() {
        std::env::remove_var("NOTIFY_SOCKET");
        let svc = create_service_lifecycle();
        // Should not panic
        svc.notify_ready();
        svc.notify_stopping();
    }
}
