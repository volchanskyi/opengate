//! A machine's identity over its life on disk: the install note, the `removed` marker, the start-up
//! decision, what a server verdict leads to, and self-removal that finishes after an interruption.

use std::io::Write;
use std::path::{Path, PathBuf};
use std::time::Duration;

use anyhow::{bail, Context, Result};
use serde::Deserialize;
use tracing::{debug, info, warn};

use crate::client_tls;
use crate::enrollment::{self, EnrollError};
use mesh_agent_core::AgentIdentity;

/// The install note's file name in the data folder.
pub(crate) const NOTE_FILE: &str = "install-note.json";
/// The marker that self-removal has begun.
pub(crate) const REMOVED_MARKER: &str = "removed";
/// The update-signing key enrollment saves.
pub(crate) const UPDATE_SIGNING_KEY_FILE: &str = "update-signing-key.hex";
/// The systemd unit the installer creates.
const SERVICE_NAME: &str = "mesh-agent";

/// The one-time note the installer leaves: where to enroll, with what, and which master key the
/// server announced.
#[derive(Debug, Clone, Deserialize)]
pub(crate) struct InstallNote {
    pub(crate) token: String,
    pub(crate) url: String,
    pub(crate) ca_pem: String,
    #[serde(default)]
    pub(crate) fresh: bool,
}

/// The agent's data folder and the file holding the master keys it trusts.
pub(crate) struct Paths {
    pub(crate) data_dir: PathBuf,
    pub(crate) server_ca: PathBuf,
}

impl Paths {
    pub(crate) fn note(&self) -> PathBuf {
        self.data_dir.join(NOTE_FILE)
    }

    fn marker(&self) -> PathBuf {
        self.data_dir.join(REMOVED_MARKER)
    }

    /// Every file a crash-safe save may stage, so recovery finds an unmarked one.
    pub(crate) fn staged_targets(&self) -> Vec<PathBuf> {
        let mut targets = AgentIdentity::files(&self.data_dir).to_vec();
        targets.push(self.server_ca.clone());
        targets.push(self.data_dir.join(UPDATE_SIGNING_KEY_FILE));
        targets
    }

    /// The install note, when one is present and readable.
    pub(crate) fn read_note(&self) -> Option<InstallNote> {
        let bytes = std::fs::read(self.note()).ok()?;
        match serde_json::from_slice(&bytes) {
            Ok(note) => Some(note),
            Err(e) => {
                warn!(error = %e, "the install note is unreadable and is left in place");
                None
            }
        }
    }

    /// Deletes a note the server has made unnecessary.
    pub(crate) fn consume_note(&self) {
        match std::fs::remove_file(self.note()) {
            Ok(()) => info!("install note no longer needed, deleted"),
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => {}
            Err(e) => warn!(error = %e, "could not delete the install note"),
        }
    }

    /// Whether the note names a server whose master keys share nothing with this identity's.
    fn note_is_foreign(&self, note: &InstallNote) -> bool {
        let trusted = std::fs::read_to_string(&self.server_ca).unwrap_or_default();
        let cert =
            std::fs::read(self.data_dir.join(mesh_agent_core::CERT_FILE)).unwrap_or_default();
        !client_tls::bundles_overlap(&trusted, &note.ca_pem)
            && !client_tls::issued_by_bundle(&cert, &note.ca_pem)
    }
}

