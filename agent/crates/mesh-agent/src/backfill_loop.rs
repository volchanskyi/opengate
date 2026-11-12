//! Reconnect-backfill loop: drains the history the server never confirmed, one answered batch at a
//! time, and answers deep-history pulls; store access runs on blocking threads.

use std::sync::{Arc, Mutex, MutexGuard};
use std::time::Duration;

use rand::RngExt;

use crate::clock::unix_now;
use edge_tsdb::TsdbError;
use mesh_agent_core::ml::backfill::{
    answer_local_history, confirm, forget_after, migrate_cursors, pace_delay, settled_until,
    BackfillConfig, BackfillDrain, ConfirmedStore, DrainPosition, PlannedBatch,
};
use mesh_agent_core::ml::store_sink::{dim_series, LocalStoreSink, BACKFILL_SERIES};
use mesh_protocol::{BackfillTier, ControlMessage};

/// Maximum full-resolution points in one deep-history pull; also the default.
const HISTORY_HARD_CAP: usize = 100_000;

/// How long the batch in flight waits for its answer: four times the server's own 15 s bound,
/// so it passes only on a server fault.
const ANSWER_DEADLINE: Duration = Duration::from_secs(60);

/// The longest wait before a batch whose answer never came is sent again.
const MAX_MISSED_ANSWER_WAIT: Duration = Duration::from_secs(30 * 60);

/// The wait before the batch is sent again after `misses` answers in a row never came: one minute,
/// doubling, at most thirty, with no limit on attempts.
pub(crate) fn missed_answer_wait(misses: u32) -> Duration {
    let minutes = 1u64 << misses.saturating_sub(1).min(5);
    Duration::from_secs(minutes * 60).min(MAX_MISSED_ANSWER_WAIT)
}

/// The batch waiting for its answer: what an ack must name, the span it confirms, and the
/// message to send again when no answer comes.
struct InFlight {
    tier: BackfillTier,
    cursor: i64,
    start: i64,
    end: i64,
    message: ControlMessage,
    /// When the answer is due; `None` while the batch waits to be sent again.
    answer_by: Option<tokio::time::Instant>,
}

/// Where a coordinator is in the request, grant, drain lifecycle for the current session.
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

/// A catch-up's own clock and place, kept from batch to batch so each stretch is read once.
#[derive(Default)]
struct Session {
    now: i64,
    at: DrainPosition,
    /// The batch the start-up check found, sent first once the slot is granted.
    first: Option<PlannedBatch>,
}

/// Drives reconnect backfill for one agent over its live control stream.
pub(crate) struct BackfillCoordinator {
    store: Arc<Mutex<LocalStoreSink>>,
    cfg: BackfillConfig,
    phase: Phase,
    in_flight: Option<InFlight>,
    /// Answers missed in a row for the batch in flight.
    misses: u32,
    session: Session,
}

impl BackfillCoordinator {
    pub(crate) fn new(store: Arc<Mutex<LocalStoreSink>>) -> Self {
        Self {
            store,
            cfg: BackfillConfig::default(),
            phase: Phase::Idle,
            in_flight: None,
            misses: 0,
            session: Session::default(),
        }
    }

    /// Begins a backfill cycle for a new session and returns the `RequestBackfillSlot` to send,
    /// or `None` when no batch is owed.
    pub(crate) async fn start_session(&mut self) -> Option<ControlMessage> {
        self.phase = Phase::Idle;
        self.in_flight = None;
        self.misses = 0;
        self.session = Session {
            now: unix_now(),
            ..Session::default()
        };
        match self.plan(true).await {
            Ok(Some(batch)) => {
                self.session.first = Some(batch);
                self.phase = Phase::AwaitingGrant;
                Some(ControlMessage::RequestBackfillSlot)
            }
            Ok(None) => {
                self.phase = Phase::Quiescent;
                None
            }
            Err(e) => {
                tracing::warn!(error = %e, "backfill: start-up read failed");
                self.phase = Phase::Quiescent;
                None
            }
        }
    }

