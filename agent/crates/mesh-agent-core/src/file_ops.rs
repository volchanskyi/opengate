//! File operations handler for session file management.
//!
//! Processes `FileListRequest`, `FileDownloadRequest`, and `FileUploadRequest`
//! control messages, streaming file data as `FileFrame`s.

use std::path::Path;

use mesh_protocol::{ControlMessage, FileEntry, FileFrame, Frame};
use tokio::sync::mpsc;
use tracing::debug;

use crate::session_error::SessionError;

/// Chunk size for file transfers: 256 KiB.
const CHUNK_SIZE: usize = 256 * 1024;

/// Handles file operations for a session.
#[derive(Debug, Clone)]
pub struct FileOpsHandler {
    can_read: bool,
    #[allow(dead_code)] // Used when file upload is implemented
    can_write: bool,
}

impl FileOpsHandler {
    /// Create a new file operations handler with the given permissions.
    pub fn new(can_read: bool, can_write: bool) -> Self {
        Self {
            can_read,
            can_write,
        }
    }

    /// List directory contents, returning a `FileListResponse` control message.
    pub fn list_directory(&self, path: &str) -> Result<ControlMessage, SessionError> {
        if !self.can_read {
            return Err(SessionError::PermissionDenied(
                "file_read not permitted".to_string(),
            ));
        }

        let dir_path = Path::new(path);
        let mut entries = Vec::new();

        let read_dir = std::fs::read_dir(dir_path)?;
        for entry in read_dir {
            let entry = entry?;
            let metadata = entry.metadata()?;
            let modified = metadata
                .modified()
                .ok()
                .and_then(|t| t.duration_since(std::time::UNIX_EPOCH).ok())
                .map(|d| d.as_secs() as i64)
                .unwrap_or(0);

            entries.push(FileEntry {
                name: entry.file_name().to_string_lossy().to_string(),
                is_dir: metadata.is_dir(),
                size: metadata.len(),
                modified,
            });
        }

        // Sort: directories first, then alphabetically
        entries.sort_by(|a, b| {
            b.is_dir
                .cmp(&a.is_dir)
                .then_with(|| a.name.to_lowercase().cmp(&b.name.to_lowercase()))
        });

        Ok(ControlMessage::FileListResponse {
            path: path.to_string(),
            entries,
        })
    }

    /// Stream a file download as `FileFrame` chunks.
    pub async fn stream_download(
        &self,
        path: &str,
        frame_tx: &mpsc::Sender<Vec<u8>>,
    ) -> Result<(), SessionError> {
        if !self.can_read {
            return Err(SessionError::PermissionDenied(
                "file_read not permitted".to_string(),
            ));
        }

        let file_path = Path::new(path).to_owned();
        let metadata = tokio::fs::metadata(&file_path).await?;
        let total_size = metadata.len();

        debug!(path, total_size, "streaming file download");

        let data = tokio::fs::read(&file_path).await?;
        let mut offset: u64 = 0;

        for chunk in data.chunks(CHUNK_SIZE) {
            let frame = Frame::FileTransfer(FileFrame {
                offset,
                total_size,
                data: chunk.to_vec(),
            });
            let encoded = frame.encode()?;
            frame_tx
                .send(encoded)
                .await
                .map_err(|_| SessionError::WebSocket("send channel closed".to_string()))?;
            offset += chunk.len() as u64;
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_list_directory_permission_denied() {
        let handler = FileOpsHandler::new(false, false);
        let result = handler.list_directory("/tmp");
        assert!(result.is_err());
        assert!(result
            .unwrap_err()
            .to_string()
            .contains("permission denied"));
    }

    #[test]
    fn test_list_directory_success() {
        let handler = FileOpsHandler::new(true, false);
        let result = handler.list_directory("/tmp");
        assert!(result.is_ok());
        match result.unwrap() {
            ControlMessage::FileListResponse { path, entries: _ } => {
                assert_eq!(path, "/tmp");
            }
            _ => panic!("expected FileListResponse"),
        }
    }

    #[test]
    fn test_list_directory_nonexistent() {
        let handler = FileOpsHandler::new(true, false);
        let result = handler.list_directory("/nonexistent/path/12345");
        assert!(result.is_err());
    }

    #[test]
    fn test_list_directory_sorts_dirs_first() {
        let handler = FileOpsHandler::new(true, false);
        // /tmp should have entries; just verify it doesn't crash
        if let Ok(ControlMessage::FileListResponse { entries, .. }) = handler.list_directory("/tmp")
        {
            // Verify directories come before files
            let mut seen_file = false;
            for entry in &entries {
                if !entry.is_dir {
                    seen_file = true;
                }
                if entry.is_dir && seen_file {
                    panic!("directory found after file in sorted listing");
                }
            }
        }
    }

    #[tokio::test]
    async fn test_stream_download_permission_denied() {
        let handler = FileOpsHandler::new(false, false);
        let (tx, _rx) = mpsc::channel(8);
        let result = handler.stream_download("/etc/hostname", &tx).await;
        assert!(result.is_err());
        assert!(result
            .unwrap_err()
            .to_string()
            .contains("permission denied"));
    }

    #[tokio::test]
    async fn test_stream_download_success() {
        // Create a temp file
        let dir = tempfile::tempdir().unwrap();
        let file_path = dir.path().join("test.txt");
        std::fs::write(&file_path, "hello world").unwrap();

        let handler = FileOpsHandler::new(true, false);
        let (tx, mut rx) = mpsc::channel(8);

        handler
            .stream_download(file_path.to_str().unwrap(), &tx)
            .await
            .unwrap();

        // Should receive one frame (file is small)
        let data = rx.try_recv().unwrap();
        let (frame, _) = Frame::decode(&data).unwrap();
        match frame {
            Frame::FileTransfer(ff) => {
                assert_eq!(ff.offset, 0);
                assert_eq!(ff.total_size, 11);
                assert_eq!(ff.data, b"hello world");
            }
            _ => panic!("expected FileTransfer frame"),
        }
    }

    #[tokio::test]
    async fn test_stream_download_chunked() {
        let dir = tempfile::tempdir().unwrap();
        let file_path = dir.path().join("big.bin");
        // Create a file larger than CHUNK_SIZE
        let data = vec![0xABu8; CHUNK_SIZE + 100];
        std::fs::write(&file_path, &data).unwrap();

        let handler = FileOpsHandler::new(true, false);
        let (tx, mut rx) = mpsc::channel(8);

        handler
            .stream_download(file_path.to_str().unwrap(), &tx)
            .await
            .unwrap();

        // Should receive 2 frames
        let frame1 = rx.try_recv().unwrap();
        let (f1, _) = Frame::decode(&frame1).unwrap();
        match f1 {
            Frame::FileTransfer(ff) => {
                assert_eq!(ff.offset, 0);
                assert_eq!(ff.data.len(), CHUNK_SIZE);
            }
            _ => panic!("expected FileTransfer"),
        }

        let frame2 = rx.try_recv().unwrap();
        let (f2, _) = Frame::decode(&frame2).unwrap();
        match f2 {
            Frame::FileTransfer(ff) => {
                assert_eq!(ff.offset, CHUNK_SIZE as u64);
                assert_eq!(ff.data.len(), 100);
            }
            _ => panic!("expected FileTransfer"),
        }
    }
}
