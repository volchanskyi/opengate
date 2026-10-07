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

/// The sampler-owned local store, shared with the backfill coordinator on the control loop.
pub(crate) type SharedSink = Arc<Mutex<LocalStoreSink>>;

/// Bit pattern standing for no reading yet; a finite percentage never collides with it.
const LOAD_NOT_TAKEN: u32 = u32::MAX;

/// The most recent host CPU reading, absent until the sampler takes one so that work waiting
/// for an idle machine never reads a missing reading as idle.
#[derive(Clone)]
pub(crate) struct LoadSignal(Arc<std::sync::atomic::AtomicU32>);

impl LoadSignal {
    /// A signal with no reading in it yet.
    pub(crate) fn new() -> Self {
        Self(Arc::new(std::sync::atomic::AtomicU32::new(LOAD_NOT_TAKEN)))
    }

    /// Publishes this second's reading when it is a finite percentage.
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

/// Interval between discovery sweeps; long because a sweep shells out to package managers
/// and lists services.
const DISCOVERY_INTERVAL: Duration = Duration::from_secs(1800);

/// Spawns the discovery task, which sends a bounded `DiscoveryReport` over `sink` whenever
/// the host profile changes and drops it when the channel is full.
pub(crate) fn spawn_discovery(
    sink: SyncSender<ControlMessage>,
    maintenance: MaintenanceGate,
) -> tokio::task::JoinHandle<()> {
    tokio::task::spawn_blocking(move || {
        let mut last_fingerprint: Option<u64> = None;
        loop {
            // Maintenance skips the sweep because services and ports churn as the admin works.
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

/// Builds a breach-only `AgentHealthSummary`; its empty sampler fields make the server record
/// no anomaly-rate sample, and the server assigns the tenant.
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

/// Sampler version stamped on anomaly-rate summaries; the server records the rate series only
/// for a summary carrying a version or per-family rates.
const SAMPLER_VERSION: &str = "edge-ensemble-v1";

/// Number of recent per-second anomaly verdicts the node anomaly rate is computed over.
const ANOMALY_WINDOW: usize = 64;

/// Minimum seconds between anomaly-rate summaries; above the server's 10 s ingest floor.
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

/// Builds the periodic anomaly-rate summary carrying the rate, its packed verdict history and
/// the sampler version.
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

/// Whether an anomaly-rate summary is due: the first emits at once, later ones at least
/// [`ANOMALY_EMIT_INTERVAL_SECS`] apart.
fn should_emit_anomaly(last_emit: Option<i64>, now: i64) -> bool {
    last_emit.is_none_or(|last| now.saturating_sub(last) >= ANOMALY_EMIT_INTERVAL_SECS)
}

/// Whether a health summary is due: while breaching and once more on clearing, at least
/// [`HEALTH_EMIT_INTERVAL_SECS`] apart.
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

/// Local-store wiring for the sampler task: the store directory and its footprint cap.
pub(crate) struct StoreConfig {
    /// Store directory (under the agent data dir).
    pub path: PathBuf,
    /// Hard footprint cap in bytes.
    pub cap_bytes: u64,
}

/// Minimum seconds between health summaries; above the server's 10 s telemetry interval floor.
const HEALTH_EMIT_INTERVAL_SECS: i64 = 15;

/// Slot the control loop fills with a pushed threshold ruleset and the sampler drains on its
/// next tick.
pub(crate) type AlertRulesMailbox = Arc<Mutex<Option<Vec<ThresholdRule>>>>;

/// Wiring for the sampler's threshold-alert path: ruleset mailbox, health-summary channel,
/// alert sink and event coverage.
pub(crate) struct AlertWiring {
    /// Latest pushed ruleset; the sampler installs it on its next tick and clears the slot.
    pub rules: AlertRulesMailbox,
    /// Sink for breach-carrying `AgentHealthSummary`, drained by the control loop on heartbeat.
    pub health_tx: SyncSender<ControlMessage>,
    /// Queue shared by every alert producer on this machine, so they share one bound and one
    /// hourly allowance.
    pub alert_sink: AlertSink,
    /// Coverage of the rules the event watch evaluates, reported with the sampler's own so
    /// every pushed rule is counted.
    pub event_coverage: EventCoverage,
}

/// Samples the required warm-up window before the ensemble can be trained.
const WARMUP_SAMPLES: usize = 30;
/// Ensemble geometry (staggered k=2 models over the CPU/mem/disk feature vector).
const ENSEMBLE_MODELS: usize = 6;
const ENSEMBLE_ITERS: usize = 20;
/// Durable flush cadence for the local store, in samples.
const STORE_COMMIT_EVERY: usize = 60;

/// Folds one sample into the windower and forwards a window it closed; a full channel drops
/// the window so telemetry never backpressures control.
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

/// Spawns the 1 s sampler: it trains the anomaly ensemble, persists samples to the store,
/// evaluates alert rules and forwards 60 s metric windows, dropping one when the channel is full.
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

/// Opens the local store, recreating it when the open fails and returning `None` if that fails too.
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

    #[test]
    fn emit_host_metric_window_forwards_closed_windows() {
        let mut windower = HostMetricWindower::new();
        let (tx, rx) = sync_channel::<ControlMessage>(4);

        emit_host_metric_window(&mut windower, &tx, 120, &host_sample(10.0));
        emit_host_metric_window(&mut windower, &tx, 125, &host_sample(30.0));
        assert!(rx.try_recv().is_err(), "an open window sends nothing");

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

    #[test]
    fn emit_host_metric_window_drops_when_channel_full() {
        let mut windower = HostMetricWindower::new();
        let (tx, rx) = sync_channel::<ControlMessage>(1);

        emit_host_metric_window(&mut windower, &tx, 120, &host_sample(10.0));
        emit_host_metric_window(&mut windower, &tx, 180, &host_sample(20.0));
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
        assert!(!should_emit_health(
            last,
            100 + HEALTH_EMIT_INTERVAL_SECS - 1,
            true,
            true
        ));
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
        assert!(should_emit_health(last, due, false, true));
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

    #[test]
    fn a_load_signal_has_no_reading_until_the_sampler_takes_one() {
        let signal = super::LoadSignal::new();
        assert_eq!(signal.cpu_percent(), None);

        signal.report(12.5);
        assert_eq!(signal.cpu_percent(), Some(12.5));

        let reader = signal.clone();
        signal.report(88.0);
        assert_eq!(reader.cpu_percent(), Some(88.0));

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

    pub(super) fn store(dir: &tempfile::TempDir) -> SharedSink {
        let sink = LocalStoreSink::open(&dir.path().join("tsdb"), 64 * 1024 * 1024, 1)
            .expect("a store opens in a fresh directory");
        Arc::new(Mutex::new(sink))
    }
}
