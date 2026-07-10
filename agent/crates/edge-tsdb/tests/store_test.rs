//! Production multi-tier local store (`LocalTsdb`) acceptance tests.
//!
//! WS-14b graduates the WS-14a spike into the agent-local sovereign store: the
//! only home for min/max/last + 1 s raw (central VM keeps `avg` only). These
//! always-run tests are the regression floor for the production store —
//! persistence, per-metric fixed-point precision, atomic multi-tier commit,
//! disk-cap eviction, the durable backfill cursor, inline anomaly bits, MVCC
//! snapshot reads, and format migration. They build with `--no-default-features`
//! (production surface only), independent of the bake-off substrates.

use edge_tsdb::corpus::{Corpus, CorpusConfig};
use edge_tsdb::store::{LocalTsdb, Tier};
use edge_tsdb::{Durability, Sample, TsdbConfig};

fn tiny_corpus() -> Corpus {
    Corpus::generate(CorpusConfig {
        seed: 0x14B_0000,
        series: 6,
        duration_secs: 3_600,
        ..CorpusConfig::default()
    })
}

/// The store round-trips a committed series and survives reopen — the offline
/// promise. Values come back within the float32 precision contract by default.
#[test]
fn commits_persist_and_reopen() {
    let dir = tempfile::tempdir().unwrap();
    let corpus = tiny_corpus();
    let series0 = &corpus.series()[0];
    {
        let mut db = LocalTsdb::open(dir.path(), TsdbConfig::default()).unwrap();
        for s in series0 {
            db.append(0, *s, false).unwrap();
        }
        db.commit(Durability::Full).unwrap();
    }
    let db = LocalTsdb::open(dir.path(), TsdbConfig::default()).unwrap();
    let got = db.range_raw(0, i64::MIN, i64::MAX).unwrap();
    assert_eq!(got.len(), series0.len());
    for ((sample, _anom), want) in got.iter().zip(series0) {
        assert_eq!(sample.ts, want.ts);
        let rel = (sample.value - want.value).abs() / want.value.abs().max(1.0);
        assert!(rel < 1e-5, "readback float32 error {rel:e}");
    }
}

/// Per-metric fixed-point `scale` is lossless to 1/scale precision. A gauge
/// stored at ×100 recovers its centi-precision value exactly (not merely within
/// f32), which float32 cannot guarantee.
#[test]
fn fixed_point_scale_is_lossless_to_precision() {
    let dir = tempfile::tempdir().unwrap();
    let mut db = LocalTsdb::open(dir.path(), TsdbConfig::default()).unwrap();
    db.set_scale(0, 100); // centi-precision percentages
    let samples: Vec<Sample> = (0..300)
        .map(|i| Sample::new(1_000 + i, 12.34 + (i % 7) as f64 * 0.01))
        .collect();
    for s in &samples {
        db.append(0, *s, false).unwrap();
    }
    db.commit(Durability::Full).unwrap();
    let got = db.range_raw(0, i64::MIN, i64::MAX).unwrap();
    assert_eq!(got.len(), samples.len());
    for ((s, _), want) in got.iter().zip(&samples) {
        // Lossless to 1/scale = 0.01: rounding to centi must be bit-exact.
        let recovered = (s.value * 100.0).round() / 100.0;
        let expected = (want.value * 100.0).round() / 100.0;
        assert!(
            (recovered - expected).abs() < 1e-9,
            "fixed-point not lossless to centi: got {} want {}",
            s.value,
            want.value
        );
    }
}

