//! The periodic system-event watch: polls a bounded window of host log records, feeds them to
//! the rule pack and sinks whatever fires.

use std::sync::{Arc, Mutex};
use std::time::Duration;

use tracing::{debug, info, warn};

use mesh_agent_core::alerts::{
    AlertSink, EventLevel, EventPack, EventRule, HostEvent, ServiceErrorRule,
};
use mesh_agent_core::maintenance::MaintenanceGate;
use mesh_protocol::{LogEntry, RuleCoverage};

use crate::clock::unix_micros;
use crate::host_logs::{self, LogSource};
use crate::logs::LogFilter;

/// How often the host log is looked at.
const POLL_INTERVAL: Duration = Duration::from_secs(60);

/// Seconds each poll reaches back, three intervals, so overlapping windows leave no gap and
/// the pack's cursor absorbs the re-presented records.
const POLL_WINDOW_SECS: i64 = 180;

const MICROS_PER_SEC: i64 = 1_000_000;

/// One device's system-event watch: the rule pack, its cursor and the alert sink.
pub(crate) struct EventWatch {
    pack: EventPack,
    sink: AlertSink,
    undated_records: u64,
}

impl EventWatch {
    /// Creates a watch that looks from `start_micros` and raises into `sink`.
    pub(crate) fn new(sink: AlertSink, start_micros: i64) -> Self {
        Self {
            pack: EventPack::new(
                EventRule::linux_pack(),
                ServiceErrorRule::default(),
                start_micros,
            ),
            sink,
            undated_records: 0,
        }
    }

    /// The least severe record level the pack acts on, used as the reader's level filter.
    pub(crate) fn min_level(&self) -> EventLevel {
        self.pack.min_level()
    }

    /// Evaluates one poll's records and sinks whatever fires; a record with an unreadable
    /// timestamp is counted, since the cursor could not recognize it on the next poll.
    pub(crate) fn ingest(&mut self, entries: &[LogEntry], saturated: bool, now_micros: i64) {
        let mut events = Vec::with_capacity(entries.len());
        for entry in entries {
            match entry_micros(entry) {
                Some(ts_micros) => events.push(HostEvent {
                    ts_micros,
                    level: &entry.level,
                    unit: &entry.target,
                    message: &entry.message,
                }),
                None => self.undated_records += 1,
            }
        }

        let alerts = self.pack.poll(&events, saturated);
        for alert in alerts {
            debug!(
                rule = %alert.rule_id,
                subject = %alert.subject,
                "system-event rule fired"
            );
            self.sink.push(alert, now_micros);
        }
        self.report_losses();
    }

    /// Moves the watch past a window without evaluating it, as maintenance mode does.
    pub(crate) fn skip(&mut self, now_micros: i64) {
        self.pack.skip_to(now_micros);
    }

    /// Logs the running totals of records and alerts this watch has lost, once any is non-zero.
    fn report_losses(&self) {
        let saturated = self.pack.saturated_polls();
        let untracked = self.pack.untracked_services();
        let stats = self.sink.stats();
        if saturated == 0
            && untracked == 0
            && self.undated_records == 0
            && stats.dropped_oldest == 0
            && stats.suppressed_by_ceiling == 0
        {
            return;
        }
        warn!(
            saturated_polls = saturated,
            untracked_services = untracked,
            undated_records = self.undated_records,
            alerts_dropped = stats.dropped_oldest,
            alerts_suppressed = stats.suppressed_by_ceiling,
            "system-event watch is losing records or alerts"
        );
    }
}

/// Reads a normalized entry's timestamp as microseconds since the Unix epoch.
fn entry_micros(entry: &LogEntry) -> Option<i64> {
    chrono::DateTime::parse_from_rfc3339(&entry.timestamp)
        .ok()
        .map(|dt| dt.timestamp_micros())
}

/// Renders an instant as the RFC 3339 string the log filter pushes down.
fn iso_from_micros(micros: i64) -> String {
    use chrono::{SecondsFormat, TimeZone, Utc};
    let secs = micros.div_euclid(MICROS_PER_SEC);
    Utc.timestamp_opt(secs, 0)
        .single()
        .map(|dt| dt.to_rfc3339_opts(SecondsFormat::Secs, true))
        .unwrap_or_default()
}

/// The level label the reader filters on; `None` reads everything, including for an unknown
/// level, so a mistaken filter never hides records from the rules.
fn level_label(level: EventLevel) -> Option<&'static str> {
    match level {
        EventLevel::Error => Some("ERROR"),
        EventLevel::Warn => Some("WARN"),
        EventLevel::Info => Some("INFO"),
        _ => None,
    }
}

/// The window one poll asks the reader for.
fn poll_filter(now_micros: i64, level: EventLevel) -> LogFilter {
    LogFilter {
        level: level_label(level).map(str::to_owned),
        time_from: Some(iso_from_micros(
            now_micros - POLL_WINDOW_SECS * MICROS_PER_SEC,
        )),
        time_to: None,
        search: None,
        offset: 0,
        limit: 0,
    }
}