    /// Finds the session's next batch from where the last one stopped and confirms the empty time
    /// read on the way, holding the store lock to take the view and to confirm, never to read.
    async fn plan(&mut self, migrate: bool) -> Result<Option<PlannedBatch>, TsdbError> {
        let (cfg, now, at) = (self.cfg, self.session.now, self.session.at);
        let horizon = self.horizon();
        let (batch, at) = self
            .with_store(move |store| {
                let (snap, confirmed) = {
                    let mut sink = lock(store);
                    if migrate && migrate_cursors(sink.store_mut(), now, cfg)? {
                        tracing::info!("backfill: watermarks read into confirmed spans");
                    }
                    (sink.snapshot()?, sink.store().load_confirmed()?)
                };
                let settled = settled_until(&snap, &BACKFILL_SERIES)?;
                let mut drain = BackfillDrain::new(&snap, now, cfg, &BACKFILL_SERIES, &confirmed)
                    .resume(at)
                    .marking_empty_until(settled);
                let batch = drain.next_batch()?;
                let (empty, at) = (drain.take_empty(), drain.position());
                if !empty.is_empty() {
                    confirm(lock(store).store_mut(), &empty, horizon)?;
                }
                Ok((batch, at))
            })
            .await?;
        self.session.at = at;
        Ok(batch)
    }

    /// The oldest time catch-up can still send; confirmations that ended before it are dropped.
    fn horizon(&self) -> i64 {
        unix_now() - self.cfg.retention_secs
    }

    /// Holds the grant and enters the draining phase.
    pub(crate) fn on_grant(&mut self, rate: u32, deadline: i64) {
        self.phase = Phase::Draining {
            rate,
            deadline,
            awaiting_ack: false,
            last_len: 0,
        };
    }

    /// Goes quiescent and returns the jittered delay before the caller restarts the session; the
    /// deferred batch stays unconfirmed, so it is planned again.
    pub(crate) fn on_defer(&mut self, retry_after: u32) -> Duration {
        self.phase = Phase::Quiescent;
        self.in_flight = None;
        self.misses = 0;
        let base = u64::from(retry_after.max(1));
        // Jitter ±25 % so a fleet-wide reconnect herd de-synchronises its retries.
        let jitter = rand::rng().random_range(0..=(base / 2).max(1));
        Duration::from_secs(base.saturating_sub(base / 4).saturating_add(jitter))
    }

