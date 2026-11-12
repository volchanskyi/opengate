//! Drives the `ml::backfill` engine against an in-memory `TierReader`: tier mapping, ordering,
//! confirmed spans skipped, retention clamp, and no minute sent before it ends.

use std::cell::RefCell;
use std::collections::BTreeMap;

use edge_tsdb::confirmed::ConfirmedRanges;
use edge_tsdb::tier::TierPoint;
use edge_tsdb::{Sample, SeriesId, Tier, TsdbError};
use mesh_agent_core::ml::backfill::{
    answer_local_history, confirm, forget_after, migrate_cursors, pace_delay, settled_until,
    BackfillConfig, BackfillDrain, ConfirmedStore, DrainPosition, PlannedBatch, TierReader,
    TIER_CURSOR_RECENT60S, TIER_CURSOR_ROLLUP1H, TIER_CURSOR_ROLLUP1M,
};
use mesh_protocol::BackfillTier;

/// In-memory `TierReader` holding per-series T0 raw and T1/T2 rollup points; a range read covers
/// `[start, end)`, as the store's does, and each read's range is logged.
#[derive(Default)]
struct FakeReader {
    raw: BTreeMap<SeriesId, Vec<(Sample, bool)>>,
    t1: BTreeMap<SeriesId, Vec<TierPoint>>,
    t2: BTreeMap<SeriesId, Vec<TierPoint>>,
    reads: RefCell<Vec<(i64, i64)>>,
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
        self.reads.borrow_mut().push((start, end));
        Ok(self
            .raw
            .get(&series)
            .map(|v| {
                v.iter()
                    .filter(|(s, _)| s.ts >= start && s.ts < end)
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
        self.reads.borrow_mut().push((start, end));
        Ok(src
            .get(&series)
            .map(|v| {
                v.iter()
                    .filter(|p| p.bucket >= start && p.bucket < end)
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
        max_batch_samples: max_batch,
    }
}

/// A minute boundary, so the minute before it has just ended.
const NOW: i64 = 100_020;
const CPU: SeriesId = 0;

/// Two hours on the store's hour grid, inside the hour band of [`wide_cfg`].
const HOUR_A: i64 = 86_400;
const HOUR_B: i64 = 93_600;

/// [`cfg`] with an hour band reaching back to 80,020, wide enough for both hours.
fn wide_cfg(max_batch: usize) -> BackfillConfig {
    BackfillConfig {
        retention_secs: 20_000,
        ..cfg(max_batch)
    }
}

fn drain_all<R: TierReader>(
    reader: &R,
    now: i64,
    cfg: BackfillConfig,
    series: &[SeriesId],
    confirmed: &ConfirmedRanges,
) -> Vec<PlannedBatch> {
    let mut drain = BackfillDrain::new(reader, now, cfg, series, confirmed);
    let mut out = Vec::new();
    while let Some(batch) = drain.next_batch().expect("drain must not error") {
        out.push(batch);
    }
    out
}

/// Every batch of a session as the agent sends them: a drain begun afresh for each batch at the
/// position the last one stopped at, each batch confirmed before the next.
fn drain_resumed<R: TierReader>(
    reader: &R,
    now: i64,
    cfg: BackfillConfig,
    series: &[SeriesId],
    confirmed: &mut ConfirmedRanges,
) -> Vec<PlannedBatch> {
    let mut at = DrainPosition::default();
    let mut out = Vec::new();
    loop {
        let mut drain = BackfillDrain::new(reader, now, cfg, series, confirmed).resume(at);
        let batch = drain.next_batch().expect("drain must not error");
        at = drain.position();
        let Some(batch) = batch else {
            return out;
        };
        confirmed.add(batch.start, batch.end);
        out.push(batch);
    }
}

/// A whole drain that marks empty time done up to `settled`, with every batch and every empty
/// stretch it found confirmed.
fn drain_marking(
    reader: &FakeReader,
    cfg: BackfillConfig,
    settled: i64,
    confirmed: &mut ConfirmedRanges,
) -> Vec<PlannedBatch> {
    let mut drain =
        BackfillDrain::new(reader, NOW, cfg, &[CPU], confirmed).marking_empty_until(settled);
    let mut batches = Vec::new();
    while let Some(batch) = drain.next_batch().expect("drain must not error") {
        batches.push(batch);
    }
    let empty = drain.take_empty();
    for (start, end) in batches.iter().map(|b| (b.start, b.end)).chain(empty) {
        confirmed.add(start, end);
    }
    batches
}

fn tiers_and_times(batches: &[PlannedBatch]) -> Vec<(BackfillTier, i64)> {
    batches
        .iter()
        .flat_map(|b| b.samples.iter().map(move |s| (b.tier, s.ts)))
        .collect()
}

#[test]
fn recent_first_then_older_tiers_in_order() {
    let mut r = FakeReader::default();
    for ts in (NOW - 20)..=(NOW - 1) {
        r.push_raw(CPU, ts, 50.0);
    }
    r.push_tier(CPU, Tier::T1, NOW - 900, 40.0);
    r.push_tier(CPU, Tier::T1, NOW - 300, 41.0);
    r.push_tier(CPU, Tier::T2, HOUR_A, 30.0);
    r.push_tier(CPU, Tier::T2, HOUR_B, 31.0);

    let batches = drain_all(&r, NOW, wide_cfg(1000), &[CPU], &ConfirmedRanges::default());

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
    assert!(ts_seen.contains(&HOUR_A));
    assert!(ts_seen.contains(&HOUR_B));
}

#[test]
fn skips_what_the_server_confirmed() {
    let mut r = FakeReader::default();
    r.push_tier(CPU, Tier::T1, NOW - 900, 40.0);
    r.push_tier(CPU, Tier::T1, NOW - 600, 41.0);
    r.push_tier(CPU, Tier::T1, NOW - 300, 42.0);

    let confirmed = ConfirmedRanges::from_spans([(NOW - 960, NOW - 540)]);
    let batches = drain_all(&r, NOW, cfg(1000), &[CPU], &confirmed);
    let ts_seen: Vec<i64> = batches
        .iter()
        .flat_map(|b| b.samples.iter().map(|s| s.ts))
        .collect();
    assert_eq!(
        ts_seen,
        vec![NOW - 300, NOW - 300],
        "only buckets the server never confirmed, as avg + max"
    );
}

#[test]
fn a_confirmed_minute_is_never_sent_again_unless_the_server_asks() {
    let mut r = FakeReader::default();
    for ts in (NOW - 120)..NOW {
        r.push_raw(CPU, ts, 50.0);
    }
    let mut confirmed = ConfirmedRanges::default();
    for batch in drain_all(&r, NOW, cfg(1000), &[CPU], &confirmed) {
        confirmed.add(batch.start, batch.end);
    }
    assert!(
        drain_all(&r, NOW, cfg(1000), &[CPU], &confirmed).is_empty(),
        "every acked span stays sent"
    );

    confirmed.forget_after(NOW - 60);
    let again: Vec<i64> = drain_all(&r, NOW, cfg(1000), &[CPU], &confirmed)
        .iter()
        .flat_map(|b| b.samples.iter().map(|s| s.ts))
        .collect();
    assert_eq!(
        again,
        vec![NOW - 60, NOW - 60],
        "what the server asks for again is sent again"
    );
}

#[test]
fn a_batch_confirms_the_span_it_was_drawn_from() {
    let mut r = FakeReader::default();
    r.push_tier(CPU, Tier::T1, NOW - 600, 41.0);
    let batches = drain_all(&r, NOW, cfg(1000), &[CPU], &ConfirmedRanges::default());
    let minute = batches
        .iter()
        .find(|b| b.tier == BackfillTier::Rollup1m)
        .expect("the minute tier ships");
    assert!(
        minute.start <= NOW - 600,
        "the span starts at or before its first bucket"
    );
    assert_eq!(
        minute.end,
        NOW - 600 + 60,
        "and ends where its last bucket does"
    );
}

#[test]
fn rollup_max_dim_carries_the_stored_extremum_not_the_average() {
    let mut r = FakeReader::default();
    r.push_tier_max(CPU, Tier::T1, NOW - 300, 26.7, 100.0);

    let batches = drain_all(&r, NOW, cfg(1000), &[CPU], &ConfirmedRanges::default());
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

    let batches = drain_all(&r, NOW, cfg(1000), &[CPU], &ConfirmedRanges::default());
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

    let batches = drain_all(&r, NOW, cfg(1000), &[CPU], &ConfirmedRanges::default());
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
fn drain_is_idempotent_over_the_same_confirmations() {
    let mut r = FakeReader::default();
    r.push_tier(CPU, Tier::T1, NOW - 900, 40.0);
    r.push_tier(CPU, Tier::T1, NOW - 300, 41.0);

    let a = drain_all(&r, NOW, cfg(1000), &[CPU], &ConfirmedRanges::default());
    let b = drain_all(&r, NOW, cfg(1000), &[CPU], &ConfirmedRanges::default());
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
        "replay over the same confirmations is deterministic"
    );
}

#[test]
fn batches_respect_the_sample_cap() {
    let mut r = FakeReader::default();
    for i in 0..10 {
        r.push_tier(CPU, Tier::T1, NOW - 900 + i * 60, 40.0 + i as f64);
    }
    let batches = drain_all(&r, NOW, cfg(3), &[CPU], &ConfirmedRanges::default());
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
struct FakeStore {
    confirmed: ConfirmedRanges,
    cursors: BTreeMap<SeriesId, i64>,
}

impl ConfirmedStore for FakeStore {
    fn load_confirmed(&self) -> Result<ConfirmedRanges, TsdbError> {
        Ok(self.confirmed.clone())
    }

    fn save_confirmed(&mut self, ranges: &ConfirmedRanges) -> Result<(), TsdbError> {
        self.confirmed = ranges.clone();
        Ok(())
    }

    fn load_cursor(&self, key: SeriesId) -> Result<Option<i64>, TsdbError> {
        Ok(self.cursors.get(&key).copied())
    }

    fn remove_cursor(&mut self, key: SeriesId) -> Result<(), TsdbError> {
        self.cursors.remove(&key);
        Ok(())
    }
}

#[test]
fn confirmations_accumulate_and_a_resend_request_forgets_after_its_time() {
    let mut store = FakeStore::default();
    confirm(&mut store, &[(NOW - 600, NOW - 300)], NOW - 10_000).unwrap();
    confirm(&mut store, &[(NOW - 300, NOW - 60)], NOW - 10_000).unwrap();
    assert_eq!(store.confirmed.spans(), &[(NOW - 600, NOW - 60)]);

    forget_after(&mut store, NOW - 420).unwrap();
    assert_eq!(store.confirmed.spans(), &[(NOW - 600, NOW - 420)]);
}

#[test]
fn an_older_agents_watermarks_become_confirmed_spans_once() {
    let mut store = FakeStore::default();
    store.cursors.insert(TIER_CURSOR_RECENT60S, NOW - 120);
    store.cursors.insert(TIER_CURSOR_ROLLUP1M, NOW - 600);
    let cfg = cfg(1000);

    assert!(migrate_cursors(&mut store, NOW, cfg).unwrap());
    assert!(
        store.cursors.is_empty(),
        "the watermarks are gone once read"
    );
    assert!(
        store.confirmed.covers(NOW - 120, NOW - 60),
        "the recent tier up to its watermark"
    );
    assert!(
        store.confirmed.covers(NOW - 900, NOW - 540),
        "the minute tier up to its watermark"
    );
    assert!(
        !store.confirmed.covers(NOW - 60, NOW),
        "nothing after the watermarks"
    );
    let migrated = store.confirmed.clone();

    assert!(
        !migrate_cursors(&mut store, NOW, cfg).unwrap(),
        "the migration runs once"
    );
    assert_eq!(store.confirmed, migrated);
    let _ = TIER_CURSOR_ROLLUP1H;
}

#[test]
fn confirmations_round_trip_through_a_real_store_and_the_watermarks_leave_it() {
    use edge_tsdb::{Durability, LocalTsdb, TsdbConfig};

    let dir = tempfile::tempdir().unwrap();
    let mut store = LocalTsdb::open(dir.path(), TsdbConfig::default()).unwrap();
    store
        .set_cursor(TIER_CURSOR_ROLLUP1H, NOW - 7_200, Durability::Full)
        .unwrap();
    assert!(migrate_cursors(&mut store, NOW, cfg(1000)).unwrap());
    confirm(&mut store, &[(NOW - 120, NOW - 60)], NOW - 10_000).unwrap();

    let confirmed = ConfirmedStore::load_confirmed(&store).unwrap();
    assert!(confirmed.covers(NOW - 120, NOW - 60));
    assert!(confirmed.covers(NOW - 7_200, NOW - 3_600));
    assert_eq!(store.cursor(TIER_CURSOR_ROLLUP1H).unwrap(), None);
    assert_eq!(store.cursor(CPU).unwrap(), None, "series 0 is untouched");
}

#[test]
fn confirming_forgets_spans_that_ended_before_the_horizon() {
    let mut store = FakeStore::default();
    confirm(&mut store, &[(NOW - 9_000, NOW - 8_400)], NOW - 10_000).unwrap();
    confirm(
        &mut store,
        &[(NOW - 600, NOW - 300), (NOW - 240, NOW - 180)],
        NOW - 8_000,
    )
    .unwrap();
    assert_eq!(
        store.confirmed.spans(),
        &[(NOW - 600, NOW - 300), (NOW - 240, NOW - 180)],
        "a span that ended before the horizon is dropped, the rest kept"
    );
}

#[test]
fn the_settled_time_is_the_newest_minute_the_store_has_committed() {
    use edge_tsdb::{Durability, LocalTsdb, TsdbConfig};

    let dir = tempfile::tempdir().unwrap();
    let mut store = LocalTsdb::open(dir.path(), TsdbConfig::default()).unwrap();
    assert_eq!(
        settled_until(&store.snapshot().unwrap(), &[CPU, 1]).unwrap(),
        i64::MIN,
        "an empty store has settled nothing"
    );
    for ts in 1_000_000..1_000_090 {
        store.append(CPU, Sample::new(ts, 25.0), false).unwrap();
    }
    store.commit(Durability::Full).unwrap();
    for ts in 1_000_090..1_000_200 {
        store.append(CPU, Sample::new(ts, 25.0), false).unwrap();
    }
    assert_eq!(
        settled_until(&store.snapshot().unwrap(), &[CPU, 1]).unwrap(),
        1_000_080,
        "the newest committed minute; a reading not yet committed is newer"
    );
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
    let none = ConfirmedRanges::default();

    let mut running = BackfillDrain::new(&snap, now, cfg(1000), &[CPU], &none);
    assert_eq!(
        running.next_batch().unwrap(),
        None,
        "the window 999,960 is still running at 1,000,000, so it is not sent"
    );

    let ended = 1_000_020i64;
    let mut drain = BackfillDrain::new(&snap, ended, cfg(1000), &[CPU], &none);
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

    let batches = drain_all(&r, NOW, cfg(1000), &[CPU], &ConfirmedRanges::default());
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

    let batches = drain_all(&r, NOW, cfg(1000), &[CPU], &ConfirmedRanges::default());
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
fn the_last_ended_minute_ships_and_the_running_one_does_not() {
    let now = 100_020i64;
    let mut r = FakeReader::default();
    r.push_raw(CPU, now - 60, 10.0);
    r.push_raw(CPU, now, 11.0);
    r.push_raw(CPU, now + 3_600, 12.0);

    let seen: Vec<i64> = drain_all(&r, now, cfg(1000), &[CPU], &ConfirmedRanges::default())
        .iter()
        .flat_map(|b| b.samples.iter().map(|s| s.ts))
        .collect();
    assert!(
        seen.contains(&(now - 60)),
        "the minute that just ended ships: {seen:?}"
    );
    assert!(
        !seen.iter().any(|&ts| ts >= now),
        "a running or future minute never ships: {seen:?}"
    );
}

#[test]
fn a_batch_reads_a_bounded_window_rather_than_the_whole_band() {
    let mut r = FakeReader::default();
    for i in 0..3 {
        r.push_tier(CPU, Tier::T1, NOW - 960 + i * 300, 40.0 + i as f64);
    }

    let batches = drain_all(&r, NOW, cfg(6), &[CPU], &ConfirmedRanges::default());
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
fn the_recent_and_hour_tiers_each_skip_their_confirmed_spans() {
    let mut r = FakeReader::default();
    for ts in 99_900..100_020 {
        r.push_raw(CPU, ts, 25.0);
    }
    r.push_tier(CPU, Tier::T2, HOUR_A, 30.0);
    r.push_tier(CPU, Tier::T2, HOUR_B, 31.0);

    let confirmed = ConfirmedRanges::from_spans([(99_840, 99_960), (80_020, HOUR_A + 3_600)]);
    let batches = drain_all(&r, NOW, wide_cfg(1000), &[CPU], &confirmed);
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
        "the recent tier ships the minute after its confirmed span, as avg + max"
    );
    assert_eq!(
        by_tier(BackfillTier::Rollup1h),
        vec![HOUR_B, HOUR_B],
        "the hour tier ships the hour after its confirmed span, as avg + max"
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

    let batches = drain_all(&snap, now, cfg(1000), &[CPU], &ConfirmedRanges::default());
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
        max_batch_samples: 1_000,
    };
    let floor = now - cfg.recent_secs;
    assert_ne!(floor % 60, 0, "the floor has to land inside a bucket");

    let mut r = FakeReader::default();
    r.push_raw(CPU, floor + 5, 42.0);

    let batches = drain_all(&r, now, cfg, &[CPU], &ConfirmedRanges::default());
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

#[test]
fn history_older_than_the_central_stores_thirty_days_is_never_planned() {
    const DAY: i64 = 24 * 3600;
    let now = 100 * DAY;
    let mut r = FakeReader::default();
    r.push_tier(CPU, Tier::T2, now - 31 * DAY, 31.0);
    r.push_tier(CPU, Tier::T1, now - 29 * DAY, 29.0);

    let batches = drain_all(
        &r,
        now,
        BackfillConfig::default(),
        &[CPU],
        &ConfirmedRanges::default(),
    );
    let ts_seen: Vec<i64> = batches
        .iter()
        .flat_map(|b| b.samples.iter().map(|s| s.ts))
        .collect();
    assert!(
        !ts_seen.contains(&(now - 31 * DAY)),
        "a 31-day-old hour the store would refuse is never sent: {ts_seen:?}"
    );
    assert!(
        ts_seen.contains(&(now - 29 * DAY)),
        "a 29-day-old minute is sent: {ts_seen:?}"
    );
}

#[test]
fn a_catch_up_carried_on_from_its_position_reads_each_stretch_once() {
    let mut r = FakeReader::default();
    for ts in (NOW - 90)..NOW {
        r.push_raw(CPU, ts, 50.0);
    }
    for i in 0..8 {
        r.push_tier(CPU, Tier::T1, NOW - 900 + i * 60, 40.0 + i as f64);
    }
    r.push_tier(CPU, Tier::T2, 93_600, 30.0);

    let whole = drain_all(&r, NOW, cfg(4), &[CPU], &ConfirmedRanges::default());
    let whole_reads = r.reads.take().len();
    let resumed = drain_resumed(&r, NOW, cfg(4), &[CPU], &mut ConfirmedRanges::default());

    assert!(whole.len() > 3, "the backlog spans several batches");
    assert_eq!(resumed, whole, "the same batches, in the same order");
    assert_eq!(
        r.reads.take().len(),
        whole_reads,
        "each stretch is read once over the session"
    );
}

#[test]
fn a_reconnect_with_nothing_owed_reads_only_time_after_the_newest_reading() {
    let mut r = FakeReader::default();
    r.push_tier(CPU, Tier::T1, NOW - 300, 41.0);
    let settled = NOW - 300;
    let mut confirmed = ConfirmedRanges::default();
    let sent = drain_marking(&r, cfg(2), settled, &mut confirmed);
    assert_eq!(
        tiers_and_times(&sent),
        vec![(BackfillTier::Rollup1m, NOW - 300); 2]
    );
    r.reads.take();

    assert!(
        drain_all(&r, NOW, cfg(2), &[CPU], &confirmed).is_empty(),
        "nothing is owed"
    );
    let reread: Vec<(i64, i64)> = r
        .reads
        .take()
        .into_iter()
        .filter(|&(_, end)| end < settled)
        .collect();
    assert!(
        reread.is_empty(),
        "empty time before the newest reading is done once read: {reread:?}"
    );
}

#[test]
fn empty_time_beside_confirmed_readings_is_marked_done() {
    let mut r = FakeReader::default();
    r.push_tier(CPU, Tier::T1, NOW - 600, 41.0);
    let mut confirmed = ConfirmedRanges::from_spans([(NOW - 600, NOW - 540)]);
    drain_marking(&r, cfg(1000), NOW - 300, &mut confirmed);
    assert!(
        confirmed.covers(NOW - 960, NOW - 300),
        "the empty time on both sides of the confirmed minute, up to the newest reading"
    );
}

#[test]
fn empty_time_after_the_newest_committed_reading_stays_owed() {
    let mut r = FakeReader::default();
    r.push_tier(CPU, Tier::T1, NOW - 600, 41.0);
    let mut confirmed = ConfirmedRanges::default();
    drain_marking(&r, cfg(2), NOW - 600, &mut confirmed);
    assert!(
        !confirmed.covers(NOW - 480, NOW - 420),
        "a reading not yet committed may still land after the newest committed one"
    );

    r.push_tier(CPU, Tier::T1, NOW - 480, 42.0);
    assert_eq!(
        tiers_and_times(&drain_all(&r, NOW, cfg(2), &[CPU], &confirmed)),
        vec![(BackfillTier::Rollup1m, NOW - 480); 2],
        "and is sent once it lands"
    );
}

#[test]
fn a_bucket_just_past_an_empty_slice_is_still_read() {
    let mut r = FakeReader::default();
    r.push_tier(CPU, Tier::T2, HOUR_A, 30.0);
    r.push_tier(CPU, Tier::T1, 99_120, 40.0);
    let shipped = tiers_and_times(&drain_all(
        &r,
        NOW,
        wide_cfg(2),
        &[CPU],
        &ConfirmedRanges::default(),
    ));
    assert!(
        shipped.contains(&(BackfillTier::Rollup1h, HOUR_A)),
        "the hour after the first empty slice: {shipped:?}"
    );
    assert!(
        shipped.contains(&(BackfillTier::Rollup1m, 99_120)),
        "the minute after the first empty slice: {shipped:?}"
    );
}

#[test]
fn a_bucket_reaching_past_its_band_is_not_taken_for_empty_time() {
    let mut r = FakeReader::default();
    r.push_tier(CPU, Tier::T2, 97_200, 30.0);
    let mut confirmed = ConfirmedRanges::default();
    let sent = drain_marking(&r, cfg(2), NOW - 60, &mut confirmed);
    assert!(
        sent.is_empty(),
        "the hour reaches past the band's top today"
    );

    let later = NOW + 2_000;
    assert!(
        tiers_and_times(&drain_all(&r, later, cfg(2), &[CPU], &confirmed))
            .contains(&(BackfillTier::Rollup1h, 97_200)),
        "it ships once it lies inside its band"
    );
}

#[test]
fn a_minutes_last_second_counts_in_its_caught_up_maximum() {
    use edge_tsdb::{Durability, LocalTsdb, TsdbConfig};

    let dir = tempfile::tempdir().unwrap();
    let mut store = LocalTsdb::open(dir.path(), TsdbConfig::default()).unwrap();
    let window = 999_960i64;
    for ts in window..window + 60 {
        let value = if ts == window + 59 { 99.0 } else { 25.0 };
        store.append(CPU, Sample::new(ts, value), false).unwrap();
    }
    store.commit(Durability::Full).unwrap();
    let snap = store.snapshot().unwrap();

    let batches = drain_all(
        &snap,
        window + 60,
        cfg(1000),
        &[CPU],
        &ConfirmedRanges::default(),
    );
    let max = batches
        .iter()
        .flat_map(|b| &b.samples)
        .find(|s| s.name == "cpu.total.max")
        .map(|s| s.value);
    assert_eq!(
        max,
        Some(99.0),
        "the reading at the minute's last second is read"
    );
}

#[test]
fn a_real_store_minute_just_past_an_empty_slice_ships_and_stays_owed_until_sent() {
    use edge_tsdb::{Durability, LocalTsdb, TsdbConfig};

    let dir = tempfile::tempdir().unwrap();
    let mut store = LocalTsdb::open(dir.path(), TsdbConfig::default()).unwrap();
    let minute = 99_120i64;
    for ts in minute..minute + 60 {
        store.append(CPU, Sample::new(ts, 40.0), false).unwrap();
    }
    store.commit(Durability::Full).unwrap();
    let snap = store.snapshot().unwrap();
    let mut confirmed = ConfirmedRanges::default();

    let mut drain =
        BackfillDrain::new(&snap, NOW, cfg(2), &[CPU], &confirmed).marking_empty_until(minute);
    let mut shipped = Vec::new();
    while let Some(batch) = drain.next_batch().unwrap() {
        shipped.extend(batch.samples.iter().map(|s| (batch.tier, s.ts)));
    }
    let empty = drain.take_empty();
    assert!(
        shipped.contains(&(BackfillTier::Rollup1m, minute)),
        "the minute after the first empty slice: {shipped:?}"
    );
    for (start, end) in empty {
        confirmed.add(start, end);
    }
    assert!(
        !confirmed.covers(minute, minute + 60),
        "empty time never takes in a minute with readings"
    );
}
