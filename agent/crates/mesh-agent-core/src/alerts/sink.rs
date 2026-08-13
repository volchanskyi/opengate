//! The in-process alert sink every edge alert producer writes to.
//!
//! An alert is raised where it is detected and delivered somewhere else, so the
//! two are decoupled by a queue the producers share. That queue holds two
//! limits, and both of them lose alerts on purpose:
//!
//! The queue is **bounded**, because an agent offline for days would otherwise
//! grow a backlog without limit. When it is full the **oldest** entry goes: the
//! newest alert describes what the device is doing now, and dropping it to keep
//! one from three days ago would answer the wrong question on reconnect.
//!
//! A device may raise at most [`DEVICE_HOURLY_CEILING`] alerts an hour, because
//! one host stuck in a loop must not drown the detection of every other host.
//! The window rolls rather than buckets, so a device that spends its allowance
//! in one minute is not deaf for the other fifty-nine.
//!
//! Every loss under either limit is counted and reported in the next summary.
//! A suppressed alert that nobody counts is indistinguishable from a quiet
//! device, which is the failure this whole program exists to remove.
//!
//! Both limits apply to every producer, including a retroactive scan raising
//! findings out of history: a scan that learned a new failure mode and found
//! five thousand instances of it is exactly the flood the ceiling exists for,
//! and "but they are old" is not a reason to let it through.

use std::collections::VecDeque;
use std::sync::{Arc, Mutex, MutexGuard, PoisonError};

/// Alerts one device may raise in a rolling hour before the excess is
/// suppressed and counted.
pub const DEVICE_HOURLY_CEILING: u32 = 20;

/// Alerts the sink holds while delivery is unavailable.
pub const DEFAULT_CAPACITY: usize = 256;

/// The width of the ceiling's rolling window, in microseconds.
const CEILING_WINDOW_MICROS: i64 = 3_600 * 1_000_000;

/// How bad an alert is. A closed set: severity drives how an incident is
/// presented, and an open scale invites a per-rule argument about numbers that
/// no two rule authors would settle the same way.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
#[non_exhaustive]
pub enum AlertSeverity {
    /// Worth recording beside an incident, not worth raising one for.
    Info,
    /// Something is wrong and a person should look at it.
    Warning,
    /// Something is broken now.
    Critical,
}

/// Whether an alert describes something happening now or something the device
/// has been carrying in its own history.
///
/// The two read completely differently to whoever picks up the queue: one is a
/// machine in trouble, the other is a newly installed rule reporting what it
/// would have caught. They also group differently — a whole retroactive scan
/// folds into one incident — so the distinction travels with the alert rather
/// than being inferred from how old its timestamp is.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
#[non_exhaustive]
pub enum AlertOrigin {
    /// Raised as it happened.
    #[default]
    Live,
    /// Found by re-running a rule over history the device already held.
    Backfilled,
}

/// One alert as the edge raises it, before any transport gets hold of it.
///
/// Every free-text field here is derived from a host log line, so all of them
/// are redacted by the producer before the alert is constructed. The sink does
/// not redact: an alert that reaches it is already safe to leave the device.
#[derive(Debug, Clone, PartialEq)]
pub struct EdgeAlert {
    /// Which rule fired, as the catalogue identifies it.
    pub rule_id: String,
    /// How bad the rule says this is.
    pub severity: AlertSeverity,
    /// When the record that fired the rule was written, in microseconds since
    /// the Unix epoch. For a backfilled finding this is when the thing
    /// *happened*, which is generally nowhere near when it was found.
    pub ts_micros: i64,
    /// What the alert is about — the service or subsystem the record came from.
    pub subject: String,
    /// What the rule means, in words a technician reads first.
    pub summary: String,
    /// The redacted record(s) the rule matched.
    pub evidence: Vec<String>,
    /// Whether this happened now or is being reported out of history.
    pub origin: AlertOrigin,
}

/// What became of an alert handed to the sink.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
#[non_exhaustive]
pub enum PushOutcome {
    /// Held for delivery.
    Queued,
    /// Held, and the oldest queued alert was dropped to make room for it.
    DroppedOldest,
    /// Not held: the device is already at its hourly ceiling.
    SuppressedByCeiling,
}

