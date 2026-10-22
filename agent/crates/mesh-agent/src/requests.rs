//! Runs the server's slow requests beside the control loop: one of each kind at a time, each
//! answered by its deadline, the reply handed back to the loop that owns the connection.

use std::path::PathBuf;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;
use std::time::Duration;

use mesh_protocol::{request_error, ControlMessage};
use tokio::sync::mpsc;
use tracing::warn;

/// How long a log read or a hardware report may take; the server waits 15 s for the answer.
pub(crate) const DEADLINE: Duration = Duration::from_secs(10);

/// Replies a connection's requests hand back; a connection that ended drops its receiver, so a
/// late reply never reaches the next connection's caller.
pub(crate) type Replies = mpsc::Sender<ControlMessage>;

/// Room for the replies of one connection's requests, at most one of each kind in flight.
pub(crate) const REPLY_ROOM: usize = 4;

/// Keeps its kind busy while any part of the work still runs, a blocking part included.
#[derive(Clone)]
pub(crate) struct Hold {
    _release: Arc<Release>,
}

struct Release(Arc<AtomicBool>);

impl Drop for Release {
    fn drop(&mut self) {
        self.0.store(false, Ordering::Release);
    }
}

/// One kind of request, busy from its claim until the last [`Hold`] on the work is dropped.
#[derive(Clone, Default)]
pub(crate) struct Lane(Arc<AtomicBool>);

impl Lane {
    /// Claims the lane, or `None` while a request of this kind still runs.
    pub(crate) fn claim(&self) -> Option<Hold> {
        self.0
            .compare_exchange(false, true, Ordering::Acquire, Ordering::Relaxed)
            .ok()
            .map(|_| Hold {
                _release: Arc::new(Release(self.0.clone())),
            })
    }
}

/// How an update run ended, handed to the loop, which acknowledges it and restarts on success.
pub(crate) struct UpdateOutcome {
    pub(crate) version: String,
    pub(crate) result: Result<bool, String>,
}

/// What an update needs to run, as the server's `AgentUpdate` names it.
pub(crate) struct UpdateOrder {
    pub(crate) version: String,
    pub(crate) url: String,
    pub(crate) sha256: String,
    pub(crate) signature: String,
}

/// The lanes the agent keeps across connections, since a request's work can outlive one, and the
/// channel update outcomes come back on, which outlives a connection too.
pub(crate) struct Requests {
    logs: Lane,
    hardware: Lane,
    update: Lane,
    outcomes_tx: mpsc::Sender<UpdateOutcome>,
    pub(crate) outcomes: mpsc::Receiver<UpdateOutcome>,
}

impl Default for Requests {
    fn default() -> Self {
        let (outcomes_tx, outcomes) = mpsc::channel(1);
        Self {
            logs: Lane::default(),
            hardware: Lane::default(),
            update: Lane::default(),
            outcomes_tx,
            outcomes,
        }
    }
}

impl Requests {
    /// Starts an update and returns the immediate acknowledgement when one is already running.
    /// The download has its own limits, so the update has no deadline here.
    pub(crate) fn start_update(
        &self,
        config: Arc<mesh_agent_core::UpdateConfig>,
        order: UpdateOrder,
    ) -> Option<ControlMessage> {
        let Some(hold) = self.update.claim() else {
            return Some(ControlMessage::AgentUpdateAck {
                version: order.version,
                success: false,
                error: request_error::BUSY.to_string(),
            });
        };
        let outcomes = self.outcomes_tx.clone();
        tokio::spawn(async move {
            let _busy = hold;
            let result = mesh_agent_core::update::apply_update(
                &config,
                &order.version,
                &order.url,
                &order.sha256,
                &order.signature,
            )
            .await
            .map_err(|e| e.to_string());
            let outcome = UpdateOutcome {
                version: order.version,
                result,
            };
            if outcomes.send(outcome).await.is_err() {
                warn!("an update finished after the agent stopped serving");
            }
        });
        None
    }

    /// Starts a log read and returns the immediate reply when one of its kind still runs.
    pub(crate) fn start_logs(
        &self,
        log_dir: PathBuf,
        request: ControlMessage,
        replies: Replies,
    ) -> Option<ControlMessage> {
        let Some(hold) = self.logs.claim() else {
            return Some(ControlMessage::DeviceLogsError {
                error: request_error::BUSY.to_string(),
            });
        };
        tokio::spawn(async move {
            let reply =
                tokio::time::timeout(DEADLINE, crate::device_logs::answer(log_dir, request, hold))
                    .await
                    .unwrap_or_else(|_| ControlMessage::DeviceLogsError {
                        error: request_error::TIMED_OUT.to_string(),
                    });
            deliver(&replies, reply).await;
        });
        None
    }

