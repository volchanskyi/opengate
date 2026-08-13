//! What a reader can learn about the history the store holds, and what a range
//! read costs.
//!
//! A retroactive scan walks months of 60 s rollups in bounded chunks, so it asks
//! the store two questions the live paths never did: *how far back does this
//! series go*, and *give me only these buckets*. Both have to answer without
//! reading everything — a chunk that decodes the whole history to return an hour
//! of it makes a work budget meaningless, and a span that has to scan every block
//! costs more than the scan it is sizing.

use edge_tsdb::store::{LocalTsdb, Tier};
use edge_tsdb::{Durability, Sample, TsdbConfig};

/// One T1 bucket, in seconds.
const MINUTE: i64 = 60;
/// Buckets in one stored T1 block (12 h), so a test crossing this crosses a
/// block boundary.
const T1_BUCKETS_PER_BLOCK: i64 = 720;

/// Bucket-aligned start, so a reading's timestamp *is* its bucket and every
/// expectation below can be read off the minute index.
const START: i64 = 1_700_000_040;

/// A store holding `minutes` of 1-per-minute readings on series 0, valued by
/// their minute index so a bucket is identifiable from its value alone.
fn store_with_minutes(dir: &std::path::Path, minutes: i64) -> LocalTsdb {
    let mut db = LocalTsdb::open(dir, TsdbConfig::default()).unwrap();
    for i in 0..minutes {
        db.append(0, Sample::new(START + i * MINUTE, i as f64), false)
            .unwrap();
    }
    db.commit(Durability::Full).unwrap();
    db
}

/// The span is the oldest and newest bucket actually stored — the two ends a
/// scan needs to know where to start and when it is finished.
#[test]
fn tier_span_reports_the_oldest_and_newest_bucket() {
    let dir = tempfile::tempdir().unwrap();
    let db = store_with_minutes(dir.path(), 3);

    assert_eq!(
        db.tier_span(0, Tier::T1).unwrap(),
        Some((START, START + 2 * MINUTE))
    );
}

/// A device enrolled this morning has no history, and the span says so rather
/// than reporting a zero-width one at the epoch. "Nothing here" and "one bucket
/// at time zero" are different answers, and only one of them is honest.
#[test]
fn tier_span_is_absent_for_a_series_the_store_never_held() {
    let dir = tempfile::tempdir().unwrap();
    let db = store_with_minutes(dir.path(), 3);

    assert_eq!(db.tier_span(7, Tier::T1).unwrap(), None);

    let empty = tempfile::tempdir().unwrap();
    let fresh = LocalTsdb::open(empty.path(), TsdbConfig::default()).unwrap();
    assert_eq!(fresh.tier_span(0, Tier::T1).unwrap(), None);
}

/// The span survives the block boundary: with more than one block stored it is
/// still the first and last bucket, not the first and last of one block.
#[test]
fn tier_span_crosses_stored_block_boundaries() {
    let dir = tempfile::tempdir().unwrap();
    let minutes = T1_BUCKETS_PER_BLOCK + 30;
    let db = store_with_minutes(dir.path(), minutes);

    assert_eq!(
        db.tier_span(0, Tier::T1).unwrap(),
        Some((START, START + (minutes - 1) * MINUTE))
    );
}

/// A range read returns exactly the buckets asked for, including when the range
/// sits inside one stored block and when it straddles two.
#[test]
fn range_tier_returns_only_the_buckets_asked_for() {
    let dir = tempfile::tempdir().unwrap();
    let db = store_with_minutes(dir.path(), T1_BUCKETS_PER_BLOCK + 30);

    let inside = db
        .range_tier(0, Tier::T1, START + 10 * MINUTE, START + 13 * MINUTE)
        .unwrap();
    let values: Vec<f64> = inside.iter().map(|p| p.avg).collect();
    assert_eq!(values, vec![10.0, 11.0, 12.0], "end is exclusive");

    let boundary = T1_BUCKETS_PER_BLOCK;
    let straddling = db
        .range_tier(
            0,
            Tier::T1,
            START + (boundary - 2) * MINUTE,
            START + (boundary + 2) * MINUTE,
        )
        .unwrap();
    let values: Vec<f64> = straddling.iter().map(|p| p.avg).collect();
    assert_eq!(
        values,
        vec![
            (boundary - 2) as f64,
            (boundary - 1) as f64,
            boundary as f64,
            (boundary + 1) as f64
        ],
        "a range spanning two stored blocks reads both"
    );
}

/// A snapshot answers both questions as of the instant it was opened, so a scan
/// walking history is never confused by the sampler writing underneath it.
#[test]
fn a_snapshot_answers_span_and_range_as_of_its_instant() {
    let dir = tempfile::tempdir().unwrap();
    let mut db = store_with_minutes(dir.path(), 5);
    let snap = db.snapshot().unwrap();

    for i in 5..10 {
        db.append(0, Sample::new(START + i * MINUTE, i as f64), false)
            .unwrap();
    }
    db.commit(Durability::Full).unwrap();

    assert_eq!(
        snap.tier_span(0, Tier::T1).unwrap(),
        Some((START, START + 4 * MINUTE)),
        "the snapshot keeps the history it opened on"
    );
    assert_eq!(
        snap.range_tier(0, Tier::T1, i64::MIN, i64::MAX)
            .unwrap()
            .len(),
        5
    );
    assert_eq!(
        db.tier_span(0, Tier::T1).unwrap(),
        Some((START, START + 9 * MINUTE)),
        "the store itself has moved on"
    );
}

/// The store's footprint policy, stated as one function of free host space: the
/// configured cap until the host gets tight, then the fraction of what is left.
/// Everything that has to stand down *before* the store changes what it evicts
/// reads its threshold from here, so the two can never drift apart.
#[test]
fn the_effective_cap_backs_off_only_once_free_space_bites() {
    let config = TsdbConfig {
        cap_bytes: 512 * 1024 * 1024,
        host_free_fraction: 0.05,
        default_scale: None,
    };

    // Nothing known about the host disk, or plenty of it: the configured cap.
    assert_eq!(config.effective_cap(None), config.cap_bytes);
    assert_eq!(
        config.effective_cap(Some(1_000 * 1024 * 1024 * 1024)),
        config.cap_bytes
    );

    // The engagement point: free × fraction == cap. Below it the store starts
    // shrinking what it will hold.
    let engage = (config.cap_bytes as f64 / config.host_free_fraction) as u64;
    assert_eq!(config.effective_cap(Some(engage)), config.cap_bytes);
    assert!(config.effective_cap(Some(engage / 2)) < config.cap_bytes);

    // A zero fraction turns the backoff off entirely.
    let fixed = TsdbConfig {
        host_free_fraction: 0.0,
        ..config
    };
    assert_eq!(fixed.effective_cap(Some(1024)), fixed.cap_bytes);
}
