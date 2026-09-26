//! One second of the sampler: what the machine does with a reading.

use std::collections::VecDeque;
use std::sync::mpsc::SyncSender;

use mesh_agent_core::alerts::AlertEvaluator;
use mesh_agent_core::maintenance::MaintenanceTransition;
use mesh_agent_core::ml::ensemble::EdgeMlEnsemble;
use mesh_agent_core::ml::host_metric_stream::HostMetricWindower;
use mesh_agent_core::ml::sampler::MetricSample;
use mesh_protocol::ControlMessage;
use tracing::{debug, info, warn};

use super::raise::{all_coverage, raise_alert};
use super::{
    anomaly_summary, breach_summary, emit_host_metric_window, pack_bitmask, should_emit_anomaly,
    should_emit_health, window_anomaly_rate, AlertWiring, LoadSignal, SharedSink, ANOMALY_WINDOW,
    ENSEMBLE_ITERS, ENSEMBLE_MODELS, WARMUP_SAMPLES,
};

/// Where one sampler tick's results go. Every destination is optional: a
/// machine with no store samples without persisting, and one with no alert
/// wiring samples without evaluating rules.
pub(crate) struct SamplerOutputs {
    /// The local store each sample is persisted into.
    pub sink: Option<SharedSink>,
    /// The threshold-rule mailbox, the health channel and the alert queue.
    pub alerts: Option<AlertWiring>,
    /// The live host-metric stream to the control loop.
    pub host_metric_tx: Option<SyncSender<ControlMessage>>,
    /// Where the latest CPU reading is published.
    pub load: LoadSignal,
}

/// Everything the sampler carries from one second to the next.
///
/// The sampler task owns one of these and hands it each second's sample, so
/// what the machine does with a reading — classify it, evaluate the pushed
/// rules against it, raise an alert, emit a summary, persist it — is the same
/// code whether a clock or a test is driving it.
pub(super) struct SamplerState {
    /// Warm-up readings collected before the ensemble exists.
    warmup: Vec<[f32; 3]>,
    /// The trained anomaly ensemble, once there is one.
    ensemble: Option<EdgeMlEnsemble<3>>,
    /// The tenant-pushed threshold ruleset, evaluated against every sample.
    alert_eval: AlertEvaluator,
    /// When a breach-carrying health summary last went out.
    last_health_emit: Option<i64>,
    /// Whether the last health summary reported a breach.
    last_breaching: bool,
    /// Rolling window of trained-ensemble verdicts behind the fleet-health badge.
    anomaly_bits: VecDeque<bool>,
    /// When a node anomaly-rate summary last went out.
    last_anomaly_emit: Option<i64>,
    /// Tracks the maintenance→Active edge, which re-baselines the sampler.
    maintenance_edge: MaintenanceTransition,
    /// Folds 1 s samples into 10 s-average windows for the live stream.
    windower: HostMetricWindower,
}

impl SamplerState {
    pub(super) fn new() -> Self {
        Self {
            warmup: Vec::with_capacity(WARMUP_SAMPLES),
            ensemble: None,
            alert_eval: AlertEvaluator::new(Vec::new()),
            last_health_emit: None,
            last_breaching: false,
            anomaly_bits: VecDeque::with_capacity(ANOMALY_WINDOW),
            last_anomaly_emit: None,
            maintenance_edge: MaintenanceTransition::new(),
            windower: HostMetricWindower::new(),
        }
    }

    /// Whether this tick samples at all.
    ///
    /// Maintenance suppresses all sampler work — no sampling, store write, or
    /// alert evaluation — so the admin's disruptive host changes never pollute
    /// the anomaly baseline or fire a breach. On leaving maintenance the
    /// pre-change ensemble and breach state are discarded, so the post-change
    /// footprint retrains as the new normal.
    pub(super) fn begin_tick(&mut self, in_maintenance: bool) -> bool {
        if self.maintenance_edge.just_exited(in_maintenance) {
            self.ensemble = None;
            self.warmup.clear();
            self.last_health_emit = None;
            self.last_breaching = false;
            self.anomaly_bits.clear();
            self.last_anomaly_emit = None;
            info!("edge-sentinel: left maintenance, re-baselining anomaly detection");
        }
        if in_maintenance {
            // Discard any partial window so none spans the maintenance
            // interval; the stream resumes cleanly on the next Active tick.
            self.windower.reset();
        }
        !in_maintenance
    }

