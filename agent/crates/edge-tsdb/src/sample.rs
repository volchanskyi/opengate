//! The one sample shape the whole spike operates on.
//!
//! WS-2 produces multi-dimensional host snapshots (`MetricSample`). The storage
//! engine sees each dimension as an independent numeric series of
//! `(timestamp, value)` points, which is the unit the Gorilla layer compresses.

/// Identifier for a numeric series (one dimension of one host, e.g. `cpu.total`).
pub type SeriesId = u32;

/// A single point in a series: a whole-second Unix timestamp and a float value.
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct Sample {
    /// Whole-second Unix timestamp. May step backward (NTP correction).
    pub ts: i64,
    /// The measured value.
    pub value: f64,
}

impl Sample {
    /// Construct a sample.
    #[must_use]
    pub fn new(ts: i64, value: f64) -> Self {
        Self { ts, value }
    }
}
