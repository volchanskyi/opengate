use std::path::PathBuf;
use std::sync::mpsc::SyncSender;
use std::sync::{Arc, Mutex};
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use tracing::{debug, info, warn};

use crate::host_logs::{build_log_rate_window, collect_host_logs, LogSource};
use mesh_agent_core::maintenance::{MaintenanceGate, MaintenanceTransition};
use mesh_agent_core::ml::store_sink::LocalStoreSink;
use mesh_protocol::{ControlMessage, ThresholdRule};

/// The sampler-owned local store, shared with the WS-15 backfill coordinator on
/// the control loop. The sampler holds the lock only for the sub-millisecond
/// `record`/`commit` each second; the coordinator holds it only to open an MVCC
/// snapshot or advance a cursor — neither blocks the other for a meaningful time.
pub(crate) type SharedSink = Arc<Mutex<LocalStoreSink>>;

/// Interval between host log-rate windows. Long enough that the bounded reads
/// never compete with control or session traffic on constrained fleet hosts.
const LOG_READER_INTERVAL: Duration = Duration::from_secs(60);

/// Host log sources the reader task sweeps each window. Sources that do not
/// apply to the current platform collect nothing and are skipped.
const LOG_SOURCES: [LogSource; 3] = [
    LogSource::AgentSelf,
    LogSource::Journald,
    LogSource::WindowsEventLog,
];

/// Spawn the bounded host log-rate reader task. Each window it
/// reads a capped slice of every host source, folds it into the log-rate
/// feature vector (level counts + top-unit ranks + volume — never message text),
/// and forwards it to the control loop as a metric window over `sink`. A window
/// is dropped when the channel is full so a log burst can never backpressure the
/// control stream. The task yields for [`LOG_READER_INTERVAL`] between windows.
pub(crate) fn spawn_log_readers(
    log_dir: PathBuf,
    sink: SyncSender<ControlMessage>,
    maintenance: MaintenanceGate,
) -> tokio::task::JoinHandle<()> {
    tokio::task::spawn_blocking(move || loop {
        std::thread::sleep(LOG_READER_INTERVAL);
        // In maintenance the device is being worked on; skip the window so log
        // churn from the admin's changes never ships or pollutes the baseline.
        if maintenance.in_maintenance() {
            continue;
        }
        for source in LOG_SOURCES {
            let entries = collect_host_logs(source, &log_dir);
            let Some(window) = build_log_rate_window(source, &entries, unix_now()) else {
                continue;
            };
            debug!(
                ?source,
                entries = entries.len(),
                "edge-sentinel log-rate window"
            );
            if sink.try_send(window).is_err() {
                debug!(?source, "log-rate window dropped: telemetry channel full");
            }
        }
    })
}

/// Interval between auto-discovery sweeps. Long by design: the host profile
/// changes rarely, and a sweep shells out to package managers and lists
/// services, so it must never compete with control or session traffic. Reports
/// are change-triggered — a sweep only ships when the profile differs from the
/// last one shipped.
const DISCOVERY_INTERVAL: Duration = Duration::from_secs(1800);

/// Spawn the bounded auto-discovery task (WS-16). Each sweep
/// profiles the host — listening ports, services, DB engines, containers,
/// installed packages — into a bounded, secret-free `DiscoveryReport` and, when
/// the profile changed since the last shipped report, forwards it to the control
/// loop over `sink`. A report is dropped when the channel is full so a sweep can
/// never backpressure the control stream. The first sweep runs immediately; the
/// task then yields for [`DISCOVERY_INTERVAL`] between sweeps.
pub(crate) fn spawn_discovery(
    sink: SyncSender<ControlMessage>,
    maintenance: MaintenanceGate,
) -> tokio::task::JoinHandle<()> {
    tokio::task::spawn_blocking(move || {
        let mut last_fingerprint: Option<u64> = None;
        loop {
            // In maintenance, skip the sweep entirely: services and ports churn
            // as the admin works, and shipping that would flap the Discovered
            // Footprint. On resume the next sweep ships the settled profile.
            if maintenance.in_maintenance() {
                std::thread::sleep(DISCOVERY_INTERVAL);
                continue;
            }
            let profile = mesh_agent_core::discovery::collect_profile();
            let fingerprint = profile.fingerprint();
            if last_fingerprint != Some(fingerprint) {
                last_fingerprint = Some(fingerprint);
                debug!(
                    ports = profile.ports.len(),
                    services = profile.services.len(),
                    db_engines = profile.db_engines.len(),
                    containers = profile.containers.len(),
                    packages = profile.packages.len(),
                    truncated = profile.truncated,
                    "edge-sentinel discovery report"
                );
                if sink.try_send(profile.into_report(unix_now())).is_err() {
                    debug!("discovery report dropped: telemetry channel full");
                }
            }
            std::thread::sleep(DISCOVERY_INTERVAL);
        }
    })
}

