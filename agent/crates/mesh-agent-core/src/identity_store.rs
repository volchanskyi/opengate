//! Crash-safe saving of a machine's identity files: staged copies, then a marker naming each new
//! file's hash as the commit point, then renames; recovery yields exactly the old or the new set.

use std::io::Write;
use std::path::{Path, PathBuf};

use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};

use crate::error::AgentError;

/// The commit marker's file name inside the data directory.
pub const COMMIT_MARKER: &str = "identity.commit";

/// Suffix of a staged file beside its target.
const STAGED_SUFFIX: &str = "next";

/// Mode of a staged file holding a private key.
#[cfg(unix)]
const PRIVATE_MODE: u32 = 0o600;

/// One file a save writes.
struct StagedWrite {
    path: PathBuf,
    contents: Vec<u8>,
    private: bool,
}

/// The files one save writes and removes, applied all together or not at all.
#[derive(Default)]
pub struct Commit {
    writes: Vec<StagedWrite>,
    removes: Vec<PathBuf>,
}

impl Commit {
    /// An empty save.
    pub fn new() -> Self {
        Self::default()
    }

    /// Adds a file readable by its owner and group.
    pub fn write(mut self, path: impl Into<PathBuf>, contents: impl Into<Vec<u8>>) -> Self {
        self.writes.push(StagedWrite {
            path: path.into(),
            contents: contents.into(),
            private: false,
        });
        self
    }

    /// Adds a file readable only by its owner, such as a private key.
    pub fn write_private(mut self, path: impl Into<PathBuf>, contents: impl Into<Vec<u8>>) -> Self {
        self.writes.push(StagedWrite {
            path: path.into(),
            contents: contents.into(),
            private: true,
        });
        self
    }

    /// Adds a file the save deletes, such as a consumed install note.
    pub fn remove(mut self, path: impl Into<PathBuf>) -> Self {
        self.removes.push(path.into());
        self
    }
}

/// What the marker records: each target with the hash of its new contents, and each removal.
#[derive(Serialize, Deserialize)]
struct Marker {
    writes: Vec<MarkedWrite>,
    removes: Vec<PathBuf>,
}

#[derive(Serialize, Deserialize)]
struct MarkedWrite {
    path: PathBuf,
    sha256: String,
}

/// What recovery found on disk.
#[derive(Debug, PartialEq, Eq)]
#[non_exhaustive]
pub enum Recovery {
    /// No save was in progress.
    Clean,
    /// A save had not reached its commit point and was undone.
    RolledBack,
    /// A save had passed its commit point and was finished.
    RolledForward,
}

/// The points a save passes; a test stops a save at one to stand in for a crash there.
#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub(crate) enum Step {
    /// The first staged file is written and the rest are not.
    FirstStaged,
    /// Every staged file is written and flushed, and no marker exists.
    Staged,
    /// The marker is written.
    Marked,
    /// The first staged file is renamed into place and the rest are not.
    FirstRenamed,
    /// Every staged file is renamed into place and the removals are not done.
    Renamed,
}

/// Saves `commit` so that a crash anywhere leaves either the old files or the new ones.
pub fn commit(data_dir: &Path, commit: Commit) -> Result<(), AgentError> {
    commit_until(data_dir, commit, None)
}

/// Runs a save up to `stop`, or to the end when `stop` is `None`.
pub(crate) fn commit_until(
    data_dir: &Path,
    commit: Commit,
    stop: Option<Step>,
) -> Result<(), AgentError> {
    for (i, write) in commit.writes.iter().enumerate() {
        write_synced(&staged_path(&write.path), &write.contents, write.private)?;
        if i == 0 && stop == Some(Step::FirstStaged) {
            return Ok(());
        }
    }
    sync_parents(commit.writes.iter().map(|w| w.path.as_path()))?;
    if stop == Some(Step::Staged) {
        return Ok(());
    }

    let marker = Marker {
        writes: commit
            .writes
            .iter()
            .map(|w| MarkedWrite {
                path: w.path.clone(),
                sha256: hex::encode(Sha256::digest(&w.contents)),
            })
            .collect(),
        removes: commit.removes,
    };
    let encoded = serde_json::to_vec(&marker)
        .map_err(|e| AgentError::Io(std::io::Error::other(format!("encode marker: {e}"))))?;
    let marker_path = data_dir.join(COMMIT_MARKER);
    write_synced(&staged_path(&marker_path), &encoded, true)?;
    std::fs::rename(staged_path(&marker_path), &marker_path)?;
    sync_dir(data_dir)?;
    if stop == Some(Step::Marked) {
        return Ok(());
    }

    finish(data_dir, &marker, stop)
}

