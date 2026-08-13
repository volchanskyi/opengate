//! Scheduling the retroactive scans: when a newly arrived rule gets re-run over
//! the device's own history, and everything that makes it stand down.
//!
//! The scan engine itself lives in
//! [`alerts::retro`](mesh_agent_core::alerts::RetroScan). What lives here is the
//! decision to start one at all. Three things shape it:
//!
//! A rule is scanned **once per version**, not once per push. The server pushes
//! the whole ruleset on every reconnect, so triggering on the push would re-run
//! every rule's history — and re-raise every historical finding — each time a
//! flaky link came back. The ledger remembers the exact definition each scan
//! covered, so a re-push of the same rule is recognised as the same rule and a
//! retuned threshold is recognised as a new one.
//!
//! A scan runs **only while the machine has nothing better to do**: not during
//! maintenance, not while the host is busy, and not while its disk is filling.
//! The past is not going anywhere.
//!
//! The host's free disk is read here for both of the jobs that need it — the
//! scan's own stand-down, and the store's cap backoff, which cannot shrink what
//! it keeps without being told how tight the host is getting.

use std::collections::BTreeMap;
use std::path::{Path, PathBuf};
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

use tracing::{debug, info, warn};

use edge_tsdb::TsdbConfig;
use mesh_agent_core::alerts::{
    retro_hold, AlertSink, RetroBudget, RetroConditions, RetroCursor, RetroHold, RetroPlan,
    RetroScan, RetroStep,
};
use mesh_agent_core::maintenance::MaintenanceGate;
use mesh_protocol::ThresholdRule;
use serde::{Deserialize, Serialize};

use crate::clock::unix_micros;
use crate::edge_sentinel::{LoadSignal, SharedSink};

/// How often the job looks for a rule version it has not scanned yet, and
/// re-reads how much room the host disk has left.
const POLL_INTERVAL: Duration = Duration::from_secs(60);

/// The ruleset the server most recently pushed, shared with the control loop.
/// Unlike the sampler's mailbox this is not drained — it is what is *installed*,
/// which is the question "is this scan's rule still current" needs.
pub(crate) type InstalledRules = Arc<Mutex<Vec<ThresholdRule>>>;

/// What one rule version's scan has covered so far.
#[derive(Debug, Clone, Serialize, Deserialize)]
struct RetroRecord {
    /// The definition that was scanned. Stored in full rather than as a digest:
    /// a digest that changes with a toolchain would silently re-scan the fleet's
    /// whole history after an upgrade.
    rule: ThresholdRule,
    cursor: RetroCursor,
    /// Whether there is anything left to scan for this version — including a
    /// version that history cannot answer at all, which is finished the moment
    /// it is recognised.
    done: bool,
}

/// What the device has already looked back over, across restarts.
#[derive(Debug, Default, Serialize, Deserialize)]
struct Ledger {
    #[serde(default)]
    rules: BTreeMap<String, RetroRecord>,
}

/// The ledger plus the file it is kept in.
pub(crate) struct RetroLedger {
    path: PathBuf,
    ledger: Ledger,
}

impl RetroLedger {
    /// Read the ledger under `data_dir`. A missing or unreadable file starts an
    /// empty one: the cost of forgetting is one repeated scan, and refusing to
    /// scan at all because a cache file is damaged is the worse failure.
    pub(crate) fn load(data_dir: &Path) -> Self {
        let path = data_dir.join("retro-scans.json");
        let ledger = std::fs::read(&path)
            .ok()
            .and_then(|bytes| serde_json::from_slice(&bytes).ok())
            .unwrap_or_default();
        Self { path, ledger }
    }

    /// Persist the ledger, writing beside the file and renaming over it so a
    /// crash mid-write leaves the previous ledger rather than half of this one.
    fn save(&self) {
        let Ok(bytes) = serde_json::to_vec_pretty(&self.ledger) else {
            return;
        };
        let temp = self.path.with_extension("json.tmp");
        if std::fs::write(&temp, &bytes)
            .and_then(|()| std::fs::rename(&temp, &self.path))
            .is_err()
        {
            debug!(path = %self.path.display(), "could not persist the retro-scan ledger");
        }
    }

