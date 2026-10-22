//! The connection loop: connect, hand-shake, register, serve the control stream, and act on how
//! each connection ended.

use std::collections::HashMap;
use std::path::PathBuf;
use std::sync::Arc;
use std::time::{Duration, Instant};

use anyhow::Result;
use tracing::{debug, error, info, warn};

use crate::client_tls::{self, ClientTls, ConnectFailure, Connected, Link};
use crate::lifecycle::{self, Next, Paths, Verdict};
use crate::requests::{self, Replies, Requests, UpdateOrder, UpdateOutcome};
use crate::{backfill_loop, cert_renewal, edge_sentinel, handshake, retro_job, EXIT_CODE_RESTART};
use mesh_agent_core::identity_store::{self, Commit};
use mesh_protocol::{close_code, ControlMessage};

type Control = mesh_agent_core::AgentConnection<
    mesh_agent_core::AsyncControlStream<tokio::io::Join<quinn::RecvStream, quinn::SendStream>>,
>;

/// Telemetry the background tasks hand to the control loop, drained on each heartbeat.
pub(crate) struct Telemetry {
    pub(crate) health_rx: std::sync::mpsc::Receiver<ControlMessage>,
    pub(crate) host_metric_rx: std::sync::mpsc::Receiver<ControlMessage>,
    pub(crate) discovery_rx: std::sync::mpsc::Receiver<ControlMessage>,
}

/// State shared with the background tasks that the control loop installs into.
pub(crate) struct Shared {
    pub(crate) alert_sink: mesh_agent_core::alerts::AlertSink,
    pub(crate) alert_rules_mailbox: edge_sentinel::AlertRulesMailbox,
    pub(crate) installed_rules: retro_job::InstalledRules,
    pub(crate) maintenance: mesh_agent_core::maintenance::MaintenanceGate,
}

/// A post-update verification still waiting for the server's acknowledgement.
pub(crate) struct PendingUpdate {
    pub(crate) watchdog_cancel: Arc<tokio::sync::Notify>,
}

/// Everything the loop owns across connections.
pub(crate) struct Agent {
    pub(crate) paths: Paths,
    pub(crate) server_addr: String,
    pub(crate) log_dir: PathBuf,
    pub(crate) identity: mesh_agent_core::AgentIdentity,
    pub(crate) tls: ClientTls,
    pub(crate) endpoint: quinn::Endpoint,
    pub(crate) service: Box<dyn mesh_agent_core::ServiceLifecycle>,
    pub(crate) update_config: Option<Arc<mesh_agent_core::UpdateConfig>>,
    pub(crate) pending_update: Option<PendingUpdate>,
    pub(crate) telemetry: Telemetry,
    pub(crate) shared: Shared,
    pub(crate) backfill: Option<backfill_loop::BackfillCoordinator>,
    pub(crate) renewal: cert_renewal::Renewal,
    pub(crate) requests: Requests,
    // `cached_ca_hash` comes from the first ServerHello and switches reconnects to SkipAuth;
    // `force_full_next` forces one full handshake after the server refused the fast path.
    pub(crate) cached_ca_hash: Option<[u8; 48]>,
    pub(crate) force_full_next: bool,
}

/// How one connection ended.
enum End {
    Shutdown,
    Verdict(Verdict),
    /// The connection dropped; `code` is the close code the server gave, when it gave one.
    Dropped {
        code: Option<u32>,
    },
    /// The server closed a session after the agent installed a renewal on it.
    Renewed,
}

/// What the loop learned while serving one connection, and where its requests reply.
struct ConnectionState {
    acknowledged: bool,
    // A renewal installed here makes the server's close a planned one, which skips the backoff.
    renewed: bool,
    replies: Replies,
}

/// Rate-limits a log line to once an hour per reason.
#[derive(Default)]
struct HourlyLog {
    last: HashMap<&'static str, Instant>,
}

impl HourlyLog {
    const PERIOD: Duration = Duration::from_secs(3600);

    fn due(&mut self, key: &'static str) -> bool {
        let now = Instant::now();
        match self.last.get(key) {
            Some(at) if now.duration_since(*at) < Self::PERIOD => false,
            _ => {
                self.last.insert(key, now);
                true
            }
        }
    }
}

