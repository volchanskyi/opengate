//! Starts each rule version's retroactive scan once, and only while the host has spare capacity.
//! The free-disk reading taken here also feeds the store's cap backoff.

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

/// The ruleset the server most recently pushed, shared with the control loop and never drained.
pub(crate) type InstalledRules = Arc<Mutex<Vec<ThresholdRule>>>;

#[derive(Debug, Clone, Serialize, Deserialize)]
struct RetroRecord {
    /// Holds the definition in full, because a digest that varies with the toolchain would
    /// re-scan the whole history after an upgrade.
    rule: ThresholdRule,
    cursor: RetroCursor,
    /// True once no scan remains for this version, including one history cannot answer.
    done: bool,
}

#[derive(Debug, Default, Serialize, Deserialize)]
struct Ledger {
    #[serde(default)]
    rules: BTreeMap<String, RetroRecord>,
}

pub(crate) struct RetroLedger {
    path: PathBuf,
    ledger: Ledger,
}

impl RetroLedger {
    /// Reads the ledger under `data_dir`; a missing or unreadable file starts an empty one,
    /// which costs one repeated scan.
    pub(crate) fn load(data_dir: &Path) -> Self {
        let path = data_dir.join("retro-scans.json");
        let ledger = std::fs::read(&path)
            .ok()
            .and_then(|bytes| serde_json::from_slice(&bytes).ok())
            .unwrap_or_default();
        Self { path, ledger }
    }

    /// Writes beside the file and renames over it, so a crash mid-write keeps the old ledger.
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

    /// The first installed rule with history left to scan, and its cursor; a changed definition
    /// is a new version that needs its own scan.
    fn pending(&self, installed: &[ThresholdRule]) -> Option<(ThresholdRule, RetroCursor)> {
        installed.iter().find_map(|rule| {
            match self.ledger.rules.get(&rule.id) {
                Some(record) if record.rule == *rule => {
                    (!record.done).then(|| (rule.clone(), record.cursor))
                }
                // An unrecorded rule or a different version starts from the beginning.
                _ => Some((rule.clone(), RetroCursor::default())),
            }
        })
    }

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

    /// Drops the scan state of rules that are not installed.
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

/// Free bytes on the filesystem holding `path`, or `None` when the host reports nothing; `None`
/// never reads as full.
fn host_free_bytes(path: &Path) -> Option<u64> {
    use sysinfo::Disks;

    let disks = Disks::new_with_refreshed_list();
    disks
        .iter()
        .filter(|disk| path.starts_with(disk.mount_point()))
        // The longest matching mount point is the filesystem the path is on.
        .max_by_key(|disk| disk.mount_point().as_os_str().len())
        .map(sysinfo::Disk::available_space)
}

/// The host's free space with its read time; a reading is reused until it ages out, and each new
/// reading is also handed to the store.
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

/// Spawns the retroactive-scan job, which returns at once when there is no local store to scan.
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
            // A rule history cannot answer is recorded as finished, so the poll skips it.
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
                // An empty store is logged as empty scope and recorded as finished without a scan.
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

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum AfterChunk {
    StandDown(Duration),
    Finished,
    /// The store holds no history for this rule; a scan that found nothing is a separate outcome.
    NothingToScan,
    Pause,
}

/// An outcome from a newer scan engine that this build does not understand pauses the scan.
fn after_chunk(step: RetroStep) -> AfterChunk {
    match step {
        RetroStep::Yielded { stand_down } => AfterChunk::StandDown(stand_down),
        RetroStep::Complete => AfterChunk::Finished,
        RetroStep::NoHistory => AfterChunk::NothingToScan,
        _ => AfterChunk::Pause,
    }
}

/// The installed ruleset; a poisoned lock reads as empty, which stops the running scan.
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
    use mesh_protocol::{AlertComparator, AlertSeverity, RulePredicate, ThresholdRule};
    use std::time::Duration;

    fn rule(id: &str, threshold: f64) -> ThresholdRule {
        ThresholdRule {
            id: id.to_string(),
            version: 1,
            severity: AlertSeverity::Warning,
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

    #[test]
    fn an_unscanned_rule_is_pending_from_the_start() {
        let dir = tempfile::tempdir().unwrap();
        let ledger = RetroLedger::load(dir.path());

        let (pending, cursor) = ledger.pending(&[rule("disk-critical", 90.0)]).unwrap();
        assert_eq!(pending.id, "disk-critical");
        assert_eq!(cursor, RetroCursor::default());
        assert_eq!(ledger.pending(&[]), None, "no rules, nothing to scan");
    }

    #[test]
    fn a_finished_scan_is_not_repeated_on_the_next_push() {
        let dir = tempfile::tempdir().unwrap();
        let mut ledger = RetroLedger::load(dir.path());
        let installed = vec![rule("disk-critical", 90.0)];

        ledger.record(&installed[0], RetroCursor::at(1_700_000_000), true);

        assert_eq!(ledger.pending(&installed), None);
    }

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

    #[test]
    fn an_uninstalled_rule_is_forgotten() {
        let dir = tempfile::tempdir().unwrap();
        let mut ledger = RetroLedger::load(dir.path());
        ledger.record(&rule("disk-critical", 90.0), RetroCursor::at(1_000), true);
        ledger.record(&rule("cpu-saturated", 95.0), RetroCursor::at(1_000), true);

        ledger.forget_uninstalled(&[rule("disk-critical", 90.0)]);

        assert_eq!(ledger.pending(&[rule("disk-critical", 90.0)]), None);
        assert!(ledger.pending(&[rule("cpu-saturated", 95.0)]).is_some());
    }

    #[test]
    fn a_damaged_ledger_starts_empty_rather_than_refusing_to_scan() {
        let dir = tempfile::tempdir().unwrap();
        std::fs::write(dir.path().join("retro-scans.json"), b"{not json").unwrap();

        let ledger = RetroLedger::load(dir.path());
        assert!(ledger.pending(&[rule("disk-critical", 90.0)]).is_some());
    }

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

    #[test]
    fn free_space_is_read_for_the_filesystem_the_data_dir_is_on() {
        let dir = tempfile::tempdir().unwrap();
        if let Some(free) = host_free_bytes(dir.path()) {
            assert!(free > 0, "a writable temp dir on a full filesystem");
        }
    }
}