    /// The first installed rule with history left to scan, and where its scan
    /// left off. A rule whose definition changed is a different version, so a
    /// finished scan of the previous one does not cover it.
    fn pending(&self, installed: &[ThresholdRule]) -> Option<(ThresholdRule, RetroCursor)> {
        installed.iter().find_map(|rule| {
            match self.ledger.rules.get(&rule.id) {
                Some(record) if record.rule == *rule => {
                    (!record.done).then(|| (rule.clone(), record.cursor))
                }
                // Never scanned, or scanned as a different version.
                _ => Some((rule.clone(), RetroCursor::default())),
            }
        })
    }

    /// Record how far a version's scan has got.
    fn record(&mut self, rule: &ThresholdRule, cursor: RetroCursor, done: bool) {
        self.ledger.rules.insert(
            rule.id.clone(),
            RetroRecord {
                rule: rule.clone(),
                cursor,
                done,
            },
        );
        self.save();
    }

    /// Forget rules that are no longer installed, so a customer who stopped a
    /// rule is not carrying its scan state forever.
    fn forget_uninstalled(&mut self, installed: &[ThresholdRule]) {
        let before = self.ledger.rules.len();
        self.ledger
            .rules
            .retain(|id, _| installed.iter().any(|rule| rule.id == *id));
        if self.ledger.rules.len() != before {
            self.save();
        }
    }
}

/// Free bytes on the filesystem holding `path`, or `None` when the host does not
/// say. `None` is not read as "full": a host that cannot report its disk is not
/// evidence of anything.
fn host_free_bytes(path: &Path) -> Option<u64> {
    use sysinfo::Disks;

    let disks = Disks::new_with_refreshed_list();
    disks
        .iter()
        .filter(|disk| path.starts_with(disk.mount_point()))
        // The longest matching mount point is the filesystem the path is really
        // on; every path starts with `/`, so the shortest always matches too.
        .max_by_key(|disk| disk.mount_point().as_os_str().len())
        .map(sysinfo::Disk::available_space)
}

/// The host's free space, and how recently it was looked at.
///
/// A scan pacing itself in fractions of a second must not stat every mount on
/// the machine between chunks, so a reading is reused until it ages out. The
/// same read tells the store, whose cap cannot back off under host pressure
/// without being told how tight the host is getting — and a long scan has to
/// keep telling it, not only the poll that started the scan.
struct DiskWatch {
    path: PathBuf,
    free: Option<u64>,
    read_at: Option<Instant>,
}

impl DiskWatch {
    fn new(path: PathBuf) -> Self {
        Self {
            path,
            free: None,
            read_at: None,
        }
    }

    /// The current reading, taken afresh when the last one has aged out.
    fn refresh(&mut self, store: &SharedSink) -> Option<u64> {
        if self.read_at.is_none_or(|at| at.elapsed() >= POLL_INTERVAL) {
            self.free = host_free_bytes(&self.path);
            self.read_at = Some(Instant::now());
            match store.lock() {
                Ok(mut sink) => sink.set_host_free_bytes(self.free),
                Err(e) => warn!(error = %e, "local store lock poisoned"),
            }
        }
        self.free
    }
}

/// Everything the job needs to decide whether, and what, to scan.
pub(crate) struct RetroWiring {
    /// Where findings go — the same bounded, rate-limited sink every other
    /// producer writes to.
    pub sink: AlertSink,
    /// The ruleset currently installed on this device.
    pub rules: InstalledRules,
    /// The local store, when it opened. Without one there is no history.
    pub store: Option<SharedSink>,
    pub maintenance: MaintenanceGate,
    /// The sampler's most recent host CPU reading.
    pub load: LoadSignal,
    /// Where the ledger is kept, and the filesystem whose free space matters.
    pub data_dir: PathBuf,
    /// The store's own footprint policy, which the scan reads its disk-pressure
    /// threshold from.
    pub store_config: TsdbConfig,
}

