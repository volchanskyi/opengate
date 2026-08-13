//! How far back the local store actually reaches, measured.
//!
//! The reach of the minute-by-minute history is what makes re-running a rule
//! over it worth doing at all, and it is the number that decided against keeping
//! a central recorder of every device's seconds. It cannot be arrived at by
//! dividing the store's cap by its density: that arithmetic assumes the store
//! holds nothing but today's vitals and that the minute tier is never evicted,
//! and neither is true — eviction takes the globally oldest block first,
//! coarsest tier before finer at the same age, so the minutes go before the
//! seconds of the same age do.
//!
//! So it is measured, through the production write path, at the production
//! vitals shape: a store is driven until its cap is evicting, and then asked for
//! the oldest minute it still holds. That is done at two cap sizes small enough
//! to run in a test, the two are checked to scale with the cap, and the shipped
//! cap is extrapolated from them with that check as the evidence.
//!
//! **The shape this assumed** is the whole vitals set as the sampler writes it —
//! thirteen series at one reading a second, percentages at centi precision,
//! disk-performance readings at milli, network rates as whole bytes. A device
//! that starts storing more series than that reaches back proportionally less
//! far, so the number below is only meaningful next to the shape it was measured
//! at.

use std::time::Instant;

use edge_tsdb::store::Tier;
use edge_tsdb::Durability;
use mesh_agent_core::ml::sampler::MetricSample;
use mesh_agent_core::ml::store_sink::{LocalStoreSink, SERIES_CPU};

/// Bucket-aligned start of the simulated history.
const START: i64 = 1_700_000_040;

/// Seconds between durable flushes. Larger than the agent's own cadence purely
/// to keep the harness's fsync count sane: what survives eviction is decided by
/// block boundaries and the cap, not by how often the store commits.
const COMMIT_EVERY: usize = 900;

/// How long to keep writing after the cap starts evicting, so the oldest
/// surviving minute is set by eviction rather than by where the run began. One
/// stored minute-block spans twelve hours, so this has to cover several of them.
const STEADY_SECS: i64 = 36 * 3_600;

/// Vitals one device writes at one reading a second.
const SERIES_COUNT: i64 = 13;

/// The cap the fleet ships with, which the measurement extrapolates to.
const SHIPPED_CAP_BYTES: u64 = 512 * 1024 * 1024;

/// The reach below which the case for keeping history on the device instead of
/// centrally stops holding — the central recorder it was weighed against keeps
/// 48 h.
const CENTRAL_RECORDER_REACH_SECS: i64 = 48 * 3_600;

/// A tiny, dependency-free, fully deterministic generator, so the measurement is
/// the same number on every machine that runs it.
struct SplitMix64(u64);

impl SplitMix64 {
    /// Uniform in `[0, 1)`.
    fn unit(&mut self) -> f64 {
        self.0 = self.0.wrapping_add(0x9E37_79B9_7F4A_7C15);
        let mut z = self.0;
        z = (z ^ (z >> 30)).wrapping_mul(0xBF58_476D_1CE4_E5B9);
        z = (z ^ (z >> 27)).wrapping_mul(0x94D0_49BB_1331_11EB);
        ((z ^ (z >> 31)) >> 11) as f64 / (1u64 << 53) as f64
    }
}

/// One vital, modelled the way host telemetry at one reading a second actually
/// behaves: it holds still for stretches and then moves, occasionally a long
/// way. How often it moves is what decides how much room a second of it takes,
/// so it is set per vital rather than shared — a disk that fills over months and
/// a processor that is asked to do something different every second are not the
/// same kind of number.
struct Gauge {
    value: f64,
    /// Chance this second's reading differs from the last one at all.
    volatility: f64,
    /// How far an ordinary move goes.
    step: f64,
    /// Chance a move is a jump rather than a nudge.
    spike: f64,
    low: f64,
    high: f64,
    /// Readings per whole unit — the precision the vital is published at.
    scale: f64,
}

impl Gauge {
    fn new(value: f64, volatility: f64, step: f64, spike: f64, high: f64, scale: f64) -> Self {
        Self {
            value,
            volatility,
            step,
            spike,
            low: 0.0,
            high,
            scale,
        }
    }

    fn next(&mut self, rng: &mut SplitMix64) -> f64 {
        if rng.unit() < self.volatility {
            let delta = if rng.unit() < self.spike {
                (rng.unit() - 0.3) * self.high
            } else {
                (rng.unit() - 0.5) * 2.0 * self.step
            };
            let moved = (self.value + delta).clamp(self.low, self.high);
            self.value = (moved * self.scale).round() / self.scale;
        }
        self.value
    }
}