/// Runs connections until `sigterm` or SIGINT, a removal, or a restart into a new identity.
pub(crate) async fn run(mut agent: Agent, mut sigterm: tokio::signal::unix::Signal) -> Result<()> {
    // The flap guard bounds the reconnect rate after a session drops right after registering,
    // and jitter de-synchronises a reconnecting herd.
    let mut governor = mesh_agent_core::ReconnectGovernor::new();
    let mut rng = rand::rng();
    let mut hourly = HourlyLog::default();

    loop {
        let connected = tokio::select! {
            biased;
            _ = sigterm.recv() => break,
            _ = tokio::signal::ctrl_c() => break,
            result = connect_until_up(&agent, &mut hourly) => result,
        };
        let Connected { link, send, recv } = match connected {
            Ok(connected) => connected,
            Err(verdict) => {
                agent.act_on(verdict).await?;
                continue;
            }
        };

        let used_fast_path = agent.cached_ca_hash.is_some() && !agent.force_full_next;
        let started = Instant::now();
        let cached = used_fast_path.then_some(agent.cached_ca_hash).flatten();
        let end = match agent.open(send, recv, cached).await {
            Ok((ca_hash, control)) => {
                if let Some(hash) = ca_hash {
                    agent.cached_ca_hash = Some(hash);
                }
                agent.force_full_next = false;
                info!(
                    fast_path = used_fast_path,
                    "registration sent, entering control loop"
                );
                agent.serve(&link, control, &mut sigterm).await
            }
            Err(e) => {
                warn!(error = %e, "connection failed before registration, will reconnect");
                End::Dropped {
                    code: closed_with(&link.conn),
                }
            }
        };

        match end {
            End::Shutdown => break,
            End::Renewed => continue,
            End::Verdict(verdict) => {
                agent.act_on(verdict).await?;
                continue;
            }
            End::Dropped { code } => match code {
                Some(close_code::DEVICE_DELETED) => {
                    info!("device deregistered by server");
                    agent.act_on(Verdict::Deleted).await?;
                    continue;
                }
                Some(close_code::CERTIFICATE_REFUSED) => {
                    if hourly.due("certificate refused") {
                        warn!("this machine's certificate refused by the server, retrying");
                    }
                    agent.act_on(Verdict::CertificateRefused).await?;
                }
                Some(close_code::HANDSHAKE_REFUSED) if used_fast_path => {
                    agent.force_full_next = true
                }
                Some(close_code::REPLACED_BY_NEWER_CONNECTION) if hourly.due("replaced") => {
                    warn!("another connection took this machine's identity");
                }
                _ => {}
            },
        }

        // A drop within the stability window backs off before reconnecting; a long session
        // resets the backoff.
        if let Some(delay) = governor.record_disconnect(started.elapsed(), &mut rng) {
            warn!(
                delay_ms = delay.as_millis(),
                flap_count = governor.flap_count(),
                "connection dropped shortly after registering, backing off before reconnect"
            );
            tokio::time::sleep(delay).await;
        }
    }

    agent.service.notify_stopping();
    info!("mesh-agent stopped");
    Ok(())
}

/// Connects, retrying with jittered backoff; a refused chain or certificate is a verdict when a
/// note offers a way out, and is retried and logged hourly otherwise.
async fn connect_until_up(agent: &Agent, hourly: &mut HourlyLog) -> Result<Connected, Verdict> {
    let mut rng = rand::rng();
    let mut attempt = 0u32;
    loop {
        match client_tls::connect(&agent.endpoint, &agent.tls, &agent.server_addr).await {
            Ok(connected) => {
                info!(attempt, "connected");
                return Ok(connected);
            }
            Err(failure) => {
                let verdict = match &failure {
                    ConnectFailure::ChainRefused(_) => Some(Verdict::ChainRefused),
                    ConnectFailure::CertificateRefused(_) => Some(Verdict::CertificateRefused),
                    ConnectFailure::Unreachable(_) => None,
                };
                if let Some(verdict) = verdict {
                    if agent.paths.read_note().is_some() {
                        return Err(verdict);
                    }
                    log_refusal(hourly, verdict, &failure);
                }
                warn!(attempt, error = %failure, "connection attempt failed");
                let delay = mesh_agent_core::full_jitter(
                    Duration::from_secs(1),
                    Duration::from_secs(30),
                    attempt,
                    &mut rng,
                );
                tokio::time::sleep(delay).await;
                attempt = attempt.saturating_add(1);
            }
        }
    }
}

