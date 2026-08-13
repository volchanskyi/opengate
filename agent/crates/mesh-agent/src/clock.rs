//! Reading the wall clock, once.
//!
//! Every collector in the agent stamps what it produces with the time it
//! happened, and every one of them wants the same thing from a clock that has
//! gone backwards: a usable instant rather than a negative one. Stating that
//! once means a host whose clock predates the epoch behaves the same way in the
//! sampler, the log watch, the backfill coordinator and a retroactive scan.

use std::time::{SystemTime, UNIX_EPOCH};

/// Now, in whole seconds since the Unix epoch. A clock before the epoch reads as
/// zero rather than as a negative instant.
pub(crate) fn unix_now() -> i64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_secs() as i64)
        .unwrap_or(0)
}

/// Now, in microseconds since the Unix epoch. Clamped at both ends: a clock
/// before the epoch reads as zero, and one far enough past it to overflow reads
/// as the largest instant this can express.
pub(crate) fn unix_micros() -> i64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| i64::try_from(d.as_micros()).unwrap_or(i64::MAX))
        .unwrap_or(0)
}

#[cfg(test)]
mod tests {
    use super::{unix_micros, unix_now};

    /// Both readings are of the same clock, so they agree about what second it
    /// is — a collector stamping seconds and one stamping microseconds must not
    /// disagree about when something happened.
    #[test]
    fn the_two_readings_agree_about_the_second() {
        let secs = unix_now();
        let micros = unix_micros();

        assert!(secs > 1_700_000_000, "the clock is past 2023");
        assert!(
            (micros / 1_000_000 - secs).abs() <= 1,
            "seconds {secs} and microseconds {micros} came from different clocks"
        );
    }
}