/// Completes a marked save: renames every staged file into place, performs the removals, and
/// deletes the marker. Every step tolerates having already happened.
fn finish(data_dir: &Path, marker: &Marker, stop: Option<Step>) -> Result<(), AgentError> {
    for (i, write) in marker.writes.iter().enumerate() {
        let staged = staged_path(&write.path);
        if staged.exists() {
            std::fs::rename(&staged, &write.path)?;
        }
        if i == 0 && stop == Some(Step::FirstRenamed) {
            sync_parents(marker.writes.iter().map(|w| w.path.as_path()))?;
            return Ok(());
        }
    }
    sync_parents(marker.writes.iter().map(|w| w.path.as_path()))?;
    if stop == Some(Step::Renamed) {
        return Ok(());
    }

    for path in &marker.removes {
        remove_if_present(path)?;
    }
    sync_parents(marker.removes.iter().map(PathBuf::as_path))?;
    remove_if_present(&data_dir.join(COMMIT_MARKER))?;
    sync_dir(data_dir)
}

/// Brings an interrupted save forward when its marker exists and every new file is on disk, back
/// otherwise; `staged_targets` names the files a save may have staged.
pub fn recover(data_dir: &Path, staged_targets: &[PathBuf]) -> Result<Recovery, AgentError> {
    let marker_path = data_dir.join(COMMIT_MARKER);
    remove_if_present(&staged_path(&marker_path))?;

    let marker = match std::fs::read(&marker_path) {
        Ok(bytes) => serde_json::from_slice::<Marker>(&bytes).ok(),
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => None,
        Err(e) => return Err(e.into()),
    };

    let Some(marker) = marker else {
        let mut found = false;
        for target in staged_targets {
            found |= remove_if_present(&staged_path(target))?;
        }
        found |= remove_if_present(&marker_path)?;
        return Ok(if found {
            Recovery::RolledBack
        } else {
            Recovery::Clean
        });
    };

    if marker.writes.iter().all(holds_new_contents) {
        finish(data_dir, &marker, None)?;
        return Ok(Recovery::RolledForward);
    }

    for write in &marker.writes {
        remove_if_present(&staged_path(&write.path))?;
    }
    remove_if_present(&marker_path)?;
    sync_dir(data_dir)?;
    Ok(Recovery::RolledBack)
}

/// Whether a marked target's new contents are on disk, staged or already renamed into place.
fn holds_new_contents(write: &MarkedWrite) -> bool {
    [staged_path(&write.path), write.path.clone()]
        .iter()
        .any(|path| match std::fs::read(path) {
            Ok(bytes) => hex::encode(Sha256::digest(&bytes)) == write.sha256,
            Err(_) => false,
        })
}

fn staged_path(path: &Path) -> PathBuf {
    let mut name = path.file_name().unwrap_or_default().to_os_string();
    name.push(".");
    name.push(STAGED_SUFFIX);
    path.with_file_name(name)
}

/// Writes `contents` to `path` and flushes it to disk; `private` files are created owner-only.
fn write_synced(path: &Path, contents: &[u8], private: bool) -> Result<(), AgentError> {
    let mut options = std::fs::OpenOptions::new();
    options.write(true).create(true).truncate(true);
    #[cfg(unix)]
    if private {
        use std::os::unix::fs::OpenOptionsExt;
        options.mode(PRIVATE_MODE);
    }
    let mut file = options.open(path)?;
    #[cfg(unix)]
    if private {
        use std::os::unix::fs::PermissionsExt;
        file.set_permissions(std::fs::Permissions::from_mode(PRIVATE_MODE))?;
    }
    file.write_all(contents)?;
    file.sync_all()?;
    Ok(())
}

