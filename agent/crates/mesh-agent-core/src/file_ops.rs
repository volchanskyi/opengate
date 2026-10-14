//! Directory listings in pages and downloads read one piece at a time, for a session.

use mesh_protocol::{ControlMessage, FileEntry, FileFrame, Frame};
use tokio::io::AsyncReadExt;
use tracing::debug;

use crate::session::outbox::{FILE_PIECE, INTERACTIVE_FRAME_CAP};
use crate::session_error::SessionError;

/// Bytes an encoded listing page's entry array header may grow by past an empty one's.
const ARRAY_HEADER_GROWTH: usize = 4;

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

    fn require_read(&self) -> Result<(), SessionError> {
        if self.can_read {
            Ok(())
        } else {
            Err(SessionError::PermissionDenied(
                "file_read not permitted".to_string(),
            ))
        }
    }

    /// Lists a directory, directories first and then by name, ignoring case.
    pub fn list_directory(&self, path: &str) -> Result<Vec<FileEntry>, SessionError> {
        self.require_read()?;

        let mut entries = Vec::new();
        for entry in std::fs::read_dir(path)? {
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
        Ok(entries)
    }

    /// Opens a file for download and returns it with its size.
    pub async fn open_download(&self, path: &str) -> Result<(tokio::fs::File, u64), SessionError> {
        self.require_read()?;
        let file = tokio::fs::File::open(path).await?;
        let total_size = file.metadata().await?.len();
        debug!(path, total_size, "opening file download");
        Ok((file, total_size))
    }
}

/// The pages of one listing, each a `FileListResponse` whose frame is at most
/// [`INTERACTIVE_FRAME_CAP`] bytes, with `more` set on every page but the last.
#[derive(Debug)]
pub struct ListingPages {
    path: String,
    entries: std::iter::Peekable<std::vec::IntoIter<FileEntry>>,
    room: usize,
    sent_one: bool,
}

/// Cuts `entries` into pages for `path`.
pub fn listing_pages(path: &str, entries: Vec<FileEntry>) -> Result<ListingPages, SessionError> {
    let empty = Frame::Control(ControlMessage::FileListResponse {
        path: path.to_string(),
        entries: Vec::new(),
        more: true,
    })
    .encode()?
    .len();
    let room = INTERACTIVE_FRAME_CAP
        .checked_sub(empty + ARRAY_HEADER_GROWTH)
        .ok_or_else(|| {
            SessionError::Io(std::io::Error::other("path too long for a listing page"))
        })?;
    Ok(ListingPages {
        path: path.to_string(),
        entries: entries.into_iter().peekable(),
        room,
        sent_one: false,
    })
}

impl Iterator for ListingPages {
    type Item = ControlMessage;

    fn next(&mut self) -> Option<ControlMessage> {
        if self.sent_one && self.entries.peek().is_none() {
            return None;
        }
        self.sent_one = true;
        let mut page = Vec::new();
        let mut used = 0;
        while let Some(entry) = self.entries.peek() {
            let size = rmp_serde::to_vec_named(entry).map_or(usize::MAX, |b| b.len());
            if used + size > self.room && !page.is_empty() {
                break;
            }
            used = used.saturating_add(size);
            page.extend(self.entries.next());
        }
        Some(ControlMessage::FileListResponse {
            path: self.path.clone(),
            entries: page,
            more: self.entries.peek().is_some(),
        })
    }
}

