//! Drives the `ml::backfill` engine against an in-memory `TierReader`: tier mapping, ordering,
//! cursor resume, retention clamp and clock-skew bounds.

use std::collections::BTreeMap;

use edge_tsdb::tier::TierPoint;
use edge_tsdb::{Sample, SeriesId, Tier, TsdbError};
use mesh_agent_core::ml::backfill::{
    answer_local_history, load_cursors, pace_delay, pending_hint, record_ack, tier_cursor_key,
    BackfillConfig, BackfillCursors, BackfillDrain, CursorStore, TierReader, TIER_CURSOR_RECENT60S,
    TIER_CURSOR_ROLLUP1H, TIER_CURSOR_ROLLUP1M,
};
use mesh_protocol::BackfillTier;

/// In-memory `TierReader` holding per-series T0 raw and T1/T2 rollup points; range reads are
/// inclusive of `[start, end]`.
#[derive(Default)]
struct FakeReader {
    raw: BTreeMap<SeriesId, Vec<(Sample, bool)>>,
    t1: BTreeMap<SeriesId, Vec<TierPoint>>,
    t2: BTreeMap<SeriesId, Vec<TierPoint>>,
}

impl FakeReader {
    fn push_raw(&mut self, series: SeriesId, ts: i64, value: f64) {
        self.raw
            .entry(series)
            .or_default()
            .push((Sample::new(ts, value), false));
    }

    fn push_tier(&mut self, series: SeriesId, tier: Tier, bucket: i64, avg: f64) {
        self.push_tier_max(series, tier, bucket, avg, avg);
    }

    /// A rollup bucket whose maximum differs from its average.
    fn push_tier_max(&mut self, series: SeriesId, tier: Tier, bucket: i64, avg: f64, max: f64) {
        let point = TierPoint {
            bucket,
            min: avg,
            max,
            avg,
            last: avg,
            count: 1,
        };
        match tier {
            Tier::T1 => self.t1.entry(series).or_default().push(point),
            Tier::T2 => self.t2.entry(series).or_default().push(point),
            _ => unreachable!("edge-tsdb has only T1/T2 rollup tiers"),
        }
    }
}

impl TierReader for FakeReader {
    fn range_raw(
        &self,
        series: SeriesId,
        start: i64,
        end: i64,
    ) -> Result<Vec<(Sample, bool)>, TsdbError> {
        Ok(self
            .raw
            .get(&series)
            .map(|v| {
                v.iter()
                    .filter(|(s, _)| s.ts >= start && s.ts <= end)
                    .copied()
                    .collect()
            })
            .unwrap_or_default())
    }

    fn range_tier(
        &self,
        series: SeriesId,
        tier: Tier,
        start: i64,
        end: i64,
    ) -> Result<Vec<TierPoint>, TsdbError> {
        let src = match tier {
            Tier::T1 => &self.t1,
            Tier::T2 => &self.t2,
            _ => unreachable!("edge-tsdb has only T1/T2 rollup tiers"),
        };
        Ok(src
            .get(&series)
            .map(|v| {
                v.iter()
                    .filter(|p| p.bucket >= start && p.bucket <= end)
                    .copied()
                    .collect()
            })
            .unwrap_or_default())
    }
}

/// Tiny bands: age < 100 is Recent60s, 100..1000 Rollup1m, 1000..10000 Rollup1h.
fn cfg(max_batch: usize) -> BackfillConfig {
    BackfillConfig {
        retention_secs: 10_000,
        recent_secs: 100,
        mid_secs: 1_000,
        future_skew_secs: 60,
        max_batch_samples: max_batch,
    }
}

const NOW: i64 = 100_000;
const CPU: SeriesId = 0;

fn drain_all<R: TierReader>(
    reader: &R,
    now: i64,
    cfg: BackfillConfig,
    series: &[SeriesId],
    cursors: BackfillCursors,
) -> Vec<mesh_agent_core::ml::backfill::PlannedBatch> {
    let mut drain = BackfillDrain::new(reader, now, cfg, series, cursors);
    let mut out = Vec::new();
    while let Some(batch) = drain.next_batch().expect("drain must not error") {
        out.push(batch);
    }
    out
}

