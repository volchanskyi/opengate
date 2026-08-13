//! The curated system-event rule pack and the rolling per-service error count.
//!
//! # Why a cursor, and not just a matcher
//!
//! The host log reader is a **bounded on-demand read, not a stream**: each poll
//! shells out for the records written since some point and returns at most a
//! fixed number of them. Successive polls therefore overlap, and the same record
//! arrives again and again. Matching alone would fire an alert per look.
//!
//! So the pack carries a cursor. A record is evaluated once — the first time it
//! is seen — and never again:
//!
//! - a record **newer** than the cursor is new, and fires;
//! - a record **at** the cursor's instant fires only if its key has not been
//!   seen at that instant, which is what keeps several records sharing one
//!   microsecond from swallowing each other;
//! - a record **older** than the cursor never fires, seen before or not. That
//!   is the deliberate trade: a record that turns up late is lost rather than
//!   duplicated, because an alert delivered twice costs an operator more trust
//!   than one delivered never.
//!
//! The cursor starts at the moment the watch begins, so an agent that started a
//! minute ago does not page anyone for yesterday's records simply because the
//! reader's window reaches back past its own start.
//!
//! # What is counted rather than guessed
//!
//! A poll that comes back at the reader's line cap saw only the newest end of
//! its window. **How many records fell off the old end is unknowable**, so a
//! saturated poll is counted as an event in itself rather than being turned into
//! an invented number of lost records.

use std::collections::hash_map::DefaultHasher;
use std::collections::{HashMap, HashSet, VecDeque};
use std::hash::{Hash, Hasher};

use crate::alerts::sink::{AlertOrigin, AlertSeverity, EdgeAlert};
use crate::ml::redact::redact_log_line;

/// Distinct record keys retained at the cursor's own instant. Reaching this
/// would need more records than a microsecond-resolution clock can distinguish,
/// so the cap bounds memory without bounding behavior.
const MAX_KEYS_AT_CURSOR: usize = 512;

/// One host log record, normalized away from whatever tool read it. A platform
/// reader converts its own records into these, so the pack stays a list of
/// (matcher, meaning) rows that knows nothing about journald, the Event Log, or
/// anything that comes later.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct HostEvent<'a> {
    /// When the record was written, in microseconds since the Unix epoch.
    pub ts_micros: i64,
    /// Normalized severity label: `ERROR`, `WARN`, `INFO` or `DEBUG`.
    pub level: &'a str,
    /// The service or subsystem that emitted it, empty when the reader could
    /// not attribute it to one.
    pub unit: &'a str,
    /// The record's text, unredacted — redaction happens where an alert is
    /// built, so a rule can still match on text that will not be published.
    pub message: &'a str,
}

/// Normalized record severity, ordered so a rule can set a floor.
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord)]
#[non_exhaustive]
pub enum EventLevel {
    /// Diagnostic detail, and the answer for any label this agent does not
    /// recognize: an unreadable level must not clear a floor it was never
    /// measured against.
    Debug,
    /// Ordinary operation.
    Info,
    /// Something to keep an eye on.
    Warn,
    /// A failure.
    Error,
}

impl EventLevel {
    /// Reads a normalized level label. Anything unrecognized is [`Debug`], the
    /// floor, so an unparseable record can never satisfy a rule by accident.
    ///
    /// [`Debug`]: EventLevel::Debug
    #[must_use]
    pub fn from_label(label: &str) -> Self {
        match label.trim().to_ascii_uppercase().as_str() {
            "ERROR" => Self::Error,
            "WARN" | "WARNING" => Self::Warn,
            "INFO" | "NOTICE" => Self::Info,
            _ => Self::Debug,
        }
    }
}

/// What a rule looks for in one record.
///
/// The exclusions are what separate a rule from a substring search. Every
/// subsystem that reports a failure also reports its recovery, usually naming
/// the same component in nearly the same words — a disk that resets its link
/// announces the link coming back up, a throttled core announces its
/// temperature returning to normal. A matcher without `none_of` looks perfectly
/// correct until it pages someone at 03:00 for a machine that just got better.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct EventMatcher {
    /// Any one of these substrings, matched without regard to case, marks the
    /// record.
    pub any_of: Vec<String>,
    /// None of these may appear, whatever else matched.
    pub none_of: Vec<String>,
    /// The record must be at least this severe.
    pub min_level: EventLevel,
}