/// Reads the next piece of a download at `offset`: up to [`FILE_PIECE`] bytes, fewer only at
/// the end of the file.
pub async fn read_piece(
    file: &mut tokio::fs::File,
    offset: u64,
    total_size: u64,
) -> Result<FileFrame, SessionError> {
    let mut data = Vec::with_capacity(FILE_PIECE);
    let mut chunk = file.take(FILE_PIECE as u64);
    chunk.read_to_end(&mut data).await?;
    Ok(FileFrame {
        offset,
        total_size,
        data,
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    fn entry(name: &str) -> FileEntry {
        FileEntry {
            name: name.to_string(),
            is_dir: false,
            size: 1,
            modified: 1_700_000_000,
        }
    }

    fn page_bytes(page: &ControlMessage) -> usize {
        Frame::Control(page.clone()).encode().unwrap().len()
    }

    #[test]
    fn listing_without_read_permission_is_refused() {
        let handler = FileOpsHandler::new(false, false);
        let err = handler.list_directory("/tmp").unwrap_err();
        assert!(err.to_string().contains("permission denied"));
    }

    #[test]
    fn a_directory_lists_directories_first_then_by_name_ignoring_case() {
        let tmp = tempfile::tempdir().unwrap();
        std::fs::write(tmp.path().join("b.txt"), b"x").unwrap();
        std::fs::write(tmp.path().join("A.txt"), b"x").unwrap();
        std::fs::create_dir(tmp.path().join("zdir")).unwrap();

        let handler = FileOpsHandler::new(true, false);
        let names: Vec<String> = handler
            .list_directory(tmp.path().to_str().unwrap())
            .unwrap()
            .into_iter()
            .map(|e| e.name)
            .collect();
        assert_eq!(names, vec!["zdir", "A.txt", "b.txt"]);
    }

    #[test]
    fn a_missing_directory_is_an_error() {
        let handler = FileOpsHandler::new(true, false);
        assert!(handler.list_directory("/nonexistent/path/12345").is_err());
    }

    #[test]
    fn listing_survives_entries_vanishing_while_it_reads() {
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
        for _ in 0..200 {
            let result = handler.list_directory(dir.to_str().expect("utf-8"));
            assert!(
                result.is_ok(),
                "listing failed under churn: {:?}",
                result.err()
            );
        }
        stop.store(true, Ordering::Relaxed);
        churn.join().expect("churn thread");
    }

    #[test]
    fn an_empty_directory_is_one_last_page() {
        let pages: Vec<ControlMessage> = listing_pages("/empty", Vec::new()).unwrap().collect();
        assert_eq!(
            pages,
            vec![ControlMessage::FileListResponse {
                path: "/empty".into(),
                entries: Vec::new(),
                more: false,
            }]
        );
    }

    #[test]
    fn a_large_directory_goes_out_in_pages_that_each_fit_one_piece() {
        let entries: Vec<FileEntry> = (0..120_000)
            .map(|i| entry(&format!("file-{i:06}-with-a-name-of-forty-two-chars.log")))
            .collect();
        let pages: Vec<ControlMessage> = listing_pages("/var/tmp", entries.clone())
            .unwrap()
            .collect();

        assert!(pages.len() > 100);
        let mut listed = Vec::new();
        for (i, page) in pages.iter().enumerate() {
            assert!(
                page_bytes(page) <= INTERACTIVE_FRAME_CAP,
                "page {i} is past one piece"
            );
            let ControlMessage::FileListResponse {
                path,
                entries,
                more,
            } = page
            else {
                panic!("a page is a listing response");
            };
            assert_eq!(path, "/var/tmp");
            assert_eq!(*more, i + 1 < pages.len(), "only the last page clears more");
            listed.extend(entries.iter().cloned());
        }
        assert_eq!(listed, entries, "every entry once, in order");
    }

    #[test]
    fn pages_fill_close_to_the_limit() {
        let entries: Vec<FileEntry> = (0..2_000).map(|i| entry(&format!("n{i}"))).collect();
        let pages: Vec<ControlMessage> = listing_pages("/", entries).unwrap().collect();
        let first = page_bytes(&pages[0]);
        assert!(
            first > INTERACTIVE_FRAME_CAP - 200,
            "a full page used {first} bytes"
        );
    }

    #[test]
    fn a_path_too_long_for_any_page_is_an_error() {
        let path = "/".repeat(INTERACTIVE_FRAME_CAP);
        assert!(listing_pages(&path, Vec::new()).is_err());
    }

    #[tokio::test]
    async fn a_download_without_read_permission_is_refused() {
        let handler = FileOpsHandler::new(false, false);
        let err = handler.open_download("/etc/hostname").await.unwrap_err();
        assert!(err.to_string().contains("permission denied"));
    }

    #[tokio::test]
    async fn a_download_is_read_in_pieces_of_256_kib() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("big.bin");
        let data: Vec<u8> = (0..FILE_PIECE + 100).map(|i| i as u8).collect();
        std::fs::write(&path, &data).unwrap();

        let handler = FileOpsHandler::new(true, false);
        let (mut file, total) = handler.open_download(path.to_str().unwrap()).await.unwrap();
        assert_eq!(total, data.len() as u64);

        let first = read_piece(&mut file, 0, total).await.unwrap();
        assert_eq!(first.offset, 0);
        assert_eq!(first.data.len(), FILE_PIECE);
        let second = read_piece(&mut file, FILE_PIECE as u64, total)
            .await
            .unwrap();
        assert_eq!(second.offset, FILE_PIECE as u64);
        assert_eq!(second.data, data[FILE_PIECE..]);
        let end = read_piece(&mut file, total, total).await.unwrap();
        assert!(end.data.is_empty(), "past the end a read is empty");
    }

    #[tokio::test]
    async fn a_missing_file_cannot_be_opened() {
        let handler = FileOpsHandler::new(true, false);
        assert!(handler.open_download("/nonexistent/file").await.is_err());
    }
}
