//! File listing and chunked download for a session.

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
    #[expect(dead_code, reason = "reserved for file upload (Phase 6)")]
    can_write: bool,
}

impl FileOpsHandler {
    /// Creates a handler with the given read and write permissions.
    pub fn new(can_read: bool, can_write: bool) -> Self {
        Self {
            can_read,
            can_write,
        }
    }

    /// Lists directory contents as a `FileListResponse`, directories first.
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
            // An entry can vanish between readdir() and metadata(); NotFound skips it.
            let entry = match entry {
                Ok(e) => e,
                Err(e) if e.kind() == std::io::ErrorKind::NotFound => continue,
                Err(e) => return Err(e.into()),
            };
            let metadata = match entry.metadata() {
                Ok(m) => m,
                Err(e) if e.kind() == std::io::ErrorKind::NotFound => continue,
                Err(e) => return Err(e.into()),
            };
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

    /// Streams a file download as `FileFrame` chunks.
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

        // An empty file yields zero chunks, so one empty frame is sent.
        if data.is_empty() {
            let frame = Frame::FileTransfer(FileFrame {
                offset: 0,
                total_size: 0,
                data: vec![],
            });
            let encoded = frame.encode()?;
            frame_tx
                .send(encoded)
                .await
                .map_err(|_| SessionError::WebSocket("send channel closed".to_string()))?;
            return Ok(());
        }

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
    fn test_list_directory_concurrent_churn() {
        use std::sync::atomic::{AtomicBool, Ordering};
        use std::sync::Arc;

        let tmp = tempfile::tempdir().expect("create tempdir");
        let dir = tmp.path().to_path_buf();
        for i in 0..5 {
            std::fs::write(dir.join(format!("stable-{i}.txt")), b"x").expect("seed file");
        }

        let stop = Arc::new(AtomicBool::new(false));
        let churn_dir = dir.clone();
        let churn_stop = Arc::clone(&stop);
        let churn = std::thread::spawn(move || {
            let mut i = 0u64;
            while !churn_stop.load(Ordering::Relaxed) {
                let p = churn_dir.join(format!("churn-{i}.txt"));
                // Transient I/O errors during churn are irrelevant to the listing under test.
                std::fs::write(&p, b"y").ok();
                std::fs::remove_file(&p).ok();
                i += 1;
            }
        });

        let handler = FileOpsHandler::new(true, false);
        let dir_str = dir.to_str().expect("utf-8");
        for _ in 0..200 {
            let result = handler.list_directory(dir_str);
            assert!(
                result.is_ok(),
                "list_directory must not fail under concurrent churn: {:?}",
                result.err()
            );
        }
        stop.store(true, Ordering::Relaxed);
        churn.join().expect("churn thread");
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
        if let Ok(ControlMessage::FileListResponse { entries, .. }) = handler.list_directory("/tmp")
        {
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
        let dir = tempfile::tempdir().unwrap();
        let file_path = dir.path().join("test.txt");
        std::fs::write(&file_path, "hello world").unwrap();

        let handler = FileOpsHandler::new(true, false);
        let (tx, mut rx) = mpsc::channel(8);

        handler
            .stream_download(file_path.to_str().unwrap(), &tx)
            .await
            .unwrap();

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
    async fn test_stream_download_empty_file() {
        let dir = tempfile::tempdir().unwrap();
        let file_path = dir.path().join("empty.txt");
        std::fs::write(&file_path, "").unwrap();

        let handler = FileOpsHandler::new(true, false);
        let (tx, mut rx) = mpsc::channel(8);

        handler
            .stream_download(file_path.to_str().unwrap(), &tx)
            .await
            .unwrap();

        let data = rx.try_recv().unwrap();
        let (frame, _) = Frame::decode(&data).unwrap();
        match frame {
            Frame::FileTransfer(ff) => {
                assert_eq!(ff.offset, 0);
                assert_eq!(ff.total_size, 0);
                assert!(ff.data.is_empty());
            }
            _ => panic!("expected FileTransfer frame"),
        }

        assert!(rx.try_recv().is_err());
    }

    #[tokio::test]
    async fn stream_download_chunk_size_is_256_kib_not_256_plus_1024() {
        let dir = tempfile::tempdir().unwrap();
        let file_path = dir.path().join("absolute_size.bin");
        std::fs::write(&file_path, vec![0xCDu8; 300_000]).unwrap();

        let handler = FileOpsHandler::new(true, false);
        let (tx, mut rx) = mpsc::channel(512);
        handler
            .stream_download(file_path.to_str().unwrap(), &tx)
            .await
            .unwrap();

        let mut frames = 0;
        while rx.try_recv().is_ok() {
            frames += 1;
        }
        assert_eq!(
            frames, 2,
            "300 KiB must split into exactly 2 chunks (CHUNK_SIZE=256 KiB)"
        );
    }

    #[tokio::test]
    async fn test_stream_download_chunked() {
        let dir = tempfile::tempdir().unwrap();
        let file_path = dir.path().join("big.bin");
        let data = vec![0xABu8; CHUNK_SIZE + 100];
        std::fs::write(&file_path, &data).unwrap();

        let handler = FileOpsHandler::new(true, false);
        let (tx, mut rx) = mpsc::channel(8);

        handler
            .stream_download(file_path.to_str().unwrap(), &tx)
            .await
            .unwrap();

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
