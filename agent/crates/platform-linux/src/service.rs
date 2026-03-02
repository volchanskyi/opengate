//! Service lifecycle for systemd via the sd_notify protocol.

use std::os::unix::net::UnixDatagram;

use mesh_agent_core::ServiceLifecycle;
use tracing::debug;

/// Systemd service lifecycle notifier.
///
/// Sends state notifications to systemd via the `NOTIFY_SOCKET` Unix datagram.
/// If `NOTIFY_SOCKET` is not set, all notifications are silently ignored.
pub struct SystemdLifecycle {
    notify_socket: Option<String>,
}

impl SystemdLifecycle {
    /// Create a new systemd lifecycle notifier.
    ///
    /// Reads `NOTIFY_SOCKET` from the environment at construction time.
    pub fn new() -> Self {
        Self {
            notify_socket: std::env::var("NOTIFY_SOCKET").ok(),
        }
    }

    fn notify(&self, state: &str) {
        if let Some(ref socket_path) = self.notify_socket {
            debug!(socket = socket_path, state, "sending sd_notify");
            if let Ok(sock) = UnixDatagram::unbound() {
                let _ = sock.send_to(state.as_bytes(), socket_path);
            }
        }
    }
}

impl Default for SystemdLifecycle {
    fn default() -> Self {
        Self::new()
    }
}

impl ServiceLifecycle for SystemdLifecycle {
    fn notify_ready(&self) {
        self.notify("READY=1");
    }

    fn notify_reloading(&self) {
        self.notify("RELOADING=1");
    }

    fn notify_stopping(&self) {
        self.notify("STOPPING=1");
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::os::unix::net::UnixDatagram;

    #[test]
    fn test_systemd_lifecycle_sends_ready() {
        let dir = std::env::temp_dir().join(format!("sd_notify_test_{}", std::process::id()));
        let _ = std::fs::remove_file(&dir);
        let receiver = UnixDatagram::bind(&dir).expect("bind test socket");

        // Point NOTIFY_SOCKET at our test socket
        let svc = SystemdLifecycle {
            notify_socket: Some(dir.to_string_lossy().into_owned()),
        };

        svc.notify_ready();

        let mut buf = [0u8; 64];
        receiver
            .set_read_timeout(Some(std::time::Duration::from_secs(1)))
            .unwrap();
        let n = receiver
            .recv(&mut buf)
            .expect("should receive notification");
        assert_eq!(&buf[..n], b"READY=1");

        let _ = std::fs::remove_file(&dir);
    }

    #[test]
    fn test_systemd_lifecycle_sends_stopping() {
        let dir = std::env::temp_dir().join(format!("sd_notify_stop_{}", std::process::id()));
        let _ = std::fs::remove_file(&dir);
        let receiver = UnixDatagram::bind(&dir).expect("bind test socket");

        let svc = SystemdLifecycle {
            notify_socket: Some(dir.to_string_lossy().into_owned()),
        };

        svc.notify_stopping();

        let mut buf = [0u8; 64];
        receiver
            .set_read_timeout(Some(std::time::Duration::from_secs(1)))
            .unwrap();
        let n = receiver
            .recv(&mut buf)
            .expect("should receive notification");
        assert_eq!(&buf[..n], b"STOPPING=1");

        let _ = std::fs::remove_file(&dir);
    }

    #[test]
    fn test_systemd_lifecycle_no_socket_does_not_panic() {
        let svc = SystemdLifecycle {
            notify_socket: None,
        };
        svc.notify_ready();
        svc.notify_reloading();
        svc.notify_stopping();
    }
}
