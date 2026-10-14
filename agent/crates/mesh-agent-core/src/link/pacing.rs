//! The host's send limit on the link and on the relay socket, set at run time: what the path
//! delivers in one round trip, plus a cushion of unsent bytes.

use std::collections::VecDeque;
use std::time::{Duration, Instant};

use mesh_protocol::direct::handoff::WINDOW_BYTES;

/// Bytes kept ready beyond what the path carries in a round trip, so it never idles.
pub const UNSENT_CUSHION: usize = 64 * 1024;

/// The limit never falls below this.
pub const FLOOR: usize = 128 * 1024;

/// The limit never rises past the window: the hand-off holds no more unconfirmed.
pub const CEILING: usize = WINDOW_BYTES;

/// How the limit starts, before any round trip or delivery is measured.
pub const INITIAL: usize = 256 * 1024;

/// How often the limit is recomputed.
pub const INTERVAL: Duration = Duration::from_millis(250);

/// How long a round-trip sample taken with the sender unheld counts towards the path's own delay.
const RTT_MEMORY: Duration = Duration::from_secs(10);

/// How far back the delivery rate reads: confirmations arrive in steps of up to a quarter window,
/// four a second when the path is slow.
const RATE_SPAN: Duration = Duration::from_secs(1);

/// The path's bandwidth-delay product plus [`UNSENT_CUSHION`]; the delay is the least round trip
/// sampled with the sender unheld, since a sample taken while it is held carries its own queue.
#[derive(Debug)]
pub struct SendLimit {
    limit: usize,
    unheld_rtts: VecDeque<(Instant, Duration)>,
    least_rtt: Option<Duration>,
    confirmations: VecDeque<(Instant, usize)>,
    interval_start: Instant,
    last_rate: Option<f64>,
    blocked_for: Duration,
}

impl SendLimit {
    /// Returns a limit at [`INITIAL`], measuring from `now`.
    pub fn new(now: Instant) -> Self {
        Self {
            limit: INITIAL,
            unheld_rtts: VecDeque::new(),
            least_rtt: None,
            confirmations: VecDeque::new(),
            interval_start: now,
            last_rate: None,
            blocked_for: Duration::ZERO,
        }
    }

    /// The bytes the sender may have outstanding now.
    pub fn current(&self) -> usize {
        self.limit
    }

    /// The peer confirmed `bytes` of link frames at `now`.
    pub fn confirmed(&mut self, bytes: usize, now: Instant) {
        self.confirmations.push_back((now, bytes));
    }

    /// A round-trip sample from the link's statistics, counted as the path's own delay only while
    /// the sender has not waited at the limit this interval.
    pub fn round_trip(&mut self, rtt: Duration, now: Instant) {
        if rtt.is_zero() {
            return;
        }
        self.least_rtt = Some(self.least_rtt.map_or(rtt, |least| least.min(rtt)));
        if self.blocked_for.is_zero() {
            self.unheld_rtts.push_back((now, rtt));
        }
    }

    /// The sender waited this long at the limit with data to send.
    pub fn blocked(&mut self, waited: Duration) {
        self.blocked_for += waited;
    }

    /// Recomputes the limit once an interval has passed, returning whether it was.
    pub fn recompute(&mut self, now: Instant) -> bool {
        let elapsed = now.duration_since(self.interval_start);
        if elapsed < INTERVAL {
            return false;
        }
        forget_before(&mut self.unheld_rtts, now, RTT_MEMORY);
        forget_before(&mut self.confirmations, now, RATE_SPAN);
        let path_delay = self
            .unheld_rtts
            .iter()
            .map(|(_, rtt)| *rtt)
            .min()
            .or(self.least_rtt);
        let mut next = match (self.rate(), path_delay) {
            (Some(rate), Some(rtt)) => (rate * rtt.as_secs_f64()) as usize + UNSENT_CUSHION,
            _ => self.limit,
        };
        // Held at the limit the whole interval while delivery grew: a faster path may be there.
        let held = self.blocked_for * 2 >= elapsed;
        let rate = self.rate().unwrap_or(0.0);
        let grew = self.last_rate.is_none_or(|last| rate > last * 1.05);
        if held && grew {
            next = next.max(self.limit + self.limit / 4);
        }
        self.limit = next.clamp(FLOOR, CEILING);
        self.last_rate = Some(rate);
        self.blocked_for = Duration::ZERO;
        self.interval_start = now;
        true
    }

