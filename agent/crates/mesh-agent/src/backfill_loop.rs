//! WS-15 reconnect-backfill drive loop (agent binary side).
//!
//! Bridges the pure [`mesh_agent_core::ml::backfill`] engine and the durable
//! WS-14b [`LocalStoreSink`] to the live QUIC control stream. On each new
//! session the coordinator requests a server-coordinated admission slot, and —
//! once granted — drains the persisted history **recent-first, tier-mapped,
//! throttled to the granted rate, one acked batch at a time**, advancing the
//! durable per-tier watermark only after the server acks. It also answers
//! server-brokered on-demand deep-history pulls from the local T0 raw tier.
//!
//! Every method that touches the store runs the redb read/write on a
//! [`spawn_blocking`](tokio::task::spawn_blocking) thread, so a cold-cache range
//! read or a long backlog scan never blocks the async control loop's reactor.
//! The store lock is shared with the sampler and is held only for the duration
//! of one snapshot/drain/cursor-write.

use std::sync::{Arc, Mutex};
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use rand::Rng;

use edge_tsdb::TsdbError;
use mesh_agent_core::ml::backfill::{
    answer_local_history, load_cursors, pace_delay, pending_hint, record_ack, BackfillConfig,
    BackfillDrain,
};
use mesh_agent_core::ml::store_sink::{dim_series, LocalStoreSink, BACKFILL_SERIES};
use mesh_protocol::{BackfillTier, ControlMessage};

/// Maximum full-resolution points returned for one deep-history pull when the
/// server does not bound the request itself (defense in depth against a huge
/// window). The admin-gated server endpoint also caps this.
const HISTORY_HARD_CAP: usize = 100_000;

/// Wall-clock unix seconds, clamped to 0 before the epoch.
fn unix_now() -> i64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_secs() as i64)
        .unwrap_or(0)
}

/// Where a coordinator is in the request → grant → drain lifecycle for the
/// current session.
#[derive(Debug, Clone, Copy, PartialEq)]
enum Phase {
    /// No slot requested yet this session (or nothing to drain).
    Idle,
    /// `RequestBackfillSlot` sent; awaiting `GrantBackfill`/`DeferBackfill`.
    AwaitingGrant,
    /// Grant held. `awaiting_ack` gates a single in-flight batch; `last_len` is
    /// its sample count (for rate pacing on the next ack).
    Draining {
        rate: u32,
        deadline: i64,
        awaiting_ack: bool,
        last_len: usize,
    },
    /// Fully drained for this session (or deferred); nothing to send until the
    /// next reconnect or a scheduled retry.
    Quiescent,
}

/// Drives WS-15 reconnect backfill for one agent over its live control stream.
pub(crate) struct BackfillCoordinator {
    store: Arc<Mutex<LocalStoreSink>>,
    cfg: BackfillConfig,
    phase: Phase,
}

impl BackfillCoordinator {
    /// Create a coordinator over the sampler-shared local store.
    pub(crate) fn new(store: Arc<Mutex<LocalStoreSink>>) -> Self {
        Self {
            store,
            cfg: BackfillConfig::default(),
            phase: Phase::Idle,
        }
    }

    /// Begin a backfill cycle for a freshly-registered session: reset state and,
    /// if history is pending, return the `RequestBackfillSlot` to send. Returns
    /// `None` (and goes quiescent) when nothing is pending — an idle agent never
    /// asks for a slot it does not need.
    pub(crate) async fn start_session(&mut self) -> Option<ControlMessage> {
        self.phase = Phase::Idle;
        let cfg = self.cfg;
        let now = unix_now();
        let hint = self
            .read_store(move |sink| {
                let snap = sink.snapshot()?;
                let cursors = load_cursors(sink.store())?;
                pending_hint(&snap, now, cfg, &BACKFILL_SERIES, cursors)
            })
            .await;
        let (pending, oldest) = match hint {
            Ok(hint) => hint,
            Err(e) => {
                tracing::warn!(error = %e, "backfill: pending-hint read failed");
                self.phase = Phase::Quiescent;
                return None;
            }
        };
        if pending == 0 {
            self.phase = Phase::Quiescent;
            return None;
        }
        self.phase = Phase::AwaitingGrant;
        Some(ControlMessage::RequestBackfillSlot {
            pending_samples: pending,
            oldest_ts: oldest,
        })
    }

