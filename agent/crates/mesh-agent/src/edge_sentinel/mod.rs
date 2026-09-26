use std::collections::VecDeque;
use std::path::PathBuf;
use std::sync::mpsc::SyncSender;
use std::sync::{Arc, Mutex};
use std::time::Duration;

use tracing::{debug, info, warn};

use crate::clock::unix_now;
use crate::event_watch::EventCoverage;
use mesh_agent_core::alerts::AlertSink;
use mesh_agent_core::maintenance::MaintenanceGate;
use mesh_agent_core::ml::host_metric_stream::HostMetricWindower;
use mesh_agent_core::ml::store_sink::LocalStoreSink;
use mesh_protocol::{ControlMessage, ThresholdRule};

/// The sampler-owned local store, shared with the WS-15 backfill coordinator on
/// the control loop. The sampler holds the lock only for the sub-millisecond
/// `record`/`commit` each second; the coordinator holds it only to open an MVCC
/// snapshot or advance a cursor — neither blocks the other for a meaningful time.
pub(crate) type SharedSink = Arc<Mutex<LocalStoreSink>>;

/// Bit pattern standing for "the sampler has not taken a reading yet". A real
/// percentage is finite, so no reading can collide with it.
const LOAD_NOT_TAKEN: u32 = u32::MAX;

/// The most recent host CPU reading, published by the sampler for anything that
/// needs to know whether the machine is busy.
///
/// Deliberately absent until the sampler has actually taken a reading: work that
/// waits for an idle machine must not treat "nobody has looked" as "idle".
#[derive(Clone)]
pub(crate) struct LoadSignal(Arc<std::sync::atomic::AtomicU32>);

impl LoadSignal {
    /// A signal with no reading in it yet.
    pub(crate) fn new() -> Self {
        Self(Arc::new(std::sync::atomic::AtomicU32::new(LOAD_NOT_TAKEN)))
    }

    /// Publish this second's reading. A reading that is not a finite percentage
    /// is not published at all, rather than stored as a number nothing can
    /// compare.
    pub(crate) fn report(&self, cpu_percent: f32) {
        if cpu_percent.is_finite() {
            self.0
                .store(cpu_percent.to_bits(), std::sync::atomic::Ordering::Relaxed);
        }
    }

