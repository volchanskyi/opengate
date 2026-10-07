//! Resolves the agent's own cgroup, so collectors avoid host-wide kernel interfaces in a container.
//! Every path resolves under an injectable root; production passes `/`.

use std::fs;
use std::path::{Path, PathBuf};

/// The agent's own cgroup directory under `root`; `None` at the root cgroup, with no unified
/// `0::` entry, or when `/proc/self/cgroup` is unreadable.
pub(crate) fn own_cgroup(root: &Path) -> Option<PathBuf> {
    let text = fs::read_to_string(root.join("proc/self/cgroup")).ok()?;
    let relative = text
        .lines()
        .find_map(|line| line.strip_prefix("0::"))?
        .trim()
        .trim_start_matches('/');
    // A parent-directory component would walk the joined path outside the cgroup tree.
    if relative.is_empty() || relative.contains("..") {
        return None;
    }
    Some(root.join("sys/fs/cgroup").join(relative))
}

/// Whether the agent runs in a container, meaning a non-root unified cgroup under `root`.
pub(crate) fn in_container(root: &Path) -> bool {
    own_cgroup(root).is_some()
}

#[cfg(test)]
mod tests {
    use super::{in_container, own_cgroup};
    use std::fs;
    use std::path::Path;

    fn put(root: &Path, rel: &str, contents: &str) {
        let path = root.join(rel);
        fs::create_dir_all(path.parent().expect("a fixture file has a parent"))
            .expect("create the fixture directory");
        fs::write(&path, contents).expect("write the fixture file");
    }

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

    #[test]
    fn the_root_cgroup_is_not_a_container() {
        let root = tempfile::tempdir().expect("a temp fixture root");

        put(root.path(), "proc/self/cgroup", "0::/\n");
        assert_eq!(own_cgroup(root.path()), None);

        put(root.path(), "proc/self/cgroup", "0::\n");
        assert_eq!(own_cgroup(root.path()), None);
        assert!(!in_container(root.path()));
    }

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
