//! The host's send limit on the link and on the relay socket, set at run time: what the path
//! delivers in one round trip, plus a cushion of unsent bytes, plus how bunched releases come back.

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

/// How long a round trip read with the sender unheld counts towards the path's own delay.
const RTT_MEMORY: Duration = Duration::from_secs(10);

/// How far back the delivery rate reads: confirmations arrive in steps of up to a quarter window,
/// four a second when the path is slow.
const RATE_SPAN: Duration = Duration::from_secs(1);

/// Round trips the bunching of releases is remembered for, as Linux's BBR remembers its
/// acknowledgement aggregation.
const BUNCHING_ROUND_TRIPS: u32 = 5;

/// The bunching allowance covers at most this much of the measured rate, Linux BBR's cap.
const BUNCHING_CAP: Duration = Duration::from_millis(100);

/// The path's bandwidth-delay product, [`UNSENT_CUSHION`] and the largest recent bunch of releases;
/// the delay is the least round trip two checks in a row agree on, against a queued or skipped one.
#[derive(Debug)]
pub struct SendLimit {
    limit: usize,
    last_check: Option<Duration>,
    unheld_agreed: VecDeque<(Instant, Duration)>,
    least: Option<Duration>,
    confirmations: VecDeque<(Instant, usize)>,
    interval_start: Instant,
    last_rate: Option<f64>,
    blocked_for: Duration,
    round: Option<Duration>,
    epoch: Option<(Instant, usize)>,
    bunching: VecDeque<(Instant, usize)>,
}

