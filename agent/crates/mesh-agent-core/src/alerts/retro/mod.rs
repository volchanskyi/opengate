//! Re-running a rule over the history the device already holds.
//!
//! When a rule reaches a machine for the first time, the useful question is not
//! only "is this happening now" but "has this been happening". The answer is on
//! the machine: the local store keeps a minute-by-minute rollup of every vital
//! going back as far as its cap allows, so a new rule can simply be evaluated
//! against it. Nothing is shipped anywhere to make that possible.
//!
//! Three things make it safe to run on a customer's endpoint.
//!
//! **A finding is stamped with the minute it happened.** A freeze from three
//! weeks ago stays three weeks old, so grouping folds a whole scan into one
//! incident instead of presenting a queue of things that all appear to be
//! happening at once.
//!
//! **A scan is bounded, resumable and paced.** It walks history in chunks of a
//! fixed number of stored readings, stands down between them for long enough to
//! keep its share of the machine small, and records where it got to — so it can
//! be stopped by a busy host, a filling disk, maintenance, or the agent
//! restarting, and pick up without repeating or skipping a finding.
//!
//! **A minute is only asked what a minute can answer.** The store keeps one
//! rollup per 60 s, so a rule about a shorter span cannot be re-run at all and
//! says so ([`RetroUnsupported`]) rather than being answered at the wrong
//! resolution. For the rest, the statistic read out of each minute is the one
//! the rule's own question needs: a rule that has to *stay* over a line reads
//! the minute's least favourable reading, so a finding means every second of it
//! was over; a rule that asks whether it was *ever* crossed reads the minute's
//! peak, which answers that exactly.
//!
//! This module holds what a scan is allowed to do — what history can answer
//! ([`RetroPlan`]), what a host will lend it ([`RetroBudget`], [`retro_hold`])
//! and where it got to ([`RetroCursor`]). The walk itself is [`RetroScan`].

use std::collections::BTreeMap;
use std::time::Duration;

use edge_tsdb::store::{Tier, TsdbSnapshot};
use edge_tsdb::{SeriesId, TsdbConfig};
use mesh_protocol::{
    canonical_rule_metric, AlertComparator, RulePredicate, ThresholdRule, MAX_RULE_TERMS,
};
use serde::{Deserialize, Serialize};

use crate::ml::store_sink::dim_series;

use super::evaluator::window_is_expressible;
use super::sink::AlertSeverity;

mod scan;

pub use scan::RetroScan;

/// The width of one stored rollup, in seconds — the finest question history can
/// answer.
pub const RETRO_BUCKET_SECS: i64 = 60;

/// Host CPU, in percent, at or below which the machine counts as quiet enough to
/// look backwards on. A retroactive scan is answering a question about the past;
/// it can always wait for the present to calm down.
pub const RETRO_IDLE_CPU_PERCENT: f32 = 40.0;

/// How much more free disk a scan insists on than the point at which the store
/// starts shrinking what it keeps. A scan competing for the last of a host's
/// disk with the eviction trying to free it helps nobody, so it stands down
/// first, with room to spare.
const RETRO_DISK_HEADROOM: f64 = 2.0;

/// Readings carried on a finding as the evidence behind it.
const EVIDENCE_READINGS: usize = 5;

const MICROS_PER_SEC: i64 = 1_000_000;

/// How bad a backfilled finding is presented as. The wire grammar carries no
/// severity — the catalogue's severity is applied centrally where the finding
/// becomes an incident — and a rule that just found itself matching this
/// machine's history is squarely something a person should look at.
const RETRO_SEVERITY: AlertSeverity = AlertSeverity::Warning;

/// A failure to read local history.
#[derive(Debug, thiserror::Error)]
#[non_exhaustive]
pub enum RetroError {
    /// The local store could not be read.
    #[error("reading local history failed: {0}")]
    History(String),
}

/// Why a rule cannot be re-run over stored history.
///
/// Every one of these is a rule that keeps working perfectly well live. Being
/// unable to answer a question about the past is not a broken rule, and it is
/// reported as its own answer rather than as a failed scan.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
#[non_exhaustive]
pub enum RetroUnsupported {
    /// The rule asks about a span shorter than one stored minute, which history
    /// simply does not record.
    FinerThanAMinute,
    /// The rule watches something the local store does not keep.
    MetricNotStored,
    /// Two of the rule's conditions read one series but need different
    /// reductions of it, and one minute cannot be both.
    ConflictingReadings,
    /// The rule's shape has no reading over a minute that means what the rule
    /// means — an unknown comparator or predicate, or more conditions than the
    /// grammar allows.
    ShapeNotReconstructible,
}

