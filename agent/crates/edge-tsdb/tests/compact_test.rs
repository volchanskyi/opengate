#![cfg(feature = "bakeoff")]

use edge_tsdb::compact::{decode_compact, encode_compact};
use edge_tsdb::corpus::{Corpus, CorpusConfig};
use edge_tsdb::gorilla::encode_block;
use edge_tsdb::redb_compact::RedbCompactStore;
use edge_tsdb::redb_store::RedbStore;
use edge_tsdb::sample::Sample;
use edge_tsdb::substrate::{Durability, Substrate};

fn corpus() -> Corpus {
    Corpus::generate(CorpusConfig {
        seed: 0xC0FFEE,
        series: 40,
        duration_secs: 3_600,
        ..CorpusConfig::default()
    })
}

#[test]
fn compact_round_trip_respects_precision_contract() {
    let gauge: Vec<Sample> = (0..500)
        .map(|i| Sample::new(1_000 + i, 12.34 + (i % 5) as f64 * 0.01))
        .collect();
    let anomaly = vec![false; gauge.len()];
    let (decoded, _bits) = decode_compact(&encode_compact(&gauge, &anomaly, 1)).unwrap();
    assert_eq!(decoded.len(), gauge.len());
    for (d, o) in decoded.iter().zip(&gauge) {
        assert_eq!(d.ts, o.ts, "timestamps are lossless");
        let rel = (d.value - o.value).abs() / o.value.abs().max(1.0);
        assert!(rel < 1e-5, "float32 error {rel:e} exceeds contract");
    }

    let counter: Vec<Sample> = (0..500)
        .map(|i| Sample::new(1_000 + i, (i * 1000) as f64))
        .collect();
    let (dc, _) =
        decode_compact(&encode_compact(&counter, &vec![false; counter.len()], 1)).unwrap();
    for (d, o) in dc.iter().zip(&counter) {
        assert_eq!(
            d.value.to_bits(),
            o.value.to_bits(),
            "integral series must be lossless"
        );
    }
}

#[test]
fn compact_anomaly_bits_round_trip() {
    let samples: Vec<Sample> = (0..600)
        .map(|i| Sample::new(2_000 + i, (i % 3) as f64))
        .collect();
    let mut anomaly = vec![false; samples.len()];
    anomaly[123] = true;
    anomaly[124] = true;
    anomaly[500] = true;
    let bytes = encode_compact(&samples, &anomaly, 1);
    let (_d, bits) = decode_compact(&bytes).unwrap();
    assert_eq!(bits, anomaly);
}

#[test]
fn compact_handles_timestamp_exceptions() {
    let mut samples: Vec<Sample> = (0..300)
        .map(|i| Sample::new(5_000 + i, (i % 7) as f64))
        .collect();
    samples[150] = Sample::new(4_990, 3.0);
    samples[151] = Sample::new(99_999, 4.0);
    let (decoded, _) =
        decode_compact(&encode_compact(&samples, &vec![false; samples.len()], 1)).unwrap();
    for (d, o) in decoded.iter().zip(&samples) {
        assert_eq!(d.ts, o.ts);
    }
}

#[test]
fn compact_encoding_is_materially_denser_than_f64_gorilla() {
    let c = corpus();
    let mut f64_bytes = 0usize;
    let mut compact_bytes = 0usize;
    let mut n = 0usize;
    for series in c.series() {
        f64_bytes += encode_block(series).len();
        compact_bytes += encode_compact(series, &vec![false; series.len()], 1).len();
        n += series.len();
    }
    let f64_bps = f64_bytes as f64 / n as f64;
    let compact_bps = compact_bytes as f64 / n as f64;
    assert!(
        compact_bps < f64_bps * 0.75,
        "compact ({compact_bps:.3}) not materially denser than f64 ({f64_bps:.3})"
    );
}

// Sized so redb's fixed file floor is amortised across the corpus.
fn steady_state_corpus() -> Corpus {
    Corpus::generate(CorpusConfig {
        seed: 0x5CA1E,
        series: 40,
        duration_secs: 6 * 3_600,
        ..CorpusConfig::default()
    })
}

#[test]
fn redb_big_block_compact_halves_footprint_at_steady_state() {
    let c = steady_state_corpus();

    let dir_bp = tempfile::tempdir().unwrap();
    let mut bp = RedbCompactStore::open(dir_bp.path()).unwrap();
    c.replay_into(&mut bp).unwrap();
    bp.commit(Durability::Full).unwrap();
    let bps_bp = bp.size_on_disk().unwrap() as f64 / c.sample_count() as f64;

    let dir_b = tempfile::tempdir().unwrap();
    let mut b = RedbStore::open(dir_b.path()).unwrap();
    c.replay_into(&mut b).unwrap();
    b.commit(Durability::Full).unwrap();
    let bps_b = b.size_on_disk().unwrap() as f64 / c.sample_count() as f64;

    assert!(
        bps_bp < 3.0,
        "redb big-block compact regressed: {bps_bp:.3}"
    );
    assert!(
        bps_bp < bps_b * 0.65,
        "big-block compact did not halve redb footprint: B+={bps_bp:.3} B={bps_b:.3}"
    );

    let got = bp.range(0, i64::MIN, i64::MAX).unwrap();
    let want = &c.series()[0];
    assert_eq!(got.len(), want.len());
    for (d, o) in got.iter().zip(want) {
        let rel = (d.value - o.value).abs() / o.value.abs().max(1.0);
        assert!(rel < 1e-5, "readback float32 error {rel:e}");
    }
}
