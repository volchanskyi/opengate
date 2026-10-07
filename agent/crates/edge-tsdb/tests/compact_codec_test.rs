use edge_tsdb::compact::{decode_compact, encode_compact, encode_compact_scaled};
use edge_tsdb::sample::Sample;

fn gauge(n: i64) -> Vec<Sample> {
    (0..n)
        .map(|i| Sample::new(1_000 + i, 40.0 + (i % 97) as f64 * 0.1))
        .collect()
}

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

#[test]
fn a_fixed_point_block_shorter_than_its_scale_header_is_refused() {
    // Hand-built block with a 3-byte value section, though the scale header alone needs 8.
    let mut block = Vec::new();
    block.extend_from_slice(&4u32.to_le_bytes());
    block.extend_from_slice(&1_000i64.to_le_bytes());
    block.extend_from_slice(&1i64.to_le_bytes());
    block.push(1);
    block.push(0);
    block.push(3);
    block.extend_from_slice(&[0x01, 0x02, 0x03]);
    block.push(1);
    block.push(4);

    assert!(
        decode_compact(&block).is_err(),
        "a truncated fixed-point block must be refused, not indexed into"
    );
}

#[test]
fn a_block_whose_anomaly_runs_stop_short_still_answers_for_every_sample() {
    let samples = gauge(100);
    let mut block = encode_compact(&samples, &vec![false; samples.len()], 1);

    // An all-quiet block ends in a single run covering every sample.
    let tail = block.split_off(block.len() - 2);
    assert_eq!(tail, vec![1u8, 100], "anomaly RLE tail is not as assumed");
    block.extend_from_slice(&[1u8, 60]);

    let (decoded, bits) = decode_compact(&block).expect("a short run list is still readable");
    assert_eq!(decoded.len(), samples.len(), "samples were dropped");
    assert_eq!(bits.len(), samples.len(), "not one bit per sample");
    assert!(bits.iter().all(|b| !b), "missing bits must read as quiet");
}

#[test]
fn the_float_path_reuses_its_bit_window_rather_than_restating_it() {
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