/// A deterministic stand-in for one machine's whole vitals set.
struct Vitals {
    rng: SplitMix64,
    cpu: Gauge,
    memory: Gauge,
    disk: Gauge,
    rx: Gauge,
    tx: Gauge,
    stall_cpu: Gauge,
    stall_mem_some: Gauge,
    stall_mem_full: Gauge,
    stall_io_some: Gauge,
    stall_io_full: Gauge,
    await_ms: Gauge,
    queue_depth: Gauge,
}

impl Vitals {
    /// A machine doing real work: a processor that changes what it is doing
    /// constantly, memory and a disk that move slowly, network rates that are
    /// different every second, and stalling that is usually absent and
    /// occasionally not.
    fn new(seed: u64) -> Self {
        Self {
            rng: SplitMix64(seed),
            cpu: Gauge::new(20.0, 0.9, 12.0, 0.02, 100.0, 100.0),
            memory: Gauge::new(55.0, 0.3, 0.6, 0.005, 99.0, 100.0),
            disk: Gauge::new(61.0, 0.05, 0.05, 0.001, 99.0, 100.0),
            rx: Gauge::new(12_000.0, 1.0, 40_000.0, 0.05, 900_000.0, 1.0),
            tx: Gauge::new(4_000.0, 1.0, 20_000.0, 0.05, 400_000.0, 1.0),
            stall_cpu: Gauge::new(3.0, 0.7, 6.0, 0.02, 100.0, 100.0),
            stall_mem_some: Gauge::new(0.0, 0.1, 1.0, 0.02, 100.0, 100.0),
            stall_mem_full: Gauge::new(0.0, 0.05, 0.5, 0.01, 100.0, 100.0),
            stall_io_some: Gauge::new(1.0, 0.5, 5.0, 0.03, 100.0, 100.0),
            stall_io_full: Gauge::new(0.0, 0.2, 2.0, 0.02, 100.0, 100.0),
            await_ms: Gauge::new(2.5, 0.8, 3.0, 0.03, 60.0, 1_000.0),
            queue_depth: Gauge::new(1.0, 0.8, 2.0, 0.03, 64.0, 1_000.0),
        }
    }

    fn next(&mut self) -> MetricSample {
        let rng = &mut self.rng;
        let disk = self.disk.next(rng);
        MetricSample {
            cpu_total_percent: self.cpu.next(rng) as f32,
            memory_used_percent: self.memory.next(rng) as f32,
            disk_used_percent: Some(disk as f32),
            disk_mounts_critical: Some(u32::from(disk > 90.0)),
            network_rx_bps: Some(self.rx.next(rng)),
            network_tx_bps: Some(self.tx.next(rng)),
            stall_cpu_some: Some(self.stall_cpu.next(rng) as f32),
            stall_mem_some: Some(self.stall_mem_some.next(rng) as f32),
            stall_mem_full: Some(self.stall_mem_full.next(rng) as f32),
            stall_io_some: Some(self.stall_io_some.next(rng) as f32),
            stall_io_full: Some(self.stall_io_full.next(rng) as f32),
            disk_await_ms: Some(self.await_ms.next(rng) as f32),
            disk_queue_depth: Some(self.queue_depth.next(rng) as f32),
            processes: Vec::new(),
        }
    }
}

/// What one cap size reaches back to.
#[derive(Debug, Clone, Copy)]
struct Reach {
    cap_bytes: u64,
    /// Seconds between the oldest and newest minute still stored.
    secs: i64,
    /// Seconds of history written before the measurement was taken.
    written_secs: i64,
    /// What one second of one vital costs, across all three tiers — the density
    /// the reach follows from, and the number that says whether the machine this
    /// was measured on was busy enough to be worth believing.
    bytes_per_sample: f64,
}

impl Reach {
    /// Where this cap's reach lands when scaled to another cap.
    fn scaled_to(self, cap_bytes: u64) -> i64 {
        let ratio = cap_bytes as f64 / self.cap_bytes as f64;
        (self.secs as f64 * ratio) as i64
    }
}

fn hours(secs: i64) -> f64 {
    secs as f64 / 3_600.0
}

/// The oldest and newest minute the store still holds.
fn stored_span(sink: &LocalStoreSink) -> (i64, i64) {
    sink.store()
        .tier_span(SERIES_CPU, Tier::T1)
        .unwrap()
        .expect("the store holds minutes as soon as one has been committed")
}