    /// Returns the next batch while draining, one at a time gated on the prior ack, or `None`.
    /// A grant past its deadline triggers a fresh slot request.
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
            // Grant expired mid-drain: confirmed spans are kept, so a fresh slot
            // resumes with what the server has not confirmed.
            return self.start_session().await;
        }
        let planned = match self.session.first.take() {
            Some(batch) => Ok(Some(batch)),
            None => self.plan(false).await,
        };
        match planned {
            Ok(Some(batch)) => {
                self.phase = Phase::Draining {
                    rate,
                    deadline,
                    awaiting_ack: true,
                    last_len: batch.samples.len(),
                };
                let message = ControlMessage::MetricBackfillBatch {
                    tier: batch.tier,
                    samples: batch.samples,
                    cursor: batch.cursor,
                };
                self.in_flight = Some(InFlight {
                    tier: batch.tier,
                    cursor: batch.cursor,
                    start: batch.start,
                    end: batch.end,
                    message: message.clone(),
                    answer_by: Some(tokio::time::Instant::now() + ANSWER_DEADLINE),
                });
                Some(message)
            }
            Ok(None) => {
                self.phase = Phase::Quiescent;
                None
            }
            Err(e) => {
                tracing::warn!(error = %e, "backfill: drain read failed");
                self.phase = Phase::Quiescent;
                None
            }
        }
    }

    /// Confirms the batch in flight when the ack names it and returns the pace delay before the
    /// next batch; an ack for any other batch, or outside the draining phase, confirms nothing.
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
        let Some(batch) = self
            .in_flight
            .take_if(|b| b.tier == tier && b.cursor == cursor)
        else {
            tracing::debug!(
                ?tier,
                cursor,
                "backfill: an ack for another batch confirms nothing"
            );
            return Duration::ZERO;
        };
        self.misses = 0;
        let (span, horizon) = ((batch.start, batch.end), self.horizon());
        if let Err(e) = self
            .write_store(move |sink| confirm(sink.store_mut(), &[span], horizon))
            .await
        {
            tracing::warn!(error = %e, "backfill: confirmation persist failed");
        }
        self.phase = Phase::Draining {
            rate,
            deadline,
            awaiting_ack: false,
            last_len,
        };
        pace_delay(last_len, rate)
    }

    /// Confirms the minute a live window stamped `ts` covers, which catch-up then skips.
    pub(crate) async fn on_live_confirmed(&mut self, ts: i64) {
        let minute = ts.div_euclid(60) * 60;
        let horizon = self.horizon();
        if let Err(e) = self
            .write_store(move |sink| confirm(sink.store_mut(), &[(minute, minute + 60)], horizon))
            .await
        {
            tracing::warn!(error = %e, "backfill: live confirmation persist failed");
        }
    }

    /// Forgets what the server confirmed from `since_ts` on, since it asks for it again, and
    /// returns the jittered wait before catch-up restarts.
    pub(crate) async fn on_resend_request(&mut self, since_ts: i64, retry_after: u32) -> Duration {
        tracing::info!(since_ts, "backfill: the server asked for history again");
        if let Err(e) = self
            .write_store(move |sink| forget_after(sink.store_mut(), since_ts))
            .await
        {
            tracing::warn!(error = %e, "backfill: forgetting confirmations failed");
        }
        self.on_defer(retry_after)
    }

    /// When the batch in flight must be answered by, or `None` when nothing is awaited.
    pub(crate) fn answer_deadline(&self) -> Option<tokio::time::Instant> {
        self.in_flight.as_ref().and_then(|b| b.answer_by)
    }

    /// Pauses catch-up after an answer that never came and returns the wait before the same batch
    /// goes again; the connection stays up, and each miss is logged.
    pub(crate) fn on_missed_answer(&mut self) -> Duration {
        let Some(batch) = self.in_flight.as_mut() else {
            return Duration::ZERO;
        };
        batch.answer_by = None;
        self.misses += 1;
        let wait = missed_answer_wait(self.misses);
        tracing::warn!(
            misses = self.misses,
            wait_s = wait.as_secs(),
            "backfill: the server never answered a batch; sending it again later"
        );
        wait
    }

    /// The batch waiting to be sent again, due its answer once more; `None` when none waits.
    pub(crate) fn resend_in_flight(&mut self) -> Option<ControlMessage> {
        let batch = self.in_flight.as_mut().filter(|b| b.answer_by.is_none())?;
        batch.answer_by = Some(tokio::time::Instant::now() + ANSWER_DEADLINE);
        Some(batch.message.clone())
    }

    /// Answers a `RequestLocalHistory` with a bounded 1 s pull of one dimension from the T0 tier.
    /// An unknown dimension yields an empty response so the broker completes.
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

    /// True while a grant is held and the drain has not finished.
    pub(crate) fn is_draining(&self) -> bool {
        matches!(self.phase, Phase::Draining { .. })
    }

    /// Runs `f` with the shared store on a blocking thread, so neither a redb commit nor a long
    /// read stalls the control loop; `f` takes the lock for as long as it needs it.
    async fn with_store<F, T>(&self, f: F) -> Result<T, TsdbError>
    where
        F: FnOnce(&Mutex<LocalStoreSink>) -> Result<T, TsdbError> + Send + 'static,
        T: Send + 'static,
    {
        let store = Arc::clone(&self.store);
        match tokio::task::spawn_blocking(move || f(&store)).await {
            Ok(res) => res,
            Err(e) => {
                tracing::warn!(error = %e, "backfill: store task panicked");
                Err(TsdbError::Io(std::io::Error::other("store task panicked")))
            }
        }
    }

    /// Runs a store write holding the lock throughout.
    async fn write_store<F, T>(&self, f: F) -> Result<T, TsdbError>
    where
        F: FnOnce(&mut LocalStoreSink) -> Result<T, TsdbError> + Send + 'static,
        T: Send + 'static,
    {
        self.with_store(move |store| f(&mut lock(store))).await
    }

    /// Runs a read-only store closure holding the lock throughout.
    async fn read_store<F, T>(&self, f: F) -> Result<T, TsdbError>
    where
        F: FnOnce(&LocalStoreSink) -> Result<T, TsdbError> + Send + 'static,
        T: Send + 'static,
    {
        self.with_store(move |store| f(&lock(store))).await
    }
}