/// What the agent does at start-up, from what it finds on disk.
#[derive(Debug, PartialEq, Eq)]
pub(crate) enum Start {
    /// A removal was under way and nothing asks to stop it.
    FinishRemoval,
    /// Enroll with the note, first wiping whatever the data folder holds when `wipe` is set.
    Enroll { wipe: bool, why: &'static str },
    /// Connect with the identity on disk.
    Load,
    /// Nothing to connect as and nothing to enroll with.
    Stranded,
}

/// The start-up rows: a note beats a `removed` marker; a fresh note or a foreign server starts
/// over; a missing identity enrolls; any other identity connects as itself.
pub(crate) fn decide_start(
    marker: bool,
    note: Option<&InstallNote>,
    identity: bool,
    foreign: bool,
) -> Start {
    match (marker, note, identity) {
        (true, None, _) => Start::FinishRemoval,
        (_, Some(note), _) if note.fresh => Start::Enroll {
            wipe: true,
            why: "the install asked for a fresh identity",
        },
        (_, Some(_), false) => Start::Enroll {
            wipe: false,
            why: "no identity yet",
        },
        (_, Some(_), true) if foreign => Start::Enroll {
            wipe: true,
            why: "the install targets a different server",
        },
        (_, _, true) => Start::Load,
        (_, None, false) => Start::Stranded,
    }
}

/// A server's verdict on the identity, read from how the connection ended.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(crate) enum Verdict {
    /// The device was deleted: an `AgentDeregistered` frame or close code 3.
    Deleted,
    /// The certificate was refused: close code 5 or a TLS alert.
    CertificateRefused,
    /// None of the trusted master keys signed the server's chain.
    ChainRefused,
}

/// What a verdict leads to: a note always means starting over with it.
#[derive(Debug)]
pub(crate) enum Next {
    StartOver(InstallNote),
    Remove,
    Retry,
}

pub(crate) fn decide_after(verdict: Verdict, note: Option<InstallNote>) -> Next {
    match (verdict, note) {
        (_, Some(note)) => Next::StartOver(note),
        (Verdict::Deleted, None) => Next::Remove,
        (_, None) => Next::Retry,
    }
}

/// What start-up hands to the connection loop.
pub(crate) enum StartUp {
    Remove,
    Connect(AgentIdentity),
}

/// Applies the start-up decision, enrolling where it says to.
pub(crate) async fn start_up(paths: &Paths) -> Result<StartUp> {
    let note = paths.read_note();
    let marker = paths.marker().exists();
    if marker && note.is_some() {
        info!("an install note arrived over a half-finished removal, which is abandoned");
        tokio::fs::remove_file(paths.marker())
            .await
            .context("delete the removed marker")?;
    }
    let identity = AgentIdentity::exists(&paths.data_dir);
    let foreign = note
        .as_ref()
        .is_some_and(|n| identity && paths.note_is_foreign(n));
    match decide_start(marker, note.as_ref(), identity, foreign) {
        Start::FinishRemoval => Ok(StartUp::Remove),
        Start::Enroll { wipe, why } => {
            let note = note.context("enrollment without a note")?;
            info!(why, "enrolling with the install note");
            if wipe {
                wipe_data_folder(paths, &[NOTE_FILE])?;
            }
            Ok(StartUp::Connect(enroll_until_answered(paths, &note).await?))
        }
        Start::Load => Ok(StartUp::Connect(
            AgentIdentity::load(&paths.data_dir).context("load agent identity")?,
        )),
        Start::Stranded => bail!(
            "no agent identity at {} and no install note; reinstall with a join code",
            paths.data_dir.display()
        ),
    }
}

/// Wipes the data folder, keeping the note, and enrolls with it; the caller restarts the agent so
/// nothing of the old device stays open.
pub(crate) async fn start_over(paths: &Paths, note: &InstallNote) -> Result<AgentIdentity> {
    wipe_data_folder(paths, &[NOTE_FILE])?;
    enroll_until_answered(paths, note).await
}

/// Enrolls, waiting out an unreachable server; a refused token or an unusable reply ends the
/// agent with the reason, keeping the note.
async fn enroll_until_answered(paths: &Paths, note: &InstallNote) -> Result<AgentIdentity> {
    let mut rng = rand::rng();
    let mut attempt = 0u32;
    loop {
        match enrollment::enroll(note, paths).await {
            Ok(identity) => return Ok(identity),
            Err(e @ EnrollError::Unreachable(_)) => {
                let delay = mesh_agent_core::full_jitter(
                    Duration::from_secs(1),
                    Duration::from_secs(30),
                    attempt,
                    &mut rng,
                );
                warn!(error = %e, delay_ms = delay.as_millis(), "enrollment will be retried");
                tokio::time::sleep(delay).await;
                attempt = attempt.saturating_add(1);
            }
            Err(e) => bail!("{e}"),
        }
    }
}

