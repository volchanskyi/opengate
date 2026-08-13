//! The periodic system-event watch: reads a bounded window of host log records,
//! hands them to the curated rule pack, and puts whatever fires into the shared
//! alert sink.
//!
//! The reader is an on-demand read rather than a stream, so this is a poll with
//! a window that deliberately reaches back further than the interval between
//! polls. A window exactly as wide as the interval would lose every record
//! written while a poll was in flight; overlapping windows re-present records
//! instead, and the pack's cursor is what makes re-presentation free.
//!
//! What the watch reads is bounded by what the pack could act on: the level
//! floor is asked of the pack rather than assumed here, so a rule that watches
//! something less severe widens the read by existing.

use std::time::{Duration, SystemTime, UNIX_EPOCH};

use tracing::{debug, info, warn};

use mesh_agent_core::alerts::{
    AlertSink, EventLevel, EventPack, EventRule, HostEvent, ServiceErrorRule,
};
use mesh_agent_core::maintenance::MaintenanceGate;
use mesh_protocol::LogEntry;

use crate::host_logs::{self, LogSource};
use crate::logs::LogFilter;

/// How often the host log is looked at.
const POLL_INTERVAL: Duration = Duration::from_secs(60);

/// How far back each poll asks for. Three times the interval, so a poll delayed
/// by a busy host still overlaps the last one rather than leaving a gap that no
/// later poll ever covers.
const POLL_WINDOW_SECS: i64 = 180;

const MICROS_PER_SEC: i64 = 1_000_000;

/// One device's system-event watch: the rule pack, its cursor, and the sink the
/// alerts go to.
pub(crate) struct EventWatch {
    pack: EventPack,
    sink: AlertSink,
    undated_records: u64,
}

impl EventWatch {
    /// A watch that starts looking from `start_micros` and raises into `sink`.
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

    /// The least severe record the pack could act on, for the reader's
    /// push-down.
    pub(crate) fn min_level(&self) -> EventLevel {
        self.pack.min_level()
    }

    /// Evaluates one poll's records and sinks whatever fires.
    ///
    /// A record whose timestamp cannot be read is not evaluated: with nothing to
    /// order it by, the cursor could neither place it nor recognize it on the
    /// next overlapping poll, so it would fire again every poll for as long as
    /// it stayed in the window. It is counted instead — an unreadable record is
    /// a reader defect, and a defect that fires alerts is worse than one that
    /// shows up as a number climbing.
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

    /// Moves the watch past a window without evaluating it — what maintenance
    /// mode does, so an admin's own disruption never pages anyone.
    pub(crate) fn skip(&mut self, now_micros: i64) {
        self.pack.skip_to(now_micros);
    }

    /// Anything either the reader or the caps cost this watch is carried on a
    /// log line with its running total, so a device losing records shows up as
    /// a climbing number rather than as silence. The wire carriage of these
    /// counts belongs to the alert transport.
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

/// Now, in microseconds since the Unix epoch. A clock before the epoch yields
/// zero rather than a negative instant.
fn unix_micros() -> i64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| i64::try_from(d.as_micros()).unwrap_or(i64::MAX))
        .unwrap_or(0)
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

/// The level label the reader pushes down for a pack floor.
///
/// `None` means no ceiling at all — read everything and let the rules decide.
/// That is also the answer for a level this build does not know, which is the
/// only safe direction: a push-down guessed too high would bound the read
/// tighter than the rules need and hide records from them.
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

/// Spawns the system-event watch.
///
/// On a platform with no host log reader the task returns instead of looping: a
/// watch that wakes every minute to read a source that does not exist is a
/// heartbeat with no signal in it.
pub(crate) fn spawn_event_watch(
    sink: AlertSink,
    maintenance: MaintenanceGate,
) -> tokio::task::JoinHandle<()> {
    tokio::task::spawn_blocking(move || {
        let Some(source) = host_logs::resolve_host_source() else {
            info!("no host log reader on this platform; system-event rules are not evaluated");
            return;
        };
        let mut watch = EventWatch::new(sink, unix_micros());
        info!("system-event watch starting");
        loop {
            std::thread::sleep(POLL_INTERVAL);
            let now = unix_micros();

            // In maintenance the window is skipped rather than deferred. An
            // admin rebooting a host produces exactly the records this pack
            // matches, and holding them until maintenance ends would page
            // someone for the maintenance itself.
            if maintenance.in_maintenance() {
                watch.skip(now);
                continue;
            }

            poll_once(&mut watch, source, now);
        }
    })
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

    /// A matching record reaches the sink as an alert, and the same record on
    /// the next overlapping poll does not reach it again.
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
        assert_eq!(alerts[0].rule_id, "linux.oom_kill");
        assert_eq!(alerts[0].severity, AlertSeverity::Critical);
    }

    /// A record the reader could not timestamp is counted rather than
    /// evaluated. Evaluating it would fire it again on every poll for as long
    /// as it sat in the window, because nothing could recognize it as the same
    /// record twice.
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

    /// A skipped window is suppressed, not deferred: records from inside it
    /// never fire, and the watch resumes afterwards.
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

    /// The window asked for reaches back further than the gap between polls, so
    /// a delayed poll overlaps the last one instead of leaving a hole.
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

    /// The push-down is derived from the pack, so it can never bound the read
    /// tighter than the rules need.
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

    /// The watch is safe to drive on a host whose reader returns nothing, which
    /// is every container without a journal.
    #[test]
    fn a_poll_that_reads_nothing_fires_nothing() {
        let sink = AlertSink::default();
        let mut watch = EventWatch::new(sink.clone(), START);
        watch.ingest(&[], false, START);
        assert!(sink.drain().is_empty());
        assert_eq!(watch.undated_records, 0);
    }
}