/// Spawn the retroactive-scan job.
///
/// Without a local store the task returns rather than looping: there is no
/// history to look back over, and a job that wakes every minute to find that out
/// is a heartbeat with no signal in it.
pub(crate) fn spawn_retro_scans(wiring: RetroWiring) -> tokio::task::JoinHandle<()> {
    tokio::task::spawn_blocking(move || {
        let Some(store) = wiring.store.clone() else {
            info!("no local store on this device; rules are not re-run over history");
            return;
        };
        let mut ledger = RetroLedger::load(&wiring.data_dir);
        let mut disk = DiskWatch::new(wiring.data_dir.clone());
        info!("retroactive scan job starting");
        loop {
            std::thread::sleep(POLL_INTERVAL);
            let free = disk.refresh(&store);

            let installed = match wiring.rules.lock() {
                Ok(rules) => rules.clone(),
                Err(e) => {
                    warn!(error = %e, "installed-ruleset lock poisoned");
                    continue;
                }
            };
            if installed.is_empty() {
                continue;
            }
            ledger.forget_uninstalled(&installed);

            if let Some(hold) = hold_now(&wiring, free) {
                debug!(?hold, "retroactive scan standing down");
                continue;
            }
            let Some((rule, cursor)) = ledger.pending(&installed) else {
                continue;
            };
            scan_one(&wiring, &store, &mut ledger, &mut disk, rule, cursor);
        }
    })
}

/// Why a scan cannot run at this moment, if it cannot.
fn hold_now(wiring: &RetroWiring, free: Option<u64>) -> Option<RetroHold> {
    retro_hold(
        &RetroConditions {
            in_maintenance: wiring.maintenance.in_maintenance(),
            cpu_percent: wiring.load.cpu_percent(),
            host_free_bytes: free,
        },
        wiring.store_config,
    )
}

/// Run one rule version's scan, chunk by chunk, until it finishes or something
/// asks it to stand down.
fn scan_one(
    wiring: &RetroWiring,
    store: &SharedSink,
    ledger: &mut RetroLedger,
    disk: &mut DiskWatch,
    rule: ThresholdRule,
    cursor: RetroCursor,
) {
    let plan = match RetroPlan::for_rule(&rule) {
        Ok(plan) => plan,
        Err(reason) => {
            // History cannot answer this rule. That is a finished question, not
            // a failed scan — recording it is what stops the job re-deciding it
            // every minute for the life of the agent.
            info!(rule = %rule.id, %reason, "rule cannot be re-run over stored history");
            ledger.record(&rule, cursor, true);
            return;
        }
    };
    let mut scan = RetroScan::resume(plan, RetroBudget::default(), cursor);

    loop {
        let step = {
            let snapshot = match store.lock() {
                Ok(sink) => sink.snapshot(),
                Err(e) => {
                    warn!(error = %e, "local store lock poisoned");
                    return;
                }
            };
            let snapshot = match snapshot {
                Ok(snapshot) => snapshot,
                Err(e) => {
                    warn!(error = %e, rule = %rule.id, "could not read local history");
                    return;
                }
            };
            scan.run_chunk(&snapshot, &wiring.sink, unix_micros())
        };
        let step = match step {
            Ok(step) => step,
            Err(e) => {
                warn!(error = %e, rule = %rule.id, "retroactive scan read failed");
                ledger.record(&rule, scan.cursor(), false);
                return;
            }
        };

        match after_chunk(step) {
            AfterChunk::Finished => {
                let stats = scan.stats();
                info!(
                    rule = %rule.id,
                    findings = stats.findings,
                    minutes = stats.buckets_evaluated,
                    scope = ?scan.scope(),
                    cpu_micros = stats.busy_micros,
                    "retroactive scan complete"
                );
                ledger.record(&rule, scan.cursor(), true);
                return;
            }
            AfterChunk::NothingToScan => {
                // Reported as what it is: this device has nothing to look back
                // over. Recording it as a completed scan would claim the machine
                // had been checked back through history it never had.
                info!(
                    rule = %rule.id,
                    "no stored history for this rule; retroactive scope is empty"
                );
                ledger.record(&rule, cursor, true);
                return;
            }
            AfterChunk::Pause => {
                ledger.record(&rule, scan.cursor(), false);
                return;
            }
            AfterChunk::StandDown(stand_down) => {
                ledger.record(&rule, scan.cursor(), false);
                std::thread::sleep(stand_down);
                if scan.superseded_by(&current_rules(wiring)) {
                    info!(
                        rule = %rule.id,
                        "retroactive scan stopped: its rule version is no longer installed"
                    );
                    return;
                }
                if let Some(hold) = hold_now(wiring, disk.refresh(store)) {
                    debug!(rule = %rule.id, ?hold, "retroactive scan suspended mid-history");
                    return;
                }
            }
        }
    }
}

