//! File listing and download messages. A listing pages out one page at a time, and a session runs
//! one download at a time, read as the bulk lane drains.

use mesh_protocol::{ControlMessage, Frame};
use tokio::task::JoinHandle;
use tracing::{debug, warn};

use super::super::outbox::{one_at_a_time, Outbox};
use super::ControlMessageHandler;
use crate::file_ops::{listing_pages, read_piece, FileOpsHandler};
use crate::session::outbox::FILE_PIECE;

/// What a second download request in the same session is told.
pub const DOWNLOAD_RUNNING: &str = "a download is already running in this session";

/// Error text sent to the browser is cut to this many bytes, so the message fits one piece.
const MAX_ERROR_TEXT: usize = 1024;

/// Handles file list, download and upload messages; `FileOpsHandler` holds the permission gates.
pub struct FileHandler;

impl ControlMessageHandler for FileHandler {}

/// The session's listing and download tasks; ending the session stops both and waits for them.
#[derive(Debug, Default)]
pub struct FileTasks {
    listing: Option<JoinHandle<()>>,
    download: Option<JoinHandle<()>>,
}

impl FileTasks {
    /// Stops the listing and the download and waits until each has let go of its file.
    pub async fn stop(&mut self) {
        for task in [self.listing.take(), self.download.take()]
            .into_iter()
            .flatten()
        {
            task.abort();
            task.await.ok();
        }
    }

    /// Whether a download is still being read.
    pub fn downloading(&self) -> bool {
        self.download.as_ref().is_some_and(|t| !t.is_finished())
    }
}

fn error_text(e: impl std::fmt::Display) -> String {
    let mut text = e.to_string();
    if text.len() > MAX_ERROR_TEXT {
        let mut cut = MAX_ERROR_TEXT;
        while !text.is_char_boundary(cut) {
            cut -= 1;
        }
        text.truncate(cut);
    }
    text
}

async fn send(outbox: &Outbox, msg: ControlMessage) {
    if let Err(e) = outbox.send(&Frame::Control(msg)).await {
        warn!("file message not sent: {e}");
    }
}

impl FileHandler {
    /// Lists `path` in pages; a newer listing replaces one still paging out.
    pub fn handle_list(
        file_ops: &FileOpsHandler,
        outbox: &Outbox,
        tasks: &mut FileTasks,
        path: &str,
    ) {
        if let Some(previous) = tasks.listing.take() {
            previous.abort();
        }
        let (file_ops, outbox, path) = (file_ops.clone(), outbox.clone(), path.to_owned());
        tasks.listing = Some(tokio::spawn(async move {
            let listed = {
                let (file_ops, path) = (file_ops.clone(), path.clone());
                tokio::task::spawn_blocking(move || file_ops.list_directory(&path)).await
            };
            let pages = match listed {
                Ok(Ok(entries)) => listing_pages(&path, entries),
                Ok(Err(e)) => Err(e),
                Err(e) => Err(std::io::Error::other(e).into()),
            };
            match pages {
                Ok(pages) => {
                    let pace = one_at_a_time();
                    for page in pages {
                        if let Err(e) = outbox.send_paced(&Frame::Control(page), &pace).await {
                            debug!("listing stopped: {e}");
                            return;
                        }
                    }
                }
                Err(e) => {
                    warn!("file list error: {e}");
                    let error = error_text(e);
                    send(&outbox, ControlMessage::FileListError { path, error }).await;
                }
            }
        }));
    }

    /// Starts reading `path` for download, or refuses it while another download runs.
    pub async fn handle_download(
        file_ops: &FileOpsHandler,
        outbox: &Outbox,
        tasks: &mut FileTasks,
        path: &str,
    ) {
        if tasks.downloading() {
            send(
                outbox,
                ControlMessage::FileDownloadError {
                    path: path.to_owned(),
                    error: DOWNLOAD_RUNNING.to_string(),
                },
            )
            .await;
            return;
        }
        let (file_ops, outbox, path) = (file_ops.clone(), outbox.clone(), path.to_owned());
        tasks.download = Some(tokio::spawn(async move {
            if let Err(error) = download(&file_ops, &outbox, &path).await {
                warn!("file download error: {error}");
                send(&outbox, ControlMessage::FileDownloadError { path, error }).await;
            }
        }));
    }

    /// Accepts and drops a `FileUploadRequest`.
    pub fn handle_upload(_path: &str, _total_size: u64) {
        // The dispatcher logs the request, and no file is written.
    }
}