impl std::fmt::Display for RetroUnsupported {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        let reason = match self {
            Self::FinerThanAMinute => "asks about less than a minute",
            Self::MetricNotStored => "watches something the local store does not keep",
            Self::ConflictingReadings => "needs one series read two different ways",
            Self::ShapeNotReconstructible => "has a shape a stored minute cannot state",
        };
        f.write_str(reason)
    }
}

/// Which stored statistic of one minute answers a condition's question.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum BucketStat {
    /// The minute's lowest reading.
    Min,
    /// The minute's highest reading.
    Max,
    /// The mean of the minute's readings.
    Avg,
    /// The minute's final reading.
    Last,
}

impl BucketStat {
    fn of(self, bucket: &RetroBucket) -> f64 {
        match self {
            Self::Min => bucket.min,
            Self::Max => bucket.max,
            Self::Avg => bucket.avg,
            Self::Last => bucket.last,
        }
    }
}

/// One stored minute, as a scan reads it.
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct RetroBucket {
    /// Start of the minute, in Unix seconds.
    pub bucket: i64,
    /// Lowest reading in the minute.
    pub min: f64,
    /// Highest reading in the minute.
    pub max: f64,
    /// Mean of the readings in the minute.
    pub avg: f64,
    /// Final reading in the minute.
    pub last: f64,
}

/// The history a scan walks. Implemented over the local store's snapshot reads,
/// which see a consistent view and never block the sampler's writes.
pub trait RetroHistory {
    /// The oldest and newest stored minute for `series`, or `None` when the
    /// store holds none.
    fn span(&self, series: SeriesId) -> Result<Option<(i64, i64)>, RetroError>;

    /// The stored minutes for `series` in `start..end`, ascending.
    fn buckets(
        &self,
        series: SeriesId,
        start: i64,
        end: i64,
    ) -> Result<Vec<RetroBucket>, RetroError>;
}

impl RetroHistory for TsdbSnapshot {
    fn span(&self, series: SeriesId) -> Result<Option<(i64, i64)>, RetroError> {
        self.tier_span(series, Tier::T1)
            .map_err(|e| RetroError::History(e.to_string()))
    }

    fn buckets(
        &self,
        series: SeriesId,
        start: i64,
        end: i64,
    ) -> Result<Vec<RetroBucket>, RetroError> {
        let points = self
            .range_tier(series, Tier::T1, start, end)
            .map_err(|e| RetroError::History(e.to_string()))?;
        Ok(points
            .into_iter()
            .map(|p| RetroBucket {
                bucket: p.bucket,
                min: p.min,
                max: p.max,
                avg: p.avg,
                last: p.last,
            })
            .collect())
    }
}

/// What one rule needs from history, worked out once before any of it is read.
#[derive(Debug, Clone)]
pub struct RetroPlan {
    rule: ThresholdRule,
    /// Every series the rule reads, and how each minute of it reduces.
    reads: Vec<(SeriesId, BucketStat)>,
    /// The rule's own metric, canonically named: what its findings are about.
    metric: &'static str,
    /// How far before a resume point the scan has to re-read to rebuild the
    /// state a rule carries.
    lookback_secs: i64,
}