impl EventMatcher {
    /// Whether a record at `level` carrying `message` is what this rule watches
    /// for.
    #[must_use]
    pub fn matches(&self, level: &str, message: &str) -> bool {
        if EventLevel::from_label(level) < self.min_level {
            return false;
        }
        let haystack = message.to_ascii_lowercase();
        let contains = |needle: &String| haystack.contains(&needle.to_ascii_lowercase());
        self.any_of.iter().any(contains) && !self.none_of.iter().any(contains)
    }
}

/// One row of the pack: what to look for, and what it means when found.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct EventRule {
    /// How the catalogue identifies this rule.
    pub rule_id: String,
    /// How bad it is when it fires.
    pub severity: AlertSeverity,
    /// What it means, in the words a technician reads first.
    pub summary: String,
    /// What it matches.
    pub matcher: EventMatcher,
}

impl EventRule {
    /// The four curated rules a Linux host's journal can answer for.
    ///
    /// Each one names a failure the machine reports about itself and that no
    /// gauge shows: a task stuck for minutes, memory reclaimed by killing
    /// something, a disk that stopped answering, a processor slowing itself down
    /// to survive its own heat. Another platform adds its own rows over its own
    /// reader; the shape of a row is what makes that an addition rather than a
    /// change.
    #[must_use]
    pub fn linux_pack() -> Vec<Self> {
        vec![
            Self {
                rule_id: "linux.hung_task".to_string(),
                severity: AlertSeverity::Warning,
                summary: "a task was blocked for over two minutes".to_string(),
                matcher: EventMatcher {
                    any_of: vec!["blocked for more than".to_string()],
                    none_of: Vec::new(),
                    min_level: EventLevel::Error,
                },
            },
            Self {
                rule_id: "linux.oom_kill".to_string(),
                severity: AlertSeverity::Critical,
                summary: "the kernel killed a process to reclaim memory".to_string(),
                matcher: EventMatcher {
                    any_of: vec![
                        "out of memory: killed process".to_string(),
                        "oom-kill:".to_string(),
                    ],
                    none_of: Vec::new(),
                    min_level: EventLevel::Error,
                },
            },
            Self {
                rule_id: "linux.ata_reset".to_string(),
                severity: AlertSeverity::Warning,
                summary: "a disk stopped responding and its bus was reset".to_string(),
                matcher: EventMatcher {
                    any_of: vec![
                        "hard resetting link".to_string(),
                        "exception emask".to_string(),
                    ],
                    none_of: vec!["link up".to_string()],
                    min_level: EventLevel::Error,
                },
            },
            Self {
                rule_id: "linux.thermal_throttle".to_string(),
                severity: AlertSeverity::Warning,
                summary: "the processor slowed itself down under thermal load".to_string(),
                matcher: EventMatcher {
                    any_of: vec![
                        "temperature above threshold".to_string(),
                        "clock throttled".to_string(),
                    ],
                    none_of: vec!["temperature/speed normal".to_string()],
                    min_level: EventLevel::Error,
                },
            },
        ]
    }
}

/// The second signal class: one service failing over and over.
///
/// A single error from a service is ordinary. The same service producing them
/// steadily for a day is a service nobody has noticed is broken, and no
/// individual record says so.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ServiceErrorRule {
    /// How the catalogue identifies this rule.
    pub rule_id: String,
    /// How bad it is when it fires.
    pub severity: AlertSeverity,
    /// Errors from one service inside the window that constitute "repeatedly".
    pub threshold: u32,
    /// How far back the count reaches, in seconds.
    pub window_secs: i64,
    /// How many services may be tracked at once. A host that produces errors
    /// from an unbounded number of distinct services would otherwise grow this
    /// map without limit; the services turned away are counted.
    pub max_services: usize,
}

impl Default for ServiceErrorRule {
    fn default() -> Self {
        Self {
            rule_id: "linux.service_errors".to_string(),
            severity: AlertSeverity::Warning,
            threshold: 10,
            window_secs: 24 * 60 * 60,
            max_services: 64,
        }
    }
}

/// Where the pack has read up to, and which records it has already answered for
/// at that exact instant.
#[derive(Debug, Default)]
struct Cursor {
    at: i64,
    keys_at: HashSet<u64>,
}

impl Cursor {
    /// Whether this record is one the pack has not answered for yet.
    fn admits(&self, event: &HostEvent<'_>, key: u64) -> bool {
        match event.ts_micros.cmp(&self.at) {
            std::cmp::Ordering::Greater => true,
            std::cmp::Ordering::Equal => !self.keys_at.contains(&key),
            std::cmp::Ordering::Less => false,
        }
    }
}

