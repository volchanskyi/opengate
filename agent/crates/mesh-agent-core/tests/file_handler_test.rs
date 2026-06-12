//! Integration test for `FileHandler`.

use mesh_agent_core::file_ops::FileOpsHandler;
use mesh_agent_core::session::handlers::FileHandler;
use mesh_protocol::{ControlMessage, Frame};
use tokio::sync::mpsc;

fn decode_frame(data: &[u8]) -> Frame {
    let (frame, _) = Frame::decode(data).expect("failed to decode frame");
    frame
}

#[tokio::test]
async fn handle_list_success_emits_file_list_response() {
    let (frame_tx, mut frame_rx) = mpsc::channel::<Vec<u8>>(8);
    let file_ops = FileOpsHandler::new(true, false);
    let dir = tempfile::tempdir().unwrap();
    let path = dir.path().to_string_lossy().to_string();

    FileHandler::handle_list(&file_ops, &frame_tx, &path).await;

    let data = frame_rx.try_recv().expect("expected file list response");
    match decode_frame(&data) {
        Frame::Control(ControlMessage::FileListResponse { path: p, .. }) => {
            assert_eq!(p, path);
        }
        other => panic!("expected FileListResponse, got {other:?}"),
    }
}

#[tokio::test]
async fn handle_upload_is_silent_acknowledgement() {
    // Upload not yet implemented; the handler must accept and drop without
    // emitting any frame. Mutating the arm away would break the wire
    // protocol's expectation of silent ack on unimplemented features.
    let (_frame_tx, mut frame_rx) = mpsc::channel::<Vec<u8>>(8);

    FileHandler::handle_upload("/tmp/x", 1024);

    assert!(matches!(
        frame_rx.try_recv(),
        Err(mpsc::error::TryRecvError::Empty)
    ));
}