fn remove_if_present(path: &Path) -> Result<bool, AgentError> {
    match std::fs::remove_file(path) {
        Ok(()) => Ok(true),
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(false),
        Err(e) => Err(e.into()),
    }
}

fn sync_dir(dir: &Path) -> Result<(), AgentError> {
    std::fs::File::open(dir)?.sync_all()?;
    Ok(())
}

/// Flushes each distinct directory holding one of `paths`.
fn sync_parents<'a>(paths: impl Iterator<Item = &'a Path>) -> Result<(), AgentError> {
    let mut seen: Vec<&Path> = Vec::new();
    for parent in paths.filter_map(Path::parent) {
        if !seen.contains(&parent) && parent.exists() {
            sync_dir(parent)?;
            seen.push(parent);
        }
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    struct Pair {
        key: Vec<u8>,
        cert: Vec<u8>,
    }

    fn pair(cn: &str) -> Pair {
        let key = rcgen::KeyPair::generate_for(&rcgen::PKCS_ECDSA_P256_SHA256).unwrap();
        let mut params = rcgen::CertificateParams::new(Vec::<String>::new()).unwrap();
        params
            .distinguished_name
            .push(rcgen::DnType::CommonName, cn.to_string());
        let cert = params.self_signed(&key).unwrap();
        Pair {
            key: key.serialize_der(),
            cert: cert.der().to_vec(),
        }
    }

    struct Fixture {
        _dir: tempfile::TempDir,
        data: PathBuf,
        trust: PathBuf,
        old: Pair,
        new: Pair,
    }

    impl Fixture {
        fn new() -> Self {
            let dir = tempfile::tempdir().unwrap();
            let data = dir.path().join("data");
            let trust = dir.path().join("trust");
            std::fs::create_dir_all(&data).unwrap();
            std::fs::create_dir_all(&trust).unwrap();
            let old = pair("old");
            std::fs::write(data.join("agent.key"), &old.key).unwrap();
            std::fs::write(data.join("agent.crt"), &old.cert).unwrap();
            std::fs::write(trust.join("ca.pem"), b"old-bundle").unwrap();
            std::fs::write(data.join("install-note.json"), b"{}").unwrap();
            Self {
                _dir: dir,
                data,
                trust,
                old,
                new: pair("new"),
            }
        }

        fn save(&self) -> Commit {
            Commit::new()
                .write_private(self.data.join("agent.key"), self.new.key.clone())
                .write(self.data.join("agent.crt"), self.new.cert.clone())
                .write(self.trust.join("ca.pem"), b"new-bundle".to_vec())
                .remove(self.data.join("install-note.json"))
        }

        fn targets(&self) -> Vec<PathBuf> {
            vec![
                self.data.join("agent.key"),
                self.data.join("agent.crt"),
                self.trust.join("ca.pem"),
            ]
        }

        fn crash_at(&self, step: Step) -> Recovery {
            commit_until(&self.data, self.save(), Some(step)).unwrap();
            recover(&self.data, &self.targets()).unwrap()
        }

        fn on_disk(&self) -> (Vec<u8>, Vec<u8>, Vec<u8>, bool) {
            (
                std::fs::read(self.data.join("agent.key")).unwrap(),
                std::fs::read(self.data.join("agent.crt")).unwrap(),
                std::fs::read(self.trust.join("ca.pem")).unwrap(),
                self.data.join("install-note.json").exists(),
            )
        }

        fn assert_old(&self) {
            assert_eq!(
                self.on_disk(),
                (
                    self.old.key.clone(),
                    self.old.cert.clone(),
                    b"old-bundle".to_vec(),
                    true
                )
            );
        }

        fn assert_new(&self) {
            assert_eq!(
                self.on_disk(),
                (
                    self.new.key.clone(),
                    self.new.cert.clone(),
                    b"new-bundle".to_vec(),
                    false
                )
            );
        }

        fn assert_no_leftovers(&self) {
            for dir in [&self.data, &self.trust] {
                for entry in std::fs::read_dir(dir).unwrap() {
                    let name = entry.unwrap().file_name().to_string_lossy().into_owned();
                    assert!(
                        !name.ends_with(".next") && name != COMMIT_MARKER,
                        "{name} left behind in {}",
                        dir.display()
                    );
                }
            }
        }
    }

    #[test]
    fn commit_then_load_yields_new_pair() {
        let f = Fixture::new();
        commit(&f.data, f.save()).unwrap();
        f.assert_new();
        f.assert_no_leftovers();
        assert_eq!(recover(&f.data, &f.targets()).unwrap(), Recovery::Clean);
    }

    #[test]
    fn no_marker_rolls_back() {
        for step in [Step::FirstStaged, Step::Staged] {
            let f = Fixture::new();
            assert_eq!(f.crash_at(step), Recovery::RolledBack, "{step:?}");
            f.assert_old();
            f.assert_no_leftovers();
        }
    }

    #[test]
    fn marker_then_crash_before_renames_rolls_forward() {
        let f = Fixture::new();
        assert_eq!(f.crash_at(Step::Marked), Recovery::RolledForward);
        f.assert_new();
        f.assert_no_leftovers();
    }

    #[test]
    fn crash_between_key_and_cert_rename_rolls_forward_and_keys_match() {
        let f = Fixture::new();
        commit_until(&f.data, f.save(), Some(Step::FirstRenamed)).unwrap();
        assert_eq!(
            std::fs::read(f.data.join("agent.key")).unwrap(),
            f.new.key,
            "the crash point sits after the key's rename"
        );
        assert_eq!(std::fs::read(f.data.join("agent.crt")).unwrap(), f.old.cert);

        assert_eq!(
            recover(&f.data, &f.targets()).unwrap(),
            Recovery::RolledForward
        );
        f.assert_new();
    }

    #[test]
    fn crash_after_renames_is_idempotent() {
        let f = Fixture::new();
        assert_eq!(f.crash_at(Step::Renamed), Recovery::RolledForward);
        f.assert_new();
        assert_eq!(recover(&f.data, &f.targets()).unwrap(), Recovery::Clean);
        f.assert_new();
        f.assert_no_leftovers();
    }

    #[test]
    fn torn_next_file_hash_mismatch_rolls_back() {
        let f = Fixture::new();
        commit_until(&f.data, f.save(), Some(Step::Marked)).unwrap();
        let staged_cert = staged_path(&f.data.join("agent.crt"));
        std::fs::write(&staged_cert, &f.new.cert[..f.new.cert.len() / 2]).unwrap();

        assert_eq!(
            recover(&f.data, &f.targets()).unwrap(),
            Recovery::RolledBack
        );
        f.assert_old();
        f.assert_no_leftovers();
    }

    #[test]
    fn recovery_never_deletes_live_pair() {
        let steps = [
            Step::FirstStaged,
            Step::Staged,
            Step::Marked,
            Step::FirstRenamed,
            Step::Renamed,
        ];
        for step in steps {
            let f = Fixture::new();
            f.crash_at(step);
            let (key, cert, _, _) = f.on_disk();
            let matched = (key == f.old.key && cert == f.old.cert)
                || (key == f.new.key && cert == f.new.cert);
            assert!(
                matched,
                "after a crash at {step:?} the key and certificate disagree"
            );
        }
    }

    #[cfg(unix)]
    #[test]
    fn key_mode_0600_after_commit() {
        use std::os::unix::fs::PermissionsExt;
        let f = Fixture::new();
        commit(&f.data, f.save()).unwrap();
        let mode = std::fs::metadata(f.data.join("agent.key"))
            .unwrap()
            .permissions()
            .mode();
        assert_eq!(mode & 0o777, 0o600);
    }

    #[test]
    fn save_signed_cert_is_atomic() {
        let f = Fixture::new();
        let only_cert = Commit::new().write(f.data.join("agent.crt"), f.new.cert.clone());
        commit_until(&f.data, only_cert, Some(Step::FirstStaged)).unwrap();
        assert_eq!(
            std::fs::read(f.data.join("agent.crt")).unwrap(),
            f.old.cert,
            "the live certificate is untouched until the rename"
        );
        recover(&f.data, &f.targets()).unwrap();
        assert_eq!(std::fs::read(f.data.join("agent.crt")).unwrap(), f.old.cert);
    }
}