impl SendLimit {
    /// Returns a limit at [`INITIAL`], measuring from `now`.
    pub fn new(now: Instant) -> Self {
        Self {
            limit: INITIAL,
            last_check: None,
            unheld_agreed: VecDeque::new(),
            least: None,
            confirmations: VecDeque::new(),
            interval_start: now,
            last_rate: None,
            blocked_for: Duration::ZERO,
            round: None,
            epoch: None,
            bunching: VecDeque::new(),
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

    /// Records `bytes` the peer acknowledged at `now`; a run of releases ahead of the rate is a bunch,
    /// and a run ends once releases fall behind the rate or a round trip passes, as in QUIC's BBR.
    pub fn released(&mut self, bytes: usize, now: Instant) {
        let Some(rate) = self.last_rate.filter(|rate| *rate > 0.0) else {
            return;
        };
        let (mut start, mut so_far) = self.epoch.unwrap_or((now, 0));
        let mut expected = (rate * now.duration_since(start).as_secs_f64()) as usize;
        let round_over = self
            .round
            .is_some_and(|round| now.duration_since(start) >= round);
        if so_far <= expected || round_over {
            (start, so_far, expected) = (now, 0, 0);
        }
        so_far += bytes;
        self.epoch = Some((start, so_far));
        let extra = (so_far - expected).min(self.limit);
        // The front holds the largest bunch remembered; an older bunch no larger than this one is
        // never the largest again.
        while self.bunching.back().is_some_and(|(_, held)| *held <= extra) {
            self.bunching.pop_back();
        }
        self.bunching.push_back((now, extra));
    }

    /// The round trip one new check read: the first sets the session's least, and each later one
    /// agrees with the check before on the longer of the two, counted unheld only while unheld.
    pub fn round_trip(&mut self, rtt: Duration, now: Instant) {
        if rtt.is_zero() {
            return;
        }
        let previous = self.last_check.replace(rtt);
        let agreed = previous.map_or(rtt, |previous| previous.max(rtt));
        self.least = Some(self.least.map_or(agreed, |least| least.min(agreed)));
        if previous.is_some() && self.blocked_for.is_zero() {
            self.unheld_agreed.push_back((now, agreed));
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
        forget_before(&mut self.unheld_agreed, now, RTT_MEMORY);
        forget_before(&mut self.confirmations, now, RATE_SPAN);
        let path_delay = self
            .unheld_agreed
            .iter()
            .map(|(_, rtt)| *rtt)
            .min()
            .or(self.least);
        self.round = path_delay;
        let remembered = path_delay.map_or(RATE_SPAN, |rtt| rtt * BUNCHING_ROUND_TRIPS);
        forget_before(&mut self.bunching, now, remembered);
        let mut next = match (self.rate(), path_delay) {
            (Some(rate), Some(rtt)) => {
                let bunch = self.bunching.front().map_or(0, |(_, held)| *held);
                let cap = (rate * BUNCHING_CAP.as_secs_f64()) as usize;
                (rate * rtt.as_secs_f64()) as usize + UNSENT_CUSHION + bunch.min(cap)
            }
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

    /// Intervals of a 100 Mbit/s path with a 40 ms round trip whose releases come back `every`
    /// apart, each holding what the path delivered since the last.
    fn released_every(limit: &mut SendLimit, now: &mut Instant, every: Duration, intervals: u32) {
        let rate = 12_500_000.0;
        for _ in 0..intervals {
            limit.round_trip(Duration::from_millis(40), *now);
            let end = *now + INTERVAL;
            while *now + every <= end {
                *now += every;
                let bytes = (rate * every.as_secs_f64()) as usize;
                limit.released(bytes, *now);
                limit.confirmed(bytes, *now);
            }
            *now = end;
            assert!(limit.recompute(*now));
        }
    }

    #[test]
    fn releases_on_time_add_nothing_past_a_piece() {
        let mut now = Instant::now();
        let mut limit = SendLimit::new(now);
        released_every(&mut limit, &mut now, Duration::from_millis(1), 8);
        let base = 500_000 + UNSENT_CUSHION;
        let got = limit.current();
        assert!(
            got >= base && got <= base + 16 * 1024,
            "limit {got} against {base}"
        );
    }

    #[test]
    fn releases_bunched_ten_milliseconds_apart_raise_the_limit_by_the_bunch() {
        let mut now = Instant::now();
        let mut limit = SendLimit::new(now);
        released_every(&mut limit, &mut now, Duration::from_millis(10), 8);
        let base = 500_000 + UNSENT_CUSHION;
        // A bunch holds 10 ms of the path's rate.
        let got = limit.current();
        assert!(
            got >= base + 125_000 && got <= base + 2 * 125_000,
            "limit {got} against {base} and a 125 000 byte bunch"
        );
    }

    #[test]
    fn the_bunching_allowance_never_passes_a_tenth_of_a_second_of_the_rate() {
        let mut now = Instant::now();
        let mut limit = SendLimit::new(now);
        released_every(&mut limit, &mut now, Duration::from_millis(250), 8);
        assert!(limit.current() <= 500_000 + UNSENT_CUSHION + 1_250_000);
    }

    #[test]
    fn bunching_older_than_five_round_trips_is_forgotten() {
        let mut now = Instant::now();
        let mut limit = SendLimit::new(now);
        released_every(&mut limit, &mut now, Duration::from_millis(10), 8);
        released_every(&mut limit, &mut now, Duration::from_millis(1), 2);
        let base = 500_000 + UNSENT_CUSHION;
        assert!(
            limit.current() <= base + 16 * 1024,
            "limit {}",
            limit.current()
        );
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
    fn one_short_check_among_steady_ones_leaves_the_limit() {
        let mut now = Instant::now();
        let mut limit = SendLimit::new(now);
        interval(&mut limit, &mut now, 12_500_000, 40, false);
        interval(&mut limit, &mut now, 12_500_000, 40, false);
        // A check reordered past the router's delay reads half the path's round trip.
        interval(&mut limit, &mut now, 12_500_000, 20, false);
        interval(&mut limit, &mut now, 12_500_000, 40, false);
        assert_eq!(limit.current(), 500_000 + UNSENT_CUSHION);
    }

    #[test]
    fn two_short_checks_in_a_row_move_the_limit() {
        let mut now = Instant::now();
        let mut limit = SendLimit::new(now);
        interval(&mut limit, &mut now, 12_500_000, 40, false);
        interval(&mut limit, &mut now, 12_500_000, 40, false);
        interval(&mut limit, &mut now, 12_500_000, 20, false);
        interval(&mut limit, &mut now, 12_500_000, 20, false);
        assert_eq!(limit.current(), 250_000 + UNSENT_CUSHION);
    }

    #[test]
    fn a_short_check_the_sender_held_through_leaves_the_session_least() {
        let mut now = Instant::now();
        let mut limit = SendLimit::new(now);
        interval(&mut limit, &mut now, 12_500_000, 40, false);
        interval(&mut limit, &mut now, 12_500_000, 40, false);
        for rtt_ms in [100, 20, 100] {
            interval(&mut limit, &mut now, 12_500_000, rtt_ms, true);
        }
        for _ in 0..44 {
            interval(&mut limit, &mut now, 12_500_000, 100, true);
        }
        assert_eq!(limit.current(), 500_000 + UNSENT_CUSHION);
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