/// Build the WS-19 breach-carrying `AgentHealthSummary` for emission. Only the
/// breach signal is populated; the anomaly-rate fields stay at their defaults and
/// the server leaves the org empty to assign (the summary is investigation-aid
/// only). The server treats a summary with no sampler computation as breach-only
/// and does not record an anomaly-rate sample for it.
fn breach_summary(now: i64, breaches: Vec<mesh_protocol::AlertBreach>) -> ControlMessage {
    ControlMessage::AgentHealthSummary {
        ts: now,
        org_id: String::new(),
        node_anomaly_rate: 0.0,
        per_family_rates: Vec::new(),
        recent_bitmask: Vec::new(),
        sampler_ver: String::new(),
        model_ver: String::new(),
        breaches,
    }
}

/// Decide whether to emit a WS-19 health summary this tick. Emission is throttled
/// to at least [`HEALTH_EMIT_INTERVAL_SECS`] apart and fires while a breach is
/// active — plus once more, when the set has just cleared, to report the clear —
/// staying silent on a steady, breach-free host.
fn should_emit_health(
    last_emit: Option<i64>,
    now: i64,
    breaching: bool,
    last_breaching: bool,
) -> bool {
    let due = last_emit.map_or(breaching, |last| {
        now.saturating_sub(last) >= HEALTH_EMIT_INTERVAL_SECS
    });
    due && (breaching || last_breaching)
}

/// Current Unix time in whole seconds, clamped to 0 before the epoch.
fn unix_now() -> i64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_secs() as i64)
        .unwrap_or(0)
}

/// Local-store wiring for the sampler task: where the redb multi-tier store
/// lives and its footprint cap.
pub(crate) struct StoreConfig {
    /// Store directory (under the agent data dir).
    pub path: PathBuf,
    /// Hard footprint cap in bytes.
    pub cap_bytes: u64,
}

/// Minimum seconds between emitted WS-19 health summaries. Above the server's
/// 10 s telemetry interval floor, so a throttled emission is never dropped for
/// arriving too fast; the first breach after quiet still emits promptly.
const HEALTH_EMIT_INTERVAL_SECS: i64 = 15;

/// Shared slot the control loop drops a freshly-pushed threshold ruleset into
/// and the sampler drains on its next tick (WS-19).
pub(crate) type AlertRulesMailbox = Arc<Mutex<Option<Vec<ThresholdRule>>>>;

/// Wiring for the sampler's WS-19 threshold-alert path: the mailbox the control
/// loop drops a freshly-pushed ruleset into, and the bounded channel the sampler
/// emits breach-carrying health summaries on.
pub(crate) struct AlertWiring {
    /// Latest pushed ruleset; the sampler installs it on its next tick and
    /// clears the slot. Shared with the control loop's `PushAlertRules` handler.
    pub rules: AlertRulesMailbox,
    /// Breach-carrying `AgentHealthSummary` sink, drained by the control loop on
    /// heartbeat alongside log-rate and discovery telemetry.
    pub health_tx: SyncSender<ControlMessage>,
}

/// Samples the required warm-up window before the ensemble can be trained.
const WARMUP_SAMPLES: usize = 30;
/// Ensemble geometry (staggered k=2 models over the CPU/mem/disk feature vector).
const ENSEMBLE_MODELS: usize = 6;
const ENSEMBLE_ITERS: usize = 20;
/// Durable flush cadence for the local store (bounded-loss window, in samples).
const STORE_COMMIT_EVERY: usize = 60;