/// Drive a store at `cap_bytes` until eviction has been running for a while,
/// then ask it how far back its minute tier still goes.
fn measure(cap_bytes: u64) -> Reach {
    let dir = tempfile::tempdir().unwrap();
    let mut sink = LocalStoreSink::open(dir.path(), cap_bytes, COMMIT_EVERY).unwrap();
    let mut vitals = Vitals::new(0x5EED_1234_ABCD_0B10);
    let started = Instant::now();

    let mut second = 0i64;
    let mut evicting_since: Option<i64> = None;
    loop {
        let sample = vitals.next();
        sink.record(START + second, &sample, false).unwrap();
        second += 1;

        // Checked on the commit boundary, because that is where eviction runs.
        // "At its cap" is asked of the history rather than of the byte count: a
        // store keeps itself just under its cap, so the moment that matters is
        // the one where the beginning of the run stops being stored.
        if second % COMMIT_EVERY as i64 == 0 {
            if evicting_since.is_none() && stored_span(&sink).0 > START {
                evicting_since = Some(second);
            }
            if evicting_since.is_some_and(|from| second - from >= STEADY_SECS) {
                break;
            }
        }
        assert!(
            second < 40 * 24 * 3_600,
            "the store never reached its {cap_bytes}-byte cap"
        );
    }
    sink.flush(Durability::Full).unwrap();

    let (oldest, newest) = stored_span(&sink);
    let reach = Reach {
        cap_bytes,
        secs: newest - oldest,
        written_secs: second,
        bytes_per_sample: sink.store().logical_bytes() as f64
            / ((newest - oldest) * SERIES_COUNT) as f64,
    };
    println!(
        "cap {:>4} KiB → reach {:>6.2} h at {:.2} B per stored reading \
         (wrote {:.1} h of history in {:.1} s of test time)",
        cap_bytes / 1024,
        hours(reach.secs),
        reach.bytes_per_sample,
        hours(reach.written_secs),
        started.elapsed().as_secs_f64()
    );
    reach
}

/// How far a measured reach may fall from what the cap below it predicts before
/// the extrapolation stops being evidence of anything.
const LINEARITY_TOLERANCE: f64 = 0.10;

/// The measurement: three cap sizes across a fourfold range, a linearity check
/// between each neighbouring pair, and the shipped cap extrapolated from the
/// largest of them.
#[test]
fn the_minute_tier_reaches_back_far_enough_to_be_worth_scanning() {
    let measured: Vec<Reach> = [2, 4, 8]
        .into_iter()
        .map(|mib| measure(mib * 1024 * 1024))
        .collect();

    // Each cap has to reach back proportionally further than the one below it.
    // Two points can be joined by any line; three across a fourfold range are
    // what makes the extrapolation below evidence rather than arithmetic.
    for pair in measured.windows(2) {
        let (below, above) = (pair[0], pair[1]);
        let predicted = below.scaled_to(above.cap_bytes);
        let error = (above.secs - predicted).abs() as f64 / predicted as f64;
        println!(
            "{} KiB predicts {:.2} h at {} KiB; measured {:.2} h ({:.1}% off)",
            below.cap_bytes / 1024,
            hours(predicted),
            above.cap_bytes / 1024,
            hours(above.secs),
            error * 100.0
        );
        assert!(
            error < LINEARITY_TOLERANCE,
            "reach does not scale with the cap, so it cannot be extrapolated: \
             {} KiB predicted {:.2} h at {} KiB, which measured {:.2} h",
            below.cap_bytes / 1024,
            hours(predicted),
            above.cap_bytes / 1024,
            hours(above.secs)
        );
    }

    let largest = *measured.last().expect("three caps were measured");
    let shipped = largest.scaled_to(SHIPPED_CAP_BYTES);
    println!(
        "extrapolated reach at the shipped {} MiB cap: {:.0} h ({:.0} days), \
         at {:.2} B per stored reading",
        SHIPPED_CAP_BYTES / (1024 * 1024),
        hours(shipped),
        hours(shipped) / 24.0,
        largest.bytes_per_sample
    );

    assert!(
        shipped > CENTRAL_RECORDER_REACH_SECS,
        "the device reaches back {:.1} h, less than the {} h a central recorder \
         would have kept — which is the argument keeping history on the device \
         rested on, so this is an owner's decision rather than a number to absorb",
        hours(shipped),
        CENTRAL_RECORDER_REACH_SECS / 3_600
    );
}
