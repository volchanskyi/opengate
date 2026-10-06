//! Substrate A: Gorilla chunks framed with length and CRC into rotating segment files.
//! Open recovers by truncating a torn tail and quarantining CRC-failing records.

use std::fs::{File, OpenOptions};
use std::io::Write;
use std::path::{Path, PathBuf};

use crate::error::{Result, TsdbError};
use crate::frame::{self, Chunk};
use crate::gorilla::encode_block;
use crate::sample::{Sample, SeriesId};
use crate::substrate::{Durability, Substrate};

/// Samples per sealed chunk (2 minutes at 1 Hz).
const CHUNK_SAMPLES: usize = 120;
/// Rotate to a fresh segment once the active one passes this size.
const SEGMENT_TARGET: u64 = 16 * 1024;

/// A segment file and the chunks it holds.
struct Segment {
    index: u64,
    path: PathBuf,
    size: u64,
    chunks: Vec<Chunk>,
    has_commit: bool,
}

/// Counts from the recovery scan at [`AppendOnlyStore::open`].
#[derive(Debug, Clone, Copy, Default)]
pub struct IntegrityReport {
    /// Records dropped for a failed CRC (bit-rot).
    pub quarantined_chunks: usize,
    /// Segments whose torn tail was truncated on open.
    pub repaired_segments: usize,
    /// Samples guaranteed durable (covered by the last commit marker).
    pub durable_samples: usize,
    /// Samples recoverable in total (durable + best-effort tail).
    pub recoverable_samples: usize,
}

/// Substrate A.
pub struct AppendOnlyStore {
    dir: PathBuf,
    segments: Vec<Segment>,
    active: Option<File>,
    open_chunks: std::collections::BTreeMap<SeriesId, Vec<Sample>>,
    byte_cap: u64,
    report: IntegrityReport,
}

impl AppendOnlyStore {
    /// Caps the on-disk footprint; sealing evicts oldest segments, and a write that
    /// still exceeds it fails with [`TsdbError::CapacityExceeded`].
    pub fn set_byte_cap(&mut self, cap: u64) {
        self.byte_cap = cap;
    }

    /// The recovery counts from the last open.
    pub fn integrity_report(&self) -> Result<IntegrityReport> {
        Ok(self.report)
    }

    fn segment_path(dir: &Path, index: u64) -> PathBuf {
        dir.join(format!("seg-{index:08}.tsdb"))
    }

    /// Seal the buffered samples of one series into a persisted chunk.
    fn seal(&mut self, series: SeriesId) -> Result<()> {
        let Some(samples) = self.open_chunks.remove(&series) else {
            return Ok(());
        };
        if samples.is_empty() {
            return Ok(());
        }
        let block = encode_block(&samples);
        let mut rec = Vec::new();
        frame::write_data_record(&mut rec, series, &block);
        self.enforce_cap(rec.len() as u64)?;
        self.write_to_active(&rec)?;
        if let Some(seg) = self.segments.last_mut() {
            seg.chunks.push(Chunk { series, block });
        }
        self.maybe_rotate()?;
        Ok(())
    }

    /// Ensure there is a writable active segment, then append `bytes` to it.
    fn write_to_active(&mut self, bytes: &[u8]) -> Result<()> {
        if self.active.is_none() {
            let index = self.segments.last().map_or(0, |s| s.index + 1);
            let path = Self::segment_path(&self.dir, index);
            let file = OpenOptions::new().create(true).append(true).open(&path)?;
            self.active = Some(file);
            self.segments.push(Segment {
                index,
                path,
                size: 0,
                chunks: Vec::new(),
                has_commit: false,
            });
        }
        self.active.as_mut().unwrap().write_all(bytes)?;
        if let Some(seg) = self.segments.last_mut() {
            seg.size += bytes.len() as u64;
        }
        Ok(())
    }

    fn maybe_rotate(&mut self) -> Result<()> {
        if self
            .segments
            .last()
            .is_some_and(|s| s.size >= SEGMENT_TARGET)
        {
            if let Some(f) = self.active.take() {
                f.sync_all()?;
            }
        }
        Ok(())
    }

    /// Evicts oldest segments until `extra` more bytes fit under the cap.
    fn enforce_cap(&mut self, extra: u64) -> Result<()> {
        loop {
            let total: u64 = self.segments.iter().map(|s| s.size).sum();
            if total + extra <= self.byte_cap {
                return Ok(());
            }
            // The segment being written into is never evicted.
            if self.segments.len() <= 1 {
                return Err(TsdbError::CapacityExceeded {
                    have: total,
                    want: extra,
                    cap: self.byte_cap,
                });
            }
            let victim = self.segments.remove(0);
            std::fs::remove_file(&victim.path)?;
        }
    }

    fn all_chunks(&self) -> impl Iterator<Item = &Chunk> {
        self.segments.iter().flat_map(|s| s.chunks.iter())
    }
}