/// What one chunk's outcome means for the scan around it.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum AfterChunk {
    /// Leave the machine alone for this long, then run another chunk.
    StandDown(Duration),
    /// Every minute the store holds has been scanned for this rule version.
    Finished,
    /// The store holds no history for this rule — a different answer from having
    /// scanned it and found nothing, and recorded as its own.
    NothingToScan,
    /// Stop where the scan stands, keeping its place for the next poll.
    Pause,
}

/// Read one chunk's outcome. An outcome a future scan engine reports and this
/// build does not understand pauses the scan rather than being looped on.
fn after_chunk(step: RetroStep) -> AfterChunk {
    match step {
        RetroStep::Yielded { stand_down } => AfterChunk::StandDown(stand_down),
        RetroStep::Complete => AfterChunk::Finished,
        RetroStep::NoHistory => AfterChunk::NothingToScan,
        _ => AfterChunk::Pause,
    }
}

/// The ruleset installed right now. A poisoned lock reads as an empty set, which
/// stops the running scan — the safe direction when nothing can say whether its
/// rule is still current.
fn current_rules(wiring: &RetroWiring) -> Vec<ThresholdRule> {
    wiring
        .rules
        .lock()
        .map(|rules| rules.clone())
        .unwrap_or_default()
}

#[cfg(test)]
mod tests {
    use super::{after_chunk, host_free_bytes, AfterChunk, RetroLedger};
    use mesh_agent_core::alerts::{RetroCursor, RetroStep};
    use mesh_protocol::{AlertComparator, RulePredicate, ThresholdRule};
    use std::time::Duration;

    fn rule(id: &str, threshold: f64) -> ThresholdRule {
        ThresholdRule {
            id: id.to_string(),
            metric: "disk.used_percent".to_string(),
            comparator: AlertComparator::Gte,
            threshold,
            clear: 85.0,
            sustain_secs: 300,
            predicate: RulePredicate::Instant,
            window_secs: 0,
            all: Vec::new(),
        }
    }

    /// A rule nobody has looked back over yet is pending, from the beginning of
    /// history.
    #[test]
    fn an_unscanned_rule_is_pending_from_the_start() {
        let dir = tempfile::tempdir().unwrap();
        let ledger = RetroLedger::load(dir.path());

        let (pending, cursor) = ledger.pending(&[rule("disk-critical", 90.0)]).unwrap();
        assert_eq!(pending.id, "disk-critical");
        assert_eq!(cursor, RetroCursor::default());
        assert_eq!(ledger.pending(&[]), None, "no rules, nothing to scan");
    }

    /// A finished scan is not repeated when the server pushes the same ruleset
    /// again — which it does on every reconnect. Re-running it would re-raise
    /// every historical finding each time a flaky link came back.
    #[test]
    fn a_finished_scan_is_not_repeated_on_the_next_push() {
        let dir = tempfile::tempdir().unwrap();
        let mut ledger = RetroLedger::load(dir.path());
        let installed = vec![rule("disk-critical", 90.0)];

        ledger.record(&installed[0], RetroCursor::at(1_700_000_000), true);

        assert_eq!(ledger.pending(&installed), None);
    }

