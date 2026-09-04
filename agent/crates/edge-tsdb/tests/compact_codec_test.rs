//! Compact-block codec behaviour that the store depends on but no store-level
//! test can see: what the encoder does with a policy it cannot honour, what the
//! decoder does with a block that is not whole, and how much the float path
//! costs per sample.
//!
//! These are the shipped codec's own contracts, so they carry no feature gate —
//! `compact_test.rs` sits behind `bakeoff` because it compares the codec against
//! the bake-off substrates, which is a different question.

use edge_tsdb::compact::{decode_compact, encode_compact, encode_compact_scaled};
use edge_tsdb::sample::Sample;

/// A fractional gauge at the 1 Hz cadence — the shape a host vital arrives in.
fn gauge(n: i64) -> Vec<Sample> {
    (0..n)
        .map(|i| Sample::new(1_000 + i, 40.0 + (i % 97) as f64 * 0.1))
        .collect()
}

/// A scale of zero is not a quantization policy — it is a policy that would map
/// every reading to zero and write a block whose own decoder refuses it. The
/// selector must ignore it and fall back to the adaptive path, so a bad scale on
/// one metric costs precision at worst and never costs the machine its history.
#[test]
fn a_zero_scale_is_ignored_rather_than_flattening_the_series() {
    let samples = gauge(400);
    let anomaly = vec![false; samples.len()];

    let bytes = encode_compact_scaled(&samples, &anomaly, 1, Some(0));
    let (decoded, _bits) = decode_compact(&bytes).expect("a zero scale must not poison the block");

    assert_eq!(decoded.len(), samples.len());
    for (d, o) in decoded.iter().zip(&samples) {
        assert_eq!(d.ts, o.ts);
        let rel = (d.value - o.value).abs() / o.value.abs().max(1.0);
        assert!(rel < 1e-5, "zero scale flattened the series: {rel:e}");
    }
}

/// A block whose fixed-point value section is shorter than its own scale header
/// is corrupt. The decoder reports it; it must not index past the end and take
/// the agent down with it — a truncated block on a customer's disk is a lost
/// window, never a crashed agent.
#[test]
fn a_fixed_point_block_shorter_than_its_scale_header_is_refused() {
    // Hand-built block: 4 samples, first_ts 1000, step 1, fixed-point codec, no
    // timestamp exceptions, a 3-byte value section (the scale alone needs 8),
    // then one anomaly run of 4.
    let mut block = Vec::new();
    block.extend_from_slice(&4u32.to_le_bytes());
    block.extend_from_slice(&1_000i64.to_le_bytes());
    block.extend_from_slice(&1i64.to_le_bytes());
    block.push(1); // CODEC_FIXED_DOD
    block.push(0); // no timestamp exceptions
    block.push(3); // value section length
    block.extend_from_slice(&[0x01, 0x02, 0x03]);
    block.push(1); // one anomaly run
    block.push(4); // …of length 4

    assert!(
        decode_compact(&block).is_err(),
        "a truncated fixed-point block must be refused, not indexed into"
    );
}

/// A block always hands back one anomaly bit per sample it declares. When its
/// run list stops short — a block that was not written whole — the remaining
/// samples read as un-flagged rather than disappearing, so a chart loses a
/// flag at worst and never loses the tail of the window.
#[test]
fn a_block_whose_anomaly_runs_stop_short_still_answers_for_every_sample() {
    let samples = gauge(100);
    let mut block = encode_compact(&samples, &vec![false; samples.len()], 1);

    // An all-quiet block ends in one run covering every sample: `[1, 100]`.
    let tail = block.split_off(block.len() - 2);
    assert_eq!(tail, vec![1u8, 100], "anomaly RLE tail is not as assumed");
    block.extend_from_slice(&[1u8, 60]); // …now covering only the first 60

    let (decoded, bits) = decode_compact(&block).expect("a short run list is still readable");
    assert_eq!(decoded.len(), samples.len(), "samples were dropped");
    assert_eq!(bits.len(), samples.len(), "not one bit per sample");
    assert!(bits.iter().all(|b| !b), "missing bits must read as quiet");
}

/// The float path spends a lead/significant-bit window only when the window
/// changes, and reuses it while it holds. Restating it on every sample is still
/// decodable and still passes every round-trip test — it costs 2.3× the bytes
/// (0.76 → 1.75 B/sample on this series), which the on-device disk cap pays for
/// in evicted history.
#[test]
fn the_float_path_reuses_its_bit_window_rather_than_restating_it() {
    // A vital that steps through a narrow band, the shape that makes the window
    // worth reusing: consecutive readings share their leading and trailing zeros.
    let samples: Vec<Sample> = (0..3_000)
        .map(|i| Sample::new(1_000 + i, 12.0 + (i % 11) as f64 * 0.25))
        .collect();
    let bytes = encode_compact(&samples, &vec![false; samples.len()], 1);
    let per_sample = bytes.len() as f64 / samples.len() as f64;

    assert!(
        per_sample < 1.2,
        "float path regressed to a window per sample: {per_sample:.3} B/sample"
    );
}