/// Takes the store lock; a poisoned lock fails the blocking task holding it.
fn lock(store: &Mutex<LocalStoreSink>) -> MutexGuard<'_, LocalStoreSink> {
    store.lock().expect("store mutex poisoned")
}

#[cfg(test)]
mod tests {
    use super::*;
    use edge_tsdb::Durability;

    /// Hours of unsent history in the recorder test: a read holding the store lock over two
    /// days holds it past half a second.
    const HOURS_OFFLINE: i64 = 48;

    /// The recorder's longest acceptable wait for the store: a quarter of its one-second beat.
    const RECORDER_WAIT: Duration = Duration::from_millis(250);
    use mesh_agent_core::ml::sampler::MetricSample;

    fn sample(cpu: f32) -> MetricSample {
        MetricSample {
            cpu_total_percent: cpu,
            memory_used_percent: 10.0,
            disk_used_percent: Some(20.0),
            disk_mounts_critical: Some(0),
            network_rx_bps: Some(0.0),
            network_tx_bps: Some(0.0),
            stall_cpu_some: Some(0.0),
            stall_mem_some: Some(0.0),
            stall_mem_full: Some(0.0),
            stall_io_some: Some(0.0),
            stall_io_full: Some(0.0),
            disk_await_ms: Some(0.0),
            disk_queue_depth: Some(0.0),
            processes: Vec::new(),
        }
    }

    /// A store holding one reading a second over each `[start, end)`, committed.
    fn recorded_store(stretches: &[(i64, i64)]) -> Arc<Mutex<LocalStoreSink>> {
        let path = tempfile::tempdir().expect("tempdir").keep();
        let mut sink = LocalStoreSink::open(&path, 256 * 1024 * 1024, 3_600).expect("open");
        for &(start, end) in stretches {
            for ts in start..end {
                sink.record(ts, &sample(50.0), false).expect("record");
            }
        }
        sink.flush(Durability::Full).expect("flush");
        Arc::new(Mutex::new(sink))
    }

