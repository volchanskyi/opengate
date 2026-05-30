//! Integration tests for `TerminalControlHandler` (ADR-024 §9 carve).
//!
//! Pins the externally-visible contract: TerminalResize delegates to
//! `TerminalHandle::resize` when a terminal session is active; silently
//! no-ops when no session exists.

use std::sync::atomic::AtomicBool;
use std::sync::Arc;

use mesh_agent_core::session::handlers::TerminalControlHandler;
use mesh_agent_core::session::TerminalHandle;
use tokio::sync::mpsc;

#[test]
fn handle_resize_no_terminal_does_not_panic() {
    // No active terminal session — handler must return cleanly.
    TerminalControlHandler::handle_resize(None, 80, 24);
}

#[tokio::test]
async fn handle_resize_with_terminal_forwards_dimensions() {
    let (stdin_tx, _stdin_rx) = mpsc::channel(8);
    let (resize_tx, mut resize_rx) = mpsc::channel(8);
    let shutdown = Arc::new(AtomicBool::new(false));
    let term = TerminalHandle::new(stdin_tx, resize_tx, shutdown);

    TerminalControlHandler::handle_resize(Some(&term), 132, 50);

    let (cols, rows) = resize_rx
        .try_recv()
        .expect("resize channel must receive dimensions");
    assert_eq!((cols, rows), (132, 50));
}
