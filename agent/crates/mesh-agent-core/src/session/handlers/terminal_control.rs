//! Terminal control-message handler.
//!
//! Owns `ControlMessage::TerminalResize` dispatch. Carved out of
//! [`super::super::handler::SessionHandler`] per ADR-024.

use tracing::info;

use super::super::terminal_handle::TerminalHandle;
use super::ControlMessageHandler;

/// Handles terminal-control messages (currently just resize).
///
/// Unit struct with associated functions — no per-session state.
/// `TerminalHandle` is threaded explicitly. Resize is a no-op when no
/// active terminal session exists.
pub struct TerminalControlHandler;

impl ControlMessageHandler for TerminalControlHandler {}

impl TerminalControlHandler {
    /// Process a `TerminalResize` control message.
    ///
    /// Logs the request unconditionally; forwards the dimensions to the
    /// terminal when a session is active. Silently drops the event when
    /// no session exists — matches the pre-carve-out behavior.
    pub fn handle_resize(terminal: Option<&TerminalHandle>, cols: u16, rows: u16) {
        info!(cols, rows, "terminal resize requested");
        if let Some(term) = terminal {
            term.resize(cols, rows);
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::atomic::AtomicBool;
    use std::sync::Arc;
    use tokio::sync::mpsc;

    fn new_test_terminal() -> (TerminalHandle, mpsc::Receiver<(u16, u16)>) {
        let (stdin_tx, _stdin_rx) = mpsc::channel(8);
        let (resize_tx, resize_rx) = mpsc::channel(8);
        let shutdown = Arc::new(AtomicBool::new(false));
        (
            TerminalHandle::new(stdin_tx, resize_tx, shutdown),
            resize_rx,
        )
    }

    #[test]
    fn resize_with_no_terminal_does_not_panic() {
        TerminalControlHandler::handle_resize(None, 80, 24);
    }

    #[tokio::test]
    async fn resize_with_terminal_forwards_dimensions() {
        let (term, mut resize_rx) = new_test_terminal();
        TerminalControlHandler::handle_resize(Some(&term), 132, 50);
        let (cols, rows) = resize_rx
            .try_recv()
            .expect("resize channel must receive dimensions");
        assert_eq!((cols, rows), (132, 50));
    }

    #[tokio::test]
    async fn resize_boundary_min_max_dimensions() {
        // u16 boundary: minimum and maximum dimensions must round-trip cleanly.
        let (term, mut resize_rx) = new_test_terminal();
        TerminalControlHandler::handle_resize(Some(&term), 1, 1);
        assert_eq!(resize_rx.try_recv().unwrap(), (1, 1));
        TerminalControlHandler::handle_resize(Some(&term), u16::MAX, u16::MAX);
        assert_eq!(resize_rx.try_recv().unwrap(), (u16::MAX, u16::MAX));
    }

    #[test]
    fn terminal_control_handler_implements_control_message_handler() {
        fn assert_impl<T: ControlMessageHandler>() {}
        assert_impl::<TerminalControlHandler>();
    }
}
