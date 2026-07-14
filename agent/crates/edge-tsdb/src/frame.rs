//! On-disk record framing for the append-only substrate.
//!
//! A segment file is a flat sequence of length-prefixed, CRC-guarded records:
//!
//! ```text
//! [u8 kind][u32 BE payload_len][u32 BE crc32(payload)][payload ...]
//! ```
//!
//! Two record kinds exist: a *data* record (`payload = [u32 BE series][gorilla
//! block]`) and a *commit* marker written by a durable commit. Recovery scans
//! records in order; a torn trailing record is truncated away (bounded loss), a
//! full-but-CRC-failing record is quarantined and skipped (never panics), and
//! the last valid commit marker delimits the guaranteed-durable prefix.

use crate::crc::crc32;
use crate::error::Result;
use crate::sample::SeriesId;

const KIND_DATA: u8 = 1;
const KIND_COMMIT: u8 = 2;
const HEADER_LEN: usize = 9;

/// Append a data record (a compressed Gorilla block for one series) to `buf`.
pub fn write_data_record(buf: &mut Vec<u8>, series: SeriesId, block: &[u8]) {
    let mut payload = Vec::with_capacity(4 + block.len());
    payload.extend_from_slice(&series.to_be_bytes());
    payload.extend_from_slice(block);
    write_record(buf, KIND_DATA, &payload);
}

/// Append a durable-commit marker to `buf`.
pub fn write_commit_record(buf: &mut Vec<u8>) {
    write_record(buf, KIND_COMMIT, &[]);
}

fn write_record(buf: &mut Vec<u8>, kind: u8, payload: &[u8]) {
    buf.push(kind);
    buf.extend_from_slice(&(payload.len() as u32).to_be_bytes());
    buf.extend_from_slice(&crc32(payload).to_be_bytes());
    buf.extend_from_slice(payload);
}

/// One recovered data chunk: its series and the raw Gorilla block bytes.
#[derive(Debug, Clone)]
pub struct Chunk {
    /// Series the chunk belongs to.
    pub series: SeriesId,
    /// Compressed Gorilla block (decode with [`crate::gorilla::decode_block`]).
    pub block: Vec<u8>,
}

/// Result of scanning a segment file.
#[derive(Debug, Default)]
pub struct ScanResult {
    /// Valid data chunks in file order (durable prefix + best-effort tail).
    pub chunks: Vec<Chunk>,
    /// How many of `chunks` are covered by the last valid commit marker.
    pub durable_chunks: usize,
    /// If `Some(len)`, a torn trailing record was found and the file should be
    /// truncated to `len` bytes to repair it.
    pub repair_offset: Option<u64>,
    /// Number of full records dropped for a failed CRC (bit-rot quarantine).
    pub quarantined: usize,
    /// Whether a valid commit marker was seen anywhere in the segment.
    pub has_commit: bool,
}

/// Scan a segment's bytes into recoverable chunks, never panicking on
/// truncation or corruption.
#[must_use]
pub fn scan(bytes: &[u8]) -> ScanResult {
    let mut out = ScanResult::default();
    let mut pos = 0usize;

    while pos < bytes.len() {
        if bytes.len() - pos < HEADER_LEN {
            // Partial header at the tail: torn write.
            out.repair_offset = Some(pos as u64);
            break;
        }
        let kind = bytes[pos];
        let len = u32::from_be_bytes(bytes[pos + 1..pos + 5].try_into().unwrap()) as usize;
        let crc = u32::from_be_bytes(bytes[pos + 5..pos + 9].try_into().unwrap());
        let body_start = pos + HEADER_LEN;
        if body_start + len > bytes.len() {
            // Payload doesn't fully fit: torn write.
            out.repair_offset = Some(pos as u64);
            break;
        }
        let payload = &bytes[body_start..body_start + len];
        let next = body_start + len;

        if crc32(payload) != crc {
            // Full record present but corrupt: quarantine and resync.
            out.quarantined += 1;
            pos = next;
            continue;
        }

        match kind {
            KIND_DATA if payload.len() >= 4 => {
                let series = u32::from_be_bytes(payload[..4].try_into().unwrap());
                out.chunks.push(Chunk {
                    series,
                    block: payload[4..].to_vec(),
                });
            }
            KIND_COMMIT => {
                out.durable_chunks = out.chunks.len();
                out.has_commit = true;
            }
            _ => out.quarantined += 1,
        }
        pos = next;
    }

    out
}