impl Substrate for AppendOnlyStore {
    fn open(path: &Path) -> Result<Self> {
        std::fs::create_dir_all(path)?;
        let mut indices: Vec<u64> = Vec::new();
        for entry in std::fs::read_dir(path)? {
            let entry = entry?;
            let name = entry.file_name();
            let name = name.to_string_lossy();
            if let Some(rest) = name
                .strip_prefix("seg-")
                .and_then(|n| n.strip_suffix(".tsdb"))
            {
                if let Ok(idx) = rest.parse::<u64>() {
                    indices.push(idx);
                }
            }
        }
        indices.sort_unstable();

        let mut segments = Vec::new();
        let mut report = IntegrityReport::default();
        let mut global = 0usize;
        let mut durable_chunks_global = 0usize;

        for idx in indices {
            let seg_path = Self::segment_path(path, idx);
            let bytes = std::fs::read(&seg_path)?;
            let scanned = frame::scan(&bytes);
            report.quarantined_chunks += scanned.quarantined;
            let mut size = bytes.len() as u64;
            if let Some(off) = scanned.repair_offset {
                let f = OpenOptions::new().write(true).open(&seg_path)?;
                f.set_len(off)?;
                f.sync_all()?;
                report.repaired_segments += 1;
                size = off;
            }
            let start = global;
            global += scanned.chunks.len();
            if scanned.has_commit {
                durable_chunks_global = start + scanned.durable_chunks;
            }
            segments.push(Segment {
                index: idx,
                path: seg_path,
                size,
                chunks: scanned.chunks,
                has_commit: scanned.has_commit,
            });
        }

        let counts: Vec<usize> = segments
            .iter()
            .flat_map(|s| s.chunks.iter())
            .map(|c| crate::gorilla::block_count(&c.block) as usize)
            .collect();
        report.recoverable_samples = counts.iter().sum();
        report.durable_samples = counts.iter().take(durable_chunks_global).sum();

        Ok(Self {
            dir: path.to_path_buf(),
            segments,
            active: None,
            open_chunks: std::collections::BTreeMap::new(),
            byte_cap: u64::MAX,
            report,
        })
    }

    fn append(&mut self, series: SeriesId, sample: Sample) -> Result<()> {
        let buf = self.open_chunks.entry(series).or_default();
        buf.push(sample);
        if buf.len() >= CHUNK_SAMPLES {
            self.seal(series)?;
        }
        Ok(())
    }

    fn commit(&mut self, durability: Durability) -> Result<()> {
        let series: Vec<SeriesId> = self.open_chunks.keys().copied().collect();
        for s in series {
            self.seal(s)?;
        }
        let mut rec = Vec::new();
        frame::write_commit_record(&mut rec);
        self.write_to_active(&rec)?;
        if let Some(seg) = self.segments.last_mut() {
            seg.has_commit = true;
        }
        if durability == Durability::Full {
            if let Some(f) = self.active.as_ref() {
                f.sync_all()?;
            }
            // The directory fsync is best-effort: a platform without it keeps a narrower guarantee.
            if let Ok(dir) = File::open(&self.dir) {
                drop(dir.sync_all());
            }
        }
        Ok(())
    }

    fn range(&self, series: SeriesId, start: i64, end: i64) -> Result<Vec<Sample>> {
        let chunks: Vec<Chunk> = self.all_chunks().cloned().collect();
        let mut out = frame::collect_series(&chunks, series, start, end)?;
        if let Some(buf) = self.open_chunks.get(&series) {
            for s in buf {
                if s.ts >= start && s.ts < end {
                    out.push(*s);
                }
            }
            out.sort_by_key(|s| s.ts);
        }
        Ok(out)
    }

    fn size_on_disk(&self) -> Result<u64> {
        Ok(self.segments.iter().map(|s| s.size).sum())
    }

    fn total_samples(&self) -> Result<usize> {
        let sealed: usize = self
            .all_chunks()
            .map(|c| crate::gorilla::block_count(&c.block) as usize)
            .sum();
        let buffered: usize = self.open_chunks.values().map(Vec::len).sum();
        Ok(sealed + buffered)
    }
}

#[cfg(test)]
mod tests {
    use super::AppendOnlyStore;
    use crate::sample::Sample;
    use crate::substrate::{Durability, Substrate};

    fn feed(store: &mut AppendOnlyStore, series: u32, n: i64) {
        for i in 0..n {
            store
                .append(series, Sample::new(1_000 + i, (i % 7) as f64))
                .unwrap();
        }
    }

    #[test]
    fn reopen_preserves_committed_data() {
        let dir = tempfile::tempdir().unwrap();
        {
            let mut s = AppendOnlyStore::open(dir.path()).unwrap();
            feed(&mut s, 1, 500);
            s.commit(Durability::Full).unwrap();
            assert_eq!(s.total_samples().unwrap(), 500);
        }
        let s = AppendOnlyStore::open(dir.path()).unwrap();
        assert_eq!(s.total_samples().unwrap(), 500);
        let got = s.range(1, 1_000, 2_000).unwrap();
        assert_eq!(got.len(), 500);
        assert_eq!(s.integrity_report().unwrap().durable_samples, 500);
    }

    #[test]
    fn buffered_samples_are_readable_before_commit() {
        let dir = tempfile::tempdir().unwrap();
        let mut s = AppendOnlyStore::open(dir.path()).unwrap();
        feed(&mut s, 2, 30);
        assert_eq!(s.range(2, 0, 10_000).unwrap().len(), 30);
        assert_eq!(s.size_on_disk().unwrap(), 0);
    }

    #[test]
    fn commit_none_then_full_is_durable() {
        let dir = tempfile::tempdir().unwrap();
        {
            let mut s = AppendOnlyStore::open(dir.path()).unwrap();
            feed(&mut s, 3, 200);
            s.commit(Durability::None).unwrap();
            feed(&mut s, 3, 200);
            s.commit(Durability::Full).unwrap();
        }
        let s = AppendOnlyStore::open(dir.path()).unwrap();
        assert_eq!(s.total_samples().unwrap(), 400);
    }
}
