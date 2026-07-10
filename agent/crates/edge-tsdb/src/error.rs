//! Error type shared across the spike substrates.

use thiserror::Error;

/// Errors returned by the local-TSDB spike layer.
#[derive(Debug, Error)]
#[non_exhaustive]
pub enum TsdbError {
    /// Underlying I/O failure (open, read, write, fsync, truncate).
    #[error("io: {0}")]
    Io(#[from] std::io::Error),

    /// The configured byte cap would be breached and nothing could be evicted
    /// to make room. Returned instead of growing the store past its footprint
    /// bound — the store never fills the host disk.
    #[error("capacity exceeded: {have} + {want} bytes over cap {cap}")]
    CapacityExceeded {
        /// Bytes already resident on disk.
        have: u64,
        /// Bytes the refused write would have added.
        want: u64,
        /// The configured cap.
        cap: u64,
    },

    /// A compressed block could not be decoded (truncated or malformed stream).
    #[error("corrupt block: {0}")]
    CorruptBlock(&'static str),

    /// The store on disk was written by a newer agent whose format this build
    /// does not understand. Returned rather than risking a mis-read; the caller
    /// recreates the store fresh (losing the local backlog, never crashing).
    #[error("unsupported store format {found} (this build understands up to {supported})")]
    UnsupportedFormat {
        /// The format version found on disk.
        found: u64,
        /// The newest format this build can read.
        supported: u64,
    },

    /// A redb operation failed.
    #[error("redb: {0}")]
    Redb(String),
}

/// Convenience alias.
pub type Result<T> = std::result::Result<T, TsdbError>;
