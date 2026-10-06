//! The `(timestamp, value)` point that every numeric series stores.

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
