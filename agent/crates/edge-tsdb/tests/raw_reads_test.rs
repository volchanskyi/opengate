//! Raw reads return exactly the readings in their span: across block edges and gaps, after a
//! clock set back, while the newest block grows, and after eviction.

use edge_tsdb::store::LocalTsdb;
use edge_tsdb::{Durability, Sample, TsdbConfig};

const T: i64 = 1_700_000_000;

fn store(dir: &std::path::Path) -> LocalTsdb {
    LocalTsdb::open(dir, TsdbConfig::default()).unwrap()
}

fn timestamps(db: &LocalTsdb, start: i64, end: i64) -> Vec<i64> {
    db.range_raw(0, start, end)
        .unwrap()
        .into_iter()
        .map(|(s, _)| s.ts)
        .collect()
}

/// What a read of `[start, end)` must return from the readings appended at `written`.
fn expected(written: &[i64], start: i64, end: i64) -> Vec<i64> {
    let mut want: Vec<i64> = written
        .iter()
        .copied()
        .filter(|ts| (start..end).contains(ts))
        .collect();
    want.sort_unstable();
    want
}

#[test]
fn a_raw_read_returns_exactly_its_span_across_blocks_and_gaps() {
    let dir = tempfile::tempdir().unwrap();
    let mut db = store(dir.path());
    let written: Vec<i64> = (T..T + 4_000)
        .chain(T + 9_000..T + 12_500)
        .chain(T + 20_000..T + 20_010)
        .collect();
    for (i, &ts) in written.iter().enumerate() {
        db.append(0, Sample::new(ts, 1.0), false).unwrap();
        if i % 700 == 0 {
            db.commit(Durability::None).unwrap();
        }
    }
    db.commit(Durability::Full).unwrap();

    let spans = [
        (T - 100, T),
        (T, T + 1),
        (T + 2_990, T + 3_010),
        (T + 3_999, T + 9_001),
        (T + 4_000, T + 9_000),
        (T + 12_000, T + 20_005),
        (T + 20_009, T + 30_000),
        (T + 50, T + 50),
        (i64::MIN, i64::MAX),
    ];
    for (start, end) in spans {
        assert_eq!(
            timestamps(&db, start, end),
            expected(&written, start, end),
            "read of [{start}, {end})"
        );
    }
}

#[test]
fn a_clock_set_back_still_reads_every_reading_in_its_span() {
    let dir = tempfile::tempdir().unwrap();
    let mut db = store(dir.path());
    let written: Vec<i64> = (T..T + 3_000)
        .chain(T + 5_000..T + 5_100)
        .chain(T + 2_000..T + 2_100)
        .chain(T + 5_100..T + 5_200)
        .collect();
    for &ts in &written {
        db.append(0, Sample::new(ts, 1.0), false).unwrap();
    }
    db.commit(Durability::Full).unwrap();

    for (start, end) in [
        (T + 2_050, T + 2_060),
        (T + 1_990, T + 2_010),
        (T + 5_050, T + 5_150),
        (T + 2_099, T + 2_101),
    ] {
        assert_eq!(
            timestamps(&db, start, end),
            expected(&written, start, end),
            "read of [{start}, {end}) after the clock went back"
        );
    }
}

#[test]
fn the_newest_block_is_read_once_as_it_grows_across_commits() {
    let dir = tempfile::tempdir().unwrap();
    let mut db = store(dir.path());
    let mut written = Vec::new();
    for round in 0..5 {
        for ts in T + round * 100..T + round * 100 + 100 {
            db.append(0, Sample::new(ts, 1.0), false).unwrap();
            written.push(ts);
        }
        db.commit(Durability::None).unwrap();
        assert_eq!(
            timestamps(&db, T, T + 1_000),
            expected(&written, T, T + 1_000),
            "after commit {round}"
        );
        let newest = T + round * 100 + 99;
        assert_eq!(timestamps(&db, newest, newest + 1), vec![newest]);
    }
}

#[test]
fn after_eviction_every_read_agrees_with_what_the_store_kept() {
    let dir = tempfile::tempdir().unwrap();
    let mut db = LocalTsdb::open(
        dir.path(),
        TsdbConfig {
            cap_bytes: 60 * 1024,
            host_free_fraction: 0.0,
            default_scale: Some(10),
        },
    )
    .unwrap();
    for i in 0..30 * 3_600 {
        db.append(0, Sample::new(T + i, 40.0 + (i % 97) as f64 * 0.1), false)
            .unwrap();
        if (i + 1) % (3 * 3_600) == 0 {
            db.commit(Durability::Full).unwrap();
        }
    }
    db.commit(Durability::Full).unwrap();

    let kept = timestamps(&db, i64::MIN, i64::MAX);
    assert!(!kept.is_empty() && kept.len() < 30 * 3_600, "eviction ran");
    assert!(kept.windows(2).all(|w| w[0] < w[1]), "no reading twice");
    for hour in 0..30 {
        let (start, end) = (T + hour * 3_600, T + (hour + 1) * 3_600);
        assert_eq!(
            timestamps(&db, start, end),
            expected(&kept, start, end),
            "hour {hour}"
        );
    }
}