/// T0/T1/T2 are written in one atomic transaction and the rollups are keyed by
/// sample timestamp, so a reopened store serves true min/max/last/avg that agree
/// with a direct fold of the raw series.
#[test]
fn multi_tier_rollups_are_atomic_and_sample_ts_keyed() {
    let dir = tempfile::tempdir().unwrap();
    let mut samples = Vec::new();
    // Minute 0: 0..60 (min 0, max 59, last 59, avg 29.5).
    for i in 0..60 {
        samples.push(Sample::new(i, i as f64));
    }
    // Minute 1: a single spike at a later ts.
    samples.push(Sample::new(60, 1000.0));
    {
        let mut db = LocalTsdb::open(dir.path(), TsdbConfig::default()).unwrap();
        for s in &samples {
            db.append(7, *s, false).unwrap();
        }
        db.commit(Durability::Full).unwrap();
    }
    let db = LocalTsdb::open(dir.path(), TsdbConfig::default()).unwrap();
    let t1 = db.range_tier(7, Tier::T1, i64::MIN, i64::MAX).unwrap();
    assert_eq!(t1.len(), 2, "two minute buckets");
    assert_eq!(t1[0].bucket, 0);
    assert_eq!(t1[0].min, 0.0);
    assert_eq!(t1[0].max, 59.0);
    assert_eq!(t1[0].last, 59.0);
    assert_eq!(t1[0].count, 60);
    assert!((t1[0].avg - 29.5).abs() < 1e-4);
    assert_eq!(t1[1].bucket, 60);
    assert_eq!(t1[1].max, 1000.0);
    // T2 (hour) folds both minutes into one bucket.
    let t2 = db.range_tier(7, Tier::T2, i64::MIN, i64::MAX).unwrap();
    assert_eq!(t2.len(), 1);
    assert_eq!(t2[0].min, 0.0);
    assert_eq!(t2[0].max, 1000.0);
    assert_eq!(t2[0].count, 61);
}

/// Rollups merge correctly across commit boundaries: samples for one minute
/// bucket split over two commits must fold into a single, correct T1 point.
#[test]
fn rollups_merge_across_commit_boundaries() {
    let dir = tempfile::tempdir().unwrap();
    let mut db = LocalTsdb::open(dir.path(), TsdbConfig::default()).unwrap();
    for i in 0..30 {
        db.append(1, Sample::new(i, i as f64), false).unwrap();
    }
    db.commit(Durability::Full).unwrap();
    for i in 30..60 {
        db.append(1, Sample::new(i, i as f64), false).unwrap();
    }
    db.commit(Durability::Full).unwrap();
    let t1 = db.range_tier(1, Tier::T1, i64::MIN, i64::MAX).unwrap();
    assert_eq!(t1.len(), 1);
    assert_eq!(t1[0].min, 0.0);
    assert_eq!(t1[0].max, 59.0);
    assert_eq!(t1[0].last, 59.0);
    assert_eq!(t1[0].count, 60);
    assert!((t1[0].avg - 29.5).abs() < 1e-4);
}

/// A long single-series 1 Hz gauge stream, `hours` long, starting at `start`.
fn long_series(start: i64, hours: i64) -> Vec<Sample> {
    (0..hours * 3_600)
        .map(|i| Sample::new(start + i, 40.0 + (i % 97) as f64 * 0.1))
        .collect()
}

/// The disk cap is a hard bound: the store evicts oldest-first (coarsest tier on
/// a timestamp tie) and never exceeds its cap, while always retaining the newest
/// raw block so live sampling is never evicted.
#[test]
fn disk_cap_evicts_oldest_and_never_exceeds_cap() {
    let start = 1_700_000_000;
    let data = long_series(start, 30);
    let cap = 60 * 1024;
    let dir = tempfile::tempdir().unwrap();
    let mut db = LocalTsdb::open(
        dir.path(),
        TsdbConfig {
            cap_bytes: cap,
            host_free_fraction: 0.0,
            default_scale: Some(10),
        },
    )
    .unwrap();
    // Commit every 3 hours so eviction runs incrementally, not just once.
    for (i, s) in data.iter().enumerate() {
        db.append(0, *s, false).unwrap();
        if (i + 1) % (3 * 3_600) == 0 {
            db.commit(Durability::Full).unwrap();
        }
    }
    db.commit(Durability::Full).unwrap();

    assert!(
        db.logical_bytes() <= cap,
        "cap breached: {} > {cap}",
        db.logical_bytes()
    );
    // Oldest hour was evicted; the newest hour survives.
    assert!(
        db.range_raw(0, start, start + 3_600).unwrap().is_empty(),
        "oldest window should have been evicted"
    );
    let newest = db
        .range_raw(0, start + 29 * 3_600, start + 30 * 3_600)
        .unwrap();
    assert!(!newest.is_empty(), "newest window must be retained");
}

