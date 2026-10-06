//! Wall-clock readings shared by every collector, reading a clock before the epoch as zero.

use std::time::{SystemTime, UNIX_EPOCH};

/// Now in whole seconds since the Unix epoch; a clock before the epoch reads as zero.
pub(crate) fn unix_now() -> i64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_secs() as i64)
        .unwrap_or(0)
}

/// Now in microseconds since the Unix epoch, clamped to zero below and `i64::MAX` above.
pub(crate) fn unix_micros() -> i64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| i64::try_from(d.as_micros()).unwrap_or(i64::MAX))
        .unwrap_or(0)
}

#[cfg(test)]
mod tests {
    use super::{unix_micros, unix_now};

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