#[test]
fn recent_first_then_older_tiers_in_order() {
    let mut r = FakeReader::default();
    for ts in (NOW - 20)..=(NOW - 1) {
        r.push_raw(CPU, ts, 50.0);
    }
    r.push_tier(CPU, Tier::T1, NOW - 900, 40.0);
    r.push_tier(CPU, Tier::T1, NOW - 300, 41.0);
    r.push_tier(CPU, Tier::T2, NOW - 9000, 30.0);
    r.push_tier(CPU, Tier::T2, NOW - 5400, 31.0);

    let batches = drain_all(&r, NOW, cfg(1000), &[CPU], BackfillCursors::default());

    let tiers: Vec<BackfillTier> = batches.iter().map(|b| b.tier).collect();
    let first_1m = tiers.iter().position(|t| *t == BackfillTier::Rollup1m);
    let first_1h = tiers.iter().position(|t| *t == BackfillTier::Rollup1h);
    assert!(
        tiers.first() == Some(&BackfillTier::Recent60s),
        "recent window first"
    );
    assert!(first_1m < first_1h, "1 min drains before 1 hr");
    for (i, t) in tiers.iter().enumerate() {
        if *t == BackfillTier::Recent60s {
            assert!(
                first_1m.is_none_or(|m| i < m),
                "no Recent60s batch after an older tier"
            );
        }
    }

    for b in &batches {
        for s in &b.samples {
            assert!(
                s.name == "cpu.total" || s.name == "cpu.total.max",
                "unexpected dim {}",
                s.name
            );
            if b.tier == BackfillTier::Recent60s {
                assert_eq!(s.ts % 60, 0, "raw is rolled to 60 s windows, never 1 s");
            }
        }
    }
    let ts_seen: Vec<i64> = batches
        .iter()
        .flat_map(|b| b.samples.iter().map(|s| s.ts))
        .collect();
    assert!(ts_seen.contains(&(NOW - 9000)));
    assert!(ts_seen.contains(&(NOW - 5400)));
}

#[test]
fn resumes_after_cursor_without_reemitting() {
    let mut r = FakeReader::default();
    r.push_tier(CPU, Tier::T1, NOW - 900, 40.0);
    r.push_tier(CPU, Tier::T1, NOW - 600, 41.0);
    r.push_tier(CPU, Tier::T1, NOW - 300, 42.0);

    let cursors = BackfillCursors {
        rollup1m: Some(NOW - 600),
        ..Default::default()
    };
    let batches = drain_all(&r, NOW, cfg(1000), &[CPU], cursors);
    let ts_seen: Vec<i64> = batches
        .iter()
        .flat_map(|b| b.samples.iter().map(|s| s.ts))
        .collect();
    assert_eq!(
        ts_seen,
        vec![NOW - 300, NOW - 300],
        "only buckets strictly after the cursor, as avg + max"
    );
}

#[test]
fn rollup_max_dim_carries_the_stored_extremum_not_the_average() {
    let mut r = FakeReader::default();
    r.push_tier_max(CPU, Tier::T1, NOW - 300, 26.7, 100.0);

    let batches = drain_all(&r, NOW, cfg(1000), &[CPU], BackfillCursors::default());
    let seen: Vec<(String, f64)> = batches
        .iter()
        .flat_map(|b| b.samples.iter().map(|s| (s.name.clone(), s.value)))
        .collect();
    assert_eq!(
        seen,
        vec![
            ("cpu.total".to_string(), 26.7),
            ("cpu.total.max".to_string(), 100.0),
        ]
    );
}

#[test]
fn recent_tier_max_dim_is_the_largest_raw_sample_in_the_minute() {
    let mut r = FakeReader::default();
    for i in 0..40 {
        let ts = NOW - 40 + i;
        r.push_raw(CPU, ts, if (20..25).contains(&i) { 100.0 } else { 20.0 });
    }

    let batches = drain_all(&r, NOW, cfg(1000), &[CPU], BackfillCursors::default());
    let seen: Vec<(String, f64)> = batches
        .iter()
        .flat_map(|b| b.samples.iter().map(|s| (s.name.clone(), s.value)))
        .collect();
    let avg = (35.0 * 20.0 + 5.0 * 100.0) / 40.0;
    assert_eq!(seen.len(), 2, "one minute, one avg and one max");
    assert_eq!(seen[0].0, "cpu.total");
    assert!(
        (seen[0].1 - avg).abs() < 1e-9,
        "the average hides the pin: {} vs {avg}",
        seen[0].1
    );
    assert_eq!(
        seen[1],
        ("cpu.total.max".to_string(), 100.0),
        "the maximum recovers it"
    );
}