    /// Handle a `GrantBackfill`: hold the grant and enter the draining phase. The
    /// caller schedules an immediate [`next_batch`](Self::next_batch) send.
    pub(crate) fn on_grant(&mut self, rate: u32, deadline: i64) {
        self.phase = Phase::Draining {
            rate,
            deadline,
            awaiting_ack: false,
            last_len: 0,
        };
    }

    /// Handle a `DeferBackfill`: hold durable data and go quiescent until a
    /// jittered retry. Returns the delay before the caller should
    /// [`start_session`](Self::start_session) again.
    pub(crate) fn on_defer(&mut self, retry_after: u32) -> Duration {
        self.phase = Phase::Quiescent;
        let base = u64::from(retry_after.max(1));
        // Jitter ±25 % so a fleet-wide reconnect herd de-synchronises its retries.
        let jitter = rand::rng().random_range(0..=(base / 2).max(1));
        Duration::from_secs(base.saturating_sub(base / 4).saturating_add(jitter))
    }

    /// Produce the next control message while draining, or `None` when there is
    /// nothing to send right now. Sends one batch at a time (gated on the prior
    /// ack). When the grant deadline has passed with work still pending it
    /// re-requests a slot rather than draining past the grant.
    pub(crate) async fn next_batch(&mut self) -> Option<ControlMessage> {
        let Phase::Draining {
            rate,
            deadline,
            awaiting_ack,
            ..
        } = self.phase
        else {
            return None;
        };
        if awaiting_ack {
            return None;
        }
        if unix_now() > deadline {
            // Grant expired mid-drain: durable data is safe, so re-request a
            // fresh slot from the last acked watermark.
            return self.start_session().await;
        }
        let cfg = self.cfg;
        let now = unix_now();
        let planned = self
            .read_store(move |sink| {
                let snap = sink.snapshot()?;
                let cursors = load_cursors(sink.store())?;
                BackfillDrain::new(&snap, now, cfg, &BACKFILL_SERIES, cursors).next_batch()
            })
            .await;
        match planned {
            Ok(Some(batch)) => {
                self.phase = Phase::Draining {
                    rate,
                    deadline,
                    awaiting_ack: true,
                    last_len: batch.samples.len(),
                };
                Some(ControlMessage::MetricBackfillBatch {
                    tier: batch.tier,
                    samples: batch.samples,
                    cursor: batch.cursor,
                })
            }
            Ok(None) => {
                self.phase = Phase::Quiescent; // fully drained this session
                None
            }
            Err(e) => {
                tracing::warn!(error = %e, "backfill: drain read failed");
                self.phase = Phase::Quiescent;
                None
            }
        }
    }

    /// Handle a `MetricBackfillAck`: advance the durable per-tier watermark and
    /// return the pace delay the caller waits before the next
    /// [`next_batch`](Self::next_batch). A stray ack outside the draining phase
    /// is ignored.
    pub(crate) async fn on_ack(&mut self, tier: BackfillTier, cursor: i64) -> Duration {
        let Phase::Draining {
            rate,
            deadline,
            last_len,
            ..
        } = self.phase
        else {
            return Duration::ZERO;
        };
        let store = Arc::clone(&self.store);
        let write = tokio::task::spawn_blocking(move || {
            let mut sink = store.lock().expect("store mutex poisoned");
            record_ack(sink.store_mut(), tier, cursor)
        })
        .await;
        match write {
            Ok(Ok(())) => {}
            Ok(Err(e)) => tracing::warn!(error = %e, "backfill: cursor persist failed"),
            Err(e) => tracing::warn!(error = %e, "backfill: cursor task panicked"),
        }
        self.phase = Phase::Draining {
            rate,
            deadline,
            awaiting_ack: false,
            last_len,
        };
        pace_delay(last_len, rate)
    }

