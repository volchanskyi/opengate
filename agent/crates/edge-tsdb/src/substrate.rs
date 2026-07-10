//! The common contract every bake-off substrate implements.
//!
//! A / B share the Gorilla tiering+compression layer and differ only in how
//! blocks are persisted (bespoke append-only files vs redb); C is the no-persist
//! control. Keeping them behind one trait means the fixture corpus and the
//! measurement runner drive all three identically, so measured differences are
//! attributable to the substrate, not the harness.

use std::path::Path;

use crate::error::Result;
use crate::sample::{Sample, SeriesId};

/// Durability requested at commit time.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
#[non_exhaustive]
pub enum Durability {
    /// Flush and fsync — survives power loss. The bounded-loss boundary.
    Full,
    /// Buffer only; a later `Full` commit or clean close makes it durable. Fast
    /// path with a bounded loss window on crash.
    None,
}

/// A local time-series substrate: append points, commit for durability, and
/// range-query them back.
pub trait Substrate: Sized {
    /// Open (creating if absent) a store rooted at `path`.
    fn open(path: &Path) -> Result<Self>;

    /// Append one sample to `series`. Buffered until a chunk seals or a commit.
    fn append(&mut self, series: SeriesId, sample: Sample) -> Result<()>;

    /// Seal open chunks and persist with the requested durability.
    fn commit(&mut self, durability: Durability) -> Result<()>;

    /// Range query `[start, end)` for one series, sorted ascending by timestamp.
    fn range(&self, series: SeriesId, start: i64, end: i64) -> Result<Vec<Sample>>;

    /// Bytes resident on disk (0 for the no-persist control).
    fn size_on_disk(&self) -> Result<u64>;

    /// Total recoverable sample count across every series.
    fn total_samples(&self) -> Result<usize>;
}