#[test]
fn clamps_out_of_retention_and_bounds_wild_clocks() {
    let mut r = FakeReader::default();
    r.push_tier(CPU, Tier::T2, NOW - 20_000, 99.0);
    r.push_tier(CPU, Tier::T2, NOW - 5_000, 31.0);
    r.push_raw(CPU, NOW + 10_000, 77.0);
    for ts in (NOW - 12)..=(NOW - 1) {
        r.push_raw(CPU, ts, 50.0);
    }

    let batches = drain_all(&r, NOW, cfg(1000), &[CPU], BackfillCursors::default());
    let ts_seen: Vec<i64> = batches
        .iter()
        .flat_map(|b| b.samples.iter().map(|s| s.ts))
        .collect();
    assert!(
        !ts_seen.iter().any(|&t| t <= NOW - 10_000),
        "out-of-retention samples must be skipped: {ts_seen:?}"
    );
    assert!(
        !ts_seen.iter().any(|&t| t > NOW + 60),
        "wild-future samples must be bounded out: {ts_seen:?}"
    );
    assert!(
        ts_seen.contains(&(NOW - 5_000)),
        "in-retention old point kept"
    );
}

#[test]
fn drain_is_idempotent_from_the_same_cursors() {
    let mut r = FakeReader::default();
    r.push_tier(CPU, Tier::T1, NOW - 900, 40.0);
    r.push_tier(CPU, Tier::T1, NOW - 300, 41.0);

    let a = drain_all(&r, NOW, cfg(1000), &[CPU], BackfillCursors::default());
    let b = drain_all(&r, NOW, cfg(1000), &[CPU], BackfillCursors::default());
    let flat =
        |v: &[mesh_agent_core::ml::backfill::PlannedBatch]| -> Vec<(BackfillTier, i64, f64)> {
            v.iter()
                .flat_map(|batch| {
                    batch
                        .samples
                        .iter()
                        .map(move |s| (batch.tier, s.ts, s.value))
                })
                .collect()
        };
    assert_eq!(
        flat(&a),
        flat(&b),
        "replay from the same cursors is deterministic"
    );
}

#[test]
fn batches_respect_the_sample_cap() {
    let mut r = FakeReader::default();
    for i in 0..10 {
        r.push_tier(CPU, Tier::T1, NOW - 900 + i * 60, 40.0 + i as f64);
    }
    let batches = drain_all(&r, NOW, cfg(3), &[CPU], BackfillCursors::default());
    assert!(
        batches.len() > 1,
        "a large backlog must split into multiple batches"
    );
    for b in &batches {
        assert!(b.samples.len() <= 3, "batch exceeded the sample cap");
        assert_eq!(
            b.cursor,
            b.samples.iter().map(|s| s.ts).max().unwrap(),
            "cursor is the newest bucket in the batch"
        );
    }
}

#[derive(Default)]
struct FakeCursors(BTreeMap<SeriesId, i64>);

impl CursorStore for FakeCursors {
    fn load_cursor(&self, key: SeriesId) -> Result<Option<i64>, TsdbError> {
        Ok(self.0.get(&key).copied())
    }

    fn save_cursor(&mut self, key: SeriesId, ts: i64) -> Result<(), TsdbError> {
        self.0.insert(key, ts);
        Ok(())
    }
}

#[test]
fn tier_cursor_keys_are_distinct_and_reserved() {
    let keys = [
        tier_cursor_key(BackfillTier::Recent60s).unwrap(),
        tier_cursor_key(BackfillTier::Rollup1m).unwrap(),
        tier_cursor_key(BackfillTier::Rollup1h).unwrap(),
    ];
    assert_eq!(
        keys,
        [
            TIER_CURSOR_RECENT60S,
            TIER_CURSOR_ROLLUP1M,
            TIER_CURSOR_ROLLUP1H
        ]
    );
    let mut sorted = keys.to_vec();
    sorted.sort_unstable();
    sorted.dedup();
    assert_eq!(sorted.len(), 3, "the three tier keys are distinct");
    for k in keys {
        assert!(k > 1_000, "tier keys sit far above real series ids");
    }
    // The keys are a durable on-disk address.
    assert_eq!(
        keys,
        [SeriesId::MAX, SeriesId::MAX - 1, SeriesId::MAX - 2],
        "the three watermarks sit at the very top of the series-id space"
    );
}

