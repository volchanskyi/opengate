//! Endpoint log-rate feature extraction for the Edge-Sentinel ensemble.
//!
//! Turns a window of normalized log entries into a fixed-width numeric vector —
//! per-level counts, the counts of the top emitting units by *rank* (never by
//! name, so cardinality is bounded), and total volume. The vector rides the same
//! anomaly ensemble as host metrics; no message content ever becomes a feature.

use std::collections::HashMap;

/// Number of tracked severity levels (Error, Warn, Info, Debug, Trace).
pub const LEVEL_DIMS: usize = 5;
/// Number of top emitting units tracked, by descending count rank.
pub const TOP_UNITS: usize = 3;
/// Width of a log-rate feature vector: per-level counts, top-unit counts, volume.
pub const LOG_RATE_DIMS: usize = LEVEL_DIMS + TOP_UNITS + 1;

/// Severity level of a normalized log entry, most severe first.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
#[non_exhaustive]
pub enum LogLevel {
    /// `ERROR`.
    Error,
    /// `WARN`.
    Warn,
    /// `INFO`.
    Info,
    /// `DEBUG`.
    Debug,
    /// `TRACE`.
    Trace,
}

impl LogLevel {
    /// Map a normalized level label (as carried by `LogEntry.level`) to a level.
    /// Unknown labels return `None` so callers can drop non-conforming lines.
    pub fn from_label(label: &str) -> Option<Self> {
        match label {
            "ERROR" => Some(Self::Error),
            "WARN" => Some(Self::Warn),
            "INFO" => Some(Self::Info),
            "DEBUG" => Some(Self::Debug),
            "TRACE" => Some(Self::Trace),
            _ => None,
        }
    }

    /// Stable feature-vector slot for this level.
    fn index(self) -> usize {
        match self {
            Self::Error => 0,
            Self::Warn => 1,
            Self::Info => 2,
            Self::Debug => 3,
            Self::Trace => 4,
        }
    }
}

/// Accumulates log observations over one window into a fixed feature vector.
///
/// Accumulation (`observe`) may allocate for a newly seen unit name; producing
/// the vector (`finish`) is allocation-free — the top-`TOP_UNITS` selection runs
/// over a fixed-size scratch array.
#[derive(Debug, Default, Clone)]
pub struct LogRateExtractor {
    level_counts: [u32; LEVEL_DIMS],
    unit_counts: HashMap<String, u32>,
    total: u32,
}

impl LogRateExtractor {
    /// Create an empty extractor for a new window.
    pub fn new() -> Self {
        Self::default()
    }

    /// Record one entry at `level` emitted by `unit`. An empty unit (no source
    /// identifier) still counts toward the level and volume totals but never
    /// occupies a unit-rank slot.
    pub fn observe(&mut self, level: LogLevel, unit: &str) {
        self.level_counts[level.index()] += 1;
        if !unit.is_empty() {
            *self.unit_counts.entry(unit.to_string()).or_insert(0) += 1;
        }
        self.total += 1;
    }

    /// Record one entry from its normalized level label, skipping unknown levels.
    /// Returns whether the label was recognized and recorded.
    pub fn observe_label(&mut self, level_label: &str, unit: &str) -> bool {
        match LogLevel::from_label(level_label) {
            Some(level) => {
                self.observe(level, unit);
                true
            }
            None => false,
        }
    }

    /// Total number of observations recorded in this window.
    pub fn observed(&self) -> u32 {
        self.total
    }

    /// Produce the fixed-width feature vector for this window:
    /// `[error, warn, info, debug, trace, unit_rank1, …, unit_rankN, total]`.
    pub fn finish(&self) -> [f32; LOG_RATE_DIMS] {
        let mut out = [0.0f32; LOG_RATE_DIMS];
        for (slot, &count) in self.level_counts.iter().enumerate() {
            out[slot] = count as f32;
        }

        let mut top = [0u32; TOP_UNITS];
        for &count in self.unit_counts.values() {
            insert_top(&mut top, count);
        }
        for (rank, &count) in top.iter().enumerate() {
            out[LEVEL_DIMS + rank] = count as f32;
        }

        out[LOG_RATE_DIMS - 1] = self.total as f32;
        out
    }
}