    /// Everything the machine does with one second's reading.
    pub(super) fn on_sample(&mut self, out: &SamplerOutputs, sample: &MetricSample, now: i64) {
        // Publish how busy the machine is, so background work that should only
        // run on an idle host has something current to look at.
        out.load.report(sample.cpu_total_percent);

        if let Some(tx) = out.host_metric_tx.as_ref() {
            emit_host_metric_window(&mut self.windower, tx, now, sample);
        }

        let anomaly = self.classify(sample);
        debug!(
            cpu = sample.cpu_total_percent,
            mem = sample.memory_used_percent,
            disk = ?sample.disk_used_percent,
            mounts_critical = ?sample.disk_mounts_critical,
            anomaly,
            "edge-sentinel sample"
        );

        if let Some(alerts) = out.alerts.as_ref() {
            self.evaluate_rules(alerts, out.sink.as_ref(), sample, now);
            self.emit_anomaly_rate(alerts, now);
        }

        if let Some(sink) = out.sink.as_ref() {
            persist(sink, now, sample, anomaly);
        }
    }

    /// The ensemble's verdict on this reading, training it once the warm-up
    /// window is full. Until it exists every reading is `false`, and those
    /// verdicts stay out of the rolling window so the emitted rate reflects the
    /// trained model only.
    fn classify(&mut self, sample: &MetricSample) -> bool {
        // The ensemble needs a fixed-width vector, and a host with no
        // measurable mount has no disk-fullness signal to detect — it
        // contributes a flat 0 to the model rather than an invented reading.
        // The published vital stays absent; this value never leaves the host.
        let features = [
            sample.cpu_total_percent,
            sample.memory_used_percent,
            sample.disk_used_percent.unwrap_or(0.0),
        ];
        let Some(model) = self.ensemble.as_ref() else {
            self.warm_up(features);
            return false;
        };
        let anomaly = model.is_anomaly(&features);
        if self.anomaly_bits.len() == ANOMALY_WINDOW {
            self.anomaly_bits.pop_front();
        }
        self.anomaly_bits.push_back(anomaly);
        anomaly
    }

    /// Collect one warm-up reading, and train once there are enough.
    fn warm_up(&mut self, features: [f32; 3]) {
        self.warmup.push(features);
        if self.warmup.len() < WARMUP_SAMPLES {
            return;
        }
        match EdgeMlEnsemble::<3>::train_staggered(&self.warmup, ENSEMBLE_MODELS, ENSEMBLE_ITERS) {
            Ok(model) => {
                info!("edge-sentinel anomaly ensemble trained");
                self.ensemble = Some(model);
            }
            Err(e) => warn!(error = %e, "edge-sentinel ensemble train failed"),
        }
    }

    /// Install any freshly-pushed ruleset, evaluate the reading, raise an alert
    /// for every rule that has just started firing, and emit a breach-carrying
    /// health summary.
    ///
    /// Emission is throttled to [`HEALTH_EMIT_INTERVAL_SECS`](super::HEALTH_EMIT_INTERVAL_SECS) and only fires
    /// while a breach is active (plus one final summary reporting the clear), so
    /// a steady host is silent and a burst never backpressures control.
    fn evaluate_rules(
        &mut self,
        alerts: &AlertWiring,
        store: Option<&SharedSink>,
        sample: &MetricSample,
        now: i64,
    ) {
        if let Some(rules) = alerts.rules.lock().ok().and_then(|mut slot| slot.take()) {
            debug!(
                count = rules.len(),
                "edge-sentinel: alert ruleset installed"
            );
            self.alert_eval.set_rules(rules);
        }
        let firing = self.alert_eval.evaluate(sample, now);

        // An episode that has just begun is one thing that happened, so it
        // raises one alert carrying everything this machine knows about why —
        // assembled here, at the moment it fired, because nothing will be asked
        // of the machine afterwards.
        for started in firing.iter().filter(|f| f.started) {
            raise_alert(&alerts.alert_sink, store, sample, started, now);
        }

        let breaching = !firing.is_empty();
        if !should_emit_health(self.last_health_emit, now, breaching, self.last_breaching) {
            return;
        }
        let summary = breach_summary(
            now,
            AlertEvaluator::breaches(&firing),
            all_coverage(&self.alert_eval, &alerts.event_coverage),
        );
        if alerts.health_tx.try_send(summary).is_err() {
            debug!("edge-sentinel health summary dropped: telemetry channel full");
        }
        self.last_health_emit = Some(now);
        self.last_breaching = breaching;
    }

    /// The periodic node anomaly-rate summary: the trained-window rate on a
    /// fixed cadence, carrying a sampler version so the server records the
    /// series behind the fleet-health badge (a steady host emits no breach
    /// summary, so this is the badge's only source). It carries rule coverage
    /// for the same reason — a rule quietly watching nothing on a calm machine
    /// is exactly what coverage exists to surface, and a calm machine sends
    /// nothing else.
    fn emit_anomaly_rate(&mut self, alerts: &AlertWiring, now: i64) {
        if self.ensemble.is_none() || !should_emit_anomaly(self.last_anomaly_emit, now) {
            return;
        }
        let summary = anomaly_summary(
            now,
            window_anomaly_rate(&self.anomaly_bits),
            pack_bitmask(&self.anomaly_bits),
            Vec::new(),
            all_coverage(&self.alert_eval, &alerts.event_coverage),
        );
        if alerts.health_tx.try_send(summary).is_err() {
            debug!("edge-sentinel anomaly summary dropped: telemetry channel full");
        }
        self.last_anomaly_emit = Some(now);
    }
}

