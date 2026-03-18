//! Frame and file entry types for wire protocol data transfer.

use serde::{Deserialize, Serialize};

/// Encoding format for desktop frame data.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[non_exhaustive]
pub enum FrameEncoding {
    Raw,
    Zlib,
    Zstd,
    H264Idr,
    H264Delta,
}

/// A desktop video frame.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct DesktopFrame {
    pub sequence: u64,
    pub x: u16,
    pub y: u16,
    pub width: u16,
    pub height: u16,
    pub encoding: FrameEncoding,
    #[serde(with = "serde_bytes")]
    pub data: Vec<u8>,
}

/// A terminal data frame.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct TerminalFrame {
    #[serde(with = "serde_bytes")]
    pub data: Vec<u8>,
}

/// A file transfer data frame.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct FileFrame {
    pub offset: u64,
    pub total_size: u64,
    #[serde(with = "serde_bytes")]
    pub data: Vec<u8>,
}

/// A file entry in a directory listing.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct FileEntry {
    pub name: String,
    pub is_dir: bool,
    pub size: u64,
    pub modified: i64,
}
