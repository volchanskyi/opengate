//! The bounded in-process alert queue shared by every edge alert producer.
//! Overflow drops the oldest alert and a rolling hourly ceiling suppresses the excess.

use std::collections::VecDeque;
use std::sync::{Arc, Mutex, MutexGuard, PoisonError};

/// Alerts one device may raise in a rolling hour before the excess is suppressed.
pub const DEVICE_HOURLY_CEILING: u32 = 20;

/// Alerts the sink holds while delivery is unavailable.
pub const DEFAULT_CAPACITY: usize = 256;

/// The width of the ceiling's rolling window, in microseconds.
const CEILING_WINDOW_MICROS: i64 = 3_600 * 1_000_000;

/// How bad an alert is.
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

/// Whether an alert describes something happening now or something found in the device's history.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
#[non_exhaustive]
pub enum AlertOrigin {
    /// Raised as it happened.
    #[default]
    Live,
    /// Found by re-running a rule over history the device already held.
    Backfilled,
}

/// One alert as the edge raises it; free-text fields are redacted by the producer.
#[derive(Debug, Clone, PartialEq)]
pub struct EdgeAlert {
    /// Which rule fired, as the catalogue identifies it.
    pub rule_id: String,
    /// Which revision of the rule fired; part of the alert's identity at the receiving end.
    pub rule_version: u32,
    /// How bad the rule says this is.
    pub severity: AlertSeverity,
    /// When the triggering record was written, in microseconds since the Unix epoch.
    pub ts_micros: i64,
    /// Start of the stretch the rule decided on, in microseconds; part of the alert's identity.
    pub window_start_micros: i64,
    /// End of that stretch, in microseconds; never before its start.
    pub window_end_micros: i64,
    /// The dimension the rule watched; empty for a rule that watches log text.
    pub metric: String,
    /// The reading that crossed the line; absent when `metric` is empty.
    pub value: Option<f64>,
    /// The service or subsystem the record came from.
    pub subject: String,
    /// What the rule means, in words a technician reads first.
    pub summary: String,
    /// Packed context for why the rule fired; may be empty.
    pub evidence: Vec<u8>,
    /// How `evidence` was packed; empty exactly when `evidence` is.
    pub evidence_codec: String,
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

/// What the sink has done since the process started; loss counts survive a drain.
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
    /// Raise times of alerts admitted inside the current window, oldest first.
    admitted: VecDeque<i64>,
    dropped_oldest: u64,
    suppressed_by_ceiling: u64,
}

/// The shared alert queue; clones share one queue and one per-device ceiling.
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

    /// Takes the lock, treating a poisoned mutex as usable since a panic leaves the queue intact.
    fn lock(&self) -> MutexGuard<'_, Inner> {
        self.inner.lock().unwrap_or_else(PoisonError::into_inner)
    }

    /// Offers an alert raised at `now_micros`; an alert over the ceiling is dropped, not deferred.
    pub fn push(&self, alert: EdgeAlert, now_micros: i64) -> PushOutcome {
        let mut inner = self.lock();

        inner.expire_admitted(now_micros);
        if inner.admitted.len() >= inner.ceiling as usize {
            inner.suppressed_by_ceiling += 1;
            return PushOutcome::SuppressedByCeiling;
        }
        inner.admitted.push_back(now_micros);

        inner.queue.push_back(alert);
        if inner.trim_to_capacity() > 0 {
            PushOutcome::DroppedOldest
        } else {
            PushOutcome::Queued
        }
    }

    /// Hands over every queued alert, oldest first, and empties the queue; loss counts stay.
    pub fn drain(&self) -> Vec<EdgeAlert> {
        self.lock().queue.drain(..).collect()
    }

    /// Puts undelivered alerts back at the front of the queue without charging the ceiling again.
    pub fn return_unsent(&self, alerts: Vec<EdgeAlert>) {
        if alerts.is_empty() {
            return;
        }
        let mut inner = self.lock();
        for alert in alerts.into_iter().rev() {
            inner.queue.push_front(alert);
        }
        inner.trim_to_capacity();
    }

    /// Sets the hourly ceiling, effective on the next alert; a ceiling of zero is ignored.
    pub fn set_ceiling(&self, ceiling: u32) {
        if ceiling == 0 {
            return;
        }
        self.lock().ceiling = ceiling;
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
    /// Drops the oldest until the queue is inside its bound, counts each loss and returns how many.
    fn trim_to_capacity(&mut self) -> u64 {
        let mut dropped = 0;
        while self.queue.len() > self.capacity {
            self.queue.pop_front();
            self.dropped_oldest += 1;
            dropped += 1;
        }
        dropped
    }

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