/// Keep `top` as the descending list of the largest counts seen so far, in place.
fn insert_top(top: &mut [u32; TOP_UNITS], count: u32) {
    for slot in 0..TOP_UNITS {
        if count > top[slot] {
            let mut k = TOP_UNITS - 1;
            while k > slot {
                top[k] = top[k - 1];
                k -= 1;
            }
            top[slot] = count;
            return;
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::ml::ensemble::EdgeMlEnsemble;

    #[test]
    fn from_label_maps_every_level_and_rejects_unknown() {
        assert_eq!(LogLevel::from_label("ERROR"), Some(LogLevel::Error));
        assert_eq!(LogLevel::from_label("WARN"), Some(LogLevel::Warn));
        assert_eq!(LogLevel::from_label("INFO"), Some(LogLevel::Info));
        assert_eq!(LogLevel::from_label("DEBUG"), Some(LogLevel::Debug));
        assert_eq!(LogLevel::from_label("TRACE"), Some(LogLevel::Trace));
        assert_eq!(LogLevel::from_label("FATAL"), None);
        assert_eq!(LogLevel::from_label("info"), None); // case-sensitive
    }

    #[test]
    fn level_counts_land_in_distinct_slots() {
        let mut ex = LogRateExtractor::new();
        ex.observe(LogLevel::Error, "a");
        ex.observe(LogLevel::Error, "a");
        ex.observe(LogLevel::Warn, "a");
        ex.observe(LogLevel::Info, "a");
        ex.observe(LogLevel::Debug, "a");
        ex.observe(LogLevel::Trace, "a");
        let v = ex.finish();
        assert_eq!(v[0], 2.0, "error slot");
        assert_eq!(v[1], 1.0, "warn slot");
        assert_eq!(v[2], 1.0, "info slot");
        assert_eq!(v[3], 1.0, "debug slot");
        assert_eq!(v[4], 1.0, "trace slot");
        assert_eq!(v[LOG_RATE_DIMS - 1], 6.0, "total volume");
        assert_eq!(ex.observed(), 6);
    }

    #[test]
    fn top_units_are_ranked_by_descending_count() {
        let mut ex = LogRateExtractor::new();
        // unit "big" x5, "mid" x3, "small" x2, "tiny" x1 → ranks [5,3,2].
        for _ in 0..5 {
            ex.observe(LogLevel::Info, "big");
        }
        for _ in 0..3 {
            ex.observe(LogLevel::Info, "mid");
        }
        for _ in 0..2 {
            ex.observe(LogLevel::Info, "small");
        }
        ex.observe(LogLevel::Info, "tiny");
        let v = ex.finish();
        assert_eq!(v[LEVEL_DIMS], 5.0, "rank 1 = busiest unit count");
        assert_eq!(v[LEVEL_DIMS + 1], 3.0, "rank 2");
        assert_eq!(v[LEVEL_DIMS + 2], 2.0, "rank 3 (tiny=1 falls off)");
        assert_eq!(v[LOG_RATE_DIMS - 1], 11.0, "total counts every entry");
    }

    #[test]
    fn empty_unit_counts_volume_but_takes_no_rank_slot() {
        let mut ex = LogRateExtractor::new();
        ex.observe(LogLevel::Error, "");
        ex.observe(LogLevel::Error, "");
        let v = ex.finish();
        assert_eq!(v[0], 2.0, "level still counts");
        assert_eq!(v[LEVEL_DIMS], 0.0, "no unit occupies a rank slot");
        assert_eq!(v[LOG_RATE_DIMS - 1], 2.0, "volume still counts");
    }

    #[test]
    fn observe_label_skips_unknown_levels() {
        let mut ex = LogRateExtractor::new();
        assert!(ex.observe_label("ERROR", "svc"));
        assert!(!ex.observe_label("NOTALEVEL", "svc"));
        assert_eq!(ex.observed(), 1, "only the recognized entry is recorded");
        assert_eq!(ex.finish()[0], 1.0);
    }

    #[test]
    fn log_rate_dims_is_levels_plus_top_units_plus_volume() {
        // Pin the vector width so a mutated dimension formula is caught even
        // though every slot index tracks the same constant.
        assert_eq!(LOG_RATE_DIMS, 9);
        assert_eq!(LEVEL_DIMS + TOP_UNITS + 1, LOG_RATE_DIMS);
    }

    #[test]
    fn insert_top_keeps_descending_order_regardless_of_arrival() {
        let mut top = [0u32; TOP_UNITS];
        for c in [2u32, 9, 1, 5, 4] {
            insert_top(&mut top, c);
        }
        assert_eq!(top, [9, 5, 4], "top-3 largest in descending order");
    }

    #[test]
    fn insert_top_shifts_smaller_entries_down() {
        // Ascending inserts force a shift on every step; without the shift loop
        // the lower slots keep stale values instead of the displaced ones.
        let mut top = [0u32; TOP_UNITS];
        for c in [1u32, 2, 3, 4, 5] {
            insert_top(&mut top, c);
        }
        assert_eq!(top, [5, 4, 3], "each new max shifts the rest down");
    }

    // WS-2 ensemble wiring: a synthetic error burst raises the log-rate vector
    // far enough that the trained ensemble flags it, while a flat window stays
    // below the boundary.
    #[test]
    fn error_burst_window_trips_the_ensemble() {
        // Baseline windows: low, quiet INFO traffic with mild variation.
        let mut baseline: Vec<[f32; LOG_RATE_DIMS]> = Vec::new();
        for i in 0..40 {
            let mut ex = LogRateExtractor::new();
            let info = 3 + (i % 3);
            for _ in 0..info {
                ex.observe(LogLevel::Info, "app");
            }
            baseline.push(ex.finish());
        }
        let ensemble = EdgeMlEnsemble::<LOG_RATE_DIMS>::train_staggered(&baseline, 3, 8)
            .expect("train ensemble");

        // A quiet window resembling the baseline is not anomalous.
        let mut calm = LogRateExtractor::new();
        for _ in 0..4 {
            calm.observe(LogLevel::Info, "app");
        }
        assert!(
            !ensemble.is_anomaly(&calm.finish()),
            "calm window must be normal"
        );

        // An error burst is far outside the baseline and must be flagged.
        let mut burst = LogRateExtractor::new();
        for _ in 0..200 {
            burst.observe(LogLevel::Error, "app");
        }
        assert!(
            ensemble.is_anomaly(&burst.finish()),
            "error burst must be anomalous"
        );
    }
}