/// Spawns the system-event watch, which returns at once on a platform with no host log reader.
pub(crate) fn spawn_event_watch(
    sink: AlertSink,
    maintenance: MaintenanceGate,
    coverage: EventCoverage,
) -> tokio::task::JoinHandle<()> {
    tokio::task::spawn_blocking(move || {
        let source = host_logs::resolve_host_source();
        // Coverage is published whether or not a reader exists, so an unreadable log reports
        // its rules as unsupported.
        publish_coverage(&coverage, source.is_some());
        let Some(source) = source else {
            info!("no host log reader on this platform; system-event rules are not evaluated");
            return;
        };
        let mut watch = EventWatch::new(sink, unix_micros());
        info!("system-event watch starting");
        loop {
            std::thread::sleep(POLL_INTERVAL);
            let now = unix_micros();

            // Maintenance skips the window because an admin's reboot
            // writes the records the pack matches.
            if maintenance.in_maintenance() {
                watch.skip(now);
                continue;
            }

            poll_once(&mut watch, source, now);
        }
    })
}

/// The rule coverage this machine reports to the server, empty until the watch has published.
pub(crate) type EventCoverage = Arc<Mutex<Vec<RuleCoverage>>>;

/// Stores the pack's per-rule coverage for the next report.
fn publish_coverage(coverage: &EventCoverage, can_read_its_log: bool) {
    if let Ok(mut slot) = coverage.lock() {
        *slot = EventPack::coverage(
            &EventRule::linux_pack(),
            &ServiceErrorRule::default(),
            can_read_its_log,
        );
    }
}

/// One poll: read the window, evaluate it, sink what fires.
fn poll_once(watch: &mut EventWatch, source: LogSource, now_micros: i64) {
    let filter = poll_filter(now_micros, watch.min_level());
    let entries = host_logs::collect_host_logs(source, &filter, "");
    let saturated = host_logs::batch_saturated(&entries);
    watch.ingest(&entries, saturated, now_micros);
}

#[cfg(test)]
mod tests {
    use super::*;
    use mesh_agent_core::alerts::AlertSeverity;

    fn entry(timestamp: &str, level: &str, target: &str, message: &str) -> LogEntry {
        LogEntry {
            timestamp: timestamp.to_string(),
            level: level.to_string(),
            target: target.to_string(),
            message: message.to_string(),
        }
    }

    const START: i64 = 1_700_000_000 * MICROS_PER_SEC;

    #[test]
    fn every_rule_in_the_pack_is_reported_whether_or_not_the_log_can_be_read() {
        let coverage: EventCoverage = Arc::new(Mutex::new(Vec::new()));

        publish_coverage(&coverage, true);
        let readable = coverage.lock().unwrap().clone();
        assert!(!readable.is_empty(), "the pack states what it watches");
        assert!(readable
            .iter()
            .all(|c| c.state == mesh_protocol::RuleCoverageState::Active));

        publish_coverage(&coverage, false);
        let unreadable = coverage.lock().unwrap().clone();
        assert_eq!(unreadable.len(), readable.len(), "no rule drops out");
        assert!(unreadable
            .iter()
            .all(|c| c.state == mesh_protocol::RuleCoverageState::Unsupported));
        assert!(unreadable.iter().any(|c| c.rule_id == "linux-oom-kill"));
    }

    #[test]
    fn a_matching_record_reaches_the_sink_once() {
        let sink = AlertSink::default();
        let mut watch = EventWatch::new(sink.clone(), START);
        let records = vec![entry(
            "2023-11-14T22:14:00.500000Z",
            "ERROR",
            "kernel",
            "Out of memory: Killed process 4242 (mysqld) total-vm:8192kB",
        )];

        watch.ingest(&records, false, START + MICROS_PER_SEC);
        watch.ingest(&records, false, START + 2 * MICROS_PER_SEC);

        let alerts = sink.drain();
        assert_eq!(alerts.len(), 1, "the overlapping poll adds nothing");
        assert_eq!(alerts[0].rule_id, "linux-oom-kill");
        assert_eq!(alerts[0].severity, AlertSeverity::Critical);
    }

    #[test]
    fn an_undated_record_is_counted_and_not_evaluated() {
        let sink = AlertSink::default();
        let mut watch = EventWatch::new(sink.clone(), START);

        watch.ingest(
            &[entry(
                "",
                "ERROR",
                "kernel",
                "Out of memory: Killed process 1 (a) total-vm:1kB",
            )],
            false,
            START,
        );

        assert!(
            sink.drain().is_empty(),
            "an unplaceable record fires nothing"
        );
        assert_eq!(watch.undated_records, 1, "and is counted");
    }

