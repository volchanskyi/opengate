//! Which cgroup this agent runs in — the one question two collectors both ask.
//!
//! Several kernel interfaces are host-wide, and an agent inside a container that
//! reads them attributes its neighbours' work to itself. That is the
//! silent-wrong-answer class the telemetry contract exists to remove, so the
//! answer to "am I containerized, and if so where is my cgroup" is resolved once,
//! here, and used by every collector that has to branch on it:
//! [`pressure`](super::pressure) reads its own cgroup's stall files, and
//! [`diskperf`](super::diskperf) refuses `/proc/diskstats` entirely.
//!
//! Every path resolves under an injectable root, so a container is an ordinary
//! fixture directory rather than an environment a test must run inside.
//! Production passes `/`.

use std::fs;
use std::path::{Path, PathBuf};

/// The directory of the agent's own cgroup under `root`, or `None` when the
/// agent is at the root cgroup, the hierarchy has no unified (`0::`) entry, or
/// `/proc/self/cgroup` cannot be read — all of which mean "read the host".
pub(crate) fn own_cgroup(root: &Path) -> Option<PathBuf> {
    let text = fs::read_to_string(root.join("proc/self/cgroup")).ok()?;
    let relative = text
        .lines()
        .find_map(|line| line.strip_prefix("0::"))?
        .trim()
        .trim_start_matches('/');
    // The kernel writes this path, but it is joined onto a filesystem path, so
    // it is screened before it becomes one: a parent-directory component would
    // walk the read outside the cgroup tree entirely.
    if relative.is_empty() || relative.contains("..") {
        return None;
    }
    Some(root.join("sys/fs/cgroup").join(relative))
}

/// Whether the agent runs inside a container — a non-root unified cgroup — as
/// seen from `root`.
pub(crate) fn in_container(root: &Path) -> bool {
    own_cgroup(root).is_some()
}

#[cfg(test)]
mod tests {
    use super::{in_container, own_cgroup};
    use std::fs;
    use std::path::Path;

    /// Write `contents` to `rel` under `root`, creating intermediate directories.
    fn put(root: &Path, rel: &str, contents: &str) {
        let path = root.join(rel);
        fs::create_dir_all(path.parent().expect("a fixture file has a parent"))
            .expect("create the fixture directory");
        fs::write(&path, contents).expect("write the fixture file");
    }

    /// A path escaping the cgroup tree is refused rather than followed, so a
    /// malformed entry can never point a reader at an arbitrary file. The screen
    /// runs on the text, before it becomes a path at all.
    #[test]
    fn a_cgroup_path_that_escapes_the_tree_is_refused() {
        let root = tempfile::tempdir().expect("a temp fixture root");

        for line in [
            "0::/../../etc\n",
            "0::/system.slice/../../../etc\n",
            "0::/..\n",
        ] {
            put(root.path(), "proc/self/cgroup", line);
            assert_eq!(own_cgroup(root.path()), None, "refused: {line:?}");
            assert!(!in_container(root.path()), "refused: {line:?}");
        }
    }

    /// The root cgroup is not a container, in either spelling the kernel uses.
    #[test]
    fn the_root_cgroup_is_not_a_container() {
        let root = tempfile::tempdir().expect("a temp fixture root");

        put(root.path(), "proc/self/cgroup", "0::/\n");
        assert_eq!(own_cgroup(root.path()), None);

        put(root.path(), "proc/self/cgroup", "0::\n");
        assert_eq!(own_cgroup(root.path()), None);
        assert!(!in_container(root.path()));
    }

    /// A unified entry below the root resolves to that cgroup's directory under
    /// the unified mount point, and is what "containerized" means here.
    #[test]
    fn a_nested_cgroup_resolves_under_the_unified_mount() {
        let root = tempfile::tempdir().expect("a temp fixture root");
        put(
            root.path(),
            "proc/self/cgroup",
            "0::/system.slice/opengate-agent.service\n",
        );

        assert_eq!(
            own_cgroup(root.path()),
            Some(
                root.path()
                    .join("sys/fs/cgroup/system.slice/opengate-agent.service")
            )
        );
        assert!(in_container(root.path()));
    }

    /// A cgroup v1 hierarchy has no unified line, and a kernel that publishes no
    /// `/proc/self/cgroup` at all resolves nothing — neither is a container.
    #[test]
    fn a_hierarchy_without_a_unified_entry_is_not_a_container() {
        let root = tempfile::tempdir().expect("a temp fixture root");
        put(
            root.path(),
            "proc/self/cgroup",
            "12:pids:/user.slice\n11:memory:/user.slice\n",
        );
        assert_eq!(own_cgroup(root.path()), None);

        fs::remove_file(root.path().join("proc/self/cgroup")).expect("remove the fixture");
        assert_eq!(own_cgroup(root.path()), None);
    }
}
