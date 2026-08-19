//! The walk: one rule re-run over one device's stored history, in paced chunks.
//!
//! A scan reads history a bounded number of stored readings at a time, stands
//! down between chunks, and records where it got to, so a busy host, a filling
//! disk, maintenance or a restart can stop it and it resumes without repeating
//! or skipping a finding. Each finding is stamped with the minute it happened
//! and carries the readings around it as evidence.

use std::collections::{BTreeMap, VecDeque};
use std::time::Instant;

use mesh_protocol::{AlertComparator, ThresholdRule};

use crate::ml::store_sink::DimReadings;

use super::super::evaluator::AlertEvaluator;
use super::super::sink::{AlertOrigin, AlertSink, EdgeAlert};
use super::{
    floor_to_minute, RetroBudget, RetroCursor, RetroError, RetroHistory, RetroPlan, RetroStats,
    RetroStep, EVIDENCE_READINGS, MICROS_PER_SEC, RETRO_BUCKET_SECS, RETRO_SEVERITY,
};

/// One rule being re-run over one device's history.
pub struct RetroScan {
    plan: RetroPlan,
    budget: RetroBudget,
    evaluator: AlertEvaluator,
    /// Findings before this minute were delivered by an earlier run of this
    /// scan; re-reading them rebuilds state without raising them twice.
    emit_from: i64,
    /// Where the next read starts. `None` means the oldest minute stored.
    next_read: Option<i64>,
    cursor: RetroCursor,
    /// The stretch of history this scan has found, once it has looked.
    scope: Option<(i64, i64)>,
    firing: bool,
    /// The rule's own readings, most recent last, for the evidence on a finding.
    recent: VecDeque<(i64, f64)>,
    stats: RetroStats,
}

/// What a chunk found, before it is dressed up as a [`RetroStep`].
enum Progress {
    More,
    Done,
    Empty,
}

impl RetroScan {
    /// A scan of the whole history the store holds.
    #[must_use]
    pub fn new(plan: RetroPlan, budget: RetroBudget) -> Self {
        Self::resume(plan, budget, RetroCursor::default())
    }

    /// A scan picking up where `cursor` left off.
    #[must_use]
    pub fn resume(plan: RetroPlan, budget: RetroBudget, cursor: RetroCursor) -> Self {
        let evaluator = AlertEvaluator::new(vec![plan.rule.clone()]);
        Self {
            emit_from: cursor.next_bucket().unwrap_or(i64::MIN),
            next_read: cursor
                .next_bucket()
                .map(|bucket| bucket.saturating_sub(plan.lookback_secs)),
            plan,
            budget,
            evaluator,
            cursor,
            scope: None,
            firing: false,
            recent: VecDeque::with_capacity(EVIDENCE_READINGS),
            stats: RetroStats::default(),
        }
    }

    /// Where the scan has got to, in a form worth persisting.
    #[must_use]
    pub fn cursor(&self) -> RetroCursor {
        self.cursor
    }

    /// What the scan has done so far.
    #[must_use]
    pub fn stats(&self) -> RetroStats {
        self.stats
    }

    /// The stretch of history the scan found, oldest and newest minute, or
    /// `None` while it has not found any.
    #[must_use]
    pub fn scope(&self) -> Option<(i64, i64)> {
        self.scope
    }

    /// Whether the exact rule version this scan started against is no longer
    /// installed. A scan that keeps going against a definition nobody is using
    /// any more delivers findings for a rule that does not exist.
    #[must_use]
    pub fn superseded_by(&self, installed: &[ThresholdRule]) -> bool {
        !installed.contains(&self.plan.rule)
    }

    /// Evaluate one budgeted chunk of history, raising whatever it finds into
    /// `sink` as of `now_micros`.
    ///
    /// The findings carry the minute they happened; `now_micros` is when the
    /// device is *raising* them, which is what the sink's hourly ceiling counts
    /// against — a scan spends the same allowance as any other producer.
    pub fn run_chunk<H: RetroHistory>(
        &mut self,
        history: &H,
        sink: &AlertSink,
        now_micros: i64,
    ) -> Result<RetroStep, RetroError> {
        let started = Instant::now();
        let progress = self.chunk(history, sink, now_micros);
        let busy = started.elapsed();
        self.stats.chunks += 1;
        self.stats.busy_micros = self
            .stats
            .busy_micros
            .saturating_add(i64::try_from(busy.as_micros()).unwrap_or(i64::MAX));
        Ok(match progress? {
            Progress::More => RetroStep::Yielded {
                stand_down: self.budget.stand_down(busy),
            },
            Progress::Done => RetroStep::Complete,
            Progress::Empty => RetroStep::NoHistory,
        })
    }

    /// One chunk's work, timed by the caller.
    fn chunk<H: RetroHistory>(
        &mut self,
        history: &H,
        sink: &AlertSink,
        now_micros: i64,
    ) -> Result<Progress, RetroError> {
        let Some((oldest, newest)) = self.history_span(history)? else {
            return Ok(Progress::Empty);
        };
        self.scope = Some((oldest, newest));

        // Onto the grid the store keeps: the window a resume re-reads is as long
        // as the rule's own memory, and a rule may remember a number of seconds
        // that is not a whole number of minutes. A read starting between two
        // stored minutes lines up with none of them and quietly finds nothing.
        let from = floor_to_minute(self.next_read.unwrap_or(oldest).max(oldest));
        if from > newest {
            return Ok(Progress::Done);
        }
        let to = from
            .saturating_add(self.chunk_span())
            .min(newest.saturating_add(RETRO_BUCKET_SECS));

        let rows = self.read(history, from, to)?;
        for bucket in (from..to).step_by(RETRO_BUCKET_SECS as usize) {
            match rows.get(&bucket) {
                Some(readings) => self.step(*readings, bucket, sink, now_micros),
                // A minute missing any reading the rule needs is a minute nobody
                // can say anything about — the device was off, or the vital was
                // not being produced. Whatever the rule was carrying is dropped
                // with it, so a breach is never read across a hole.
                None => self.restart(),
            }
        }

        self.next_read = Some(to);
        self.cursor = RetroCursor::at(to.max(self.emit_from));
        Ok(if to > newest {
            Progress::Done
        } else {
            Progress::More
        })
    }

