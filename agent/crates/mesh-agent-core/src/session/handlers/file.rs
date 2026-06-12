//! File-operations control-message handler.
//!
//! Owns `ControlMessage::FileListRequest`, `FileDownloadRequest`, and
//! `FileUploadRequest` dispatch so file operations remain isolated from the
//! [`super::super::handler::SessionHandler`] multiplexer.

use mesh_protocol::{ControlMessage, Frame};
use tokio::sync::mpsc;
use tracing::warn;

use super::super::relay::send_frame;
use super::ControlMessageHandler;
use crate::file_ops::FileOpsHandler;

/// Handles file-related control messages (list, download, upload).
///
/// Unit struct with associated functions — no per-session state. The
/// FileOpsHandler is threaded explicitly and carries the file_read /
/// file_write permission gates internally.
pub struct FileHandler;

impl ControlMessageHandler for FileHandler {}

impl FileHandler {
    /// Process a `FileListRequest` control message. Lists the directory
    /// via FileOpsHandler and sends the response (or a FileListError on
    /// failure) over the frame channel.
    pub async fn handle_list(
        file_ops: &FileOpsHandler,
        frame_tx: &mpsc::Sender<Vec<u8>>,
        path: &str,
    ) {
        match file_ops.list_directory(path) {
            Ok(response) => {
                if let Err(e) = send_frame(frame_tx, &Frame::Control(response)).await {
                    warn!("failed to send file list response: {e}");
                }
            }
            Err(e) => {
                warn!("file list error: {e}");
                if let Err(e) = send_frame(
                    frame_tx,
                    &Frame::Control(ControlMessage::FileListError {
                        path: path.to_string(),
                        error: e.to_string(),
                    }),
                )
                .await
                {
                    warn!("failed to send file list error: {e}");
                }
            }
        }
    }

    /// Process a `FileDownloadRequest` control message. Spawns a
    /// background task that streams the file via FileOpsHandler;
    /// failures are logged but do not propagate (the download stream
    /// owns its own error reporting via the frame channel).
    pub fn handle_download(
        file_ops: &FileOpsHandler,
        frame_tx: &mpsc::Sender<Vec<u8>>,
        path: &str,
    ) {
        let tx = frame_tx.clone();
        let file_ops = file_ops.clone();
        let path = path.to_owned();
        tokio::spawn(async move {
            if let Err(e) = file_ops.stream_download(&path, &tx).await {
                warn!("file download error: {e}");
            }
        });
    }

    /// Process a `FileUploadRequest` control message. Upload is not yet
    /// implemented; the handler accepts and drops the message silently
    /// (no frame emitted, no panic). Matches the pre-carve-out behavior.
    pub fn handle_upload(_path: &str, _total_size: u64) {
        // Intentional no-op — upload feature deferred. See ControlMessage
        // protocol notes in mesh-protocol/src/control.rs.
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn decode(data: &[u8]) -> Frame {
        let (frame, _) = Frame::decode(data).expect("decode failed");
        frame
    }

    #[tokio::test]
    async fn list_success_emits_response() {
        let (frame_tx, mut frame_rx) = mpsc::channel::<Vec<u8>>(8);
        let file_ops = FileOpsHandler::new(true, false);
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().to_string_lossy().to_string();

        FileHandler::handle_list(&file_ops, &frame_tx, &path).await;

        let data = frame_rx.try_recv().expect("expected response");
        match decode(&data) {
            Frame::Control(ControlMessage::FileListResponse { path: p, .. }) => {
                assert_eq!(p, path);
            }
            other => panic!("expected FileListResponse, got {other:?}"),
        }
    }

    #[tokio::test]
    async fn list_error_emits_file_list_error_frame() {
        let (frame_tx, mut frame_rx) = mpsc::channel::<Vec<u8>>(8);
        let file_ops = FileOpsHandler::new(true, false);
        let path = "/nonexistent_abc123_xyz789";

        FileHandler::handle_list(&file_ops, &frame_tx, path).await;

        let data = frame_rx.try_recv().expect("expected error frame");
        match decode(&data) {
            Frame::Control(ControlMessage::FileListError { path: p, error }) => {
                assert_eq!(p, path);
                assert!(!error.is_empty());
            }
            other => panic!("expected FileListError, got {other:?}"),
        }
    }

    #[tokio::test]
    async fn download_streams_to_frame_channel() {
        let (frame_tx, mut frame_rx) = mpsc::channel::<Vec<u8>>(8);
        let file_ops = FileOpsHandler::new(true, false);
        let dir = tempfile::tempdir().unwrap();
        let file_path = dir.path().join("test.txt");
        std::fs::write(&file_path, b"download payload").unwrap();

        FileHandler::handle_download(&file_ops, &frame_tx, &file_path.to_string_lossy());

        let data = tokio::time::timeout(std::time::Duration::from_secs(2), frame_rx.recv())
            .await
            .expect("download task must send within timeout")
            .expect("download task must produce a frame");
        match decode(&data) {
            Frame::FileTransfer(ff) => assert_eq!(ff.data, b"download payload"),
            other => panic!("expected FileTransfer, got {other:?}"),
        }
    }

    #[tokio::test]
    async fn upload_is_silent_acknowledgement() {
        let (frame_tx, mut frame_rx) = mpsc::channel::<Vec<u8>>(8);
        FileHandler::handle_upload("/tmp/x", 1024);
        assert!(matches!(
            frame_rx.try_recv(),
            Err(mpsc::error::TryRecvError::Empty)
        ));
        drop(frame_tx);
    }

    #[test]
    fn file_handler_implements_control_message_handler() {
        fn assert_impl<T: ControlMessageHandler>() {}
        assert_impl::<FileHandler>();
    }
}
