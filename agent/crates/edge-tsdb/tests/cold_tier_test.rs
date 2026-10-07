#![cfg(feature = "cold-deflate")]

use edge_tsdb::compact::{decode_compact, encode_compact_scaled};
use edge_tsdb::corpus::{Corpus, CorpusConfig};
use edge_tsdb::deflate::{deflate, inflate};
use edge_tsdb::store::{LocalTsdb, Tier};
use edge_tsdb::{Durability, Sample, TsdbConfig};

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
        let block = encode_compact_scaled(series, &no_anom, 1, Some(100));
        let z = deflate(&block).unwrap();
        deflated_bytes += z.len();
        samples += series.len();

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

#[test]
fn compact_cold_tiers_shrinks_t1_and_preserves_reads() {
    // 14 h at 1 Hz yields two T1 blocks: one sealed 12 h block and a tail.
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

    let t1_after = db.range_tier(0, Tier::T1, i64::MIN, i64::MAX).unwrap();
    assert_eq!(t1_after, t1_before, "cold-tier read changed after DEFLATE");
    assert_eq!(
        db.range_raw(0, i64::MIN, i64::MAX).unwrap().len(),
        data.len(),
        "hot T0 raw must survive cold-tier compaction untouched"
    );

    let logical_once = db.logical_bytes();
    db.compact_cold_tiers().unwrap();
    assert_eq!(db.logical_bytes(), logical_once);
}