/// What the sink has done since the process started. The two loss counts are
/// cumulative and survive a drain, because the drain is the moment they become
/// reportable — a backlog that lost entries has to be able to say so after it
/// has been handed over.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
#[non_exhaustive]
pub struct SinkStats {
    /// Alerts waiting for delivery right now.
    pub queued: usize,
    /// Alerts dropped because the queue was full.
    pub dropped_oldest: u64,
    /// Alerts refused because the device was at its hourly ceiling.
    pub suppressed_by_ceiling: u64,
}

struct Inner {
    queue: VecDeque<EdgeAlert>,
    capacity: usize,
    ceiling: u32,
    /// When each alert admitted inside the current window was raised, oldest
    /// first. Never longer than `ceiling`.
    admitted: VecDeque<i64>,
    dropped_oldest: u64,
    suppressed_by_ceiling: u64,
}

/// The shared alert queue. Cloning yields the *same* sink, not a copy of it:
/// the ceiling is per device, so four producers holding four clones share one
/// allowance rather than four.
#[derive(Clone)]
pub struct AlertSink {
    inner: Arc<Mutex<Inner>>,
}

impl Default for AlertSink {
    fn default() -> Self {
        Self::new(DEFAULT_CAPACITY, DEVICE_HOURLY_CEILING)
    }
}

impl std::fmt::Debug for AlertSink {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("AlertSink")
            .field("stats", &self.stats())
            .finish()
    }
}

impl AlertSink {
    /// A sink holding at most `capacity` alerts and admitting at most `ceiling`
    /// of them per rolling hour.
    #[must_use]
    pub fn new(capacity: usize, ceiling: u32) -> Self {
        Self {
            inner: Arc::new(Mutex::new(Inner {
                queue: VecDeque::new(),
                capacity,
                ceiling,
                admitted: VecDeque::new(),
                dropped_oldest: 0,
                suppressed_by_ceiling: 0,
            })),
        }
    }

    /// Takes the lock, treating a poisoned mutex as a usable one. A producer
    /// that panicked mid-push leaves the queue structurally intact, and losing
    /// every later alert on this device because of it would turn one failed
    /// alert into total silence.
    fn lock(&self) -> MutexGuard<'_, Inner> {
        self.inner.lock().unwrap_or_else(PoisonError::into_inner)
    }

    /// Offers an alert raised at `now_micros`.
    ///
    /// The ceiling is checked first: it governs what the device *raises*, so an
    /// alert refused by it is gone rather than deferred — holding it would
    /// deliver a storm late instead of not delivering it.
    pub fn push(&self, alert: EdgeAlert, now_micros: i64) -> PushOutcome {
        let mut inner = self.lock();

        inner.expire_admitted(now_micros);
        if inner.admitted.len() >= inner.ceiling as usize {
            inner.suppressed_by_ceiling += 1;
            return PushOutcome::SuppressedByCeiling;
        }
        inner.admitted.push_back(now_micros);

        inner.queue.push_back(alert);
        let mut dropped = false;
        while inner.queue.len() > inner.capacity {
            inner.queue.pop_front();
            inner.dropped_oldest += 1;
            dropped = true;
        }

        if dropped {
            PushOutcome::DroppedOldest
        } else {
            PushOutcome::Queued
        }
    }

    /// Hands over every queued alert, oldest first, and empties the queue. The
    /// loss counts are deliberately left standing: they describe the whole run,
    /// not the batch.
    pub fn drain(&self) -> Vec<EdgeAlert> {
        self.lock().queue.drain(..).collect()
    }

    /// What the sink is holding and what it has lost.
    #[must_use]
    pub fn stats(&self) -> SinkStats {
        let inner = self.lock();
        SinkStats {
            queued: inner.queue.len(),
            dropped_oldest: inner.dropped_oldest,
            suppressed_by_ceiling: inner.suppressed_by_ceiling,
        }
    }
}

impl Inner {
    /// Forgets admissions that have aged out of the rolling window.
    fn expire_admitted(&mut self, now_micros: i64) {
        while let Some(&oldest) = self.admitted.front() {
            if now_micros.saturating_sub(oldest) >= CEILING_WINDOW_MICROS {
                self.admitted.pop_front();
            } else {
                break;
            }
        }
    }
}
