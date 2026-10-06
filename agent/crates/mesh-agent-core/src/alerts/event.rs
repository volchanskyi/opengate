//! The curated system-event rule pack and the rolling per-service error count.
//! A cursor evaluates each record once: newer fires, same instant fires if unseen, older never.

use std::collections::hash_map::DefaultHasher;
use std::collections::{HashMap, HashSet, VecDeque};
use std::hash::{Hash, Hasher};

use mesh_protocol::{RuleCoverage, RuleCoverageState};

use crate::alerts::evidence::{pack_evidence, EvidenceSource};
use crate::alerts::sink::{AlertOrigin, AlertSeverity, EdgeAlert};

/// Microseconds in a second; records are stamped in microseconds and windows in seconds.
const MICROS_PER_SEC: i64 = 1_000_000;

/// Distinct record keys retained at the cursor's own instant, bounding cursor memory.
const MAX_KEYS_AT_CURSOR: usize = 512;

/// One host log record, normalized from whatever platform reader produced it.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct HostEvent<'a> {
    /// When the record was written, in microseconds since the Unix epoch.
    pub ts_micros: i64,
    /// Normalized severity label: `ERROR`, `WARN`, `INFO` or `DEBUG`.
    pub level: &'a str,
    /// The service or subsystem that emitted it, empty when unattributed.
    pub unit: &'a str,
    /// The record's text, unredacted; redaction happens when an alert is built.
    pub message: &'a str,
}

/// Normalized record severity, ordered so a rule can set a floor.
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord)]
#[non_exhaustive]
pub enum EventLevel {
    /// Diagnostic detail, and the level of any unrecognized label.
    Debug,
    /// Ordinary operation.
    Info,
    /// Something to keep an eye on.
    Warn,
    /// A failure.
    Error,
}

impl EventLevel {
    /// Reads a normalized level label; anything unrecognized is the lowest level.
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

/// What a rule looks for in one record; `none_of` excludes recovery messages that name the same
/// component as the failure.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct EventMatcher {
    /// Any one of these substrings, matched case-insensitively, marks the record.
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
    /// The rule revision this machine runs, carried onto every alert it raises.
    pub version: u32,
    /// How bad it is when it fires.
    pub severity: AlertSeverity,
    /// What it means, in the words a technician reads first.
    pub summary: String,
    /// What it matches.
    pub matcher: EventMatcher,
}