    /// The most recent reading, or `None` if there has not been one.
    pub(crate) fn cpu_percent(&self) -> Option<f32> {
        let bits = self.0.load(std::sync::atomic::Ordering::Relaxed);
        (bits != LOAD_NOT_TAKEN).then(|| f32::from_bits(bits))
    }
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
/// breach signal and per-rule coverage are populated; the anomaly-rate fields
/// stay at their defaults and the server leaves the tenant empty to assign (the
/// summary is investigation-aid only). The server treats a summary with no
/// sampler computation as breach-only and does not record an anomaly-rate sample
/// for it.
fn breach_summary(
    now: i64,
    breaches: Vec<mesh_protocol::AlertBreach>,
    rule_coverage: Vec<mesh_protocol::RuleCoverage>,
) -> ControlMessage {
    ControlMessage::AgentHealthSummary {
        ts: now,
        tenant_id: String::new(),
        node_anomaly_rate: 0.0,
        per_family_rates: Vec::new(),
        recent_bitmask: Vec::new(),
        sampler_ver: String::new(),
        model_ver: String::new(),
        breaches,
        rule_coverage,
    }
}

/// Sampler/model version stamped on emitted node anomaly-rate summaries. The
/// server records the anomaly-rate series only for a summary that carries a
/// sampler computation (non-empty version or per-family rates), so this must be
/// set for the fleet-health badge to receive data.
const SAMPLER_VERSION: &str = "edge-ensemble-v1";

/// Rolling window of recent per-second anomaly verdicts the node anomaly rate is
/// computed over — roughly the last minute at the 1 s sample cadence.
const ANOMALY_WINDOW: usize = 64;

/// Minimum seconds between emitted node anomaly-rate summaries. Above the
/// server's 10 s ingest floor and well within its instant-query lookback, so the
/// fleet-health badge always reads a fresh sample from a steady host.
const ANOMALY_EMIT_INTERVAL_SECS: i64 = 60;

/// Fraction of anomalous verdicts in the rolling window, in `[0, 1]`; `0` when
/// the window is empty.
fn window_anomaly_rate(bits: &VecDeque<bool>) -> f64 {
    if bits.is_empty() {
        return 0.0;
    }
    let anomalous = bits.iter().filter(|&&b| b).count();
    anomalous as f64 / bits.len() as f64
}

/// Pack the rolling anomaly window into bytes, oldest verdict first and MSB-first
/// within each byte, so the recent per-sample sequence survives the wire.
fn pack_bitmask(bits: &VecDeque<bool>) -> Vec<u8> {
    let mut out = vec![0u8; bits.len().div_ceil(8)];
    for (i, &bit) in bits.iter().enumerate() {
        if bit {
            out[i / 8] |= 1 << (7 - (i % 8));
        }
    }
    out
}

/// Build the periodic node anomaly-rate summary. Unlike [`breach_summary`] this
/// carries the sampler computation — the rate, its packed verdict history, and a
/// version — which the server records as the `opengate_edge_node_anomaly_rate`
/// series behind the fleet-health badge.
fn anomaly_summary(
    now: i64,
    rate: f64,
    bitmask: Vec<u8>,
    breaches: Vec<mesh_protocol::AlertBreach>,
    rule_coverage: Vec<mesh_protocol::RuleCoverage>,
) -> ControlMessage {
    ControlMessage::AgentHealthSummary {
        ts: now,
        tenant_id: String::new(),
        node_anomaly_rate: rate,
        per_family_rates: Vec::new(),
        recent_bitmask: bitmask,
        sampler_ver: SAMPLER_VERSION.to_string(),
        model_ver: String::new(),
        breaches,
        rule_coverage,
    }
}

/// Whether a node anomaly-rate summary is due this tick, throttled to at least
/// [`ANOMALY_EMIT_INTERVAL_SECS`] apart. The first summary after training (or a
/// re-baseline) emits promptly so the badge populates quickly.
fn should_emit_anomaly(last_emit: Option<i64>, now: i64) -> bool {
    last_emit.is_none_or(|last| now.saturating_sub(last) >= ANOMALY_EMIT_INTERVAL_SECS)
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
    /// Where an alert goes when a rule starts firing. The same queue every
    /// other producer on this machine writes to, so one machine's whole output
    /// shares one bound and one hourly allowance.
    pub alert_sink: AlertSink,
    /// What the rules reading this machine's own words can answer for. They are
    /// evaluated by a different watch entirely, but the estate is told what
    /// every rule is doing in one place, so the report carries both — a rule
    /// missing from the count reads as a rule nobody pushed.
    pub event_coverage: EventCoverage,
}

/// Samples the required warm-up window before the ensemble can be trained.
const WARMUP_SAMPLES: usize = 30;
/// Ensemble geometry (staggered k=2 models over the CPU/mem/disk feature vector).
const ENSEMBLE_MODELS: usize = 6;
const ENSEMBLE_ITERS: usize = 20;
/// Durable flush cadence for the local store (bounded-loss window, in samples).
const STORE_COMMIT_EVERY: usize = 60;

/// Fold one sample into the live host-metric windower and forward a window this
/// tick closed. A full channel drops the window rather than backpressuring the
/// control stream (same contract as discovery/health telemetry). Separated from
/// the sampler loop so the emit contract is unit-testable.
fn emit_host_metric_window(
    windower: &mut HostMetricWindower,
    tx: &SyncSender<ControlMessage>,
    ts: i64,
    sample: &mesh_agent_core::ml::sampler::MetricSample,
) {
    if let Some(window) = windower.push(ts, sample) {
        if tx.try_send(window).is_err() {
            debug!("host-metric window dropped: telemetry channel full");
        }
    }
}

mod raise;
mod tick;

pub(crate) use tick::SamplerOutputs;
use tick::SamplerState;

/// Spawn the Edge-Sentinel sampler task. It samples host metrics once per second,
/// trains a local anomaly ensemble on a warm-up window, and — when a store is
/// set — persists each raw sample with its inline anomaly bit into the graduated
/// `LocalTsdb` (the sovereign min/max/last + 1 s raw copy). The store is shared
/// with the WS-15 backfill coordinator; the sampler holds the lock only for the
/// per-second append/commit. No store degrades to log-only sampling.
///
/// When alert wiring is set, the same 1 s tick also evaluates the tenant-pushed
/// WS-19 threshold ruleset over the sample, raises an alert for every rule that
/// has just started firing, and emits a breach-carrying `AgentHealthSummary`
/// (throttled, breach-driven, silent when nothing breaches).
///
/// When a host-metric channel is set, the same tick folds the sample into a
/// 10 s average and forwards each closed window as an `AgentMetricWindow` — the
/// live host-metric stream that lights up the central Telemetry charts
/// continuously (averaging identical to reconnect-backfill, so the two never
/// diverge). A window is dropped when the channel is full so a burst never
/// backpressures control.
pub(crate) fn spawn_sampler(
    out: SamplerOutputs,
    maintenance: MaintenanceGate,
) -> tokio::task::JoinHandle<()> {
    tokio::task::spawn_blocking(move || {
        use mesh_agent_core::ml::sampler::{MetricSampler, SysinfoSampler};

        let mut sampler = match SysinfoSampler::new(10) {
            Ok(sampler) => sampler,
            Err(e) => {
                warn!(error = %e, "edge-sentinel sampler disabled");
                return;
            }
        };
        let mut state = SamplerState::new();

        loop {
            std::thread::sleep(Duration::from_secs(1));
            if !state.begin_tick(maintenance.in_maintenance()) {
                continue;
            }
            match sampler.sample() {
                Ok(sample) => state.on_sample(&out, &sample, unix_now()),
                Err(e) => warn!(error = %e, "edge-sentinel sample failed"),
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
    use super::test_support::host_sample;
    use super::{
        anomaly_summary, breach_summary, emit_host_metric_window, pack_bitmask,
        should_emit_anomaly, should_emit_health, window_anomaly_rate, ANOMALY_EMIT_INTERVAL_SECS,
        HEALTH_EMIT_INTERVAL_SECS, SAMPLER_VERSION,
    };
    use mesh_agent_core::ml::host_metric_stream::HostMetricWindower;
    use mesh_protocol::{AlertBreach, ControlMessage};
    use std::collections::VecDeque;
    use std::sync::mpsc::sync_channel;

    fn bits(values: &[bool]) -> VecDeque<bool> {
        values.iter().copied().collect()
    }

    /// A closed window (a sample crossing into a later 10 s bucket) is forwarded
    /// on the channel; samples inside one window send nothing yet.
    #[test]
    fn emit_host_metric_window_forwards_closed_windows() {
        let mut windower = HostMetricWindower::new();
        let (tx, rx) = sync_channel::<ControlMessage>(4);

        emit_host_metric_window(&mut windower, &tx, 120, &host_sample(10.0));
        emit_host_metric_window(&mut windower, &tx, 125, &host_sample(30.0));
        assert!(rx.try_recv().is_err(), "an open window sends nothing");

        // A later-window sample closes the 120-window and forwards it.
        emit_host_metric_window(&mut windower, &tx, 180, &host_sample(99.0));
        match rx.try_recv().expect("closed window is forwarded") {
            ControlMessage::AgentMetricWindow { ts, dims, .. } => {
                assert_eq!(ts, 120);
                assert_eq!(dims[0].name, "cpu.total");
                assert_eq!(dims[0].avg, 20.0, "mean(10,30)");
                assert_eq!(dims[1].name, "cpu.total.max");
                assert_eq!(dims[1].avg, 30.0, "the minute's peak, not its mean");
            }
            other => panic!("expected AgentMetricWindow, got {other:?}"),
        }
    }

    /// A full channel drops the closed window silently — a metric burst never
    /// backpressures the control stream.
    #[test]
    fn emit_host_metric_window_drops_when_channel_full() {
        let mut windower = HostMetricWindower::new();
        let (tx, rx) = sync_channel::<ControlMessage>(1);

        // Fill the single channel slot with a first closed window.
        emit_host_metric_window(&mut windower, &tx, 120, &host_sample(10.0));
        emit_host_metric_window(&mut windower, &tx, 180, &host_sample(20.0));
        // The next close finds the channel full; it must drop without panicking.
        emit_host_metric_window(&mut windower, &tx, 240, &host_sample(30.0));

        assert!(rx.try_recv().is_ok(), "the first window occupied the slot");
        assert!(
            rx.try_recv().is_err(),
            "the second window was dropped, not queued"
        );
    }

    #[test]
    fn breach_summary_carries_breaches_and_coverage() {
        let breaches = vec![AlertBreach {
            rule_id: "disk-critical".to_string(),
            metric: "disk.used_percent".to_string(),
            value: 96.0,
        }];
        let coverage = vec![mesh_protocol::RuleCoverage {
            rule_id: "io-stalled".to_string(),
            state: mesh_protocol::RuleCoverageState::Unsupported,
        }];
        match breach_summary(1_700_000_000, breaches, coverage) {
            ControlMessage::AgentHealthSummary {
                ts,
                tenant_id,
                node_anomaly_rate,
                sampler_ver,
                breaches,
                rule_coverage,
                ..
            } => {
                assert_eq!(ts, 1_700_000_000);
                assert!(
                    tenant_id.is_empty(),
                    "server assigns the authoritative tenant"
                );
                assert_eq!(node_anomaly_rate, 0.0);
                assert!(
                    sampler_ver.is_empty(),
                    "breach-only: no sampler computation"
                );
                assert_eq!(breaches.len(), 1);
                assert_eq!(breaches[0].rule_id, "disk-critical");
                assert_eq!(rule_coverage.len(), 1);
                assert_eq!(
                    rule_coverage[0].state,
                    mesh_protocol::RuleCoverageState::Unsupported
                );
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

    #[test]
    fn window_rate_is_fraction_anomalous() {
        assert_eq!(window_anomaly_rate(&bits(&[])), 0.0, "empty window is 0");
        assert_eq!(window_anomaly_rate(&bits(&[false, false])), 0.0);
        assert_eq!(
            window_anomaly_rate(&bits(&[true, false, false, false])),
            0.25
        );
        assert_eq!(window_anomaly_rate(&bits(&[true, true])), 1.0);
    }

    #[test]
    fn bitmask_packs_oldest_first_msb_first() {
        // 10 bits: byte 0 = 1010_0000, byte 1 = 11xx_xxxx → 0xA0, 0xC0.
        let packed = pack_bitmask(&bits(&[
            true, false, true, false, false, false, false, false, true, true,
        ]));
        assert_eq!(packed, vec![0b1010_0000, 0b1100_0000]);
        assert!(pack_bitmask(&bits(&[])).is_empty());
    }

    #[test]
    fn anomaly_summary_carries_sampler_computation_and_coverage() {
        // A calm machine emits no breach summary at all, so this is the only
        // place its coverage can travel — which is why coverage rides here too.
        let coverage = vec![mesh_protocol::RuleCoverage {
            rule_id: "disk-critical".to_string(),
            state: mesh_protocol::RuleCoverageState::Active,
        }];
        match anomaly_summary(1_700_000_000, 0.5, vec![0b1000_0000], Vec::new(), coverage) {
            ControlMessage::AgentHealthSummary {
                ts,
                node_anomaly_rate,
                recent_bitmask,
                sampler_ver,
                breaches,
                rule_coverage,
                ..
            } => {
                assert_eq!(ts, 1_700_000_000);
                assert_eq!(node_anomaly_rate, 0.5);
                assert_eq!(recent_bitmask, vec![0b1000_0000]);
                assert_eq!(
                    sampler_ver, SAMPLER_VERSION,
                    "non-empty version makes the server record the rate series"
                );
                assert!(breaches.is_empty());
                assert_eq!(rule_coverage.len(), 1);
                assert_eq!(
                    rule_coverage[0].state,
                    mesh_protocol::RuleCoverageState::Active
                );
            }
            other => panic!("expected AgentHealthSummary, got {other:?}"),
        }
    }

    /// Nothing reads a load signal as idle before the sampler has taken a
    /// reading — "nobody has looked" is not "the machine is quiet".
    #[test]
    fn a_load_signal_has_no_reading_until_the_sampler_takes_one() {
        let signal = super::LoadSignal::new();
        assert_eq!(signal.cpu_percent(), None);

        signal.report(12.5);
        assert_eq!(signal.cpu_percent(), Some(12.5));

        // Every holder sees the same reading: the sampler publishes once and
        // the readers share it.
        let reader = signal.clone();
        signal.report(88.0);
        assert_eq!(reader.cpu_percent(), Some(88.0));

        // A reading that is not a number leaves the last real one standing.
        signal.report(f32::NAN);
        assert_eq!(reader.cpu_percent(), Some(88.0));
    }

    #[test]
    fn first_anomaly_summary_emits_immediately_then_throttles() {
        assert!(should_emit_anomaly(None, 500), "no prior emit → due now");
        let last = Some(500);
        assert!(!should_emit_anomaly(
            last,
            500 + ANOMALY_EMIT_INTERVAL_SECS - 1
        ));
        assert!(should_emit_anomaly(last, 500 + ANOMALY_EMIT_INTERVAL_SECS));
    }
}

/// Readings, rules and stores the sampler's tests share.
#[cfg(test)]
mod test_support {
    use super::SharedSink;
    use mesh_agent_core::ml::sampler::{MetricSample, ProcessSample};
    use mesh_agent_core::ml::store_sink::LocalStoreSink;
    use mesh_protocol::{AlertComparator, RulePredicate, ThresholdRule};
    use std::sync::{Arc, Mutex};

    pub(super) const T0: i64 = 1_700_000_000;

    pub(super) fn host_sample(cpu: f32) -> MetricSample {
        MetricSample {
            cpu_total_percent: cpu,
            memory_used_percent: 50.0,
            disk_used_percent: Some(50.0),
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

    /// A rule that fires the moment the processor passes 80%.
    pub(super) fn cpu_rule() -> ThresholdRule {
        ThresholdRule {
            id: "cpu-saturated".to_string(),
            version: 3,
            severity: mesh_protocol::AlertSeverity::Critical,
            metric: "cpu.total".to_string(),
            comparator: AlertComparator::Gt,
            threshold: 80.0,
            clear: 80.0,
            sustain_secs: 0,
            predicate: RulePredicate::Instant,
            window_secs: 0,
            all: Vec::new(),
        }
    }

    /// A reading taken while a database dump was the busiest thing running.
    pub(super) fn busy_sample(cpu: f32) -> MetricSample {
        let mut sample = host_sample(cpu);
        sample.processes = vec![ProcessSample {
            rank: 1,
            basename: "pg_dump".to_string(),
            cmdline_hash: None,
            pid: 4242,
            cpu: 88.0,
            mem: 3.5,
        }];
        sample
    }

    /// A store in a fresh directory, committing every reading.
    pub(super) fn store(dir: &tempfile::TempDir) -> SharedSink {
        let sink = LocalStoreSink::open(&dir.path().join("tsdb"), 64 * 1024 * 1024, 1)
            .expect("a store opens in a fresh directory");
        Arc::new(Mutex::new(sink))
    }
}