/// Identifies a record within one instant. A hash rather than the text itself,
/// so a burst of long records at one instant cannot grow the cursor's memory
/// with their contents.
fn record_key(event: &HostEvent<'_>) -> u64 {
    let mut hasher = DefaultHasher::new();
    event.unit.hash(&mut hasher);
    event.level.hash(&mut hasher);
    event.message.hash(&mut hasher);
    hasher.finish()
}

/// The rolling per-service error count.
#[derive(Debug)]
struct ServiceErrors {
    rule: ServiceErrorRule,
    /// The most recent error timestamps per service, oldest first, never more
    /// than the threshold: knowing whether the threshold's worth of errors all
    /// sit inside the window needs no more history than that.
    recent: HashMap<String, VecDeque<i64>>,
    /// Services currently over the threshold, so the crossing fires once rather
    /// than once per error thereafter.
    over: HashSet<String>,
    untracked: u64,
}

impl ServiceErrors {
    fn new(rule: ServiceErrorRule) -> Self {
        Self {
            rule,
            recent: HashMap::new(),
            over: HashSet::new(),
            untracked: 0,
        }
    }

    /// Records one error from `unit` at `ts` and reports whether the service has
    /// just crossed from below the threshold to at or above it.
    fn record(&mut self, unit: &str, ts: i64) -> bool {
        let window = self.rule.window_secs.saturating_mul(1_000_000);
        let threshold = self.rule.threshold as usize;
        if threshold == 0 {
            return false;
        }

        self.forget_idle(ts, window);

        if !self.recent.contains_key(unit) && self.recent.len() >= self.rule.max_services {
            self.untracked += 1;
            return false;
        }

        let seen = self.recent.entry(unit.to_string()).or_default();
        while seen.len() >= threshold {
            seen.pop_front();
        }
        seen.push_back(ts);
        Self::forget_stale(seen, ts, window);

        if seen.len() >= threshold {
            // `insert` answers false when the service was already over, which is
            // exactly the "fires once per crossing" rule.
            self.over.insert(unit.to_string())
        } else {
            self.over.remove(unit);
            false
        }
    }

    /// Drops timestamps that have aged out of the window.
    fn forget_stale(seen: &mut VecDeque<i64>, now: i64, window: i64) {
        while let Some(&oldest) = seen.front() {
            if now.saturating_sub(oldest) > window {
                seen.pop_front();
            } else {
                break;
            }
        }
    }

    /// Forgets services whose errors have all aged out, so a host that produces
    /// a burst from many services once does not hold them for the rest of the
    /// run. This is what keeps `max_services` a backstop rather than a limit
    /// reached in ordinary operation.
    fn forget_idle(&mut self, now: i64, window: i64) {
        self.recent.retain(|unit, seen| {
            Self::forget_stale(seen, now, window);
            let live = !seen.is_empty();
            if !live {
                self.over.remove(unit);
            }
            live
        });
    }
}

/// The system-event rule pack: curated per-record rules, plus the rolling
/// per-service error count, evaluated over the records of one bounded poll.
#[derive(Debug)]
pub struct EventPack {
    rules: Vec<EventRule>,
    services: ServiceErrors,
    cursor: Cursor,
    saturated_polls: u64,
}

impl EventPack {
    /// A pack watching from `start_micros` onward. Rule instances are supplied
    /// rather than assumed, so the catalogue decides what a device watches for
    /// and this type only decides what watching means.
    #[must_use]
    pub fn new(rules: Vec<EventRule>, services: ServiceErrorRule, start_micros: i64) -> Self {
        Self {
            rules,
            services: ServiceErrors::new(services),
            cursor: Cursor {
                at: start_micros,
                keys_at: HashSet::new(),
            },
            saturated_polls: 0,
        }
    }

