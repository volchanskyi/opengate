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

#[test]
fn fixed_point_scale_is_lossless_to_precision() {
    let dir = tempfile::tempdir().unwrap();
    let mut db = LocalTsdb::open(dir.path(), TsdbConfig::default()).unwrap();
    db.set_scale(0, 100);
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

#[test]
fn multi_tier_rollups_are_atomic_and_sample_ts_keyed() {
    let dir = tempfile::tempdir().unwrap();
    let mut samples = Vec::new();
    for i in 0..60 {
        samples.push(Sample::new(i, i as f64));
    }
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
    let t2 = db.range_tier(7, Tier::T2, i64::MIN, i64::MAX).unwrap();
    assert_eq!(t2.len(), 1);
    assert_eq!(t2[0].min, 0.0);
    assert_eq!(t2[0].max, 1000.0);
    assert_eq!(t2[0].count, 61);
}

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

/// A single-series 1 Hz gauge stream of `hours` hours from `start`.
fn long_series(start: i64, hours: i64) -> Vec<Sample> {
    (0..hours * 3_600)
        .map(|i| Sample::new(start + i, 40.0 + (i % 97) as f64 * 0.1))
        .collect()
}

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
    assert!(
        db.range_raw(0, start, start + 3_600).unwrap().is_empty(),
        "oldest window should have been evicted"
    );
    let newest = db
        .range_raw(0, start + 29 * 3_600, start + 30 * 3_600)
        .unwrap();
    assert!(!newest.is_empty(), "newest window must be retained");
}

#[test]
fn host_disk_pressure_tightens_the_cap() {
    let start = 1_700_000_000;
    let data = long_series(start, 20);
    let dir = tempfile::tempdir().unwrap();
    let mut db = LocalTsdb::open(
        dir.path(),
        TsdbConfig {
            cap_bytes: 10 * 1024 * 1024,
            host_free_fraction: 0.05,
            default_scale: Some(10),
        },
    )
    .unwrap();
    // About 1 MiB of host disk free makes the effective cap 5 %, roughly 50 KiB.
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

#[test]
fn mvcc_snapshot_is_stable_while_the_sampler_writes() {
    let dir = tempfile::tempdir().unwrap();
    let mut db = LocalTsdb::open(dir.path(), TsdbConfig::default()).unwrap();
    for i in 0..100 {
        db.append(0, Sample::new(1_000 + i, i as f64), false)
            .unwrap();
    }
    db.commit(Durability::Full).unwrap();

    let snap = db.snapshot().unwrap();
    let before = snap.range_raw(0, i64::MIN, i64::MAX).unwrap().len();
    assert_eq!(before, 100);

    for i in 100..250 {
        db.append(0, Sample::new(1_000 + i, i as f64), false)
            .unwrap();
    }
    db.commit(Durability::Full).unwrap();

    assert_eq!(snap.range_raw(0, i64::MIN, i64::MAX).unwrap().len(), 100);
    assert_eq!(db.range_raw(0, i64::MIN, i64::MAX).unwrap().len(), 250);
}

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
    }
    let db = LocalTsdb::open(dir.path(), TsdbConfig::default()).unwrap();
    assert_eq!(db.range_raw(0, i64::MIN, i64::MAX).unwrap().len(), 400);
}

#[test]
fn corrupt_store_file_opens_gracefully_without_panic() {
    let dir = tempfile::tempdir().unwrap();
    {
        let mut db = LocalTsdb::open(dir.path(), TsdbConfig::default()).unwrap();
        db.append(0, Sample::new(1, 1.0), false).unwrap();
        db.commit(Durability::Full).unwrap();
    }
    std::fs::write(dir.path().join("localtsdb.redb"), vec![0xAB; 8192]).unwrap();
    let result = LocalTsdb::open(dir.path(), TsdbConfig::default());
    assert!(
        result.is_err(),
        "corrupt file must be a graceful error, not a panic"
    );
}

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