fn log_refusal(hourly: &mut HourlyLog, verdict: Verdict, failure: &ConnectFailure) {
    match verdict {
        Verdict::ChainRefused if hourly.due("chain refused") => error!(
            error = %failure,
            "the server's master key is unknown; reinstall this machine, or uninstall it by hand"
        ),
        Verdict::CertificateRefused if hourly.due("certificate refused") => {
            warn!(error = %failure, "this machine's certificate refused by the server, retrying")
        }
        _ => {}
    }
}

/// The application close code the server ended the connection with, if any.
fn closed_with(conn: &quinn::Connection) -> Option<u32> {
    match conn.close_reason() {
        Some(quinn::ConnectionError::ApplicationClosed(close)) => {
            u32::try_from(close.error_code.into_inner()).ok()
        }
        _ => None,
    }
}

impl Agent {
    /// Acts on a verdict: start over with a note, remove the machine, or keep retrying.
    async fn act_on(&mut self, verdict: Verdict) -> Result<()> {
        match lifecycle::decide_after(verdict, self.paths.read_note()) {
            Next::StartOver(note) => {
                info!(?verdict, "starting over with the install note");
                lifecycle::start_over(&self.paths, &note).await?;
                info!("restarting with the new identity");
                self.service.notify_stopping();
                std::process::exit(EXIT_CODE_RESTART);
            }
            Next::Remove => {
                self.service.notify_stopping();
                lifecycle::self_remove(&self.paths)
            }
            Next::Retry => Ok(()),
        }
    }

    /// Hand-shakes and registers; returns the fingerprint a full handshake learned.
    async fn open(
        &mut self,
        mut send: quinn::SendStream,
        mut recv: quinn::RecvStream,
        cached: Option<[u8; 48]>,
    ) -> Result<(Option<[u8; 48]>, Control)> {
        let ca_hash = match cached {
            Some(hash) => {
                handshake::perform_fast_handshake(&mut send, &hash).await?;
                None
            }
            None => Some(
                handshake::perform_full_handshake(&mut send, &mut recv, &self.identity.cert_der)
                    .await?,
            ),
        };
        let mut control = mesh_agent_core::AgentConnection::new(
            mesh_agent_core::AsyncControlStream::new(tokio::io::join(recv, send)),
        );
        control
            .send_control(ControlMessage::AgentRegister {
                capabilities: crate::agent_capabilities(self.backfill.is_some()),
                hostname: gethostname::gethostname().to_string_lossy().to_string(),
                os: crate::os_pretty_name(),
                arch: std::env::consts::ARCH.to_string(),
                version: env!("AGENT_VERSION").to_string(),
            })
            .await?;
        // The server pushes `SetMaintenanceMode(true)` only for a device in maintenance, so every
        // registration starts Active.
        self.shared.maintenance.set(false);
        Ok((ca_hash, control))
    }

    /// The first acknowledgement on a connection: the note and an update awaiting verification
    /// are settled by it, never by the agent's own registration.
    async fn on_acknowledged(&mut self, state: &mut ConnectionState) {
        if state.acknowledged {
            return;
        }
        state.acknowledged = true;
        if self.paths.note().exists() {
            self.paths.consume_note();
        }
        if let Some(pending) = self.pending_update.take() {
            pending.watchdog_cancel.notify_one();
            mesh_agent_core::update::clear_update_pending(&self.paths.data_dir).await;
            mesh_agent_core::update::reset_rollback_count(&self.paths.data_dir).await;
            info!("post-update verification passed, sentinel cleared");
        }
    }