    #[test]
    fn a_skipped_window_fires_nothing_and_the_watch_resumes() {
        let sink = AlertSink::default();
        let mut watch = EventWatch::new(sink.clone(), START);

        watch.skip(START + 60 * MICROS_PER_SEC);
        watch.ingest(
            &[entry(
                "2023-11-14T22:13:30Z",
                "ERROR",
                "kernel",
                "Out of memory: Killed process 1 (during) total-vm:1kB",
            )],
            false,
            START + 61 * MICROS_PER_SEC,
        );
        assert!(
            sink.drain().is_empty(),
            "records from inside the skipped window never fire"
        );

        watch.ingest(
            &[entry(
                "2023-11-14T22:15:00Z",
                "ERROR",
                "kernel",
                "Out of memory: Killed process 2 (after) total-vm:1kB",
            )],
            false,
            START + 120 * MICROS_PER_SEC,
        );
        assert_eq!(sink.drain().len(), 1, "the watch resumes after the window");
    }

    #[test]
    fn the_poll_window_overlaps_the_interval() {
        assert!(
            POLL_WINDOW_SECS * MICROS_PER_SEC > POLL_INTERVAL.as_micros() as i64,
            "a window no wider than the interval loses whatever is written during a poll"
        );

        let filter = poll_filter(START, EventLevel::Error);
        assert_eq!(filter.level.as_deref(), Some("ERROR"));
        assert_eq!(
            filter.time_from.as_deref(),
            Some("2023-11-14T22:10:20Z"),
            "the read starts one window back"
        );
        assert!(filter.time_to.is_none(), "a poll reads up to now");
    }

    #[test]
    fn the_push_down_level_comes_from_the_pack() {
        let watch = EventWatch::new(AlertSink::default(), START);
        assert_eq!(watch.min_level(), EventLevel::Error);
        assert_eq!(level_label(EventLevel::Error), Some("ERROR"));
        assert_eq!(level_label(EventLevel::Warn), Some("WARN"));
        assert_eq!(level_label(EventLevel::Info), Some("INFO"));
        assert_eq!(
            level_label(EventLevel::Debug),
            None,
            "a floor at the bottom pushes nothing down and reads everything"
        );
    }

    #[test]
    fn entry_timestamps_are_read_as_epoch_micros() {
        let dated = entry("2023-11-14T22:13:20.123456Z", "ERROR", "a", "m");
        assert_eq!(
            entry_micros(&dated),
            Some(1_700_000_000 * MICROS_PER_SEC + 123_456)
        );
        assert_eq!(entry_micros(&entry("", "ERROR", "a", "m")), None);
        assert_eq!(entry_micros(&entry("yesterday", "ERROR", "a", "m")), None);
    }

    #[test]
    fn a_poll_that_reads_nothing_fires_nothing() {
        let sink = AlertSink::default();
        let mut watch = EventWatch::new(sink.clone(), START);
        watch.ingest(&[], false, START);
        assert!(sink.drain().is_empty());
        assert_eq!(watch.undated_records, 0);
    }

    fn watch_log_lines(f: impl FnOnce()) -> String {
        use std::io::Write;
        use std::sync::{Arc, Mutex};

        #[derive(Clone)]
        struct Lines(Arc<Mutex<Vec<u8>>>);
        impl Write for Lines {
            fn write(&mut self, buf: &[u8]) -> std::io::Result<usize> {
                self.0.lock().expect("log buffer").extend_from_slice(buf);
                Ok(buf.len())
            }
            fn flush(&mut self) -> std::io::Result<()> {
                Ok(())
            }
        }

        let lines = Lines(Arc::new(Mutex::new(Vec::new())));
        let writer = lines.clone();
        let subscriber = tracing_subscriber::fmt()
            .with_writer(move || writer.clone())
            .with_ansi(false)
            .finish();
        tracing::subscriber::with_default(subscriber, f);
        let captured = lines.0.lock().expect("log buffer").clone();
        String::from_utf8_lossy(&captured).into_owned()
    }

    #[test]
    fn a_watch_reports_what_it_lost_and_stays_quiet_when_it_lost_nothing() {
        let sink = AlertSink::default();
        let mut watch = EventWatch::new(sink.clone(), START);

        let quiet = watch_log_lines(|| watch.ingest(&[], false, START + MICROS_PER_SEC));
        assert!(
            !quiet.contains("losing records"),
            "a watch that lost nothing raised the alarm: {quiet}"
        );

        let lossy = watch_log_lines(|| {
            watch.ingest(
                &[entry(
                    "",
                    "ERROR",
                    "kernel",
                    "a record with no readable time",
                )],
                false,
                START + 2 * MICROS_PER_SEC,
            );
        });
        assert!(
            lossy.contains("losing records"),
            "a lost record went unreported: {lossy}"
        );
        assert!(
            lossy.contains("undated_records=1"),
            "the line does not carry the count: {lossy}"
        );
    }
}