/// Removes everything in the data folder whose name is not in `keep`.
fn wipe_data_folder(paths: &Paths, keep: &[&str]) -> Result<()> {
    let entries = match std::fs::read_dir(&paths.data_dir) {
        Ok(entries) => entries,
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Ok(()),
        Err(e) => return Err(e).context("read the data folder"),
    };
    for entry in entries {
        let entry = entry.context("read the data folder")?;
        if keep.iter().any(|k| entry.file_name() == *k) {
            continue;
        }
        let path = entry.path();
        let removed = if entry.file_type().is_ok_and(|t| t.is_dir()) {
            std::fs::remove_dir_all(&path)
        } else {
            std::fs::remove_file(&path)
        };
        removed.with_context(|| format!("remove {}", path.display()))?;
    }
    Ok(())
}

/// Removes the agent from the machine in an order that resumes after any interruption: marker,
/// wipe, unit, binary, folders. It never stops its own service, and exits 0 so nothing restarts it.
pub(crate) fn self_remove(paths: &Paths) -> ! {
    info!("removing this agent from the machine");
    if let Err(e) = write_marker(paths) {
        warn!(error = %e, "could not write the removed marker");
    }
    if let Err(e) = wipe_data_folder(paths, &[REMOVED_MARKER]) {
        warn!(error = %e, "could not wipe the data folder");
    }
    remove_file_logged(&paths.server_ca);
    remove_unit();
    remove_binary();
    remove_file_logged(&paths.marker());
    remove_dir_logged(&paths.data_dir);
    if let Some(trust) = paths.server_ca.parent() {
        remove_dir_logged(trust);
    }
    info!("agent fully uninstalled, exiting");
    std::process::exit(0)
}

fn write_marker(paths: &Paths) -> Result<()> {
    std::fs::create_dir_all(&paths.data_dir)?;
    let mut file = std::fs::File::create(paths.marker())?;
    file.flush()?;
    file.sync_all()?;
    std::fs::File::open(&paths.data_dir)?.sync_all()?;
    Ok(())
}

/// Disables the unit, deletes the file systemd loaded it from, and reloads systemd.
fn remove_unit() {
    systemctl(&["disable", SERVICE_NAME]);
    let unit = systemctl(&["show", "-p", "FragmentPath", "--value", SERVICE_NAME]);
    let unit = PathBuf::from(unit.trim());
    let expected = format!("{SERVICE_NAME}.service");
    if unit.file_name().is_some_and(|n| *n == *expected) {
        remove_file_logged(&unit);
    }
    systemctl(&["daemon-reload"]);
}

/// Runs `systemctl` and returns its output; a host without it logs and carries on.
fn systemctl(args: &[&str]) -> String {
    match std::process::Command::new("systemctl").args(args).output() {
        Ok(out) => {
            if !out.status.success() {
                debug!(?args, stderr = %String::from_utf8_lossy(&out.stderr), "systemctl failed");
            }
            String::from_utf8_lossy(&out.stdout).into_owned()
        }
        Err(e) => {
            debug!(?args, error = %e, "systemctl unavailable");
            String::new()
        }
    }
}

/// Deletes the running binary and the copy an update keeps for rollback.
fn remove_binary() {
    let Ok(exe) = std::env::current_exe() else {
        return;
    };
    let exe = PathBuf::from(exe.to_string_lossy().trim_end_matches(" (deleted)"));
    remove_file_logged(&exe);
    let mut prev = exe.into_os_string();
    prev.push(".prev");
    remove_file_logged(Path::new(&prev));
}

fn remove_file_logged(path: &Path) {
    match std::fs::remove_file(path) {
        Ok(()) => {}
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => {}
        Err(e) => warn!(file = %path.display(), error = %e, "could not remove file"),
    }
}