/// Locate the payload byte range (Gorilla bits, past the series prefix) of the
/// middle data record, for deterministic bit-rot fault injection. Returns
/// `None` if there is no eligible data record.
#[must_use]
pub fn middle_data_payload_range(bytes: &[u8]) -> Option<std::ops::Range<usize>> {
    let mut ranges = Vec::new();
    let mut pos = 0usize;
    while pos + HEADER_LEN <= bytes.len() {
        let kind = bytes[pos];
        let len = u32::from_be_bytes(bytes[pos + 1..pos + 5].try_into().unwrap()) as usize;
        let body_start = pos + HEADER_LEN;
        if body_start + len > bytes.len() {
            break;
        }
        // Skip the 4-byte series prefix so the flip lands in Gorilla bits.
        if kind == KIND_DATA && len > 4 {
            ranges.push(body_start + 4..body_start + len);
        }
        pos = body_start + len;
    }
    let mid = ranges.len() / 2;
    ranges.into_iter().nth(mid)
}

/// Decode all samples for `series` from a set of chunks, sorted by timestamp.
pub fn collect_series(
    chunks: &[Chunk],
    series: SeriesId,
    start: i64,
    end: i64,
) -> Result<Vec<crate::sample::Sample>> {
    let mut out = Vec::new();
    for c in chunks.iter().filter(|c| c.series == series) {
        for s in crate::gorilla::decode_block(&c.block)? {
            if s.ts >= start && s.ts < end {
                out.push(s);
            }
        }
    }
    out.sort_by_key(|s| s.ts);
    Ok(out)
}

/// Total decoded sample count across every chunk (any series).
pub fn total_samples(chunks: &[Chunk]) -> usize {
    chunks
        .iter()
        .map(|c| crate::gorilla::block_count(&c.block) as usize)
        .sum()
}

#[cfg(test)]
mod tests {
    use super::{
        collect_series, middle_data_payload_range, scan, total_samples, write_commit_record,
        write_data_record, Chunk,
    };
    use crate::gorilla::{decode_block, encode_block};
    use crate::sample::Sample;

    fn block(n: i64) -> Vec<u8> {
        let s: Vec<Sample> = (0..n).map(|i| Sample::new(1000 + i, i as f64)).collect();
        encode_block(&s)
    }

    #[test]
    fn scans_clean_stream() {
        let mut buf = Vec::new();
        write_data_record(&mut buf, 7, &block(10));
        write_data_record(&mut buf, 7, &block(5));
        write_commit_record(&mut buf);
        let r = scan(&buf);
        assert_eq!(r.chunks.len(), 2);
        assert_eq!(r.durable_chunks, 2);
        assert!(r.repair_offset.is_none());
        assert_eq!(r.quarantined, 0);
        assert_eq!(total_samples(&r.chunks), 15);
    }

    #[test]
    fn torn_tail_is_flagged_for_repair() {
        let mut buf = Vec::new();
        write_data_record(&mut buf, 1, &block(10));
        write_commit_record(&mut buf);
        let durable_len = buf.len();
        write_data_record(&mut buf, 1, &block(10));
        buf.truncate(buf.len() - 3); // shear the last record
        let r = scan(&buf);
        assert_eq!(r.chunks.len(), 1);
        assert_eq!(r.durable_chunks, 1);
        assert_eq!(r.repair_offset, Some(durable_len as u64));
    }

    #[test]
    fn crc_failure_is_quarantined_not_fatal() {
        let mut buf = Vec::new();
        write_data_record(&mut buf, 1, &block(10));
        write_data_record(&mut buf, 1, &block(10));
        // Corrupt a byte inside the first payload (past its 9-byte header + series).
        buf[15] ^= 0xFF;
        let r = scan(&buf);
        assert_eq!(r.quarantined, 1);
        assert_eq!(r.chunks.len(), 1); // second chunk still recovered
    }

