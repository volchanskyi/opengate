//! Fault-injection harness: the failure modes a storage engine exists to
//! survive, applied to substrate A's on-disk segments.
//!
//! These functions manipulate raw segment bytes to simulate a `kill -9` torn
//! write (tail truncation), silent bit-rot (a flipped payload byte), and are
//! paired with the byte-cap disk-full path exercised directly in the gate
//! tests. Every injected fault must leave the store openable — the recovery
//! path may drop or quarantine data, but must never panic the agent.

use std::io::{Read, Write};
use std::path::Path;

use crate::error::{Result, TsdbError};

/// Return the path of the highest-indexed `seg-*.tsdb` file in `dir`.
fn newest_segment(dir: &Path) -> Result<std::path::PathBuf> {
    let mut best: Option<(u64, std::path::PathBuf)> = None;
    for entry in std::fs::read_dir(dir)? {
        let entry = entry?;
        let name = entry.file_name();
        let name = name.to_string_lossy();
        if let Some(rest) = name
            .strip_prefix("seg-")
            .and_then(|n| n.strip_suffix(".tsdb"))
        {
            if let Ok(idx) = rest.parse::<u64>() {
                if best.as_ref().is_none_or(|(b, _)| idx > *b) {
                    best = Some((idx, entry.path()));
                }
            }
        }
    }
    best.map(|(_, p)| p)
        .ok_or(TsdbError::CorruptBlock("no segment to fault-inject"))
}

/// Simulate a torn write by shearing `bytes` off the end of the newest segment.
pub fn truncate_newest_segment(dir: &Path, bytes: u64) -> Result<()> {
    let path = newest_segment(dir)?;
    let f = std::fs::OpenOptions::new().write(true).open(&path)?;
    let len = f.metadata()?.len();
    f.set_len(len.saturating_sub(bytes))?;
    f.sync_all()?;
    Ok(())
}

/// Flip one payload byte of the middle data chunk in the newest segment,
/// simulating silent bit-rot. `frac` selects the position within that chunk's
/// Gorilla payload (`0.0`..`1.0`).
pub fn flip_byte_in_newest_segment(dir: &Path, frac: f64) -> Result<()> {
    let path = newest_segment(dir)?;
    let mut bytes = Vec::new();
    std::fs::File::open(&path)?.read_to_end(&mut bytes)?;

    let target = match crate::frame::middle_data_payload_range(&bytes) {
        Some(range) => {
            let span = range.len().max(1);
            let offset = ((span as f64 * frac.clamp(0.0, 0.999)) as usize).min(span - 1);
            range.start + offset
        }
        // No frameable payload: flip the middle byte so the fault still lands.
        None if !bytes.is_empty() => bytes.len() / 2,
        None => return Err(TsdbError::CorruptBlock("empty segment to fault-inject")),
    };

    bytes[target] ^= 0xFF;
    let mut f = std::fs::OpenOptions::new()
        .write(true)
        .truncate(true)
        .open(&path)?;
    f.write_all(&bytes)?;
    f.sync_all()?;
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::{flip_byte_in_newest_segment, newest_segment, truncate_newest_segment};
    use crate::sample::Sample;
    use crate::substrate::{Durability, Substrate};

    fn seeded_store(dir: &std::path::Path, chunks: i64) {
        let mut s = crate::append_only::AppendOnlyStore::open(dir).unwrap();
        for i in 0..(chunks * 120) {
            s.append(1, Sample::new(1_000 + i, (i % 9) as f64)).unwrap();
        }
        s.commit(Durability::Full).unwrap();
    }

    #[test]
    fn newest_segment_picks_highest_index() {
        let dir = tempfile::tempdir().unwrap();
        seeded_store(dir.path(), 6);
        let p = newest_segment(dir.path()).unwrap();
        assert!(p.file_name().unwrap().to_string_lossy().starts_with("seg-"));
    }

    #[test]
    fn truncate_shrinks_the_file() {
        let dir = tempfile::tempdir().unwrap();
        seeded_store(dir.path(), 6);
        let path = newest_segment(dir.path()).unwrap();
        let before = std::fs::metadata(&path).unwrap().len();
        truncate_newest_segment(dir.path(), 10).unwrap();
        let after = std::fs::metadata(&path).unwrap().len();
        assert_eq!(after, before - 10);
    }

    #[test]
    fn flip_changes_exactly_one_byte() {
        let dir = tempfile::tempdir().unwrap();
        seeded_store(dir.path(), 6);
        let path = newest_segment(dir.path()).unwrap();
        let before = std::fs::read(&path).unwrap();
        flip_byte_in_newest_segment(dir.path(), 0.5).unwrap();
        let after = std::fs::read(&path).unwrap();
        assert_eq!(before.len(), after.len());
        let diffs = before.iter().zip(&after).filter(|(a, b)| a != b).count();
        assert_eq!(diffs, 1);
    }
}
