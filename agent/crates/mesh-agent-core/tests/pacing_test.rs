//! The send limit in a closed loop: a sender held at it over a shaped path whose acknowledgements
//! come back on time or in bursts, as a busy browser returns them.

use std::collections::VecDeque;
use std::time::{Duration, Instant};

use mesh_agent_core::link::pacing::{SendLimit, INTERVAL};

/// The bytes one send puts on the link: the link's maximum message.
const PIECE: usize = 16 * 1024;

/// 100 Mbit/s.
const LINE: f64 = 12_500_000.0;

const RTT: Duration = Duration::from_millis(40);

/// The share of the line a sender held at the limit carries over the second half of `secs`, its
/// acknowledgements handed back `burst` apart; the path's checks see its queue, never the bursts.
fn carried_share(burst: Duration, secs: u64) -> f64 {
    let step = Duration::from_micros(100);
    let start = Instant::now();
    let end = start + Duration::from_secs(secs);
    let half = start + Duration::from_secs(secs) / 2;
    let mut limit = SendLimit::new(start);
    let (mut outstanding, mut queue, mut ackable, mut carried) = (0usize, 0f64, 0usize, 0usize);
    let mut on_the_wire: VecDeque<(Instant, usize)> = VecDeque::new();
    let (mut next_burst, mut next_tick) = (start + burst, start);
    let mut now = start;
    while now < end {
        now += step;
        while outstanding + PIECE <= limit.current() {
            outstanding += PIECE;
            queue += PIECE as f64;
        }
        limit.blocked(step);
        let sent = queue.min(LINE * step.as_secs_f64());
        queue -= sent;
        on_the_wire.push_back((now + RTT, sent as usize));
        while let Some(&(at, bytes)) = on_the_wire.front() {
            if at > now {
                break;
            }
            on_the_wire.pop_front();
            ackable += bytes;
            if now >= half {
                carried += bytes;
            }
        }
        if now >= next_burst {
            next_burst += burst;
            if ackable > 0 {
                outstanding = outstanding.saturating_sub(ackable);
                limit.released(ackable, now);
                limit.confirmed(ackable, now);
                ackable = 0;
            }
        }
        if now >= next_tick {
            next_tick += INTERVAL;
            limit.round_trip(RTT + Duration::from_secs_f64(queue / LINE), now);
            limit.recompute(now);
        }
    }
    carried as f64 / (LINE * secs as f64 / 2.0)
}

#[test]
fn acknowledgements_on_time_carry_the_line() {
    let share = carried_share(Duration::from_micros(100), 20);
    assert!(share >= 0.9, "carried {:.1}% of the line", share * 100.0);
}

#[test]
fn acknowledgements_in_bursts_ten_milliseconds_apart_still_carry_the_line() {
    let share = carried_share(Duration::from_millis(10), 20);
    assert!(share >= 0.9, "carried {:.1}% of the line", share * 100.0);
}