#[test]
fn ack_persists_the_matching_tier_watermark_only() {
    let mut c = FakeCursors::default();
    assert_eq!(load_cursors(&c).unwrap().recent60s, None);

    record_ack(&mut c, BackfillTier::Rollup1m, NOW - 300).unwrap();
    let cursors = load_cursors(&c).unwrap();
    assert_eq!(cursors.rollup1m, Some(NOW - 300), "acked tier advanced");
    assert_eq!(cursors.recent60s, None, "other tiers untouched");
    assert_eq!(cursors.rollup1h, None);

    record_ack(&mut c, BackfillTier::Rollup1m, NOW - 60).unwrap();
    assert_eq!(load_cursors(&c).unwrap().rollup1m, Some(NOW - 60));
}

#[test]
fn cursors_round_trip_through_a_real_store() {
    use edge_tsdb::{LocalTsdb, TsdbConfig};

    let dir = tempfile::tempdir().unwrap();
    let mut store = LocalTsdb::open(dir.path(), TsdbConfig::default()).unwrap();
    record_ack(&mut store, BackfillTier::Recent60s, 12_340).unwrap();
    record_ack(&mut store, BackfillTier::Rollup1h, 9_000).unwrap();

    let cursors = load_cursors(&store).unwrap();
    assert_eq!(cursors.recent60s, Some(12_340));
    assert_eq!(cursors.rollup1h, Some(9_000));
    assert_eq!(cursors.rollup1m, None);

    assert_eq!(
        store.cursor(CPU).unwrap(),
        None,
        "series 0 cursor untouched"
    );
}

#[test]
fn pending_hint_counts_backlog_and_reports_oldest() {
    let mut r = FakeReader::default();
    r.push_tier(CPU, Tier::T1, NOW - 900, 40.0);
    r.push_tier(CPU, Tier::T1, NOW - 300, 41.0);
    r.push_tier(CPU, Tier::T2, NOW - 5400, 30.0);

    let (pending, oldest) =
        pending_hint(&r, NOW, cfg(1000), &[CPU], BackfillCursors::default()).unwrap();
    assert_eq!(
        pending, 6,
        "three pending buckets across the tiers, each an avg and a max"
    );
    assert_eq!(oldest, NOW - 5400, "oldest pending bucket is the T2 point");

    let empty = FakeReader::default();
    let (n, ts) = pending_hint(&empty, NOW, cfg(1000), &[CPU], BackfillCursors::default()).unwrap();
    assert_eq!((n, ts), (0, 0));
}

#[test]
fn pace_delay_bounds_the_drain_to_the_granted_rate() {
    use std::time::Duration;
    assert_eq!(pace_delay(100, 50), Duration::from_secs(2));
    assert_eq!(pace_delay(100, 0), Duration::ZERO);
    assert_eq!(pace_delay(0, 50), Duration::ZERO);
}

#[test]
fn drains_a_real_local_store_snapshot() {
    use edge_tsdb::{Durability, LocalTsdb, TsdbConfig};

    let dir = tempfile::tempdir().unwrap();
    let mut store = LocalTsdb::open(dir.path(), TsdbConfig::default()).unwrap();
    let now = 1_000_000i64;
    for ts in (now - 30)..now {
        store.append(CPU, Sample::new(ts, 25.0), false).unwrap();
    }
    store.commit(Durability::Full).unwrap();
    let snap = store.snapshot().unwrap();

    let mut drain = BackfillDrain::new(&snap, now, cfg(1000), &[CPU], BackfillCursors::default());
    let mut dims = Vec::new();
    while let Some(b) = drain.next_batch().unwrap() {
        assert_eq!(b.tier, BackfillTier::Recent60s);
        for s in &b.samples {
            assert_eq!(s.ts % 60, 0);
            dims.push(s.name.clone());
        }
    }
    assert_eq!(
        dims,
        vec!["cpu.total".to_string(), "cpu.total.max".to_string()],
        "30 one-second samples fold into one 60 s window, as an avg and a max"
    );

    let (points, truncated) = answer_local_history(&snap, CPU, now - 30, now, 100).unwrap();
    assert_eq!(points.len(), 30, "full-res 1 s pull from the real store");
    assert!(!truncated);
}