    /// Saves a changed trust bundle that verifies the current connection's server.
    fn on_trust_bundle(&mut self, link: &Link, bundle: &str) {
        let current = std::fs::read_to_string(&self.paths.server_ca).unwrap_or_default();
        if bundle.is_empty() || bundle == current {
            return;
        }
        let chain = link.server_chain();
        if client_tls::parse_bundle(bundle).is_err()
            || !client_tls::bundle_verifies_chain(bundle, &chain, &link.server_name)
        {
            warn!("ignoring a trust bundle that does not verify this server");
            return;
        }
        let commit = Commit::new().write(&self.paths.server_ca, bundle.to_string());
        match identity_store::commit(&self.paths.data_dir, commit) {
            Ok(()) => info!("trust bundle updated"),
            Err(e) => warn!(error = %e, "could not save the trust bundle"),
        }
    }

    /// Serves one registered connection until it ends.
    async fn serve(
        &mut self,
        link: &Link,
        mut control: Control,
        sigterm: &mut tokio::signal::unix::Signal,
    ) -> End {
        let (replies, mut replies_rx) = tokio::sync::mpsc::channel(requests::REPLY_ROOM);
        let mut state = ConnectionState {
            acknowledged: false,
            renewed: false,
            replies,
        };
        // `bf_send_at` paces the drain to the granted rate and `bf_retry_at` schedules a deferred
        // re-request; both stay `None` until a grant or defer arrives.
        let mut bf_send_at: Option<tokio::time::Instant> = None;
        let mut bf_retry_at: Option<tokio::time::Instant> = None;
        if let Some(bf) = self.backfill.as_mut() {
            if let Some(msg) = bf.start_session().await {
                match control.send_control(msg).await {
                    Ok(()) => debug!("backfill: requested admission slot"),
                    Err(e) => warn!(error = %e, "backfill slot request failed"),
                }
            }
        }

        let mut heartbeat = tokio::time::interval(Duration::from_secs(60));
        heartbeat.tick().await;
        loop {
            tokio::select! {
                biased;
                _ = tokio::signal::ctrl_c() => {
                    info!("received SIGINT, shutting down");
                    return End::Shutdown;
                }
                _ = sigterm.recv() => {
                    info!("received SIGTERM, shutting down");
                    return End::Shutdown;
                }
                _ = heartbeat.tick() => {
                    if !self.on_heartbeat(&mut control).await {
                        break;
                    }
                }
                Some(reply) = replies_rx.recv() => {
                    if let Err(e) = control.send_control(reply).await {
                        warn!(error = %e, "request reply failed, will reconnect");
                        break;
                    }
                }
                Some(outcome) = self.requests.outcomes.recv() => {
                    self.on_update_outcome(&mut control, outcome).await;
                }
                _ = sleep_until_opt(bf_send_at) => {
                    // Sends one batch, or a re-slot request when the grant expired mid-drain.
                    bf_send_at = None;
                    if let Some(msg) = self.next_backfill_batch().await {
                        if let Err(e) = control.send_control(msg).await {
                            warn!(error = %e, "backfill send failed, will reconnect");
                            break;
                        }
                    }
                }
                _ = sleep_until_opt(bf_retry_at) => {
                    // A deferred admission slot's jittered retry has elapsed.
                    bf_retry_at = None;
                    if let Some(msg) = self.restart_backfill().await {
                        if let Err(e) = control.send_control(msg).await {
                            warn!(error = %e, "backfill retry request failed, will reconnect");
                            break;
                        }
                    }
                }
                msg = control.receive_control() => {
                    let msg = match msg {
                        Ok(msg) => msg,
                        Err(mesh_agent_core::ConnectionError::PingAnswered) => continue,
                        Err(mesh_agent_core::ConnectionError::Io(_)) => {
                            warn!("connection lost, will reconnect");
                            break;
                        }
                        Err(e) => {
                            warn!(error = %e, "control error, will reconnect");
                            break;
                        }
                    };
                    match self.dispatch(link, &mut control, &mut state, msg).await {
                        Flow::Continue => {}
                        Flow::Reconnect => break,
                        Flow::End(end) => return end,
                        Flow::Backfill { send_at, retry_at } => {
                            bf_send_at = send_at.or(bf_send_at);
                            bf_retry_at = retry_at.or(bf_retry_at);
                        }
                    }
                }
            }
        }
        let code = closed_with(&link.conn);
        if state.renewed {
            return End::Renewed;
        }
        End::Dropped { code }
    }

