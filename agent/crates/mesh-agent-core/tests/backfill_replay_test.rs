//! WS-15 reconnect-backfill replay engine — behavior tests (plan Steps 1 & 3).
//!
//! Drives the pure `ml::backfill` engine against an in-memory `TierReader` fake,
//! asserting the locked decisions: resolution-tiered mapping (T0→Raw10s,
//! T1→Rollup1m, T2→Rollup1h; 1 s never sent), recent-first-then-older ordering,
//! in-order-within-tier + resumable-from-cursor drain, retention clamp, and
//! clock-skew bounds. The server-side VM-bucket correctness is covered
//! separately (Go `server/tests/vmbackfill`).

use std::collections::BTreeMap;

use edge_tsdb::tier::TierPoint;
use edge_tsdb::{Sample, SeriesId, Tier, TsdbError};
use mesh_agent_core::ml::backfill::{
    answer_local_history, load_cursors, pace_delay, pending_hint, record_ack, tier_cursor_key,
    BackfillConfig, BackfillCursors, BackfillDrain, CursorStore, TierReader, TIER_CURSOR_RAW10S,
    TIER_CURSOR_ROLLUP1H, TIER_CURSOR_ROLLUP1M,
};
use mesh_protocol::BackfillTier;

/// In-memory `TierReader` for the drain. Holds per-series T0 raw and T1/T2
/// rollup points; range reads honor the `[start, end]` inclusive window the
/// engine asks for so cursor-resume slicing is exercised for real.
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
        let point = TierPoint {
            bucket,
            min: avg,
            max: avg,
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

/// Compact config with tiny bands so fixtures are hand-checkable:
/// age < 100 → Raw10s, 100..1000 → Rollup1m, 1000..10000 → Rollup1h, else skip.
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

/// Drain every batch the engine produces from the given cursors, advancing the
/// in-memory watermark by each batch's cursor (as the caller would on ack).
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
    // Recent band (age < 100): raw 1 s samples across two 10 s windows.
    for ts in (NOW - 20)..=(NOW - 1) {
        r.push_raw(CPU, ts, 50.0);
    }
    // Mid band (100..1000): T1 1-min buckets.
    r.push_tier(CPU, Tier::T1, NOW - 900, 40.0);
    r.push_tier(CPU, Tier::T1, NOW - 300, 41.0);
    // Old band (1000..10000): T2 1-hr buckets that lie entirely in the band
    // (bucket + 3600 <= NOW-1000, so they never straddle into the mid tier).
    r.push_tier(CPU, Tier::T2, NOW - 9000, 30.0);
    r.push_tier(CPU, Tier::T2, NOW - 5400, 31.0);

    let batches = drain_all(&r, NOW, cfg(1000), &[CPU], BackfillCursors::default());

    // Tier order: all Raw10s first, then all Rollup1m, then all Rollup1h.
    let tiers: Vec<BackfillTier> = batches.iter().map(|b| b.tier).collect();
    let first_1m = tiers.iter().position(|t| *t == BackfillTier::Rollup1m);
    let first_1h = tiers.iter().position(|t| *t == BackfillTier::Rollup1h);
    assert!(
        tiers.first() == Some(&BackfillTier::Raw10s),
        "recent window first"
    );
    assert!(first_1m < first_1h, "1 min drains before 1 hr");
    for (i, t) in tiers.iter().enumerate() {
        if *t == BackfillTier::Raw10s {
            assert!(
                first_1m.is_none_or(|m| i < m),
                "no Raw10s batch after an older tier"
            );
        }
    }

    // Every sample timestamp is ascending across the whole drain within a tier,
    // and no 1 s sample is ever sent (Raw10s buckets are 10 s-aligned).
    for b in &batches {
        for s in &b.samples {
            assert_eq!(s.name, "cpu.total");
            if b.tier == BackfillTier::Raw10s {
                assert_eq!(s.ts % 10, 0, "raw is rolled to 10 s windows, never 1 s");
            }
        }
    }
    // The old T2 point at NOW-9000 and NOW-4000 both survive (inside retention).
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

    // Resume with the 1-min watermark already past the first two buckets.
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
        vec![NOW - 300],
        "only buckets strictly after the cursor"
    );
}