#[test]
fn local_history_pull_is_bounded_and_flags_truncation() {
    let mut r = FakeReader::default();
    for ts in (NOW - 100)..=(NOW - 1) {
        r.push_raw(CPU, ts, ts as f64);
    }
    let (points, truncated) = answer_local_history(&r, CPU, NOW - 100, NOW, 10).unwrap();
    assert_eq!(points.len(), 10);
    assert!(truncated, "a capped window reports truncation");
    for w in points.windows(2) {
        assert!(w[1].ts > w[0].ts);
    }

    let (all, truncated) = answer_local_history(&r, CPU, NOW - 100, NOW, 1000).unwrap();
    assert_eq!(all.len(), 100);
    assert!(!truncated, "a roomy cap does not report truncation");
}

#[test]
fn the_recent_floor_keeps_an_older_raw_sample_out_of_the_recent_tier() {
    let mut r = FakeReader::default();
    r.push_raw(CPU, NOW - 150, 42.0);
    r.push_raw(CPU, NOW - 30, 43.0);

    let batches = drain_all(&r, NOW, cfg(1000), &[CPU], BackfillCursors::default());
    let recent: Vec<i64> = batches
        .iter()
        .filter(|b| b.tier == BackfillTier::Recent60s)
        .flat_map(|b| b.samples.iter().map(|s| s.ts))
        .collect();
    assert!(
        recent.iter().all(|&ts| ts >= NOW - 100),
        "the recent tier ships nothing below its floor: {recent:?}"
    );
    assert!(!recent.is_empty(), "and still ships what is inside it");
}

#[test]
fn the_minute_floor_leaves_older_buckets_to_the_hour_tier() {
    let mut r = FakeReader::default();
    r.push_tier(CPU, Tier::T1, NOW - 5_000, 40.0);
    r.push_tier(CPU, Tier::T1, NOW - 300, 41.0);

    let batches = drain_all(&r, NOW, cfg(1000), &[CPU], BackfillCursors::default());
    let minute: Vec<i64> = batches
        .iter()
        .filter(|b| b.tier == BackfillTier::Rollup1m)
        .flat_map(|b| b.samples.iter().map(|s| s.ts))
        .collect();
    assert!(
        minute.iter().all(|&ts| ts >= NOW - 1_000),
        "the minute tier ships nothing below its floor: {minute:?}"
    );
    assert!(minute.contains(&(NOW - 300)), "and ships what is inside it");
}

#[test]
fn a_bucket_exactly_on_the_recent_ceiling_still_ships() {
    let now = 100_020i64;
    let cfg = BackfillConfig {
        retention_secs: 10_000,
        recent_secs: 120,
        mid_secs: 1_000,
        future_skew_secs: 60,
        max_batch_samples: 2,
    };
    let ceiling = now + cfg.future_skew_secs;

    let mut r = FakeReader::default();
    r.push_raw(CPU, ceiling - 60, 10.0);
    r.push_raw(CPU, ceiling, 11.0);

    let batches = drain_all(&r, now, cfg, &[CPU], BackfillCursors::default());
    let seen: Vec<i64> = batches
        .iter()
        .flat_map(|b| b.samples.iter().map(|s| s.ts))
        .collect();
    assert!(
        seen.contains(&ceiling),
        "the bucket on the ceiling is inside the band: {seen:?}"
    );
}

#[test]
fn a_batch_reads_a_bounded_window_rather_than_the_whole_band() {
    let mut r = FakeReader::default();
    for i in 0..3 {
        r.push_tier(CPU, Tier::T1, NOW - 960 + i * 300, 40.0 + i as f64);
    }

    let batches = drain_all(&r, NOW, cfg(6), &[CPU], BackfillCursors::default());
    let first = batches
        .iter()
        .find(|b| b.tier == BackfillTier::Rollup1m)
        .expect("the minute tier ships");
    assert_eq!(
        first.samples.len(),
        2,
        "the first batch carries only what its read window reached: {:?}",
        first.samples
    );
    let all: Vec<i64> = batches
        .iter()
        .flat_map(|b| b.samples.iter().map(|s| s.ts))
        .collect();
    for i in 0..3 {
        assert!(
            all.contains(&(NOW - 960 + i * 300)),
            "every bucket still ships across the batches: {all:?}"
        );
    }
}