/// Persist one reading with its anomaly bit into the local store.
pub(super) fn persist(sink: &SharedSink, now: i64, sample: &MetricSample, anomaly: bool) {
    match sink.lock() {
        Ok(mut sink) => {
            if let Err(e) = sink.record(now, sample, anomaly) {
                warn!(error = %e, "edge-sentinel store write failed");
            }
        }
        Err(e) => warn!(error = %e, "edge-sentinel store lock poisoned"),
    }
}

#[cfg(test)]
mod tests {
    use super::{SamplerOutputs, SamplerState};
    use crate::edge_sentinel::raise::MICROS_PER_SEC;
    use crate::edge_sentinel::test_support::{busy_sample, cpu_rule, host_sample, store, T0};
    use crate::edge_sentinel::{
        AlertWiring, LoadSignal, SharedSink, HEALTH_EMIT_INTERVAL_SECS, SAMPLER_VERSION,
        WARMUP_SAMPLES,
    };
    use mesh_agent_core::alerts::{AlertOrigin, AlertSeverity, AlertSink};
    use mesh_protocol::{
        AlertEvidence, ControlMessage, RuleCoverage, RuleCoverageState, ThresholdRule,
    };
    use std::sync::mpsc::{sync_channel, Receiver};
    use std::sync::{Arc, Mutex};

    /// A machine wired the way the agent wires it, with the rule already
    /// pushed and one word rule reported by the log watch.
    struct Wired {
        out: SamplerOutputs,
        alerts: AlertSink,
        health: Receiver<ControlMessage>,
        windows: Receiver<ControlMessage>,
    }

    fn wired(sink: Option<SharedSink>, rules: Vec<ThresholdRule>) -> Wired {
        let (health_tx, health) = sync_channel(16);
        let (host_metric_tx, windows) = sync_channel(16);
        let alerts = AlertSink::default();
        let event_coverage = Arc::new(Mutex::new(vec![RuleCoverage {
            rule_id: "linux-oom-kill".to_string(),
            state: RuleCoverageState::Unsupported,
        }]));
        Wired {
            out: SamplerOutputs {
                sink,
                alerts: Some(AlertWiring {
                    rules: Arc::new(Mutex::new(Some(rules))),
                    health_tx,
                    alert_sink: alerts.clone(),
                    event_coverage,
                }),
                host_metric_tx: Some(host_metric_tx),
                load: LoadSignal::new(),
            },
            alerts,
            health,
            windows,
        }
    }

    /// A reading crossing a pushed rule's line raises one alert, stamped with
    /// the revision and severity the server sent, naming the stretch it held
    /// over, and carrying what was running — the only moment that exists.
    #[test]
    fn a_rule_that_starts_firing_raises_one_alert_carrying_what_was_running() {
        let dir = tempfile::tempdir().unwrap();
        let machine = wired(Some(store(&dir)), vec![cpu_rule()]);
        let mut state = SamplerState::new();

        for second in 0..3 {
            assert!(state.begin_tick(false));
            state.on_sample(&machine.out, &busy_sample(95.0), T0 + second);
        }

        let raised = machine.alerts.drain();
        assert_eq!(
            raised.len(),
            1,
            "one episode is one alert, not one a second"
        );
        let alert = &raised[0];
        assert_eq!(alert.rule_id, "cpu-saturated");
        assert_eq!(
            alert.rule_version, 3,
            "the revision the server sent with the rule"
        );
        assert_eq!(alert.severity, AlertSeverity::Critical);
        assert_eq!(alert.metric, "cpu.total");
        assert_eq!(alert.value, Some(95.0));
        assert_eq!(alert.ts_micros, T0 * MICROS_PER_SEC);
        assert_eq!(alert.window_start_micros, T0 * MICROS_PER_SEC);
        assert_eq!(alert.origin, AlertOrigin::Live);

        let behind = AlertEvidence::decode(&alert.evidence, &alert.evidence_codec)
            .expect("the evidence reads back at the far end");
        assert_eq!(behind.processes.len(), 1);
        assert_eq!(behind.processes[0].basename, "pg_dump");
        assert_eq!(behind.processes[0].pid, 4242);

        assert_eq!(
            machine.out.load.cpu_percent(),
            Some(95.0),
            "the reading is published for work waiting on an idle machine"
        );
    }