/// Host-disk pressure tightens the effective cap below the configured cap, so a
/// nearly-full host disk shrinks the store further — it never fills the host.
#[test]
fn host_disk_pressure_tightens_the_cap() {
    let start = 1_700_000_000;
    let data = long_series(start, 20);
    let dir = tempfile::tempdir().unwrap();
    let mut db = LocalTsdb::open(
        dir.path(),
        TsdbConfig {
            cap_bytes: 10 * 1024 * 1024, // generous configured cap
            host_free_fraction: 0.05,
            default_scale: Some(10),
        },
    )
    .unwrap();
    // Only ~1 MiB of host disk free → effective cap = 5 % = ~50 KiB.
    db.set_host_free_bytes(Some(1024 * 1024));
    for s in &data {
        db.append(0, *s, false).unwrap();
    }
    db.commit(Durability::Full).unwrap();
    assert!(
        db.logical_bytes() <= 50 * 1024,
        "host-pressure cap not honored: {}",
        db.logical_bytes()
    );
}

/// The durable backfill cursor (WS-15's resume point) survives a restart.
#[test]
fn backfill_cursor_is_durable_across_restart() {
    let dir = tempfile::tempdir().unwrap();
    {
        let mut db = LocalTsdb::open(dir.path(), TsdbConfig::default()).unwrap();
        assert_eq!(db.cursor(0).unwrap(), None, "no cursor before first set");
        db.set_cursor(0, 1_700_000_500, Durability::Full).unwrap();
        db.set_cursor(9, 1_700_000_999, Durability::Full).unwrap();
    }
    let db = LocalTsdb::open(dir.path(), TsdbConfig::default()).unwrap();
    assert_eq!(db.cursor(0).unwrap(), Some(1_700_000_500));
    assert_eq!(db.cursor(9).unwrap(), Some(1_700_000_999));
    assert_eq!(db.cursor(3).unwrap(), None, "unset series has no cursor");
}

/// Inline anomaly bits persist with their samples and read back exactly.
#[test]
fn anomaly_bits_persist_and_read_back() {
    let dir = tempfile::tempdir().unwrap();
    let samples: Vec<Sample> = (0..600)
        .map(|i| Sample::new(1_000 + i, (i % 5) as f64))
        .collect();
    let anomaly: Vec<bool> = (0..600).map(|i| i % 97 == 0).collect();
    {
        let mut db = LocalTsdb::open(dir.path(), TsdbConfig::default()).unwrap();
        for (s, a) in samples.iter().zip(&anomaly) {
            db.append(0, *s, *a).unwrap();
        }
        db.commit(Durability::Full).unwrap();
    }
    let db = LocalTsdb::open(dir.path(), TsdbConfig::default()).unwrap();
    let got = db.range_raw(0, i64::MIN, i64::MAX).unwrap();
    assert_eq!(got.len(), samples.len());
    for ((s, a), (ws, wa)) in got.iter().zip(samples.iter().zip(&anomaly)) {
        assert_eq!(s.ts, ws.ts);
        assert_eq!(*a, *wa, "anomaly bit mismatch at ts {}", ws.ts);
    }
}