#[test]
fn the_recent_and_hour_tiers_resume_from_their_own_watermarks() {
    let mut r = FakeReader::default();
    for ts in 99_900..100_020 {
        r.push_raw(CPU, ts, 25.0);
    }
    r.push_tier(CPU, Tier::T2, NOW - 9_000, 30.0);
    r.push_tier(CPU, Tier::T2, NOW - 5_400, 31.0);

    let cursors = BackfillCursors {
        recent60s: Some(99_900),
        rollup1h: Some(NOW - 9_000),
        ..Default::default()
    };
    let batches = drain_all(&r, NOW, cfg(1000), &[CPU], cursors);

    let by_tier = |tier: BackfillTier| -> Vec<i64> {
        batches
            .iter()
            .filter(|b| b.tier == tier)
            .flat_map(|b| b.samples.iter().map(|s| s.ts))
            .collect()
    };
    assert_eq!(
        by_tier(BackfillTier::Recent60s),
        vec![99_960, 99_960],
        "the recent tier resumes strictly after its own watermark, as avg + max"
    );
    assert_eq!(
        by_tier(BackfillTier::Rollup1h),
        vec![NOW - 5_400, NOW - 5_400],
        "the hour tier resumes strictly after its own watermark, as avg + max"
    );
}

#[test]
fn a_history_pull_exactly_at_the_cap_is_not_truncated() {
    let mut r = FakeReader::default();
    for ts in (NOW - 10)..NOW {
        r.push_raw(CPU, ts, ts as f64);
    }
    let (points, truncated) = answer_local_history(&r, CPU, NOW - 10, NOW, 10).unwrap();
    assert_eq!(points.len(), 10);
    assert!(!truncated, "a window that fits the cap exactly is complete");
}

#[test]
fn drains_the_rollup_tiers_from_a_real_local_store_snapshot() {
    use edge_tsdb::{Durability, LocalTsdb, TsdbConfig};

    let dir = tempfile::tempdir().unwrap();
    let mut store = LocalTsdb::open(dir.path(), TsdbConfig::default()).unwrap();
    let now = 3_600_000i64;

    for ts in (now - 900)..(now - 840) {
        store.append(CPU, Sample::new(ts, 40.0), false).unwrap();
    }
    let hour_bucket = now - 7_200;
    for ts in hour_bucket..(hour_bucket + 60) {
        store.append(CPU, Sample::new(ts, 30.0), false).unwrap();
    }
    store.commit(Durability::Full).unwrap();
    let snap = store.snapshot().unwrap();

    let batches = drain_all(&snap, now, cfg(1000), &[CPU], BackfillCursors::default());
    let by_tier = |tier: BackfillTier| -> Vec<i64> {
        batches
            .iter()
            .filter(|b| b.tier == tier)
            .flat_map(|b| b.samples.iter().map(|s| s.ts))
            .collect()
    };

    assert_eq!(
        by_tier(BackfillTier::Rollup1m),
        vec![now - 900, now - 900],
        "the minute rollup the store built comes back through range_tier"
    );
    assert_eq!(
        by_tier(BackfillTier::Rollup1h),
        vec![hour_bucket, hour_bucket],
        "and so does the hour rollup"
    );
}

#[test]
fn a_bucket_straddling_the_recent_floor_does_not_ship_at_full_resolution() {
    let now = 100_020i64;
    let cfg = BackfillConfig {
        retention_secs: 10_000,
        recent_secs: 90,
        mid_secs: 1_000,
        future_skew_secs: 60,
        max_batch_samples: 1_000,
    };
    let floor = now - cfg.recent_secs;
    assert_ne!(floor % 60, 0, "the floor has to land inside a bucket");

    let mut r = FakeReader::default();
    r.push_raw(CPU, floor + 5, 42.0);

    let batches = drain_all(&r, now, cfg, &[CPU], BackfillCursors::default());
    let recent: Vec<i64> = batches
        .iter()
        .filter(|b| b.tier == BackfillTier::Recent60s)
        .flat_map(|b| b.samples.iter().map(|s| s.ts))
        .collect();
    assert!(
        recent.is_empty(),
        "a bucket that starts before the window is not the recent tier's to ship: {recent:?}"
    );
}
