//! The host's sending pace: one interval of the send limit's bookkeeping at 100 Mbit/s, from the
//! confirmations and round trip the path reports to the recomputed limit.

use criterion::{criterion_group, criterion_main, Criterion};
use mesh_agent_core::link::pacing::{SendLimit, INTERVAL};
use std::hint::black_box;
use std::time::{Duration, Instant};

/// Confirmations one interval carries at 100 Mbit/s, in the hand-off's quarter-window steps.
const CONFIRMATIONS_PER_INTERVAL: u32 = 3;

/// The bytes one quarter-window confirmation reports.
const CONFIRMATION_BYTES: usize = 1024 * 1024;

/// The round trip the office router holds the speed tests' line to.
const ROUND_TRIP: Duration = Duration::from_millis(40);

fn bench_send_limit_interval(c: &mut Criterion) {
    let mut now = Instant::now();
    let mut limit = SendLimit::new(now);
    let step = INTERVAL / CONFIRMATIONS_PER_INTERVAL;

    c.bench_function("send_limit_interval", |b| {
        b.iter(|| {
            for _ in 0..CONFIRMATIONS_PER_INTERVAL {
                now += step;
                limit.confirmed(black_box(CONFIRMATION_BYTES), now);
            }
            limit.round_trip(black_box(ROUND_TRIP), now);
            black_box(limit.recompute(now));
            black_box(limit.current())
        })
    });
}

criterion_group!(benches, bench_send_limit_interval);
criterion_main!(benches);