/// Spawn the Edge-Sentinel sampler task. It samples host metrics once per second,
/// trains a local anomaly ensemble on a warm-up window, and — when `sink` is
/// set — persists each raw sample with its inline anomaly bit into the graduated
/// `LocalTsdb` (the sovereign min/max/last + 1 s raw copy). The store is shared
/// with the WS-15 backfill coordinator; the sampler holds the lock only for the
/// per-second append/commit. A `None` sink degrades to log-only sampling.
///
/// When `alerts` is set, the same 1 s tick also evaluates the tenant-pushed
/// WS-19 threshold ruleset over the sample and emits a breach-carrying
/// `AgentHealthSummary` (throttled, breach-driven, silent when nothing breaches).
pub(crate) fn spawn_sampler(
    sink: Option<SharedSink>,
    alerts: Option<AlertWiring>,
    maintenance: MaintenanceGate,
) -> tokio::task::JoinHandle<()> {
    tokio::task::spawn_blocking(move || {
        use mesh_agent_core::alerts::AlertEvaluator;
        use mesh_agent_core::ml::ensemble::EdgeMlEnsemble;
        use mesh_agent_core::ml::sampler::{MetricSampler, SysinfoSampler};

        let mut sampler = match SysinfoSampler::new(10) {
            Ok(sampler) => sampler,
            Err(e) => {
                warn!(error = %e, "edge-sentinel sampler disabled");
                return;
            }
        };

        let mut warmup: Vec<[f32; 3]> = Vec::with_capacity(WARMUP_SAMPLES);
        let mut ensemble: Option<EdgeMlEnsemble<3>> = None;

        // WS-19 threshold-alert evaluator, fed the tenant ruleset the control
        // loop pushes into the mailbox. Empty until rules arrive; breach
        // emission is throttled and breach-driven.
        let mut alert_eval = AlertEvaluator::new(Vec::new());
        let mut last_health_emit: Option<i64> = None;
        let mut last_breaching = false;

        // Tracks the maintenance→Active edge so the sampler re-baselines when the
        // device leaves maintenance.
        let mut maintenance_edge = MaintenanceTransition::new();

        loop {
            std::thread::sleep(Duration::from_secs(1));

            // Maintenance suppresses all sampler work — no sampling, store write,
            // or alert evaluation — so the admin's disruptive host changes never
            // pollute the anomaly baseline or fire a breach. On leaving
            // maintenance, discard the pre-change ensemble and breach state so the
            // post-change footprint retrains as the new normal (re-baseline).
            let in_maintenance = maintenance.in_maintenance();
            if maintenance_edge.just_exited(in_maintenance) {
                ensemble = None;
                warmup.clear();
                last_health_emit = None;
                last_breaching = false;
                info!("edge-sentinel: left maintenance, re-baselining anomaly detection");
            }
            if in_maintenance {
                continue;
            }

            let sample = match sampler.sample() {
                Ok(sample) => sample,
                Err(e) => {
                    warn!(error = %e, "edge-sentinel sample failed");
                    continue;
                }
            };
            let features = [
                sample.cpu_total_percent,
                sample.memory_used_percent,
                sample.disk_used_percent,
            ];
            // Cold start: collect a warm-up window, then train once. Until the
            // ensemble exists, samples are stored with a `false` anomaly bit.
            let anomaly = match &ensemble {
                Some(model) => model.is_anomaly(&features),
                None => {
                    warmup.push(features);
                    if warmup.len() >= WARMUP_SAMPLES {
                        match EdgeMlEnsemble::<3>::train_staggered(
                            &warmup,
                            ENSEMBLE_MODELS,
                            ENSEMBLE_ITERS,
                        ) {
                            Ok(model) => {
                                info!("edge-sentinel anomaly ensemble trained");
                                ensemble = Some(model);
                            }
                            Err(e) => warn!(error = %e, "edge-sentinel ensemble train failed"),
                        }
                    }
                    false
                }
            };
            debug!(
                cpu = sample.cpu_total_percent,
                mem = sample.memory_used_percent,
                disk = sample.disk_used_percent,
                anomaly,
                "edge-sentinel sample"
            );

            // WS-19: install any freshly-pushed ruleset, evaluate the sample, and
            // emit a breach-carrying health summary. Emission is throttled to
            // >= HEALTH_EMIT_INTERVAL_SECS and only fires while a breach is
            // active (plus one final summary reporting the clear), so a steady
            // host is silent and a burst never backpressures control.
            if let Some(alerts) = alerts.as_ref() {
                let now = unix_now();
                if let Ok(mut slot) = alerts.rules.lock() {
                    if let Some(rules) = slot.take() {
                        debug!(
                            count = rules.len(),
                            "edge-sentinel: alert ruleset installed"
                        );
                        alert_eval.set_rules(rules);
                    }
                }
                let breaches = alert_eval.evaluate(&sample, now);
                let breaching = !breaches.is_empty();
                if should_emit_health(last_health_emit, now, breaching, last_breaching) {
                    if alerts
                        .health_tx
                        .try_send(breach_summary(now, breaches))
                        .is_err()
                    {
                        debug!("edge-sentinel health summary dropped: telemetry channel full");
                    }
                    last_health_emit = Some(now);
                    last_breaching = breaching;
                }
            }

            if let Some(sink) = sink.as_ref() {
                match sink.lock() {
                    Ok(mut sink) => {
                        if let Err(e) = sink.record(unix_now(), &sample, anomaly) {
                            warn!(error = %e, "edge-sentinel store write failed");
                        }
                    }
                    Err(e) => warn!(error = %e, "edge-sentinel store lock poisoned"),
                }
            }
        }
    })
}