    /// Sends the heartbeat, the telemetry the background tasks queued, and the queued alerts;
    /// returns whether the connection is still usable.
    async fn on_heartbeat(&mut self, control: &mut Control) -> bool {
        let now = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .map(|d| d.as_secs() as i64)
            .unwrap_or(0);
        if let Err(e) = control
            .send_control(ControlMessage::AgentHeartbeat { timestamp: now })
            .await
        {
            warn!(error = %e, "heartbeat failed, will reconnect");
            return false;
        }
        debug!("heartbeat sent");
        if self
            .backfill
            .as_ref()
            .is_some_and(backfill_loop::BackfillCoordinator::is_draining)
        {
            debug!("backfill: reconnect drain in progress");
        }

        // Draining into a Vec first keeps every receiver out of the send await.
        let t = &self.telemetry;
        let mut windows: Vec<ControlMessage> =
            std::iter::from_fn(|| t.host_metric_rx.try_recv().ok()).collect();
        windows.extend(std::iter::from_fn(|| t.discovery_rx.try_recv().ok()));
        windows.extend(std::iter::from_fn(|| t.health_rx.try_recv().ok()));
        for window in windows {
            if let Err(e) = control.send_control(window).await {
                warn!(error = %e, "telemetry send failed, will reconnect");
                return false;
            }
        }

        // Alerts go last: a failed telemetry send leaves them queued in the sink.
        crate::send_queued_alerts(control, &self.shared.alert_sink).await
    }

    async fn next_backfill_batch(&mut self) -> Option<ControlMessage> {
        self.backfill.as_mut()?.next_batch().await
    }

    async fn restart_backfill(&mut self) -> Option<ControlMessage> {
        self.backfill.as_mut()?.start_session().await
    }
}

/// What handling one message leaves the loop to do.
enum Flow {
    Continue,
    Reconnect,
    End(End),
    Backfill {
        send_at: Option<tokio::time::Instant>,
        retry_at: Option<tokio::time::Instant>,
    },
}

impl Agent {
    async fn dispatch(
        &mut self,
        link: &Link,
        control: &mut Control,
        state: &mut ConnectionState,
        msg: ControlMessage,
    ) -> Flow {
        match msg {
            ControlMessage::TrustBundle { ca_bundle } => {
                self.on_trust_bundle(link, &ca_bundle);
                self.on_acknowledged(state).await;
                Flow::Continue
            }
            ControlMessage::RenewCertificate { reason } => {
                info!(reason, "the server asked for a certificate renewal");
                match self
                    .renewal
                    .request(&self.identity.device_id, Instant::now())
                {
                    Ok(request) => match control.send_control(request).await {
                        Ok(()) => Flow::Continue,
                        Err(e) => {
                            warn!(error = %e, "renewal request failed, will reconnect");
                            Flow::Reconnect
                        }
                    },
                    Err(e) => {
                        warn!(error = %e, "could not build a renewal request");
                        Flow::Continue
                    }
                }
            }
            ControlMessage::CertificateIssued { cert, ca_bundle } => {
                self.on_certificate_issued(link, control, state, cert, &ca_bundle)
                    .await
            }
            ControlMessage::CertificateRefused {
                reason,
                retry_after,
            } => {
                warn!(
                    reason,
                    retry_after, "the server refused the certificate renewal"
                );
                self.renewal.refused();
                Flow::Continue
            }
            ControlMessage::AgentDeregistered { reason } => {
                warn!(reason, "device deregistered by server, cleaning up");
                // Hanging up ends the server's wait for this machine at once.
                link.conn.close(close_code::BYE.into(), b"bye");
                Flow::End(End::Verdict(Verdict::Deleted))
            }
            ControlMessage::RestartAgent { reason } => {
                info!(reason, "restart requested by server, exiting with code 42");
                self.service.notify_stopping();
                std::process::exit(EXIT_CODE_RESTART);
            }
            ControlMessage::SetMaintenanceMode { enabled } => {
                // The applied state is echoed so the server can track convergence.
                self.shared.maintenance.set(enabled);
                info!(enabled, "maintenance mode set by server");
                let applied = ControlMessage::MaintenanceApplied { enabled };
                if let Err(e) = control.send_control(applied).await {
                    warn!(error = %e, "failed to send MaintenanceApplied, will reconnect");
                    return Flow::Reconnect;
                }
                Flow::Continue
            }
            ControlMessage::PushAlertRules {
                rules,
                device_hourly_ceiling,
            } => {
                self.install_rules(rules, device_hourly_ceiling);
                Flow::Continue
            }
            other => self.dispatch_request(control, state, other).await,
        }
    }