    #[test]
    fn malformed_records_are_classified_without_losing_valid_data() {
        let mut buf = Vec::new();
        write_data_record(&mut buf, 3, &block(2));
        write_commit_record(&mut buf);
        let committed_len = buf.len();

        // A valid CRC with a too-short data payload is an invalid full record.
        super::write_record(&mut buf, super::KIND_DATA, &[1, 2, 3]);
        // An unknown record kind is also quarantined, then scanning continues.
        super::write_record(&mut buf, 99, &[4, 5, 6, 7]);
        write_data_record(&mut buf, 4, &block(1));
        buf.extend_from_slice(&[super::KIND_DATA, 0, 0]);

        let result = scan(&buf);
        assert_eq!(result.chunks.len(), 2);
        assert_eq!(result.durable_chunks, 1);
        assert!(result.has_commit);
        assert_eq!(result.quarantined, 2);
        assert_eq!(result.repair_offset, Some((buf.len() - 3) as u64));
        assert!(committed_len < result.repair_offset.unwrap() as usize);
    }

    #[test]
    fn middle_payload_range_selects_only_the_middle_nonempty_block() {
        let mut buf = Vec::new();
        write_commit_record(&mut buf);
        write_data_record(&mut buf, 1, &[]);
        let first_start = buf.len() + super::HEADER_LEN + 4;
        write_data_record(&mut buf, 2, &[10, 11]);
        let second_start = buf.len() + super::HEADER_LEN + 4;
        write_data_record(&mut buf, 3, &[20, 21, 22]);
        let third_start = buf.len() + super::HEADER_LEN + 4;
        write_data_record(&mut buf, 4, &[30]);

        assert_eq!(
            middle_data_payload_range(&buf),
            Some(second_start..second_start + 3)
        );
        assert!(first_start < second_start);
        assert!(second_start < third_start);
        assert_eq!(middle_data_payload_range(&[]), None);
        assert_eq!(
            middle_data_payload_range(&buf[..super::HEADER_LEN - 1]),
            None
        );
    }

    #[test]
    fn middle_payload_range_respects_record_and_payload_boundaries() {
        let mut complete = Vec::new();
        let complete_start = super::HEADER_LEN + 4;
        write_data_record(&mut complete, 7, &[1]);
        assert_eq!(
            middle_data_payload_range(&complete),
            Some(complete_start..complete_start + 1)
        );

        let mut truncated = complete.clone();
        truncated.pop();
        assert_eq!(middle_data_payload_range(&truncated), None);

        let mut prefix_only = Vec::new();
        write_data_record(&mut prefix_only, 7, &[]);
        assert_eq!(middle_data_payload_range(&prefix_only), None);

        let mut unknown = Vec::new();
        super::write_record(&mut unknown, 99, &[1, 2, 3, 4, 5]);
        assert_eq!(middle_data_payload_range(&unknown), None);

        let mut two_records = Vec::new();
        write_data_record(&mut two_records, 1, &[10]);
        let second_start = two_records.len() + super::HEADER_LEN + 4;
        write_data_record(&mut two_records, 2, &[20]);
        assert_eq!(
            middle_data_payload_range(&two_records),
            Some(second_start..second_start + 1)
        );
    }

    #[test]
    fn collect_series_filters_series_and_half_open_time_range() {
        let chunks = vec![
            Chunk {
                series: 7,
                block: encode_block(&[
                    Sample::new(12, 12.0),
                    Sample::new(10, 10.0),
                    Sample::new(11, 11.0),
                ]),
            },
            Chunk {
                series: 8,
                block: encode_block(&[Sample::new(11, 80.0)]),
            },
            Chunk {
                series: 7,
                block: encode_block(&[Sample::new(13, 13.0)]),
            },
        ];

        let got = collect_series(&chunks, 7, 10, 13).unwrap();
        assert_eq!(
            got,
            vec![
                Sample::new(10, 10.0),
                Sample::new(11, 11.0),
                Sample::new(12, 12.0),
            ]
        );
        assert_eq!(decode_block(&chunks[1].block).unwrap().len(), 1);
    }
}
