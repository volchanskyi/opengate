//! Integration tests for `TerminalControlHandler` resize delegation.

use mesh_agent_core::session::handlers::TerminalControlHandler;
use mesh_agent_core::session::TerminalHandle;
use tokio::sync::mpsc;

#[test]
fn handle_resize_no_terminal_does_not_panic() {
    TerminalControlHandler::handle_resize(None, 80, 24);
}

#[tokio::test]
async fn handle_resize_with_terminal_forwards_dimensions() {
    let (stdin_tx, _stdin_rx) = mpsc::channel(8);
    let (resize_tx, mut resize_rx) = mpsc::channel(8);
    let (hangup, _) = tokio::sync::oneshot::channel();
    let term = TerminalHandle::new(stdin_tx, resize_tx, hangup);

    TerminalControlHandler::handle_resize(Some(&term), 132, 50);

    let (cols, rows) = resize_rx
        .try_recv()
        .expect("resize channel must receive dimensions");
    assert_eq!((cols, rows), (132, 50));
}