    /// A retuned threshold is a different version of the rule, and history has
    /// not been checked against it — so it is scanned again, from the start.
    #[test]
    fn a_retuned_rule_is_a_version_history_has_not_been_checked_against() {
        let dir = tempfile::tempdir().unwrap();
        let mut ledger = RetroLedger::load(dir.path());
        ledger.record(&rule("disk-critical", 90.0), RetroCursor::at(1_000), true);

        let (pending, cursor) = ledger.pending(&[rule("disk-critical", 95.0)]).unwrap();
        assert_eq!(pending.threshold, 95.0);
        assert_eq!(
            cursor,
            RetroCursor::default(),
            "the new version starts from the beginning of history"
        );
    }

    /// A part-finished scan resumes where it stopped, including across the agent
    /// restarting — which is what the file is for.
    #[test]
    fn a_part_finished_scan_survives_a_restart() {
        let dir = tempfile::tempdir().unwrap();
        let installed = vec![rule("disk-critical", 90.0)];
        {
            let mut ledger = RetroLedger::load(dir.path());
            ledger.record(&installed[0], RetroCursor::at(1_700_000_060), false);
        }

        let reopened = RetroLedger::load(dir.path());
        let (pending, cursor) = reopened.pending(&installed).unwrap();
        assert_eq!(pending.id, "disk-critical");
        assert_eq!(cursor, RetroCursor::at(1_700_000_060));
    }

    /// A rule a customer stopped takes its scan state with it, rather than being
    /// carried for the life of the device.
    #[test]
    fn an_uninstalled_rule_is_forgotten() {
        let dir = tempfile::tempdir().unwrap();
        let mut ledger = RetroLedger::load(dir.path());
        ledger.record(&rule("disk-critical", 90.0), RetroCursor::at(1_000), true);
        ledger.record(&rule("cpu-saturated", 95.0), RetroCursor::at(1_000), true);

        ledger.forget_uninstalled(&[rule("disk-critical", 90.0)]);

        // The survivor is still finished; the forgotten one would be scanned
        // afresh if it were ever installed again.
        assert_eq!(ledger.pending(&[rule("disk-critical", 90.0)]), None);
        assert!(ledger.pending(&[rule("cpu-saturated", 95.0)]).is_some());
    }

    /// A damaged ledger file costs one repeated scan, not the ability to scan.
    #[test]
    fn a_damaged_ledger_starts_empty_rather_than_refusing_to_scan() {
        let dir = tempfile::tempdir().unwrap();
        std::fs::write(dir.path().join("retro-scans.json"), b"{not json").unwrap();

        let ledger = RetroLedger::load(dir.path());
        assert!(ledger.pending(&[rule("disk-critical", 90.0)]).is_some());
    }

    /// A chunk that yielded is paced; one that finished, or found nothing to
    /// look back over, ends the scan — and the two endings are distinct,
    /// because "scanned it all" and "there was nothing to scan" are different
    /// answers to give about a machine.
    #[test]
    fn a_chunks_outcome_decides_whether_the_scan_goes_on() {
        assert_eq!(
            after_chunk(RetroStep::Yielded {
                stand_down: Duration::from_millis(250)
            }),
            AfterChunk::StandDown(Duration::from_millis(250))
        );
        assert_eq!(after_chunk(RetroStep::Complete), AfterChunk::Finished);
        assert_eq!(after_chunk(RetroStep::NoHistory), AfterChunk::NothingToScan);
        assert_ne!(
            after_chunk(RetroStep::NoHistory),
            after_chunk(RetroStep::Complete),
            "an empty scope is not a completed scan"
        );
    }

    /// The free-space read resolves to a real filesystem on any host that
    /// reports its disks, and never panics on one that does not.
    #[test]
    fn free_space_is_read_for_the_filesystem_the_data_dir_is_on() {
        let dir = tempfile::tempdir().unwrap();
        if let Some(free) = host_free_bytes(dir.path()) {
            assert!(free > 0, "a writable temp dir on a full filesystem");
        }
    }
}