    /// Checks, saves and starts using a renewed certificate, then tells the server it is installed;
    /// the issue is the server's acknowledgement too.
    async fn on_certificate_issued(
        &mut self,
        link: &Link,
        control: &mut Control,
        state: &mut ConnectionState,
        cert: Vec<u8>,
        bundle: &str,
    ) -> Flow {
        let chain = link.server_chain();
        let server = cert_renewal::ServerSeen {
            chain: &chain,
            name: &link.server_name,
        };
        let installed = match self.renewal.issued(
            &self.identity.device_id,
            cert,
            bundle,
            server,
            Instant::now(),
        ) {
            Ok(installed) => installed,
            Err(why) => {
                warn!(
                    ?why,
                    "ignoring an issued certificate that could not be installed"
                );
                return Flow::Continue;
            }
        };
        if let Err(e) = cert_renewal::save(&installed, &self.paths) {
            warn!(error = %e, "could not save the renewed certificate");
            return Flow::Continue;
        }
        self.identity = installed.identity;
        self.tls = installed.tls;
        info!(
            not_after = ?x509_not_after(&self.identity.cert_der),
            presenting = %self.tls.presented_certificate().map(|c| short_fingerprint(&c)).unwrap_or_default(),
            "certificate renewed and saved"
        );
        // A renewal can move the fingerprint the fast path presents, so the next connection is full.
        self.cached_ca_hash = None;
        state.renewed = true;
        self.on_acknowledged(state).await;
        if let Err(e) = control
            .send_control(ControlMessage::CertificateInstalled)
            .await
        {
            warn!(error = %e, "install notice failed; the next connection reports the renewal");
            return Flow::Reconnect;
        }
        Flow::Continue
    }

    /// Installs a ruleset: the sampler reads the mailbox on its next tick, and the per-machine
    /// alert allowance applies on arrival.
    fn install_rules(
        &mut self,
        rules: Vec<mesh_protocol::ThresholdRule>,
        device_hourly_ceiling: u32,
    ) {
        debug!(
            count = rules.len(),
            ceiling = device_hourly_ceiling,
            "edge-sentinel: threshold-alert ruleset received"
        );
        self.shared.alert_sink.set_ceiling(device_hourly_ceiling);
        if let Ok(mut installed) = self.shared.installed_rules.lock() {
            installed.clone_from(&rules);
        }
        if let Ok(mut slot) = self.shared.alert_rules_mailbox.lock() {
            *slot = Some(rules);
        }
    }

    /// Handles a server request that sends something back.
    async fn dispatch_request(
        &mut self,
        control: &mut Control,
        state: &ConnectionState,
        msg: ControlMessage,
    ) -> Flow {
        match msg {
            ControlMessage::SessionRequest {
                token,
                relay_url,
                permissions,
                ice_servers,
            } => {
                let capture: Box<dyn mesh_agent_core::ScreenCapture> =
                    Box::new(mesh_agent_core::NullCapture);
                let injector: Box<dyn mesh_agent_core::InputInjector> =
                    Box::new(mesh_agent_core::NullInput);
                if let Err(e) = control
                    .handle_session_request(
                        token,
                        relay_url,
                        permissions,
                        ice_servers,
                        capture,
                        injector,
                    )
                    .await
                {
                    warn!(error = %e, "failed to accept session");
                }
                Flow::Continue
            }
            ControlMessage::AgentUpdate {
                version,
                url,
                sha256,
                signature,
            } => {
                let order = UpdateOrder {
                    version,
                    url,
                    sha256,
                    signature,
                };
                let busy = self.start_update(control, order).await;
                answer_now(control, busy).await
            }
            ControlMessage::RequestHardwareReport => {
                info!("hardware report requested by server");
                let busy = self.requests.start_hardware(state.replies.clone());
                answer_now(control, busy).await
            }
            ControlMessage::RequestDeviceLogs { .. } => {
                let busy =
                    self.requests
                        .start_logs(self.log_dir.clone(), msg, state.replies.clone());
                answer_now(control, busy).await
            }
            other => self.dispatch_backfill(control, other).await,
        }
    }