    /// Evaluates one poll's records and returns the alerts they raise.
    ///
    /// `saturated` says the reader returned as many records as it is willing to
    /// return, which means the window held at least that many and the oldest of
    /// them were never seen.
    pub fn poll(&mut self, events: &[HostEvent<'_>], saturated: bool) -> Vec<EdgeAlert> {
        if saturated {
            self.saturated_polls += 1;
        }

        let mut alerts = Vec::new();
        let mut newest = self.cursor.at;
        let mut keys_at_newest: HashSet<u64> = self.cursor.keys_at.clone();

        for event in events {
            let key = record_key(event);
            if !self.cursor.admits(event, key) {
                continue;
            }

            alerts.extend(self.evaluate(event));

            match event.ts_micros.cmp(&newest) {
                std::cmp::Ordering::Greater => {
                    newest = event.ts_micros;
                    keys_at_newest.clear();
                    keys_at_newest.insert(key);
                }
                std::cmp::Ordering::Equal => {
                    if keys_at_newest.len() < MAX_KEYS_AT_CURSOR {
                        keys_at_newest.insert(key);
                    }
                }
                std::cmp::Ordering::Less => {}
            }
        }

        self.cursor.at = newest;
        self.cursor.keys_at = keys_at_newest;
        alerts
    }

    /// Moves the watch to `ts_micros` without evaluating anything, so records
    /// written before it never fire.
    ///
    /// This is what maintenance mode does with its window. The disruptive work
    /// an admin performs under maintenance produces exactly the records this
    /// pack matches — a host being rebooted stops answering its disks and kills
    /// processes — so holding those records until maintenance ends would page
    /// someone for the maintenance itself. Suppressing the window is the point,
    /// not a side effect of skipping the read.
    pub fn skip_to(&mut self, ts_micros: i64) {
        if ts_micros > self.cursor.at {
            self.cursor.at = ts_micros;
            self.cursor.keys_at.clear();
        }
    }

    /// The least severe record any rule in this pack could act on.
    ///
    /// A reader uses it to bound what it fetches. Deriving it from the rules is
    /// the point: a floor hardcoded at the call site would keep working right
    /// up until someone adds a rule that watches warnings, which would then
    /// match nothing and say nothing about matching nothing.
    #[must_use]
    pub fn min_level(&self) -> EventLevel {
        // The per-service count is fed by errors, so errors are needed whatever
        // the per-record rules ask for.
        self.rules
            .iter()
            .map(|rule| rule.matcher.min_level)
            .min()
            .unwrap_or(EventLevel::Error)
            .min(EventLevel::Error)
    }

    /// Polls that came back at the reader's cap, each one a window whose oldest
    /// records were never seen. The number of records lost is not knowable, so
    /// it is not reported as one.
    #[must_use]
    pub fn saturated_polls(&self) -> u64 {
        self.saturated_polls
    }

    /// Services the tracking cap turned away, each one a service whose repeated
    /// errors are going uncounted.
    #[must_use]
    pub fn untracked_services(&self) -> u64 {
        self.services.untracked
    }

    /// Evaluates one previously unseen record.
    ///
    /// A record that a curated rule explains does **not** also feed the
    /// per-service count. The pack has already said what it was; counting it
    /// again toward "this service keeps failing" would report one event twice
    /// under two names, and the second name would be the vaguer of the two.
    fn evaluate(&mut self, event: &HostEvent<'_>) -> Vec<EdgeAlert> {
        let matched: Vec<EdgeAlert> = self
            .rules
            .iter()
            .filter(|rule| rule.matcher.matches(event.level, event.message))
            .map(|rule| EdgeAlert {
                rule_id: rule.rule_id.clone(),
                severity: rule.severity,
                ts_micros: event.ts_micros,
                subject: event.unit.to_string(),
                summary: rule.summary.clone(),
                evidence: vec![redact_log_line(event.message)],
                origin: AlertOrigin::Live,
            })
            .collect();

        if !matched.is_empty() {
            return matched;
        }

        // Only a failure attributable to a named service can say that *that*
        // service keeps failing. Records the reader could not attribute would
        // otherwise pile into one unnamed bucket and fire as though a single
        // service were broken.
        if EventLevel::from_label(event.level) < EventLevel::Error || event.unit.is_empty() {
            return Vec::new();
        }

        if !self.services.record(event.unit, event.ts_micros) {
            return Vec::new();
        }

        let rule = &self.services.rule;
        vec![EdgeAlert {
            rule_id: rule.rule_id.clone(),
            severity: rule.severity,
            ts_micros: event.ts_micros,
            subject: event.unit.to_string(),
            summary: format!(
                "{} logged {} errors within {} hours",
                event.unit,
                rule.threshold,
                rule.window_secs / 3_600
            ),
            evidence: vec![redact_log_line(event.message)],
            origin: AlertOrigin::Live,
        }]
    }
}
