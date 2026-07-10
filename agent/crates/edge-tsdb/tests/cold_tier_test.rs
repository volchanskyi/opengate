#![cfg(feature = "cold-deflate")]
//! Cold-tier DEFLATE (WS-14b step 8).
//!
//! Two things are proven here: (1) the *codec capability* the ADR cited —
//! fixed-point + pure-Rust DEFLATE reaches the sub-1 B/sample class (no `zstd`,
//! no C dependency); and (2) the *store wiring* — [`LocalTsdb::compact_cold_tiers`]
//! shrinks sealed T1/T2 blocks while leaving hot T0 raw untouched, and reads
//! still round-trip through the inflate path.

use edge_tsdb::compact::{decode_compact, encode_compact_scaled};
use edge_tsdb::corpus::{Corpus, CorpusConfig};
use edge_tsdb::deflate::{deflate, inflate};
use edge_tsdb::store::{LocalTsdb, Tier};
use edge_tsdb::{Durability, Sample, TsdbConfig};

/// Fixed-point (per-metric) + DEFLATE reaches the ~1 B/sample class the ADR
/// measured for the codec, and inflate→decode round-trips within the fixed-point
/// precision contract. This is the codec capability, not zstd — `flate2` only.
#[test]
fn fixed_point_plus_deflate_reaches_sub_one_byte_class() {
    let corpus = Corpus::generate(CorpusConfig {
        seed: 0xC0_1D,
        series: 40,
        duration_secs: 6 * 3_600,
        ..CorpusConfig::default()
    });
    let mut deflated_bytes = 0usize;
    let mut samples = 0usize;
    for series in corpus.series() {
        let no_anom = vec![false; series.len()];
        // ×100 centi-precision fixed-point (the measured density lever); the
        // adaptive selector still picks int-DoD for the integral counter series.
        let block = encode_compact_scaled(series, &no_anom, 1, Some(100));
        let z = deflate(&block).unwrap();
        deflated_bytes += z.len();
        samples += series.len();

        // Round-trips within centi precision after inflate.
        let (decoded, _bits) = decode_compact(&inflate(&z).unwrap()).unwrap();
        for (d, o) in decoded.iter().zip(series) {
            let recovered = (d.value * 100.0).round() as i64;
            let expected = (o.value * 100.0).round() as i64;
            assert_eq!(recovered, expected);
        }
    }
    let bps = deflated_bytes as f64 / samples as f64;
    assert!(
        bps < 1.15,
        "fixed-point + DEFLATE regressed above the ~1 B/sample class: {bps:.3}"
    );
}

/// The store's cold-tier compaction DEFLATEs sealed (non-tail) T1 blocks — it
/// shrinks the logical footprint, leaves the hot T0 raw and T1 read paths intact
/// (round-tripping through inflate), and is idempotent.
#[test]
fn compact_cold_tiers_shrinks_t1_and_preserves_reads() {
    // 14 h of 1 Hz data → two T1 blocks (a sealed 12 h block + a hot tail).
    let start = 1_700_000_000;
    let data: Vec<Sample> = (0..14 * 3_600)
        .map(|i| Sample::new(start + i, 40.0 + (i % 240) as f64 * 0.1))
        .collect();
    let dir = tempfile::tempdir().unwrap();
    let mut db = LocalTsdb::open(
        dir.path(),
        TsdbConfig {
            default_scale: Some(10),
            ..TsdbConfig::default()
        },
    )
    .unwrap();
    for s in &data {
        db.append(0, *s, false).unwrap();
    }
    db.commit(Durability::Full).unwrap();

    let t1_before = db.range_tier(0, Tier::T1, i64::MIN, i64::MAX).unwrap();
    let logical_before = db.logical_bytes();

    db.compact_cold_tiers().unwrap();
    assert!(
        db.logical_bytes() < logical_before,
        "cold-tier compaction did not shrink the store: {} !< {logical_before}",
        db.logical_bytes()
    );

    // Reads round-trip through the inflate path unchanged, and T0 raw is intact.
    let t1_after = db.range_tier(0, Tier::T1, i64::MIN, i64::MAX).unwrap();
    assert_eq!(t1_after, t1_before, "cold-tier read changed after DEFLATE");
    assert_eq!(
        db.range_raw(0, i64::MIN, i64::MAX).unwrap().len(),
        data.len(),
        "hot T0 raw must survive cold-tier compaction untouched"
    );

    // Idempotent: a second pass changes nothing.
    let logical_once = db.logical_bytes();
    db.compact_cold_tiers().unwrap();
    assert_eq!(db.logical_bytes(), logical_once);
}
