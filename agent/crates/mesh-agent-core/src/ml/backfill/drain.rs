//! The tier walk: recent-first, then oldest-first per tier on its own grid, skipping confirmed
//! spans and running minutes, never one time at two resolutions.

use edge_tsdb::confirmed::ConfirmedRanges;
use edge_tsdb::{SeriesId, TsdbError};
use mesh_protocol::{BackfillSample, BackfillTier};

use super::{BackfillConfig, TierReader};

mod read;

/// A batch of pre-rolled samples for one tier, ascending by timestamp, ending at bucket `cursor`.
/// Its ack confirms `[start, end)`: the span the drain scanned for it, empty stretches included.
#[derive(Debug, Clone, PartialEq)]
pub struct PlannedBatch {
    pub tier: BackfillTier,
    pub samples: Vec<BackfillSample>,
    pub cursor: i64,
    pub start: i64,
    pub end: i64,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
enum Phase {
    #[default]
    Recent,
    Mid,
    Old,
    Done,
}

impl Phase {
    fn tier(self) -> Option<BackfillTier> {
        match self {
            Phase::Recent => Some(BackfillTier::Recent60s),
            Phase::Mid => Some(BackfillTier::Rollup1m),
            Phase::Old => Some(BackfillTier::Rollup1h),
            Phase::Done => None,
        }
    }

    fn next(self) -> Phase {
        match self {
            Phase::Recent => Phase::Mid,
            Phase::Mid => Phase::Old,
            Phase::Old | Phase::Done => Phase::Done,
        }
    }
}

/// Where a drain stands: its age band and the next bucket it reads, `None` at the band's floor.
/// A catch-up keeps it from batch to batch, so each stretch is read once per session.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub struct DrainPosition {
    phase: Phase,
    pos: Option<i64>,
}

/// A recent-first drain over a [`TierReader`]; `next_batch` yields batches until `None`.
pub struct BackfillDrain<'a, R: TierReader> {
    reader: &'a R,
    now: i64,
    cfg: BackfillConfig,
    series: &'a [SeriesId],
    confirmed: &'a ConfirmedRanges,
    at: DrainPosition,
    /// Empty time ending after this stays owed: a reading not yet committed is newer.
    settled: i64,
    empty: Vec<(i64, i64)>,
}

impl<'a, R: TierReader> BackfillDrain<'a, R> {
    /// Starts a drain over everything the server has not confirmed.
    pub fn new(
        reader: &'a R,
        now: i64,
        cfg: BackfillConfig,
        series: &'a [SeriesId],
        confirmed: &'a ConfirmedRanges,
    ) -> Self {
        Self {
            reader,
            now,
            cfg,
            series,
            confirmed,
            at: DrainPosition::default(),
            settled: i64::MIN,
            empty: Vec::new(),
        }
    }

    /// Carries on from `at`, where an earlier drain of the same catch-up stopped.
    #[must_use]
    pub fn resume(mut self, at: DrainPosition) -> Self {
        self.at = at;
        self
    }

    /// Records each stretch it reads with nothing left to send, cut at `settled`, the newest
    /// committed minute; [`Self::take_empty`] hands them over to be confirmed.
    #[must_use]
    pub fn marking_empty_until(mut self, settled: i64) -> Self {
        self.settled = settled;
        self
    }

    /// Where the next batch starts.
    #[must_use]
    pub fn position(&self) -> DrainPosition {
        self.at
    }

    /// The empty stretches found since the last call, coalesced and ascending within a band.
    pub fn take_empty(&mut self) -> Vec<(i64, i64)> {
        std::mem::take(&mut self.empty)
    }

    fn mark_empty(&mut self, start: i64, end: i64) {
        let end = end.min(self.settled);
        if start >= end {
            return;
        }
        match self.empty.last_mut() {
            Some(last) if last.1 == start => last.1 = end,
            _ => self.empty.push((start, end)),
        }
    }

    /// Produces the next batch, or `None` when drained; the caller records each confirmation.
    pub fn next_batch(&mut self) -> Result<Option<PlannedBatch>, TsdbError> {
        loop {
            let Some(tier) = self.at.phase.tier() else {
                return Ok(None);
            };
            let (band_lo, band_hi, step) = self.band(self.at.phase);

            // Every position sits on the band's grid, so no bucket falls between two slices.
            let mut pos = self.at.pos.unwrap_or_else(|| ceil_to(band_lo, step));
            // A confirmed span is skipped whole, to the first bucket at or after its end.
            while let Some(end) = self.confirmed.span_end_at(pos) {
                pos = ceil_to(end, step);
            }
            self.at.pos = Some(pos);

            if pos > band_hi {
                self.advance_phase();
                continue;
            }

            let read_end = (pos + step * self.buckets_per_batch()).min(band_hi);
            let (buckets, owed_elsewhere) = self.read_buckets(tier, step, pos, read_end)?;

            if buckets.is_empty() {
                // A slice with nothing left to send is done; a reading the band cannot send and
                // the server has not confirmed keeps it owed.
                if !owed_elsewhere {
                    let end = (read_end + step).min(self.band_end(self.at.phase));
                    self.mark_empty(pos, end);
                }
                if read_end >= band_hi {
                    self.advance_phase();
                } else {
                    self.at.pos = Some(read_end + step);
                }
                continue;
            }

            let cursor = *buckets.keys().next_back().expect("non-empty");
            self.at.pos = Some(cursor + step);
            return Ok(Some(PlannedBatch {
                tier,
                samples: read::named_samples(buckets),
                cursor,
                start: pos.min(cursor),
                end: cursor + step,
            }));
        }
    }

    fn advance_phase(&mut self) {
        self.at = DrainPosition {
            phase: self.at.phase.next(),
            pos: None,
        };
    }
}

/// The first multiple of `step` at or after `ts`.
fn ceil_to(ts: i64, step: i64) -> i64 {
    let floor = ts.div_euclid(step) * step;
    if floor == ts {
        ts
    } else {
        floor + step
    }
}