    /// Starts a hardware report and returns the immediate reply when one still runs.
    pub(crate) fn start_hardware(&self, replies: Replies) -> Option<ControlMessage> {
        let Some(hold) = self.hardware.claim() else {
            return Some(ControlMessage::HardwareReportError {
                error: request_error::BUSY.to_string(),
            });
        };
        tokio::spawn(async move {
            let work = blocking(hold, crate::hardware_report::collect_hardware_info);
            let reply = match tokio::time::timeout(DEADLINE, work).await {
                Ok(Ok(report)) => report,
                Ok(Err(_)) => ControlMessage::HardwareReportError {
                    error: "failed to collect hardware info".to_string(),
                },
                Err(_) => ControlMessage::HardwareReportError {
                    error: request_error::TIMED_OUT.to_string(),
                },
            };
            deliver(&replies, reply).await;
        });
        None
    }
}

/// Runs blocking work on the blocking pool, holding its kind busy until the work itself ends,
/// which can be after the request's deadline has answered the server.
pub(crate) async fn blocking<T: Send + 'static>(
    hold: Hold,
    work: impl FnOnce() -> T + Send + 'static,
) -> Result<T, tokio::task::JoinError> {
    tokio::task::spawn_blocking(move || {
        let _busy = hold;
        work()
    })
    .await
}

async fn deliver(replies: &Replies, reply: ControlMessage) {
    if replies.send(reply).await.is_err() {
        warn!("a request finished after its connection ended; its reply is dropped");
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn a_lane_is_busy_until_its_blocking_work_ends_past_the_deadline() {
        let lane = Lane::default();
        let hold = lane.claim().expect("a free lane");
        assert!(
            lane.claim().is_none(),
            "a second request of the kind is refused"
        );

        let (release, wait) = std::sync::mpsc::channel::<()>();
        let work = blocking(hold, move || wait.recv().is_ok());
        assert!(
            tokio::time::timeout(Duration::from_millis(50), work)
                .await
                .is_err(),
            "the work outlives its deadline"
        );
        assert!(
            lane.claim().is_none(),
            "the abandoned work still holds its lane"
        );

        release.send(()).unwrap();
        let deadline = tokio::time::Instant::now() + Duration::from_secs(5);
        let next = loop {
            if let Some(hold) = lane.claim() {
                break hold;
            }
            assert!(
                tokio::time::Instant::now() < deadline,
                "the lane was never freed"
            );
            tokio::time::sleep(Duration::from_millis(5)).await;
        };
        drop(next);
    }

    #[tokio::test]
    async fn a_second_hardware_request_while_one_runs_is_answered_busy() {
        let requests = Requests::default();
        let _running = requests.hardware.claim().expect("a free lane");
        let (replies, _rx) = mpsc::channel(REPLY_ROOM);
        assert_eq!(
            requests.start_hardware(replies),
            Some(ControlMessage::HardwareReportError {
                error: request_error::BUSY.to_string()
            })
        );
    }

    #[tokio::test]
    async fn a_reply_after_its_connection_ended_is_dropped_quietly() {
        let (replies, rx) = mpsc::channel(REPLY_ROOM);
        drop(rx);
        deliver(&replies, ControlMessage::RequestHardwareReport).await;
    }

    #[tokio::test]
    async fn a_hardware_report_is_handed_back_and_frees_its_lane() {
        let requests = Requests::default();
        let (replies, mut rx) = mpsc::channel(REPLY_ROOM);
        assert!(requests.start_hardware(replies).is_none());
        let reply = tokio::time::timeout(Duration::from_secs(30), rx.recv())
            .await
            .expect("a reply")
            .expect("an open channel");
        assert!(
            matches!(reply, ControlMessage::HardwareReport { .. }),
            "{reply:?}"
        );
        let deadline = tokio::time::Instant::now() + Duration::from_secs(5);
        while requests.hardware.claim().is_none() {
            assert!(
                tokio::time::Instant::now() < deadline,
                "the lane was never freed"
            );
            tokio::time::sleep(Duration::from_millis(5)).await;
        }
    }
}