    /// Answer a server-brokered `RequestLocalHistory`: a bounded full-resolution
    /// 1 s pull of one dimension from the local T0 raw tier. An unknown dimension
    /// yields an empty (non-truncated) response so the broker always completes.
    pub(crate) async fn answer_history(
        &self,
        dim: String,
        from_ts: i64,
        to_ts: i64,
        max_points: u32,
    ) -> ControlMessage {
        let cap = if max_points == 0 {
            HISTORY_HARD_CAP
        } else {
            (max_points as usize).min(HISTORY_HARD_CAP)
        };
        let (points, truncated) = match dim_series(&dim) {
            Some(series) => {
                let read = self
                    .read_store(move |sink| {
                        let snap = sink.snapshot()?;
                        answer_local_history(&snap, series, from_ts, to_ts, cap)
                    })
                    .await;
                match read {
                    Ok(res) => res,
                    Err(e) => {
                        tracing::warn!(error = %e, dim, "backfill: local-history read failed");
                        (Vec::new(), false)
                    }
                }
            }
            None => (Vec::new(), false),
        };
        ControlMessage::LocalHistoryResponse {
            dim,
            points,
            truncated,
        }
    }

    /// True while a grant is held and the drain has not finished — the control
    /// loop arms its paced-send timer only in this phase.
    pub(crate) fn is_draining(&self) -> bool {
        matches!(self.phase, Phase::Draining { .. })
    }