#[test]
fn committing_every_sample_still_packs_one_block() {
    let dir = tempfile::tempdir().unwrap();
    let mut db = LocalTsdb::open(
        dir.path(),
        TsdbConfig {
            default_scale: Some(10),
            ..TsdbConfig::default()
        },
    )
    .unwrap();
    for i in 0..600 {
        db.append(
            0,
            Sample::new(1_000 + i, 40.0 + (i % 97) as f64 * 0.1),
            false,
        )
        .unwrap();
        db.commit(Durability::None).unwrap();
    }

    assert_eq!(db.range_raw(0, i64::MIN, i64::MAX).unwrap().len(), 600);
    let per_sample = db.logical_bytes() as f64 / 600.0;
    assert!(
        per_sample < 4.0,
        "a block per commit: {per_sample:.3} B/sample"
    );
}

#[test]
fn eviction_gives_up_no_more_history_than_the_cap_demands() {
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
    for (i, s) in data.iter().enumerate() {
        db.append(0, *s, false).unwrap();
        if (i + 1) % (3 * 3_600) == 0 {
            db.commit(Durability::Full).unwrap();
        }
    }
    db.commit(Durability::Full).unwrap();

    assert!(db.logical_bytes() <= cap);
    assert!(
        db.logical_bytes() > cap / 4 * 3,
        "evicted far past the cap, losing history it could have kept: {} of {cap}",
        db.logical_bytes()
    );
    // redb reuses the pages eviction frees, so the file tracks the allowance.
    let file = db.size_on_disk().unwrap();
    assert!(
        file > db.logical_bytes() && file < 20 * cap,
        "on-disk file untracked by the cap: {file} against {cap}"
    );
}

#[test]
fn the_block_being_written_survives_a_cap_that_cannot_hold_it() {
    let start = 1_700_000_000;
    let dir = tempfile::tempdir().unwrap();
    let mut db = LocalTsdb::open(
        dir.path(),
        TsdbConfig {
            cap_bytes: 1,
            host_free_fraction: 0.0,
            default_scale: Some(10),
        },
    )
    .unwrap();
    for s in long_series(start, 4) {
        db.append(0, s, false).unwrap();
    }
    db.commit(Durability::Full).unwrap();

    let kept = db.range_raw(0, i64::MIN, i64::MAX).unwrap();
    assert!(
        !kept.is_empty(),
        "live sampling was evicted along with the history"
    );
    let (newest, _) = kept.last().unwrap();
    assert_eq!(
        newest.ts,
        start + 4 * 3_600 - 1,
        "the retained block is not the one being written into"
    );
}

#[test]
fn a_reopened_store_remembers_how_much_it_holds() {
    let start = 1_700_000_000;
    let dir = tempfile::tempdir().unwrap();
    let held;
    {
        let mut db = LocalTsdb::open(dir.path(), TsdbConfig::default()).unwrap();
        for s in long_series(start, 2) {
            db.append(0, s, false).unwrap();
        }
        db.commit(Durability::Full).unwrap();
        held = db.logical_bytes();
        assert!(held > 0);
    }

    let reopened = LocalTsdb::open(dir.path(), TsdbConfig::default()).unwrap();
    assert_eq!(
        reopened.logical_bytes(),
        held,
        "the store forgot its footprint across a restart"
    );
}

#[test]
fn a_large_gauge_keeps_centi_precision_only_under_its_scale() {
    let dir = tempfile::tempdir().unwrap();
    let mut db = LocalTsdb::open(dir.path(), TsdbConfig::default()).unwrap();
    db.set_scale(0, 100);
    let samples: Vec<Sample> = (0..300)
        .map(|i| Sample::new(1_000 + i, 500_000.0 + (i % 240) as f64 * 0.01))
        .collect();
    for s in &samples {
        db.append(0, *s, false).unwrap();
    }
    db.commit(Durability::Full).unwrap();

    let got = db.range_raw(0, i64::MIN, i64::MAX).unwrap();
    assert_eq!(got.len(), samples.len());
    for ((read, _), want) in got.iter().zip(&samples) {
        assert_eq!(
            (read.value * 100.0).round() as i64,
            (want.value * 100.0).round() as i64,
            "centi-precision lost at {}",
            want.ts
        );
    }
}