    /// Bytes a second confirmed over the last [`RATE_SPAN`], from the first confirmation in it to the
    /// last; zero with none, unknown with one.
    fn rate(&self) -> Option<f64> {
        let (Some(&(first, _)), Some(&(last, _))) =
            (self.confirmations.front(), self.confirmations.back())
        else {
            return Some(0.0);
        };
        let span = last.duration_since(first);
        if span.is_zero() {
            return None;
        }
        let bytes: usize = self.confirmations.iter().skip(1).map(|(_, b)| b).sum();
        Some(bytes as f64 / span.as_secs_f64())
    }
}

/// Drops the entries older than `span`.
fn forget_before<T>(entries: &mut VecDeque<(Instant, T)>, now: Instant, span: Duration) {
    while entries
        .front()
        .is_some_and(|(at, _)| now.duration_since(*at) > span)
    {
        entries.pop_front();
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    /// One interval of a path delivering `bytes_per_sec` with a `rtt_ms` round trip, confirmed in
    /// five steps through the interval.
    fn interval(
        limit: &mut SendLimit,
        now: &mut Instant,
        bytes_per_sec: usize,
        rtt_ms: u64,
        held: bool,
    ) {
        if held {
            limit.blocked(INTERVAL);
        }
        limit.round_trip(Duration::from_millis(rtt_ms), *now);
        for _ in 0..5 {
            *now += INTERVAL / 5;
            limit.confirmed(bytes_per_sec / 20, *now);
        }
        assert!(limit.recompute(*now));
    }

    #[test]
    fn the_limit_is_what_the_path_delivers_in_a_round_trip_plus_the_cushion() {
        let mut now = Instant::now();
        let mut limit = SendLimit::new(now);
        // 100 Mbit/s with a 40 ms round trip carries 500 000 bytes in flight.
        interval(&mut limit, &mut now, 12_500_000, 40, false);
        assert_eq!(limit.current(), 500_000 + UNSENT_CUSHION);
    }

    #[test]
    fn a_slow_path_keeps_the_limit_at_its_floor() {
        let mut now = Instant::now();
        let mut limit = SendLimit::new(now);
        // 10 Mbit/s at 40 ms is 50 000 bytes in flight, under the floor with its cushion.
        interval(&mut limit, &mut now, 1_250_000, 40, false);
        assert_eq!(limit.current(), FLOOR);
    }

    #[test]
    fn confirmations_in_whole_steps_still_measure_the_path_rate() {
        let mut now = Instant::now();
        let mut limit = SendLimit::new(now);
        limit.round_trip(Duration::from_millis(40), now);
        // 80 Mbit/s confirmed a mebibyte at a time: a step every 104.9 ms, two or three a tick.
        let step = Duration::from_secs_f64(1_048_576.0 / 10_000_000.0);
        let mut next_step = now + step;
        let want = 400_000 + UNSENT_CUSHION;
        for tick in 1..=24 {
            let tick_end = now + INTERVAL;
            while next_step <= tick_end {
                limit.confirmed(1_048_576, next_step);
                next_step += step;
            }
            now = tick_end;
            limit.blocked(INTERVAL);
            assert!(limit.recompute(now));
            if tick > 4 {
                let got = limit.current();
                assert!(
                    got.abs_diff(want) * 100 <= want * 3,
                    "tick {tick}: limit {got}, the path needs {want}"
                );
            }
        }
    }

    #[test]
    fn the_queue_a_sender_holds_never_counts_as_path_delay() {
        let mut now = Instant::now();
        let mut limit = SendLimit::new(now);
        interval(&mut limit, &mut now, 12_500_000, 40, false);
        // A download holds the sender at its limit for 15 s, and its own queue adds 60 ms.
        for _ in 0..60 {
            interval(&mut limit, &mut now, 12_500_000, 100, true);
        }
        assert_eq!(
            limit.current(),
            500_000 + UNSENT_CUSHION,
            "the round trip of the empty path rules"
        );
    }

    #[test]
    fn a_slow_path_held_at_its_floor_stays_there() {
        let mut now = Instant::now();
        let mut limit = SendLimit::new(now);
        interval(&mut limit, &mut now, 1_250_000, 40, false);
        // At 10 Mbit/s the floor alone queues 62 ms ahead of the path's 40 ms.
        for _ in 0..60 {
            interval(&mut limit, &mut now, 1_250_000, 102, true);
        }
        assert_eq!(limit.current(), FLOOR);
    }

    #[test]
    fn a_longer_path_is_learnt_once_the_link_runs_unheld() {
        let mut now = Instant::now();
        let mut limit = SendLimit::new(now);
        interval(&mut limit, &mut now, 12_500_000, 40, false);
        for _ in 0..48 {
            interval(&mut limit, &mut now, 12_500_000, 60, false);
        }
        assert_eq!(limit.current(), 750_000 + UNSENT_CUSHION);
    }

    #[test]
    fn held_at_the_limit_while_delivery_grows_the_limit_rises_by_a_quarter() {
        let mut now = Instant::now();
        let mut limit = SendLimit::new(now);
        let start = limit.current();
        // A path whose capacity the limit hides: delivery is limit / rtt and still growing.
        interval(&mut limit, &mut now, start * 1000 / 40, 40, true);
        assert!(limit.current() >= start + start / 4);
        let second = limit.current();
        interval(&mut limit, &mut now, second * 1000 / 40, 40, true);
        assert!(limit.current() >= second + second / 4);
    }

    #[test]
    fn held_at_the_limit_with_flat_delivery_the_limit_stays_at_the_measured_need() {
        let mut now = Instant::now();
        let mut limit = SendLimit::new(now);
        interval(&mut limit, &mut now, 3_000_000, 40, false);
        let settled = limit.current();
        assert_eq!(settled, 120_000 + UNSENT_CUSHION);
        interval(&mut limit, &mut now, 3_000_000, 40, true);
        assert_eq!(limit.current(), settled, "a full path gets no more queue");
    }

    #[test]
    fn the_limit_never_passes_the_window() {
        let mut now = Instant::now();
        let mut limit = SendLimit::new(now);
        interval(&mut limit, &mut now, 1_000_000_000, 100, false);
        assert_eq!(limit.current(), CEILING);
    }

    #[test]
    fn an_interval_not_yet_over_changes_nothing() {
        let now = Instant::now();
        let mut limit = SendLimit::new(now);
        limit.confirmed(1_000_000, now);
        assert!(!limit.recompute(now + INTERVAL / 2));
        assert_eq!(limit.current(), INITIAL);
    }

    #[test]
    fn without_a_round_trip_sample_the_limit_holds() {
        let mut now = Instant::now();
        let mut limit = SendLimit::new(now);
        for _ in 0..5 {
            now += INTERVAL / 5;
            limit.confirmed(2_000_000, now);
        }
        assert!(limit.recompute(now));
        assert_eq!(limit.current(), INITIAL);
    }

    #[test]
    fn without_two_confirmations_the_rate_is_unknown_and_the_limit_holds() {
        let mut now = Instant::now();
        let mut limit = SendLimit::new(now);
        limit.round_trip(Duration::from_millis(40), now);
        now += INTERVAL;
        limit.confirmed(1_048_576, now);
        assert!(limit.recompute(now));
        assert_eq!(limit.current(), INITIAL);
    }

    #[test]
    fn an_idle_link_falls_to_the_floor() {
        let mut now = Instant::now();
        let mut limit = SendLimit::new(now);
        limit.round_trip(Duration::from_millis(40), now);
        now += RATE_SPAN + INTERVAL;
        assert!(limit.recompute(now));
        assert_eq!(limit.current(), FLOOR);
    }
}