/// Removes a folder that is empty; a folder still holding something is left.
fn remove_dir_logged(path: &Path) {
    match std::fs::remove_dir(path) {
        Ok(()) => {}
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => {}
        Err(e) => warn!(dir = %path.display(), error = %e, "could not remove folder"),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn note(fresh: bool) -> InstallNote {
        InstallNote {
            token: "t".into(),
            url: "https://example.test".into(),
            ca_pem: String::new(),
            fresh,
        }
    }

    #[test]
    fn the_shared_fixture_is_a_note_the_agent_reads() {
        let text = include_str!("../tests/fixtures/install-note.json");
        let note: InstallNote = serde_json::from_str(text).unwrap();
        assert_eq!(note.token, "enroll-token-do-not-log");
        assert_eq!(note.url, "https://control.example.test");
        assert!(note.ca_pem.starts_with("-----BEGIN CERTIFICATE-----\n"));
        assert!(!note.fresh);
    }

    #[test]
    fn start_up_rows() {
        let plain = note(false);
        let fresh = note(true);
        let rows: [(bool, Option<&InstallNote>, bool, bool, Start); 8] = [
            (true, None, true, false, Start::FinishRemoval),
            (true, None, false, false, Start::FinishRemoval),
            (
                true,
                Some(&plain),
                false,
                false,
                Start::Enroll {
                    wipe: false,
                    why: "no identity yet",
                },
            ),
            (
                false,
                Some(&plain),
                false,
                false,
                Start::Enroll {
                    wipe: false,
                    why: "no identity yet",
                },
            ),
            (false, Some(&plain), true, false, Start::Load),
            (
                false,
                Some(&plain),
                true,
                true,
                Start::Enroll {
                    wipe: true,
                    why: "the install targets a different server",
                },
            ),
            (
                false,
                Some(&fresh),
                true,
                false,
                Start::Enroll {
                    wipe: true,
                    why: "the install asked for a fresh identity",
                },
            ),
            (false, None, false, false, Start::Stranded),
        ];
        for (marker, note, identity, foreign, expected) in rows {
            assert_eq!(
                decide_start(marker, note, identity, foreign),
                expected,
                "marker={marker} note={} identity={identity} foreign={foreign}",
                note.is_some()
            );
        }
    }

    #[test]
    fn a_note_turns_every_verdict_into_starting_over() {
        for verdict in [
            Verdict::Deleted,
            Verdict::CertificateRefused,
            Verdict::ChainRefused,
        ] {
            assert!(matches!(
                decide_after(verdict, Some(note(false))),
                Next::StartOver(_)
            ));
        }
        assert!(matches!(decide_after(Verdict::Deleted, None), Next::Remove));
        assert!(matches!(
            decide_after(Verdict::CertificateRefused, None),
            Next::Retry
        ));
        assert!(matches!(
            decide_after(Verdict::ChainRefused, None),
            Next::Retry
        ));
    }

    #[test]
    fn a_wipe_keeps_only_what_it_is_told_to() {
        let dir = tempfile::tempdir().unwrap();
        let paths = Paths {
            data_dir: dir.path().to_path_buf(),
            server_ca: dir.path().join("ca.pem"),
        };
        std::fs::create_dir_all(dir.path().join("edge-tsdb")).unwrap();
        std::fs::write(dir.path().join("edge-tsdb/store"), b"x").unwrap();
        std::fs::write(dir.path().join(NOTE_FILE), b"{}").unwrap();
        std::fs::write(dir.path().join("agent.key"), b"k").unwrap();

        wipe_data_folder(&paths, &[NOTE_FILE]).unwrap();

        let left: Vec<_> = std::fs::read_dir(dir.path())
            .unwrap()
            .map(|e| e.unwrap().file_name())
            .collect();
        assert_eq!(left, vec![std::ffi::OsString::from(NOTE_FILE)]);
    }
}
