//! Service lifecycle for Windows Service Control Manager.
//!
//! On Windows, notifies the SCM of service state transitions.
//! On non-Windows targets, this module is empty.

#[cfg(windows)]
mod scm {
    use mesh_agent_core::ServiceLifecycle;

    /// Windows Service Control Manager lifecycle notifier.
    pub struct WindowsServiceLifecycle;

    impl WindowsServiceLifecycle {
        /// Create a new Windows SCM lifecycle notifier.
        pub fn new() -> Self {
            Self
        }
    }

    impl Default for WindowsServiceLifecycle {
        fn default() -> Self {
            Self::new()
        }
    }

    impl ServiceLifecycle for WindowsServiceLifecycle {
        fn notify_ready(&self) {
            // TODO: SetServiceStatus(SERVICE_RUNNING)
        }

        fn notify_reloading(&self) {
            // TODO: SetServiceStatus with custom state
        }

        fn notify_stopping(&self) {
            // TODO: SetServiceStatus(SERVICE_STOP_PENDING)
        }
    }
}

#[cfg(windows)]
pub use scm::WindowsServiceLifecycle;
