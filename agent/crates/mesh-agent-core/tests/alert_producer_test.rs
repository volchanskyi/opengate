//! A reading crosses a line on this machine, and what leaves it is a message
//! the far end will accept.
//!
//! The pieces are proven separately elsewhere: the state machine decides when a
//! rule fires, the queue decides what it is allowed to lose, and the mapping
//! decides how an alert is spelled on the wire. What is proven here is the
//! whole run of them together, because that is the join that did not exist —
//! every alert every machine raised went into a queue nothing emptied, and was
//! discarded.
//!
//! It is driven at the seconds the sampler would have handed over, so the only
//! thing not real about it is the clock.

use mesh_agent_core::alerts::{
    alert_message, pack_evidence, AlertEvaluator, AlertOrigin, AlertSeverity, AlertSink, EdgeAlert,
    EvidenceSource, Firing,
};
use mesh_agent_core::ml::sampler::{MetricSample, ProcessSample};
use mesh_protocol::{
    AlertComparator, AlertEvidence, AlertSeverity as WireSeverity, ControlMessage, RulePredicate,
    ThresholdRule,
};

const MICROS_PER_SEC: i64 = 1_000_000;

/// The moment the machine starts being watched. Whole seconds, so every
/// expectation below reads off the offset from here.
const START: i64 = 1_763_000_000;

/// `disk-critical` as the product ships it, shortened so a case is about the
/// firing rather than about waiting five minutes for one.
fn disk_critical() -> ThresholdRule {
    ThresholdRule {
        id: "disk-critical".to_string(),
        version: 1,
        severity: WireSeverity::Critical,
        metric: "disk.used_percent".to_string(),
        comparator: AlertComparator::Gte,
        threshold: 90.0,
        clear: 85.0,
        sustain_secs: 60,
        predicate: RulePredicate::Instant,
        window_secs: 0,
        all: Vec::new(),
    }
}

/// One second of readings from a machine whose data volume is at `disk`, with
/// a backup process at the top of its process list.
fn second_at(disk: f32) -> MetricSample {
    MetricSample {
        cpu_total_percent: 12.0,
        memory_used_percent: 41.0,
        disk_used_percent: Some(disk),
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
        processes: vec![ProcessSample {
            rank: 1,
            basename: "pg_dump".to_string(),
            cmdline_hash: None,
            pid: 4242,
            cpu: 88.0,
            mem: 2_147_483_648.0,
        }],
    }
}

/// The machine, as the sampler drives it: one rule installed, one queue, and a
/// second of readings a second.
struct Machine {
    evaluator: AlertEvaluator,
    queue: AlertSink,
}

impl Machine {
    fn watching(rule: ThresholdRule) -> Self {
        Self {
            evaluator: AlertEvaluator::new(vec![rule]),
            queue: AlertSink::default(),
        }
    }

    /// Hand the machine one second's readings, raising an alert for any rule
    /// that has just started firing — which is what the sampler does.
    fn reads(&mut self, sample: &MetricSample, at: i64) {
        for started in self
            .evaluator
            .evaluate(sample, at)
            .iter()
            .filter(|f| f.started)
        {
            self.queue.push(raise(started, sample), at * MICROS_PER_SEC);
        }
    }

    /// Everything waiting to go, as the messages that would carry it.
    fn drains(&self) -> Vec<ControlMessage> {
        self.queue.drain().iter().map(alert_message).collect()
    }
}

/// The alert a firing rule raises, with what the machine knew about it packed
/// at the moment it fired.
fn raise(firing: &Firing, sample: &MetricSample) -> EdgeAlert {
    let processes: Vec<_> = sample
        .processes
        .iter()
        .map(|p| mesh_protocol::ProcessReportEntry {
            rank: u32::from(p.rank),
            basename: p.basename.clone(),
            cmdline_hash: p.cmdline_hash.clone(),
            pid: p.pid,
            cpu: p.cpu,
            mem: p.mem,
        })
        .collect();
    let packed = pack_evidence(&EvidenceSource {
        ranked: &[],
        readings: &[],
        processes: &processes,
        log_lines: &[],
        event_ts: firing.at,
    });
    EdgeAlert {
        rule_id: firing.rule_id.clone(),
        rule_version: firing.rule_version,
        severity: match firing.severity {
            WireSeverity::Critical => AlertSeverity::Critical,
            WireSeverity::Info => AlertSeverity::Info,
            _ => AlertSeverity::Warning,
        },
        ts_micros: firing.at * MICROS_PER_SEC,
        window_start_micros: firing.since * MICROS_PER_SEC,
        window_end_micros: firing.at * MICROS_PER_SEC,
        metric: firing.metric.clone(),
        value: Some(firing.value),
        subject: firing.metric.clone(),
        summary: String::new(),
        evidence: packed.bytes,
        evidence_codec: packed.codec.to_string(),
        origin: AlertOrigin::Live,
    }
}