/// Open the local store, recreating it fresh on a corrupt/incompatible file, and
/// degrading to `None` (log-only sampling) if even that fails.
pub(crate) fn open_sink(cfg: &StoreConfig) -> Option<LocalStoreSink> {
    match LocalStoreSink::open(&cfg.path, cfg.cap_bytes, STORE_COMMIT_EVERY) {
        Ok(sink) => {
            info!(path = %cfg.path.display(), "edge-sentinel local store opened");
            Some(sink)
        }
        Err(e) => {
            warn!(error = %e, path = %cfg.path.display(), "local store open failed; recreating fresh");
            if let Err(e) = std::fs::remove_dir_all(&cfg.path) {
                debug!(error = %e, "could not remove the old store dir before recreate");
            }
            match LocalStoreSink::open(&cfg.path, cfg.cap_bytes, STORE_COMMIT_EVERY) {
                Ok(sink) => Some(sink),
                Err(e) => {
                    warn!(error = %e, "edge-sentinel local store disabled (sampling continues)");
                    None
                }
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::{breach_summary, should_emit_health, HEALTH_EMIT_INTERVAL_SECS};
    use mesh_protocol::{AlertBreach, ControlMessage};

    #[test]
    fn breach_summary_carries_only_breaches() {
        let breaches = vec![AlertBreach {
            rule_id: "disk-critical".to_string(),
            metric: "disk.used".to_string(),
            value: 96.0,
        }];
        match breach_summary(1_700_000_000, breaches) {
            ControlMessage::AgentHealthSummary {
                ts,
                org_id,
                node_anomaly_rate,
                sampler_ver,
                breaches,
                ..
            } => {
                assert_eq!(ts, 1_700_000_000);
                assert!(org_id.is_empty(), "server assigns the authoritative org");
                assert_eq!(node_anomaly_rate, 0.0);
                assert!(
                    sampler_ver.is_empty(),
                    "breach-only: no sampler computation"
                );
                assert_eq!(breaches.len(), 1);
                assert_eq!(breaches[0].rule_id, "disk-critical");
            }
            other => panic!("expected AgentHealthSummary, got {other:?}"),
        }
    }

    #[test]
    fn first_breach_emits_immediately() {
        assert!(should_emit_health(None, 100, true, false));
    }

    #[test]
    fn no_breach_and_no_prior_emit_stays_silent() {
        assert!(!should_emit_health(None, 100, false, false));
    }

    #[test]
    fn active_breach_is_throttled_between_emits() {
        let last = Some(100);
        // Within the throttle window: suppressed even though still breaching.
        assert!(!should_emit_health(
            last,
            100 + HEALTH_EMIT_INTERVAL_SECS - 1,
            true,
            true
        ));
        // At the window boundary: re-emits the still-active breach.
        assert!(should_emit_health(
            last,
            100 + HEALTH_EMIT_INTERVAL_SECS,
            true,
            true
        ));
    }

    #[test]
    fn clear_is_reported_once_then_silent() {
        let last = Some(100);
        let due = 100 + HEALTH_EMIT_INTERVAL_SECS;
        // Just cleared (breaching=false, last_breaching=true): emits the clear.
        assert!(should_emit_health(last, due, false, true));
        // Already reported clear (last_breaching=false): silent thereafter.
        assert!(!should_emit_health(
            Some(due),
            due + HEALTH_EMIT_INTERVAL_SECS,
            false,
            false
        ));
    }
}