impl RetroPlan {
    /// Work out how `rule` reads against stored minutes, or why it cannot.
    pub fn for_rule(rule: &ThresholdRule) -> Result<Self, RetroUnsupported> {
        if rule.all.len() > MAX_RULE_TERMS {
            return Err(RetroUnsupported::ShapeNotReconstructible);
        }
        if finer_than_a_minute(rule.sustain_secs) {
            return Err(RetroUnsupported::FinerThanAMinute);
        }

        let sustained = rule.sustain_secs > 0;
        let mut by_series: BTreeMap<SeriesId, BucketStat> = BTreeMap::new();
        let mut widest_window = 0u32;
        let mut metric = None;

        let primary = (
            rule.metric.as_str(),
            rule.comparator,
            rule.predicate,
            rule.window_secs,
        );
        let extras = rule.all.iter().map(|term| {
            (
                term.metric.as_str(),
                term.comparator,
                term.predicate,
                term.window_secs,
            )
        });
        for (name, comparator, predicate, window_secs) in std::iter::once(primary).chain(extras) {
            let canonical = canonical_rule_metric(name).ok_or(RetroUnsupported::MetricNotStored)?;
            let series = dim_series(canonical).ok_or(RetroUnsupported::MetricNotStored)?;
            if !window_is_expressible(predicate, window_secs) {
                return Err(RetroUnsupported::ShapeNotReconstructible);
            }
            if finer_than_a_minute(window_secs) {
                return Err(RetroUnsupported::FinerThanAMinute);
            }
            let stat = bucket_stat(predicate, comparator, sustained)
                .ok_or(RetroUnsupported::ShapeNotReconstructible)?;
            match by_series.insert(series, stat) {
                Some(previous) if previous != stat => {
                    return Err(RetroUnsupported::ConflictingReadings)
                }
                _ => {}
            }
            widest_window = widest_window.max(window_secs);
            metric.get_or_insert(canonical);
        }

        let metric = metric.ok_or(RetroUnsupported::ShapeNotReconstructible)?;
        Ok(Self {
            rule: rule.clone(),
            reads: by_series.into_iter().collect(),
            metric,
            // Everything a rule remembers is bounded by how long it has to hold
            // a breach plus how far its window reaches back, so re-reading that
            // much rebuilds its state exactly. The extra minute covers the
            // bucket the state was decided on.
            lookback_secs: i64::from(rule.sustain_secs)
                + i64::from(widest_window)
                + RETRO_BUCKET_SECS,
        })
    }
}

/// Whether a declared span is non-zero but shorter than one stored minute.
fn finer_than_a_minute(secs: u32) -> bool {
    secs > 0 && i64::from(secs) < RETRO_BUCKET_SECS
}

/// The stored minute `ts` falls in — the store keys its rollups by the start of
/// the minute, so a read has to start on one to line up with anything.
fn floor_to_minute(ts: i64) -> i64 {
    ts.div_euclid(RETRO_BUCKET_SECS)
        .saturating_mul(RETRO_BUCKET_SECS)
}

/// Which reading of a minute means what the condition means.
///
/// A windowed predicate reconstructs exactly: the largest reading in a window is
/// the largest of its minutes' largest, and the mean of a window is the mean of
/// its minutes' means. An instantaneous one cannot, so it is read in the
/// direction that cannot invent a finding: a rule that has to *stay* over its
/// line reads the minute's least favourable reading, so a finding means the
/// whole minute was over it — while a rule with no sustain is asking whether the
/// line was *ever* crossed, which the minute's peak answers exactly.
fn bucket_stat(
    predicate: RulePredicate,
    comparator: AlertComparator,
    sustained: bool,
) -> Option<BucketStat> {
    match predicate {
        RulePredicate::Instant => match (comparator, sustained) {
            (AlertComparator::Gt | AlertComparator::Gte, true) => Some(BucketStat::Min),
            (AlertComparator::Gt | AlertComparator::Gte, false) => Some(BucketStat::Max),
            (AlertComparator::Lt | AlertComparator::Lte, true) => Some(BucketStat::Max),
            (AlertComparator::Lt | AlertComparator::Lte, false) => Some(BucketStat::Min),
            // A comparator this build does not understand has no reading that
            // means what it means.
            _ => None,
        },
        RulePredicate::WindowMax => Some(BucketStat::Max),
        RulePredicate::WindowMean => Some(BucketStat::Avg),
        RulePredicate::Rate => Some(BucketStat::Last),
        _ => None,
    }
}

/// How much of the machine a scan may take, and how much it may read at a time.
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct RetroBudget {
    /// The most stored readings one chunk may read.
    pub chunk_points: usize,
    /// The share of wall-clock the scan may occupy, in percent.
    pub duty_percent: f64,
}

impl RetroBudget {
    /// The shortest stand-down between chunks. A chunk that costs almost nothing
    /// still stands down, or a scan over an empty stretch of history becomes a
    /// spin.
    pub const MIN_STAND_DOWN: Duration = Duration::from_millis(250);

    /// The longest stand-down, which only a chunk far larger than any budget
    /// allows could reach.
    const MAX_STAND_DOWN_SECS: f64 = 3_600.0;

    /// A budget reading at most `chunk_points` stored readings per chunk and
    /// taking at most `duty_percent` of the machine.
    #[must_use]
    pub fn new(chunk_points: usize, duty_percent: f64) -> Self {
        Self {
            chunk_points: chunk_points.max(1),
            duty_percent: duty_percent.clamp(0.1, 100.0),
        }
    }