    /// The breach goes out as a health summary counting every rule on the
    /// machine — the ones the sampler evaluates and the ones the log watch
    /// answers for — and the clear goes out once when it ends.
    #[test]
    fn a_breach_is_reported_with_every_rule_counted_and_its_clear_once() {
        let machine = wired(None, vec![cpu_rule()]);
        let mut state = SamplerState::new();

        state.on_sample(&machine.out, &busy_sample(95.0), T0);
        match machine
            .health
            .try_recv()
            .expect("the first breach emits at once")
        {
            ControlMessage::AgentHealthSummary {
                breaches,
                rule_coverage,
                ..
            } => {
                assert_eq!(breaches.len(), 1);
                assert_eq!(breaches[0].rule_id, "cpu-saturated");
                let ids: Vec<_> = rule_coverage.iter().map(|c| c.rule_id.as_str()).collect();
                assert!(ids.contains(&"cpu-saturated"), "the sampler's own rule");
                assert!(ids.contains(&"linux-oom-kill"), "and the log watch's");
            }
            other => panic!("expected AgentHealthSummary, got {other:?}"),
        }

        let cleared_at = T0 + HEALTH_EMIT_INTERVAL_SECS;
        state.on_sample(&machine.out, &busy_sample(10.0), cleared_at);
        match machine.health.try_recv().expect("the clear is reported") {
            ControlMessage::AgentHealthSummary { breaches, .. } => assert!(breaches.is_empty()),
            other => panic!("expected AgentHealthSummary, got {other:?}"),
        }
        state.on_sample(
            &machine.out,
            &busy_sample(10.0),
            cleared_at + HEALTH_EMIT_INTERVAL_SECS,
        );
        assert!(
            machine.health.try_recv().is_err(),
            "a calm machine is silent"
        );
    }

    /// Readings persist into the store, and a closed window reaches the live
    /// stream.
    #[test]
    fn each_reading_is_stored_and_closed_windows_are_streamed() {
        let dir = tempfile::tempdir().unwrap();
        let sink = store(&dir);
        let machine = wired(Some(sink.clone()), Vec::new());
        let mut state = SamplerState::new();

        for second in 0..70 {
            state.on_sample(&machine.out, &host_sample(40.0), T0 + second);
        }

        assert!(
            machine.windows.try_recv().is_ok(),
            "a window closed across its boundary"
        );
        let series = mesh_agent_core::ml::store_sink::dim_series("cpu.total").unwrap();
        let points = sink
            .lock()
            .unwrap()
            .snapshot()
            .unwrap()
            .range_raw(series, T0, T0 + 70)
            .unwrap();
        assert_eq!(points.len(), 70, "every second was written");
    }

    /// Maintenance stops the tick, and leaving it throws away what was learnt
    /// before the admin changed the machine.
    #[test]
    fn leaving_maintenance_rebaselines_the_sampler() {
        let machine = wired(None, Vec::new());
        let mut state = SamplerState::new();
        for second in 0..WARMUP_SAMPLES as i64 {
            state.on_sample(
                &machine.out,
                &host_sample(20.0 + second as f32 % 3.0),
                T0 + second,
            );
        }
        assert!(
            state.ensemble.is_some(),
            "a full warm-up trains the ensemble"
        );

        assert!(!state.begin_tick(true), "maintenance samples nothing");
        assert!(
            state.ensemble.is_some(),
            "and changes nothing while it lasts"
        );

        assert!(state.begin_tick(false), "leaving it samples again");
        assert!(state.ensemble.is_none(), "from a fresh baseline");
        assert!(state.warmup.is_empty());
        assert!(state.anomaly_bits.is_empty());
        assert_eq!(state.last_anomaly_emit, None);
    }

    /// Once trained, the ensemble's verdicts feed the rate behind the
    /// fleet-health badge, which goes out stamped with the sampler version.
    #[test]
    fn a_trained_sampler_emits_its_anomaly_rate() {
        let machine = wired(None, Vec::new());
        let mut state = SamplerState::new();
        // The warm-up's last reading is the one that trains the ensemble.
        let trained_at = T0 + WARMUP_SAMPLES as i64 - 1;
        for second in 0..=WARMUP_SAMPLES as i64 {
            state.on_sample(
                &machine.out,
                &host_sample(20.0 + second as f32 % 3.0),
                T0 + second,
            );
        }

        assert_eq!(
            state.anomaly_bits.len(),
            1,
            "only verdicts from the trained model count, not the warm-up's"
        );
        assert_eq!(state.last_anomaly_emit, Some(trained_at));
        let summary = std::iter::from_fn(|| machine.health.try_recv().ok()).find(|m| {
            matches!(m, ControlMessage::AgentHealthSummary { sampler_ver, .. }
                    if sampler_ver == SAMPLER_VERSION)
        });
        assert!(summary.is_some(), "the rate summary went out");
    }
}
