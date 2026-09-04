//! What the shipped agent is allowed to contain.
//!
//! `edge-tsdb` carries two things: the on-device store the agent runs, and the
//! bake-off substrates it was chosen against — append-only files, two redb
//! references, a no-persist control, and a fault-injection harness that corrupts
//! and truncates a store on purpose. The second set is kept as the measured
//! off-ramp reference and the bench corpus, behind the `bakeoff` feature.
//!
//! `bakeoff` is on by default, so the whole arrangement rests on every consumer
//! remembering `default-features = false`. Nothing was checking that, and a
//! single forgetful dependency would put a fault-injection harness on a
//! customer's machine — feature flags unify across the normal dependency graph,
//! so it would not even have to be a direct one.
//!
//! The mutation run rests on the same thing from the other side: those modules
//! are carved out of it because no customer runs them, and the carve-out is only
//! honest while the feature stays off.

use std::fs;
use std::path::{Path, PathBuf};

/// The modules kept behind `bakeoff`, and out of the mutation scope with them.
const BAKEOFF_MODULES: &[&str] = &[
    "append_only",
    "baseline",
    "crc",
    "fault",
    "frame",
    "redb_backend",
    "redb_compact",
    "redb_store",
    "substrate",
];

/// The agent workspace root, from this crate's directory.
fn workspace_root() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR"))
        .join("..")
        .join("..")
        .canonicalize()
        .expect("the agent workspace is two levels above this crate")
}

#[test]
fn every_bakeoff_module_is_behind_the_bakeoff_feature() {
    let lib = fs::read_to_string(workspace_root().join("crates/edge-tsdb/src/lib.rs"))
        .expect("edge-tsdb's crate root is readable");

    for module in BAKEOFF_MODULES {
        let declaration = format!("pub mod {module};");
        let at = lib.find(&declaration).unwrap_or_else(|| {
            panic!("edge-tsdb no longer declares `{declaration}` — update BAKEOFF_MODULES")
        });
        let preceding = lib[..at].trim_end();
        assert!(
            preceding.ends_with("#[cfg(feature = \"bakeoff\")]"),
            "edge-tsdb::{module} is not behind the bakeoff feature, so it ships to every \
             machine and is mutated as if it did not"
        );
    }
}

#[test]
fn no_crate_in_the_workspace_turns_the_bakeoff_feature_on() {
    let root = workspace_root();
    let mut checked = 0;

    for manifest in workspace_manifests(&root) {
        let text = fs::read_to_string(&manifest).expect("a workspace manifest is readable");
        let shown = manifest.strip_prefix(&root).unwrap_or(&manifest).display();

        assert!(
            !text.contains("[dependencies.edge-tsdb]"),
            "{shown} declares edge-tsdb as its own table; declare it inline so the \
             default-features setting is readable on one line"
        );

        for line in text.lines() {
            let line = line.trim();
            if !line.starts_with("edge-tsdb") || !line.contains('{') {
                continue;
            }
            checked += 1;
            assert!(
                line.contains("default-features = false"),
                "{shown} depends on edge-tsdb with its default features, which turns the \
                 bakeoff feature on. Feature flags unify across the normal dependency graph, \
                 so this puts the fault-injection harness and the bake-off substrates in the \
                 shipped agent. Add `default-features = false`."
            );
        }
    }

    assert!(
        checked >= 2,
        "found only {checked} edge-tsdb dependency declarations — the manifest scan has \
         drifted, not the workspace"
    );
}

/// Every Cargo manifest in the agent workspace.
fn workspace_manifests(root: &Path) -> Vec<PathBuf> {
    let mut out = vec![root.join("Cargo.toml")];
    let crates = fs::read_dir(root.join("crates")).expect("the crates directory is readable");
    for entry in crates {
        let path = entry.expect("a crates entry is readable").path();
        let manifest = path.join("Cargo.toml");
        if manifest.is_file() {
            out.push(manifest);
        }
    }
    let fuzz = root.join("fuzz/Cargo.toml");
    if fuzz.is_file() {
        out.push(fuzz);
    }
    out
}