/// The detection/backfill read path takes an MVCC snapshot: reads on the
/// snapshot are a stable view unaffected by the sampler's concurrent writes.
#[test]
fn mvcc_snapshot_is_stable_while_the_sampler_writes() {
    let dir = tempfile::tempdir().unwrap();
    let mut db = LocalTsdb::open(dir.path(), TsdbConfig::default()).unwrap();
    for i in 0..100 {
        db.append(0, Sample::new(1_000 + i, i as f64), false)
            .unwrap();
    }
    db.commit(Durability::Full).unwrap();

    // Detection opens a snapshot of the past context...
    let snap = db.snapshot().unwrap();
    let before = snap.range_raw(0, i64::MIN, i64::MAX).unwrap().len();
    assert_eq!(before, 100);

    // ...while the sampler keeps writing and committing.
    for i in 100..250 {
        db.append(0, Sample::new(1_000 + i, i as f64), false)
            .unwrap();
    }
    db.commit(Durability::Full).unwrap();

    // The snapshot is unchanged; a fresh read sees the new samples.
    assert_eq!(snap.range_raw(0, i64::MIN, i64::MAX).unwrap().len(), 100);
    assert_eq!(db.range_raw(0, i64::MIN, i64::MAX).unwrap().len(), 250);
}

/// Crash safety is inherited from redb's two-phase commit: a durable (`Full`)
/// commit and a buffered (`None`) commit both survive a process crash (reopen),
/// so the bounded-loss boundary is only power loss, never a process kill.
#[test]
fn durable_and_buffered_commits_survive_reopen() {
    let dir = tempfile::tempdir().unwrap();
    {
        let mut db = LocalTsdb::open(dir.path(), TsdbConfig::default()).unwrap();
        for i in 0..200 {
            db.append(0, Sample::new(1_000 + i, i as f64), false)
                .unwrap();
        }
        db.commit(Durability::Full).unwrap();
        for i in 200..400 {
            db.append(0, Sample::new(1_000 + i, i as f64), false)
                .unwrap();
        }
        db.commit(Durability::None).unwrap();
        // Simulate a process crash: drop the handle with no further clean-up.
    }
    let db = LocalTsdb::open(dir.path(), TsdbConfig::default()).unwrap();
    assert_eq!(db.range_raw(0, i64::MIN, i64::MAX).unwrap().len(), 400);
}

/// A badly corrupted store file must fail to open *gracefully* — a `TsdbError`,
/// never a panic — so the agent's open path can recreate the cache and keep the
/// device online.
#[test]
fn corrupt_store_file_opens_gracefully_without_panic() {
    let dir = tempfile::tempdir().unwrap();
    {
        let mut db = LocalTsdb::open(dir.path(), TsdbConfig::default()).unwrap();
        db.append(0, Sample::new(1, 1.0), false).unwrap();
        db.commit(Durability::Full).unwrap();
    }
    // Overwrite the redb file with garbage.
    std::fs::write(dir.path().join("localtsdb.redb"), vec![0xAB; 8192]).unwrap();
    let result = LocalTsdb::open(dir.path(), TsdbConfig::default());
    assert!(
        result.is_err(),
        "corrupt file must be a graceful error, not a panic"
    );
}

/// Deprovision purges the entire store (WS-20): every tier and cursor is gone.
#[test]
fn purge_clears_the_entire_store() {
    let dir = tempfile::tempdir().unwrap();
    let mut db = LocalTsdb::open(dir.path(), TsdbConfig::default()).unwrap();
    for i in 0..500 {
        db.append(0, Sample::new(1_000 + i, i as f64), i % 50 == 0)
            .unwrap();
    }
    db.set_cursor(0, 1_400, Durability::Full).unwrap();
    db.commit(Durability::Full).unwrap();
    assert!(db.logical_bytes() > 0);

    db.purge().unwrap();
    assert_eq!(db.logical_bytes(), 0);
    assert!(db.range_raw(0, i64::MIN, i64::MAX).unwrap().is_empty());
    assert!(db
        .range_tier(0, Tier::T1, i64::MIN, i64::MAX)
        .unwrap()
        .is_empty());
    assert_eq!(db.cursor(0).unwrap(), None);
}