/// Pull the alert fields out of one message.
fn fields(msg: &ControlMessage) -> (&str, u32, WireSeverity, i64, i64, Option<f64>, &[u8], &str) {
    match msg {
        ControlMessage::AgentAlert {
            rule_id,
            rule_version,
            severity,
            window_start_ts,
            window_end_ts,
            value,
            evidence,
            evidence_codec,
            ..
        } => (
            rule_id,
            *rule_version,
            *severity,
            *window_start_ts,
            *window_end_ts,
            *value,
            evidence,
            evidence_codec,
        ),
        other => panic!("an alert must travel as an AgentAlert, got {other:?}"),
    }
}

/// CONTOSO-FS01's data volume passes its line at 06:12 and stays there. One
/// minute later the rule fires, and what leaves the machine is one message
/// carrying everything the far end identifies an alert by.
#[test]
fn a_disk_that_fills_produces_one_message_the_server_can_admit() {
    let mut machine = Machine::watching(disk_critical());

    // Half an hour of a healthy volume raises nothing.
    for second in 0..1_800 {
        machine.reads(&second_at(62.0), START + second);
    }
    assert!(
        machine.drains().is_empty(),
        "a machine that is fine says nothing"
    );

    // It passes the line and holds there for the minute the rule requires.
    let crossed = START + 1_800;
    for second in 0..=60 {
        machine.reads(&second_at(91.4), crossed + second);
    }

    let sent = machine.drains();
    assert_eq!(sent.len(), 1, "one episode is one thing that happened");

    let (rule_id, version, severity, window_start, window_end, value, evidence, codec) =
        fields(&sent[0]);
    assert_eq!(rule_id, "disk-critical");
    assert_eq!(version, 1, "a revision of nothing is refused outright");
    assert_eq!(
        severity,
        WireSeverity::Critical,
        "as bad as the rule says, so the queue can be ordered by it"
    );
    assert_eq!(
        window_start, crossed,
        "the window starts where the breach started, not where the hold elapsed"
    );
    assert_eq!(window_end, crossed + 60);
    assert!(window_end >= window_start, "a window never runs backwards");
    assert_eq!(value, Some(91.4_f32.into()));
    assert!(!evidence.is_empty(), "with what the machine knew attached");
    assert_eq!(codec, mesh_protocol::EVIDENCE_CODEC);
}

/// What the machine attached is readable at the far end, and says what was
/// running when the rule fired. There is no path for asking the machine later,
/// so this is the whole of what will ever be known about that moment.
#[test]
fn what_the_machine_attached_reads_back_at_the_other_end() {
    let mut machine = Machine::watching(disk_critical());
    for second in 0..=60 {
        machine.reads(&second_at(91.4), START + second);
    }

    let sent = machine.drains();
    let (.., evidence, codec) = fields(&sent[0]);

    let behind = AlertEvidence::decode(evidence, codec).expect("evidence must read back");
    assert_eq!(behind.processes.len(), 1);
    assert_eq!(behind.processes[0].basename, "pg_dump");
    assert_eq!(
        behind.processes[0].cpu, 88.0,
        "a ranking without the numbers behind it has to be taken on trust"
    );
    assert!(!behind.truncated, "nothing here is near the size cap");
}