    /// The stretch of history covering every series the rule reads. A rule with
    /// one side missing from the store has no history to be re-run over, rather
    /// than a shorter one.
    fn history_span<H: RetroHistory>(&self, history: &H) -> Result<Option<(i64, i64)>, RetroError> {
        let mut span: Option<(i64, i64)> = None;
        for &(series, _) in &self.plan.reads {
            let Some((oldest, newest)) = history.span(series)? else {
                return Ok(None);
            };
            span = Some(match span {
                Some((have_oldest, have_newest)) => {
                    (have_oldest.min(oldest), have_newest.max(newest))
                }
                None => (oldest, newest),
            });
        }
        Ok(span)
    }

    /// Read every series the rule needs over `from..to`, folded into one set of
    /// readings per minute.
    fn read<H: RetroHistory>(
        &mut self,
        history: &H,
        from: i64,
        to: i64,
    ) -> Result<BTreeMap<i64, DimReadings>, RetroError> {
        let mut rows: BTreeMap<i64, DimReadings> = BTreeMap::new();
        for &(series, stat) in &self.plan.reads {
            for bucket in history.buckets(series, from, to)? {
                self.stats.points_read += 1;
                rows.entry(bucket.bucket)
                    .or_default()
                    .set(series, stat.of(&bucket));
            }
        }
        // A minute that is missing one of the rule's sides cannot be evaluated,
        // and dropping it here is what turns it into the hole handled above.
        rows.retain(|_, readings| {
            self.plan
                .reads
                .iter()
                .all(|&(series, _)| readings.get(series).is_some())
        });
        Ok(rows)
    }

    /// Evaluate one reconstructed minute.
    fn step(&mut self, readings: DimReadings, bucket: i64, sink: &AlertSink, now_micros: i64) {
        self.stats.buckets_evaluated += 1;
        if let Some(value) = readings.of_metric(self.plan.metric) {
            if self.recent.len() == EVIDENCE_READINGS {
                self.recent.pop_front();
            }
            self.recent.push_back((bucket, value));
        }

        let firing = !self
            .evaluator
            .evaluate_readings(&readings, bucket)
            .is_empty();
        // Only the moment a rule *starts* firing is a finding. A ten-hour
        // episode is one thing that happened, not six hundred.
        if firing && !self.firing && bucket >= self.emit_from {
            sink.push(self.finding(bucket), now_micros);
            self.stats.findings += 1;
        }
        self.firing = firing;
    }

    /// Forget everything the rule was carrying, at a hole in history.
    fn restart(&mut self) {
        self.evaluator = AlertEvaluator::new(vec![self.plan.rule.clone()]);
        self.firing = false;
        self.recent.clear();
    }

    /// Minutes one chunk covers: the budgeted readings, split across the series
    /// the rule reads.
    fn chunk_span(&self) -> i64 {
        let per_series = (self.budget.chunk_points / self.plan.reads.len().max(1)).max(1);
        i64::try_from(per_series).unwrap_or(i64::MAX / RETRO_BUCKET_SECS) * RETRO_BUCKET_SECS
    }

    /// The finding for a rule that started firing at `bucket`.
    fn finding(&self, bucket: i64) -> EdgeAlert {
        EdgeAlert {
            rule_id: self.plan.rule.id.clone(),
            severity: RETRO_SEVERITY,
            ts_micros: bucket.saturating_mul(MICROS_PER_SEC),
            subject: self.plan.metric.to_string(),
            summary: self.summary(),
            evidence: self.evidence(bucket),
            origin: AlertOrigin::Backfilled,
        }
    }

    /// What the rule means, in the words the queue shows first.
    fn summary(&self) -> String {
        let rule = &self.plan.rule;
        let held = if rule.sustain_secs >= RETRO_BUCKET_SECS as u32 {
            format!(
                " for {} min",
                i64::from(rule.sustain_secs) / RETRO_BUCKET_SECS
            )
        } else {
            String::new()
        };
        format!(
            "{} {} {}{held}",
            self.plan.metric,
            relation(rule.comparator),
            rule.threshold
        )
    }

    /// The readings behind a finding, oldest first.
    fn evidence(&self, bucket: i64) -> Vec<String> {
        self.recent
            .iter()
            .map(|&(ts, value)| {
                let minutes = bucket.saturating_sub(ts) / RETRO_BUCKET_SECS;
                if minutes == 0 {
                    format!("as it fired: {} = {value:.2}", self.plan.metric)
                } else {
                    format!("{minutes} min earlier: {} = {value:.2}", self.plan.metric)
                }
            })
            .collect()
    }
}

/// A comparator in the words a summary reads in.
fn relation(comparator: AlertComparator) -> &'static str {
    match comparator {
        AlertComparator::Gt => "above",
        AlertComparator::Gte => "at or above",
        AlertComparator::Lt => "below",
        AlertComparator::Lte => "at or below",
        _ => "past",
    }
}