    fn seeded_store(now: i64, n: i64) -> Arc<Mutex<LocalStoreSink>> {
        let dir = tempfile::tempdir().expect("tempdir");
        // The temp dir is kept so the redb file outlives the test body.
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
    async fn full_cycle_requests_grants_drains_and_confirms() {
        let now = unix_now();
        let store = seeded_store(now - 60, 120);
        let mut c = BackfillCoordinator::new(store);

        match c.start_session().await {
            Some(ControlMessage::RequestBackfillSlot) => {}
            other => panic!("expected RequestBackfillSlot, got {other:?}"),
        }

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
                    assert_eq!(tier, BackfillTier::Recent60s);
                    assert!(!samples.is_empty());
                    for s in &samples {
                        assert_eq!(s.ts % 60, 0, "60 s windows, never 1 s");
                    }
                    batches += 1;
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
        let store = seeded_store(unix_now() - 60, 120);
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

    /// Drives a session to its first batch and returns it with its tier and cursor.
    async fn first_batch(c: &mut BackfillCoordinator) -> (BackfillTier, i64) {
        c.start_session().await.expect("history is pending");
        c.on_grant(1_000, unix_now() + 3600);
        match c.next_batch().await {
            Some(ControlMessage::MetricBackfillBatch { tier, cursor, .. }) => (tier, cursor),
            other => panic!("expected a batch, got {other:?}"),
        }
    }

    #[tokio::test]
    async fn an_acked_session_leaves_nothing_to_send_next_time() {
        let store = seeded_store(unix_now() - 60, 120);
        let mut c = BackfillCoordinator::new(store);
        let (tier, cursor) = first_batch(&mut c).await;
        c.on_ack(tier, cursor).await;
        while let Some(ControlMessage::MetricBackfillBatch { tier, cursor, .. }) =
            c.next_batch().await
        {
            c.on_ack(tier, cursor).await;
        }

        assert!(
            c.start_session().await.is_none(),
            "every confirmed minute stays sent across sessions"
        );
    }

    #[tokio::test]
    async fn an_ack_for_another_batch_confirms_nothing() {
        let store = seeded_store(unix_now() - 60, 120);
        let mut c = BackfillCoordinator::new(store);
        let (tier, cursor) = first_batch(&mut c).await;

        c.on_ack(tier, cursor - 60).await;

        assert!(
            c.answer_deadline().is_some(),
            "the batch in flight still waits for its own answer"
        );
        assert!(
            c.next_batch().await.is_none(),
            "and nothing else is sent meanwhile"
        );
        c.on_ack(tier, cursor).await;
        assert!(c.answer_deadline().is_none());
    }

    #[tokio::test]
    async fn a_live_confirmation_confirms_its_minute() {
        let now = unix_now();
        let minute = (now - 120).div_euclid(60) * 60;
        let store = seeded_store(minute + 60, 60);
        let mut c = BackfillCoordinator::new(Arc::clone(&store));

        c.on_live_confirmed(minute + 17).await;

        assert!(
            c.start_session().await.is_none(),
            "a minute the server confirmed live is not caught up"
        );
    }

    #[test]
    fn a_missed_answer_waits_one_two_four_minutes_and_on_to_thirty_without_limit() {
        let waits: Vec<u64> = (1..=9)
            .map(|m| missed_answer_wait(m).as_secs() / 60)
            .collect();
        assert_eq!(waits, vec![1, 2, 4, 8, 16, 30, 30, 30, 30]);
        assert_eq!(
            missed_answer_wait(10_000),
            Duration::from_secs(30 * 60),
            "no limit on attempts"
        );
    }

    #[tokio::test]
    async fn a_missed_answer_sends_the_same_batch_again_and_keeps_the_connection() {
        let store = seeded_store(unix_now() - 60, 120);
        let mut c = BackfillCoordinator::new(store);
        let sent = c.start_session().await;
        assert!(sent.is_some());
        c.on_grant(1_000, unix_now() + 3600);
        let first = c.next_batch().await.expect("a batch");
        let deadline = c.answer_deadline().expect("an answer is due");
        assert!(deadline <= tokio::time::Instant::now() + ANSWER_DEADLINE);

        for misses in 1..=3 {
            assert_eq!(c.on_missed_answer(), missed_answer_wait(misses));
            assert!(
                c.answer_deadline().is_none(),
                "catch-up pauses while it waits"
            );
            assert!(
                c.next_batch().await.is_none(),
                "no other batch overtakes it"
            );
            assert_eq!(
                c.resend_in_flight(),
                Some(first.clone()),
                "the same batch goes again"
            );
            assert!(c.answer_deadline().is_some());
        }
        assert!(
            c.is_draining(),
            "the session, and the connection under it, carry on"
        );
    }

    #[tokio::test]
    async fn a_resend_request_forgets_what_came_after_its_time_and_restarts_catch_up() {
        let now = unix_now();
        let store = seeded_store(now - 60, 180);
        let mut c = BackfillCoordinator::new(store);
        let (tier, cursor) = first_batch(&mut c).await;
        c.on_ack(tier, cursor).await;
        while let Some(ControlMessage::MetricBackfillBatch { tier, cursor, .. }) =
            c.next_batch().await
        {
            c.on_ack(tier, cursor).await;
        }
        assert!(c.start_session().await.is_none());

        let wait = c.on_resend_request(now - 120, 2).await;

        assert!(
            wait <= Duration::from_secs(3),
            "catch-up restarts after the requested wait"
        );
        assert!(!c.is_draining());
        assert!(
            matches!(
                c.start_session().await,
                Some(ControlMessage::RequestBackfillSlot)
            ),
            "the forgotten minutes are caught up again"
        );
    }

    #[tokio::test]
    async fn an_older_agents_watermarks_are_read_once_at_the_first_session() {
        let now = unix_now();
        let store = seeded_store(now - 60, 120);
        {
            let mut sink = store.lock().unwrap();
            sink.store_mut()
                .set_cursor(
                    mesh_agent_core::ml::backfill::TIER_CURSOR_RECENT60S,
                    now,
                    Durability::Full,
                )
                .unwrap();
        }
        let mut c = BackfillCoordinator::new(Arc::clone(&store));

        assert!(
            c.start_session().await.is_none(),
            "what the old watermark covered is not sent again"
        );
        let sink = store.lock().unwrap();
        assert_eq!(
            sink.store()
                .cursor(mesh_agent_core::ml::backfill::TIER_CURSOR_RECENT60S)
                .unwrap(),
            None
        );
    }

    #[tokio::test]
    async fn expired_grant_reslots_instead_of_draining_past_it() {
        let now = unix_now();
        let store = seeded_store(now - 60, 120);
        let mut c = BackfillCoordinator::new(store);
        c.start_session().await;
        // Deadline already in the past: the next drive re-requests a slot.
        c.on_grant(100, now - 1);
        match c.next_batch().await {
            Some(ControlMessage::RequestBackfillSlot) => {}
            other => panic!("expected a re-slot request, got {other:?}"),
        }
    }

    #[tokio::test]
    async fn a_reconnect_with_nothing_owed_marks_its_empty_time_done() {
        let minute = unix_now().div_euclid(60) * 60;
        let on = [
            (minute - 7_200, minute - 5_400),
            (minute - 1_800, minute - 600),
        ];
        let store = recorded_store(&on);
        confirm(store.lock().unwrap().store_mut(), &on, i64::MIN).unwrap();
        let mut c = BackfillCoordinator::new(Arc::clone(&store));

        assert!(c.start_session().await.is_none(), "nothing is owed");
        let confirmed = store.lock().unwrap().store().confirmed().unwrap();
        assert!(
            confirmed.covers(minute - 5_400, minute - 1_800),
            "the hour the machine was off is done once read: {:?}",
            confirmed.spans()
        );
    }

    #[tokio::test]
    async fn confirmations_older_than_thirty_days_are_forgotten() {
        let minute = unix_now().div_euclid(60) * 60;
        let month_ago = minute - 40 * 86_400;
        let store = recorded_store(&[(minute - 60, minute)]);
        confirm(
            store.lock().unwrap().store_mut(),
            &[(month_ago, month_ago + 600)],
            i64::MIN,
        )
        .unwrap();
        let mut c = BackfillCoordinator::new(Arc::clone(&store));

        c.on_live_confirmed(minute - 60).await;

        let confirmed = store.lock().unwrap().store().confirmed().unwrap();
        assert_eq!(confirmed.spans(), &[(minute - 60, minute)]);
    }

    #[tokio::test(flavor = "multi_thread", worker_threads = 2)]
    async fn the_recorder_waits_under_a_quarter_second_for_the_store_during_a_catch_up() {
        use std::sync::atomic::{AtomicBool, Ordering};
        use std::time::Instant;

        let minute = unix_now().div_euclid(60) * 60;
        let store = recorded_store(&[(minute - HOURS_OFFLINE * 3_600, minute)]);
        let stop = Arc::new(AtomicBool::new(false));
        // Records as the once-a-second sampler does, every 10 ms, and keeps its longest wait.
        let recorder = {
            let (store, stop) = (Arc::clone(&store), Arc::clone(&stop));
            std::thread::spawn(move || {
                let mut longest = Duration::ZERO;
                while !stop.load(Ordering::Relaxed) {
                    let asked = Instant::now();
                    let mut sink = store.lock().unwrap();
                    longest = longest.max(asked.elapsed());
                    sink.record(unix_now(), &sample(50.0), false).unwrap();
                    drop(sink);
                    std::thread::sleep(Duration::from_millis(10));
                }
                longest
            })
        };

        let mut c = BackfillCoordinator::new(Arc::clone(&store));
        assert!(
            c.start_session().await.is_some(),
            "the offline hours are owed"
        );
        c.on_grant(0, unix_now() + 3_600);
        let mut batches = 0;
        while let Some(ControlMessage::MetricBackfillBatch { tier, cursor, .. }) =
            c.next_batch().await
        {
            c.on_ack(tier, cursor).await;
            batches += 1;
        }
        stop.store(true, Ordering::Relaxed);
        let longest = recorder.join().unwrap();

        assert!(batches > 1, "the catch-up spans several batches");
        assert!(
            longest < RECORDER_WAIT,
            "the recorder waited {longest:?} for the store during a catch-up"
        );
    }
}