    /// How long to stand down after a chunk that cost `busy`, so that the work
    /// and the rest together stay inside the budgeted share.
    #[must_use]
    pub fn stand_down(&self, busy: Duration) -> Duration {
        let factor = (100.0 / self.duty_percent) - 1.0;
        let rest = (busy.as_secs_f64() * factor).min(Self::MAX_STAND_DOWN_SECS);
        Duration::from_secs_f64(rest).max(Self::MIN_STAND_DOWN)
    }
}

impl Default for RetroBudget {
    /// Reads about an hour of one series per chunk, at two percent of the
    /// machine — comfortably inside the agent's own budget, with the rest of it
    /// left for the work the customer notices.
    fn default() -> Self {
        Self::new(4_096, 2.0)
    }
}

/// What a scan has done. Reported so a scan that is costing a machine something
/// shows up as a number rather than as a mystery.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
#[non_exhaustive]
pub struct RetroStats {
    /// Chunks run.
    pub chunks: u64,
    /// Stored readings read.
    pub points_read: u64,
    /// Minutes actually evaluated (a minute missing any of the rule's readings
    /// is not one of them).
    pub buckets_evaluated: u64,
    /// Findings handed to the alert sink.
    pub findings: u64,
    /// Processor time spent inside chunks, in microseconds.
    pub busy_micros: i64,
}

/// How far a scan has got, in a form that survives the agent restarting.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct RetroCursor {
    /// The oldest minute not yet evaluated and delivered. `None` means the
    /// oldest minute the store holds.
    #[serde(default)]
    next_bucket: Option<i64>,
}

impl RetroCursor {
    /// A cursor resuming at `bucket`.
    #[must_use]
    pub fn at(bucket: i64) -> Self {
        Self {
            next_bucket: Some(bucket),
        }
    }

    /// The oldest minute not yet evaluated and delivered.
    #[must_use]
    pub fn next_bucket(&self) -> Option<i64> {
        self.next_bucket
    }
}

/// What one chunk of a scan concluded.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
#[non_exhaustive]
pub enum RetroStep {
    /// A chunk was evaluated. Stand down for this long, then run another.
    Yielded {
        /// How long to leave the machine alone before the next chunk.
        stand_down: Duration,
    },
    /// Every minute the store holds has been evaluated.
    Complete,
    /// The store holds no history for this rule — which is not the same as
    /// having looked and found nothing.
    NoHistory,
}

/// Everything about the machine that decides whether a scan may run now.
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct RetroConditions {
    /// Whether an administrator has the device in maintenance.
    pub in_maintenance: bool,
    /// The most recent host CPU reading, if the sampler has taken one.
    pub cpu_percent: Option<f32>,
    /// Free bytes on the host disk, if anything has reported it.
    pub host_free_bytes: Option<u64>,
}

/// Why a scan is not running right now.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
#[non_exhaustive]
pub enum RetroHold {
    /// An administrator is working on the machine.
    Maintenance,
    /// The machine is busy doing what its user asked it to.
    Busy,
    /// The host disk is filling.
    DiskPressure,
}

/// Whether a scan has to stand down, and why. `None` means it may run.
///
/// The disk threshold is derived from the store's own configuration, so the scan
/// always stands down while the store is still keeping everything it was
/// keeping — the two cannot drift apart.
#[must_use]
pub fn retro_hold(conditions: &RetroConditions, store: TsdbConfig) -> Option<RetroHold> {
    if conditions.in_maintenance {
        return Some(RetroHold::Maintenance);
    }
    // A machine that has reported no load is not assumed to be idle.
    match conditions.cpu_percent {
        Some(cpu) if cpu <= RETRO_IDLE_CPU_PERCENT => {}
        _ => return Some(RetroHold::Busy),
    }
    if conditions
        .host_free_bytes
        .is_some_and(|free| free < disk_floor(store))
    {
        return Some(RetroHold::DiskPressure);
    }
    None
}

/// Free host bytes below which a scan stands down: comfortably above the point
/// where the store starts trading away history for space.
fn disk_floor(store: TsdbConfig) -> u64 {
    if store.host_free_fraction > 0.0 {
        let engages_at = store.cap_bytes as f64 / store.host_free_fraction;
        let floor = engages_at * RETRO_DISK_HEADROOM;
        if floor >= u64::MAX as f64 {
            u64::MAX
        } else {
            floor as u64
        }
    } else {
        // With the backoff switched off the store will grow to its cap
        // regardless, so the scan stands down once free space is inside it.
        store.cap_bytes
    }
}