/// Reads the file one piece at a time, each read waiting for room in the bulk lane, so neither
/// side ever holds more than the lane's two pieces of it.
async fn download(file_ops: &FileOpsHandler, outbox: &Outbox, path: &str) -> Result<(), String> {
    let (mut file, total) = file_ops.open_download(path).await.map_err(error_text)?;
    let mut offset = 0u64;
    loop {
        let piece = read_piece(&mut file, offset, total)
            .await
            .map_err(error_text)?;
        let len = piece.data.len();
        if len == 0 && offset > 0 {
            return Ok(());
        }
        outbox
            .send(&Frame::FileTransfer(piece))
            .await
            .map_err(error_text)?;
        offset += len as u64;
        if len < FILE_PIECE {
            return Ok(());
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::session::outbox::Lanes;
    use mesh_protocol::FileFrame;
    use std::time::Duration;

    async fn next_frame(lanes: &mut Lanes) -> Frame {
        tokio::time::timeout(Duration::from_secs(5), async {
            loop {
                if let Some(bytes) = lanes.try_next() {
                    return Frame::decode(&bytes).unwrap().0;
                }
                tokio::time::sleep(Duration::from_millis(2)).await;
            }
        })
        .await
        .expect("a frame arrives")
    }

    #[tokio::test]
    async fn a_listing_arrives_as_its_pages() {
        let (outbox, mut lanes) = Outbox::new();
        let mut tasks = FileTasks::default();
        let dir = tempfile::tempdir().unwrap();
        for i in 0..2000 {
            std::fs::write(dir.path().join(format!("f{i:04}")), b"").unwrap();
        }
        let path = dir.path().to_string_lossy().to_string();

        FileHandler::handle_list(
            &FileOpsHandler::new(true, false),
            &outbox,
            &mut tasks,
            &path,
        );

        let mut listed = 0;
        loop {
            match next_frame(&mut lanes).await {
                Frame::Control(ControlMessage::FileListResponse { entries, more, .. }) => {
                    listed += entries.len();
                    if !more {
                        break;
                    }
                }
                other => panic!("expected a listing page, got {other:?}"),
            }
        }
        assert_eq!(listed, 2000);
    }

    #[tokio::test]
    async fn a_listing_that_fails_says_why() {
        let (outbox, mut lanes) = Outbox::new();
        let mut tasks = FileTasks::default();
        FileHandler::handle_list(
            &FileOpsHandler::new(true, false),
            &outbox,
            &mut tasks,
            "/nonexistent_abc123_xyz789",
        );
        match next_frame(&mut lanes).await {
            Frame::Control(ControlMessage::FileListError { path, error }) => {
                assert_eq!(path, "/nonexistent_abc123_xyz789");
                assert!(!error.is_empty());
            }
            other => panic!("expected FileListError, got {other:?}"),
        }
    }

    #[tokio::test]
    async fn a_download_streams_its_pieces_and_a_second_is_refused_while_it_runs() {
        let (outbox, mut lanes) = Outbox::new();
        let mut tasks = FileTasks::default();
        let dir = tempfile::tempdir().unwrap();
        let big = dir.path().join("big.bin");
        let data: Vec<u8> = (0..4 * FILE_PIECE + 10).map(|i| (i % 251) as u8).collect();
        std::fs::write(&big, &data).unwrap();
        let ops = FileOpsHandler::new(true, false);

        FileHandler::handle_download(&ops, &outbox, &mut tasks, &big.to_string_lossy()).await;
        tokio::time::sleep(Duration::from_millis(50)).await;
        assert!(
            tasks.downloading(),
            "the reader waits on the bulk lane's two pieces"
        );

        FileHandler::handle_download(&ops, &outbox, &mut tasks, "/etc/hostname").await;
        match next_frame(&mut lanes).await {
            Frame::Control(ControlMessage::FileDownloadError { path, error }) => {
                assert_eq!(path, "/etc/hostname");
                assert_eq!(error, DOWNLOAD_RUNNING);
            }
            other => panic!("the refusal goes ahead of queued file data, got {other:?}"),
        }

        let mut received = Vec::new();
        while received.len() < data.len() {
            match next_frame(&mut lanes).await {
                Frame::FileTransfer(FileFrame {
                    offset,
                    total_size,
                    data: piece,
                }) => {
                    assert_eq!(offset as usize, received.len());
                    assert_eq!(total_size as usize, data.len());
                    received.extend(piece);
                }
                other => panic!("expected file data, got {other:?}"),
            }
        }
        assert_eq!(received, data);
    }

    #[tokio::test]
    async fn an_empty_file_is_one_empty_piece() {
        let (outbox, mut lanes) = Outbox::new();
        let mut tasks = FileTasks::default();
        let dir = tempfile::tempdir().unwrap();
        let empty = dir.path().join("empty");
        std::fs::write(&empty, b"").unwrap();

        FileHandler::handle_download(
            &FileOpsHandler::new(true, false),
            &outbox,
            &mut tasks,
            &empty.to_string_lossy(),
        )
        .await;
        match next_frame(&mut lanes).await {
            Frame::FileTransfer(f) => {
                assert_eq!((f.offset, f.total_size), (0, 0));
                assert!(f.data.is_empty());
            }
            other => panic!("expected an empty piece, got {other:?}"),
        }
    }

    #[tokio::test]
    async fn a_download_that_cannot_be_read_ends_with_its_error() {
        let (outbox, mut lanes) = Outbox::new();
        let mut tasks = FileTasks::default();
        FileHandler::handle_download(
            &FileOpsHandler::new(true, false),
            &outbox,
            &mut tasks,
            "/nonexistent/download",
        )
        .await;
        match next_frame(&mut lanes).await {
            Frame::Control(ControlMessage::FileDownloadError { path, error }) => {
                assert_eq!(path, "/nonexistent/download");
                assert!(!error.is_empty());
            }
            other => panic!("expected FileDownloadError, got {other:?}"),
        }
    }

    #[tokio::test]
    async fn stopping_the_session_stops_the_reader() {
        let (outbox, _lanes) = Outbox::new();
        let mut tasks = FileTasks::default();
        let dir = tempfile::tempdir().unwrap();
        let big = dir.path().join("big.bin");
        std::fs::write(&big, vec![0u8; 8 * FILE_PIECE]).unwrap();

        FileHandler::handle_download(
            &FileOpsHandler::new(true, false),
            &outbox,
            &mut tasks,
            &big.to_string_lossy(),
        )
        .await;
        tokio::time::sleep(Duration::from_millis(20)).await;
        assert!(tasks.downloading());
        tasks.stop().await;
        assert!(!tasks.downloading());
    }

    #[test]
    fn error_text_is_cut_on_a_character_boundary() {
        let long = "é".repeat(MAX_ERROR_TEXT);
        let cut = error_text(long);
        assert!(cut.len() <= MAX_ERROR_TEXT);
        assert!(cut.chars().all(|c| c == 'é'));
    }
}
