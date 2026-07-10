//! WS-14a Path-1 measurement: does a Netdata/tsink-grade compact block codec
//! (float32 + implicit fixed-step timestamps + adaptive per-block codec + inline
//! anomaly bit), packed into big blocks inside redb, close redb's write-amp gap
//! and approach the sub-1 B/sample class — and at what precision cost?
//!
//! These always-run tests pin the answer as regression guards; the full numeric
//! table is reproduced by `cargo bench -p edge-tsdb`.

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

/// A fractional (gauge) series is float32-lossy but must stay within f32
/// precision; an integral (counter) series must round-trip bit-exact via the
/// lossless integer-DoD codec the adaptive selector picks for it.
#[test]
fn compact_round_trip_respects_precision_contract() {
    // Fractional gauge at a fixed 1 s cadence.
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

    // Monotonic integral counter must be bit-exact.
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

/// The inline anomaly bit round-trips and, when sparse, costs almost nothing.
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

/// Out-of-cadence timestamps (NTP steps) are stored as sparse exceptions and
/// reconstructed exactly.
#[test]
fn compact_handles_timestamp_exceptions() {
    let mut samples: Vec<Sample> = (0..300)
        .map(|i| Sample::new(5_000 + i, (i % 7) as f64))
        .collect();
    samples[150] = Sample::new(4_990, 3.0); // step back
    samples[151] = Sample::new(99_999, 4.0); // jump forward
    let (decoded, _) =
        decode_compact(&encode_compact(&samples, &vec![false; samples.len()], 1)).unwrap();
    for (d, o) in decoded.iter().zip(&samples) {
        assert_eq!(d.ts, o.ts);
    }
}

/// Density gate: on the shared corpus the compact codec must be materially
/// denser than lossless f64 Gorilla — the Path-1 thesis. Measured baseline is
/// roughly a 2× reduction; asserted with headroom.
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

/// A steady-state corpus (6 h × 40 series), sized so redb's ~1 MB fixed file
/// floor is amortised and bytes/sample reflects real per-sample cost rather
/// than the minimum allocation.
fn steady_state_corpus() -> Corpus {
    Corpus::generate(CorpusConfig {
        seed: 0x5CA1E,
        series: 40,
        duration_secs: 6 * 3_600,
        ..CorpusConfig::default()
    })
}

/// In-substrate gate: at steady state, big-block compact packing must roughly
/// **halve** redb's persisted bytes/sample versus the small-block f64 store —
/// the measured effect of the two Path-1 levers combined. (It does not erase
/// redb's residual ~1.9× page overhead over the raw encoding — reaching the
/// ~1 B class needs an append-structured store, which is the sharpened
/// off-ramp, not this gate.)
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

    // Big-block compact lands in the low single digits (~2.4) and is decisively
    // denser than the small-block f64 store (~4.9) — the gap closes.
    assert!(
        bps_bp < 3.0,
        "redb big-block compact regressed: {bps_bp:.3}"
    );
    assert!(
        bps_bp < bps_b * 0.65,
        "big-block compact did not halve redb footprint: B+={bps_bp:.3} B={bps_b:.3}"
    );

    // And it must still read back within the float32 precision contract.
    let got = bp.range(0, i64::MIN, i64::MAX).unwrap();
    let want = &c.series()[0];
    assert_eq!(got.len(), want.len());
    for (d, o) in got.iter().zip(want) {
        let rel = (d.value - o.value).abs() / o.value.abs().max(1.0);
        assert!(rel < 1e-5, "readback float32 error {rel:e}");
    }
}