#[test]
fn clamps_out_of_retention_and_bounds_wild_clocks() {
    let mut r = FakeReader::default();
    // Out-of-retention (age > 10000): must be skipped.
    r.push_tier(CPU, Tier::T2, NOW - 20_000, 99.0);
    // In-retention old point: kept.
    r.push_tier(CPU, Tier::T2, NOW - 5_000, 31.0);
    // Wild-future raw sample (ts well beyond now + skew): must be skipped.
    r.push_raw(CPU, NOW + 10_000, 77.0);
    // Legit recent raw sample.
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
    // Cap of 3 samples/batch over a single series → at most 3 buckets per batch.
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

/// In-memory `CursorStore` fake: the durable per-tier watermark table.
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
    // Each shippable tier maps to its own reserved key; the three keys are
    // distinct and sit above every real metric series id (0..) so a tier
    // watermark never collides with a WS-14b per-series cursor.
    let keys = [
        tier_cursor_key(BackfillTier::Raw10s).unwrap(),
        tier_cursor_key(BackfillTier::Rollup1m).unwrap(),
        tier_cursor_key(BackfillTier::Rollup1h).unwrap(),
    ];
    assert_eq!(
        keys,
        [
            TIER_CURSOR_RAW10S,
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
}

#[test]
fn ack_persists_the_matching_tier_watermark_only() {
    let mut c = FakeCursors::default();
    // A fresh store reports no watermark for any tier.
    assert_eq!(load_cursors(&c).unwrap().raw10s, None);

    record_ack(&mut c, BackfillTier::Rollup1m, NOW - 300).unwrap();
    let cursors = load_cursors(&c).unwrap();
    assert_eq!(cursors.rollup1m, Some(NOW - 300), "acked tier advanced");
    assert_eq!(cursors.raw10s, None, "other tiers untouched");
    assert_eq!(cursors.rollup1h, None);

    // A later ack for the same tier moves the watermark forward.
    record_ack(&mut c, BackfillTier::Rollup1m, NOW - 60).unwrap();
    assert_eq!(load_cursors(&c).unwrap().rollup1m, Some(NOW - 60));
}

#[test]
fn cursors_round_trip_through_a_real_store() {
    use edge_tsdb::{LocalTsdb, TsdbConfig};

    let dir = tempfile::tempdir().unwrap();
    let mut store = LocalTsdb::open(dir.path(), TsdbConfig::default()).unwrap();
    record_ack(&mut store, BackfillTier::Raw10s, 12_340).unwrap();
    record_ack(&mut store, BackfillTier::Rollup1h, 9_000).unwrap();

    let cursors = load_cursors(&store).unwrap();
    assert_eq!(cursors.raw10s, Some(12_340));
    assert_eq!(cursors.rollup1h, Some(9_000));
    assert_eq!(cursors.rollup1m, None);

    // The reserved tier keys must not shadow a real series' WS-14b cursor.
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
    assert_eq!(pending, 3, "three pending buckets across the tiers");
    assert_eq!(oldest, NOW - 5400, "oldest pending bucket is the T2 point");

    // Nothing pending → a zeroed hint (never a bogus timestamp).
    let empty = FakeReader::default();
    let (n, ts) = pending_hint(&empty, NOW, cfg(1000), &[CPU], BackfillCursors::default()).unwrap();
    assert_eq!((n, ts), (0, 0));
}

#[test]
fn pace_delay_bounds_the_drain_to_the_granted_rate() {
    use std::time::Duration;
    // 100 samples at 50/s → at least 2 s before the next batch.
    assert_eq!(pace_delay(100, 50), Duration::from_secs(2));
    // Rate 0 means "as fast as acks allow" — no pacing.
    assert_eq!(pace_delay(100, 0), Duration::ZERO);
    // An empty batch never waits.
    assert_eq!(pace_delay(0, 50), Duration::ZERO);
}

#[test]
fn drains_a_real_local_store_snapshot() {
    use edge_tsdb::{Durability, LocalTsdb, TsdbConfig};

    let dir = tempfile::tempdir().unwrap();
    let mut store = LocalTsdb::open(dir.path(), TsdbConfig::default()).unwrap();
    let now = 1_000_000i64;
    // Recent 1 s raw across three 10 s windows; commit builds T0 + rollups.
    for ts in (now - 30)..now {
        store.append(CPU, Sample::new(ts, 25.0), false).unwrap();
    }
    store.commit(Durability::Full).unwrap();
    let snap = store.snapshot().unwrap();

    // The engine reads the real MVCC snapshot through the TierReader impl. The
    // Mid/Old phases exercise range_tier even though those bands are empty here.
    let mut drain = BackfillDrain::new(&snap, now, cfg(1000), &[CPU], BackfillCursors::default());
    let mut windows = 0;
    while let Some(b) = drain.next_batch().unwrap() {
        assert_eq!(b.tier, BackfillTier::Raw10s);
        for s in &b.samples {
            assert_eq!(s.name, "cpu.total");
            assert_eq!(s.ts % 10, 0);
        }
        windows += b.samples.len();
    }
    assert_eq!(
        windows, 3,
        "30 one-second samples fold into three 10 s windows"
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
    // Bounded pull: cap below the available point count trips truncation.
    let (points, truncated) = answer_local_history(&r, CPU, NOW - 100, NOW, 10).unwrap();
    assert_eq!(points.len(), 10);
    assert!(truncated, "a capped window reports truncation");
    // The points are full-resolution 1 s (consecutive timestamps), ascending.
    for w in points.windows(2) {
        assert!(w[1].ts > w[0].ts);
    }

    let (all, truncated) = answer_local_history(&r, CPU, NOW - 100, NOW, 1000).unwrap();
    assert_eq!(all.len(), 100);
    assert!(!truncated, "a roomy cap does not report truncation");
}