    /// Run a read-only store closure on a blocking thread, so a cold-cache redb
    /// read never stalls the async control loop.
    async fn read_store<F, T>(&self, f: F) -> Result<T, TsdbError>
    where
        F: FnOnce(&LocalStoreSink) -> Result<T, TsdbError> + Send + 'static,
        T: Send + 'static,
    {
        let store = Arc::clone(&self.store);
        match tokio::task::spawn_blocking(move || {
            let sink = store.lock().expect("store mutex poisoned");
            f(&sink)
        })
        .await
        {
            Ok(res) => res,
            Err(e) => {
                tracing::warn!(error = %e, "backfill: store read task panicked");
                Err(TsdbError::Io(std::io::Error::other(
                    "store read task panicked",
                )))
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use edge_tsdb::Durability;
    use mesh_agent_core::ml::sampler::MetricSample;

    fn sample(cpu: f32) -> MetricSample {
        MetricSample {
            cpu_total_percent: cpu,
            memory_used_percent: 10.0,
            disk_used_percent: 20.0,
            network_rx_bytes: 0,
            network_tx_bytes: 0,
            processes: Vec::new(),
        }
    }

    /// A store seeded with `n` recent 1 s samples ending just before `now`.
    fn seeded_store(now: i64, n: i64) -> Arc<Mutex<LocalStoreSink>> {
        let dir = tempfile::tempdir().expect("tempdir");
        // Keep the temp dir so the redb file outlives the test body.
        let path = dir.keep();
        let mut sink = LocalStoreSink::open(&path, 64 * 1024 * 1024, 1).expect("open");
        for ts in (now - n)..now {
            sink.record(ts, &sample(50.0), false).expect("record");
        }
        sink.flush(Durability::Full).expect("flush");
        Arc::new(Mutex::new(sink))
    }

    #[tokio::test]
    async fn idle_store_requests_no_slot() {
        let dir = tempfile::tempdir().unwrap();
        let sink = LocalStoreSink::open(dir.path(), 1024 * 1024, 1).unwrap();
        let mut c = BackfillCoordinator::new(Arc::new(Mutex::new(sink)));
        assert!(
            c.start_session().await.is_none(),
            "no history → no slot request"
        );
        assert!(!c.is_draining());
    }

    #[tokio::test]
    async fn full_cycle_requests_grants_drains_and_advances_cursor() {
        let now = unix_now();
        let store = seeded_store(now, 30);
        let mut c = BackfillCoordinator::new(store);

        // Session start asks for a slot with a non-empty backlog hint.
        match c.start_session().await {
            Some(ControlMessage::RequestBackfillSlot {
                pending_samples,
                oldest_ts,
            }) => {
                assert!(pending_samples > 0);
                assert!(oldest_ts > 0);
            }
            other => panic!("expected RequestBackfillSlot, got {other:?}"),
        }

        // Grant → drain. Every batch is the recent 10 s tier, never 1 s raw.
        c.on_grant(100, now + 3600);
        assert!(c.is_draining());
        let mut batches = 0;
        while let Some(msg) = c.next_batch().await {
            match msg {
                ControlMessage::MetricBackfillBatch {
                    tier,
                    samples,
                    cursor,
                } => {
                    assert_eq!(tier, BackfillTier::Raw10s);
                    assert!(!samples.is_empty());
                    for s in &samples {
                        assert_eq!(s.ts % 10, 0, "10 s windows, never 1 s");
                    }
                    batches += 1;
                    // Server acks the batch; cursor advances, pace is honored.
                    let delay = c.on_ack(tier, cursor).await;
                    assert!(delay <= Duration::from_secs(60));
                }
                other => panic!("expected a batch, got {other:?}"),
            }
        }
        assert!(batches >= 1, "the seeded recent window drained");
        assert!(!c.is_draining(), "drain finished this session");
    }

    #[tokio::test]
    async fn defer_yields_a_bounded_jittered_retry_and_no_drain() {
        let store = seeded_store(unix_now(), 30);
        let mut c = BackfillCoordinator::new(store);
        c.start_session().await;
        let delay = c.on_defer(20);
        assert!(!c.is_draining(), "a deferral never drains");
        assert!(
            delay >= Duration::from_secs(10) && delay <= Duration::from_secs(30),
            "retry stays within the ±25 % jitter band: {delay:?}"
        );
    }

    #[tokio::test]
    async fn deep_history_pull_returns_full_res_and_bounds_unknown_dims() {
        let now = unix_now();
        let store = seeded_store(now, 40);
        let c = BackfillCoordinator::new(store);

        // A known dimension returns 1 s full-resolution points, capped + flagged.
        match c
            .answer_history("cpu.total".into(), now - 40, now, 10)
            .await
        {
            ControlMessage::LocalHistoryResponse {
                dim,
                points,
                truncated,
            } => {
                assert_eq!(dim, "cpu.total");
                assert_eq!(points.len(), 10);
                assert!(truncated, "a capped window flags truncation");
            }
            other => panic!("expected LocalHistoryResponse, got {other:?}"),
        }

        // An unknown dimension still completes the broker with an empty response.
        match c
            .answer_history("bogus.metric".into(), now - 40, now, 100)
            .await
        {
            ControlMessage::LocalHistoryResponse {
                points, truncated, ..
            } => {
                assert!(points.is_empty());
                assert!(!truncated);
            }
            other => panic!("expected LocalHistoryResponse, got {other:?}"),
        }
    }

    #[tokio::test]
    async fn expired_grant_reslots_instead_of_draining_past_it() {
        let now = unix_now();
        let store = seeded_store(now, 30);
        let mut c = BackfillCoordinator::new(store);
        c.start_session().await;
        // Deadline already in the past: the next drive re-requests a slot.
        c.on_grant(100, now - 1);
        match c.next_batch().await {
            Some(ControlMessage::RequestBackfillSlot { .. }) => {}
            other => panic!("expected a re-slot request, got {other:?}"),
        }
    }
}