/// A machine offline while its disk fills raises the alert anyway, and hands it
/// over whole when the link comes back. That is the point of the queue: an
/// alert raised during an outage is the one thing that cannot be reconstructed
/// afterwards.
#[test]
fn an_alert_raised_while_offline_is_still_there_when_the_link_returns() {
    let mut machine = Machine::watching(disk_critical());
    for second in 0..=60 {
        machine.reads(&second_at(91.4), START + second);
    }

    // Three minutes of nobody to send to. Nothing drains it.
    for second in 61..240 {
        machine.reads(&second_at(91.4), START + second);
    }

    let sent = machine.drains();
    assert_eq!(
        sent.len(),
        1,
        "the alert survived the outage, and the episode did not become three minutes of them"
    );
    let (_, _, _, window_start, ..) = fields(&sent[0]);
    assert_eq!(
        window_start, START,
        "and it still describes the moment it happened rather than the moment it was delivered"
    );
}

/// A send that failed offers the same alert again, and the far end has to be
/// able to recognise it. The identity is what makes that one row rather than
/// two, so it has to survive the round trip through the queue unchanged.
#[test]
fn an_alert_offered_again_after_a_failed_send_is_the_same_alert() {
    let mut machine = Machine::watching(disk_critical());
    for second in 0..=60 {
        machine.reads(&second_at(91.4), START + second);
    }

    let unsent = machine.queue.drain();
    machine.queue.return_unsent(unsent);

    let again = machine.drains();
    assert_eq!(again.len(), 1);

    let (rule_id, version, _, window_start, ..) = fields(&again[0]);
    assert_eq!(
        (rule_id, version, window_start),
        ("disk-critical", 1, START),
        "the three fields the far end resolves a re-delivery by have not moved"
    );
}

/// A machine stuck over its line for ten hours is one thing that happened. The
/// queue holds 256 and would otherwise fill with the same episode, leaving no
/// room for the alert that says what else broke.
#[test]
fn ten_hours_over_the_line_is_one_alert_rather_than_thirty_six_thousand() {
    let mut machine = Machine::watching(disk_critical());

    for second in 0..36_000 {
        machine.reads(&second_at(91.4), START + second);
    }

    assert_eq!(machine.drains().len(), 1);
    let stats = machine.queue.stats();
    assert_eq!(
        stats.dropped_oldest, 0,
        "nothing was crowded out, because nothing crowded"
    );
    assert_eq!(stats.suppressed_by_ceiling, 0);
}

/// An episode that ends and returns is a second thing that happened, and both
/// travel — with different identities, so the far end opens a second room
/// rather than treating the return as a re-delivery of the first.
#[test]
fn an_episode_that_returns_is_a_second_alert_with_its_own_identity() {
    let mut machine = Machine::watching(disk_critical());

    for second in 0..=60 {
        machine.reads(&second_at(91.4), START + second);
    }
    // Back under the clear boundary: the episode is over.
    for second in 61..120 {
        machine.reads(&second_at(70.0), START + second);
    }
    // And over the line again.
    for second in 120..=181 {
        machine.reads(&second_at(93.0), START + second);
    }

    let sent = machine.drains();
    assert_eq!(sent.len(), 2, "two episodes are two things that happened");

    let (_, _, _, first_start, ..) = fields(&sent[0]);
    let (_, _, _, second_start, ..) = fields(&sent[1]);
    assert_ne!(
        first_start, second_start,
        "two episodes with one identity would collapse into one room"
    );
    assert!(second_start > first_start, "and they read forwards");
}

/// A machine in a loop cannot drown the detection of every other machine, and
/// what the ceiling costs is counted rather than lost quietly. Alerts nobody
/// counts are indistinguishable from a quiet machine, which is the failure this
/// whole path exists to remove.
#[test]
fn a_machine_in_a_loop_is_capped_and_says_how_much_it_cost() {
    let mut machine = Machine::watching(disk_critical());
    machine.queue.set_ceiling(3);

    // Five separate episodes: over the line, back under it, over again.
    let mut at = START;
    for _ in 0..5 {
        for second in 0..=60 {
            machine.reads(&second_at(91.4), at + second);
        }
        for second in 61..120 {
            machine.reads(&second_at(70.0), at + second);
        }
        at += 120;
    }

    assert_eq!(machine.drains().len(), 3, "the allowance is what it says");
    assert_eq!(
        machine.queue.stats().suppressed_by_ceiling,
        2,
        "and the two it cost are counted, not silently absent"
    );
}