    async fn dispatch_backfill(&mut self, control: &mut Control, msg: ControlMessage) -> Flow {
        let Some(bf) = self.backfill.as_mut() else {
            return Flow::Continue;
        };
        match msg {
            ControlMessage::GrantBackfill { rate, deadline } => {
                info!(rate, deadline, "backfill: admission slot granted");
                bf.on_grant(rate, deadline);
                Flow::Backfill {
                    send_at: Some(tokio::time::Instant::now()),
                    retry_at: None,
                }
            }
            ControlMessage::DeferBackfill { retry_after } => {
                let delay = bf.on_defer(retry_after);
                debug!(retry_after, delay_s = delay.as_secs(), "backfill: deferred");
                Flow::Backfill {
                    send_at: None,
                    retry_at: Some(tokio::time::Instant::now() + delay),
                }
            }
            ControlMessage::MetricBackfillAck { tier, cursor } => {
                let delay = bf.on_ack(tier, cursor).await;
                Flow::Backfill {
                    send_at: Some(tokio::time::Instant::now() + delay),
                    retry_at: None,
                }
            }
            ControlMessage::RequestLocalHistory {
                dim,
                from_ts,
                to_ts,
                max_points,
            } => {
                let resp = bf.answer_history(dim, from_ts, to_ts, max_points).await;
                if let Err(e) = control.send_control(resp).await {
                    warn!(error = %e, "failed to send local-history response");
                }
                Flow::Continue
            }
            _ => Flow::Continue,
        }
    }

    /// Starts an update beside the loop; returns the acknowledgement owed at once, when one is.
    async fn start_update(
        &mut self,
        control: &mut Control,
        order: UpdateOrder,
    ) -> Option<ControlMessage> {
        let config = self.update_config.clone()?;
        if crate::should_skip_version(&order.version) {
            info!(
                version = order.version,
                "update skipped: already up to date"
            );
            crate::send_update_ack(control, order.version, true, "already up to date".into()).await;
            return None;
        }
        info!(version = order.version, "update started");
        self.requests.start_update(config, order)
    }

    /// Acknowledges a finished update, and restarts into the new binary when it was applied.
    async fn on_update_outcome(&mut self, control: &mut Control, outcome: UpdateOutcome) {
        let UpdateOutcome { version, result } = outcome;
        match result {
            Ok(true) => {
                info!(version, "update applied, restarting");
                crate::send_update_ack(control, version, true, String::new()).await;
                self.service.notify_stopping();
                std::process::exit(EXIT_CODE_RESTART);
            }
            Ok(false) => info!("update skipped (same version)"),
            Err(e) => {
                error!(error = %e, "update failed");
                crate::send_update_ack(control, version, false, e).await;
            }
        }
    }
}

/// Sends a reply owed at once, such as a busy answer; the connection keeps serving either way.
async fn answer_now(control: &mut Control, reply: Option<ControlMessage>) -> Flow {
    if let Some(reply) = reply {
        if let Err(e) = control.send_control(reply).await {
            warn!(error = %e, "failed to send a reply, will reconnect");
            return Flow::Reconnect;
        }
    }
    Flow::Continue
}

/// Sleeps until `at`, or forever when `at` is `None`, so a `select!` arm can hold an optional
/// deadline.
async fn sleep_until_opt(at: Option<tokio::time::Instant>) {
    match at {
        Some(t) => tokio::time::sleep_until(t).await,
        None => std::future::pending::<()>().await,
    }
}

/// The expiry a certificate states, for the log line that follows a renewal.
fn x509_not_after(cert_der: &[u8]) -> Option<String> {
    x509_parser::parse_x509_certificate(cert_der)
        .ok()
        .map(|(_, cert)| cert.validity().not_after.to_string())
}

/// The first bytes of a certificate's SHA-256, as the server's refusal log prints them.
fn short_fingerprint(cert_der: &[u8]) -> String {
    use sha2::{Digest, Sha256};
    hex::encode(&Sha256::digest(cert_der)[..6])
}