impl EventRule {
    /// The four curated rules a Linux host's journal can answer for: a stuck task, an
    /// out-of-memory kill, a disk that stopped answering and thermal throttling.
    #[must_use]
    pub fn linux_pack() -> Vec<Self> {
        vec![
            Self {
                rule_id: "linux-hung-task".to_string(),
                version: 1,
                severity: AlertSeverity::Warning,
                summary: "a task was blocked for over two minutes".to_string(),
                matcher: EventMatcher {
                    any_of: vec!["blocked for more than".to_string()],
                    none_of: Vec::new(),
                    min_level: EventLevel::Error,
                },
            },
            Self {
                rule_id: "linux-oom-kill".to_string(),
                version: 1,
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
                rule_id: "linux-ata-reset".to_string(),
                version: 1,
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
                rule_id: "linux-thermal-throttle".to_string(),
                version: 1,
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

/// The second signal class: one service producing errors repeatedly.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ServiceErrorRule {
    /// How the catalogue identifies this rule.
    pub rule_id: String,
    /// The rule revision this machine runs, carried onto every alert it raises.
    pub version: u32,
    /// How bad it is when it fires.
    pub severity: AlertSeverity,
    /// Errors from one service inside the window that constitute "repeatedly".
    pub threshold: u32,
    /// How far back the count reaches, in seconds.
    pub window_secs: i64,
    /// How many services may be tracked at once; services turned away are counted.
    pub max_services: usize,
}

impl Default for ServiceErrorRule {
    fn default() -> Self {
        Self {
            rule_id: "linux-service-errors".to_string(),
            version: 1,
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

/// Hashes a record to a key identifying it within one instant, so cursor memory is independent of
/// record length.
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
    /// The most recent error timestamps per service, oldest first, at most the threshold.
    recent: HashMap<String, VecDeque<i64>>,
    /// Services currently over the threshold, so a crossing fires once.
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
            // `insert` answers false when the service was already over.
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

    /// Forgets services whose errors have all aged out.
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

/// The system-event rule pack: curated per-record rules plus the rolling per-service error count.
#[derive(Debug)]
pub struct EventPack {
    rules: Vec<EventRule>,
    services: ServiceErrors,
    cursor: Cursor,
    saturated_polls: u64,
}

impl EventPack {
    /// What this pack is doing on this machine, one entry per rule, read from the pack's own rows
    /// so a machine that never started its watch still reports.
    #[must_use]
    pub fn coverage(
        rules: &[EventRule],
        services: &ServiceErrorRule,
        can_read_its_log: bool,
    ) -> Vec<RuleCoverage> {
        let state = if can_read_its_log {
            RuleCoverageState::Active
        } else {
            RuleCoverageState::Unsupported
        };
        rules
            .iter()
            .map(|rule| rule.rule_id.clone())
            .chain(std::iter::once(services.rule_id.clone()))
            .map(|rule_id| RuleCoverage { rule_id, state })
            .collect()
    }

    /// A pack watching from `start_micros` onward over the supplied rule instances.
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

    /// Evaluates one poll's records; `saturated` marks a poll that returned the reader's full cap.
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

    /// Moves the watch to `ts_micros` without evaluating anything, so records written before it
    /// never fire; maintenance mode uses it to suppress its window.
    pub fn skip_to(&mut self, ts_micros: i64) {
        if ts_micros > self.cursor.at {
            self.cursor.at = ts_micros;
            self.cursor.keys_at.clear();
        }
    }

    /// The least severe record any rule in this pack could act on, derived from the rules to bound
    /// what a reader fetches.
    #[must_use]
    pub fn min_level(&self) -> EventLevel {
        // The per-service count is fed by errors, so errors are always needed.
        self.rules
            .iter()
            .map(|rule| rule.matcher.min_level)
            .min()
            .unwrap_or(EventLevel::Error)
            .min(EventLevel::Error)
    }

    /// Polls that came back at the reader's cap, each a window whose oldest records went unseen.
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

    /// Evaluates one unseen record; a record a curated rule explains skips the per-service count.
    fn evaluate(&mut self, event: &HostEvent<'_>) -> Vec<EdgeAlert> {
        let matched: Vec<EdgeAlert> = self
            .rules
            .iter()
            .filter(|rule| rule.matcher.matches(event.level, event.message))
            .map(|rule| {
                alert_for(
                    &rule.rule_id,
                    rule.version,
                    rule.severity,
                    rule.summary.clone(),
                    event,
                )
            })
            .collect();

        if !matched.is_empty() {
            return matched;
        }

        // Only errors attributed to a named service count toward that service.
        if EventLevel::from_label(event.level) < EventLevel::Error || event.unit.is_empty() {
            return Vec::new();
        }

        if !self.services.record(event.unit, event.ts_micros) {
            return Vec::new();
        }

        let rule = &self.services.rule;
        vec![alert_for(
            &rule.rule_id,
            rule.version,
            rule.severity,
            format!(
                "{} logged {} errors within {} hours",
                event.unit,
                rule.threshold,
                rule.window_secs / 3_600
            ),
            event,
        )]
    }
}

/// One record's alert: the window is the record's instant at both ends, the redacted record is the
/// evidence, and the alert names no dimension and carries no value.
fn alert_for(
    rule_id: &str,
    version: u32,
    severity: AlertSeverity,
    summary: String,
    event: &HostEvent<'_>,
) -> EdgeAlert {
    let line = event.message.to_string();
    let packed = pack_evidence(&EvidenceSource {
        ranked: &[],
        readings: &[],
        processes: &[],
        log_lines: std::slice::from_ref(&line),
        event_ts: event.ts_micros.div_euclid(MICROS_PER_SEC),
    });
    EdgeAlert {
        rule_id: rule_id.to_string(),
        rule_version: version,
        severity,
        ts_micros: event.ts_micros,
        window_start_micros: event.ts_micros,
        window_end_micros: event.ts_micros,
        metric: String::new(),
        value: None,
        subject: event.unit.to_string(),
        summary,
        evidence: packed.bytes,
        evidence_codec: packed.codec.to_string(),
        origin: AlertOrigin::Live,
    }
}
