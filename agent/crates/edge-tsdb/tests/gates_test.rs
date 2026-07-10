//! Acceptance-gate integration tests for the WS-14a local-TSDB spike.
//!
//! These tests double as the spike's measured evidence: each one drives a
//! substrate through the fixture corpus or the fault-injection harness and
//! asserts the acceptance gate as a hard threshold. They always run (no skips),
//! so a regression in any substrate's crash-safety, density, or recovery
//! behaviour fails the gauntlet rather than silently rotting the bake-off.

use edge_tsdb::corpus::{Corpus, CorpusConfig};
use edge_tsdb::fault;
use edge_tsdb::sample::Sample;
use edge_tsdb::substrate::{Durability, Substrate};
use edge_tsdb::{append_only::AppendOnlyStore, baseline::BaselineStore, redb_store::RedbStore};

/// A modest corpus that every gate test replays. Kept small enough to run fast
/// inside the workspace test job while still exercising multi-chunk framing.
fn small_corpus() -> Corpus {
    Corpus::generate(CorpusConfig {
        seed: 0xED9E_5E77,
        series: 8,
        duration_secs: 3_600,
        ..CorpusConfig::default()
    })
}

/// Every persistent substrate must round-trip the corpus exactly: what you
/// append is what a range query returns, across chunk boundaries.
#[test]
fn round_trip_is_lossless_for_all_persistent_substrates() {
    let corpus = small_corpus();

    let dir_a = tempfile::tempdir().unwrap();
    let mut a = AppendOnlyStore::open(dir_a.path()).unwrap();
    corpus.replay_into(&mut a).unwrap();
    a.commit(Durability::Full).unwrap();
    corpus.assert_readback(&a);

    let dir_b = tempfile::tempdir().unwrap();
    let mut b = RedbStore::open(dir_b.path()).unwrap();
    corpus.replay_into(&mut b).unwrap();
    b.commit(Durability::Full).unwrap();
    corpus.assert_readback(&b);
}

/// Density gate: measured at a scale where redb's ~1 MB fixed file floor is
/// amortised (redb over-allocates when nearly empty, so bytes/sample is only
/// meaningful once the store holds real data). Baselines: A ≈ 2.6 B/sample and
/// B(redb) ≈ 7.3 B/sample (raw would be 16 B/sample: an 8-byte ts + 8-byte
/// value). These are regression guards with headroom, not the aspirational
/// ~1 B target — a lossless f64 XOR codec over mixed gauges + monotonic
/// counters lands in the low single digits, and redb's COW B-tree adds ~2.8×
/// storage overhead over the bespoke append-only files. That ~2.8× write-amp
/// gap is the A-vs-KV decider recorded in ADR-051.
#[test]
fn bytes_per_sample_clears_the_bar() {
    let corpus = Corpus::generate(CorpusConfig {
        seed: 0xED9E_5E77,
        series: 40,
        duration_secs: 3_600,
        ..CorpusConfig::default()
    });

    let dir_a = tempfile::tempdir().unwrap();
    let mut a = AppendOnlyStore::open(dir_a.path()).unwrap();
    corpus.replay_into(&mut a).unwrap();
    a.commit(Durability::Full).unwrap();
    let bps_a = a.size_on_disk().unwrap() as f64 / corpus.sample_count() as f64;
    assert!(
        bps_a < 4.0,
        "append-only bytes/sample regressed: {bps_a:.3}"
    );

    let dir_b = tempfile::tempdir().unwrap();
    let mut b = RedbStore::open(dir_b.path()).unwrap();
    corpus.replay_into(&mut b).unwrap();
    b.commit(Durability::Full).unwrap();
    let bps_b = b.size_on_disk().unwrap() as f64 / corpus.sample_count() as f64;
    assert!(bps_b < 12.0, "redb bytes/sample regressed: {bps_b:.3}");

    // The bespoke substrate must stay materially denser than the KV store —
    // this is the write-amplification decider, asserted, not just measured.
    assert!(
        bps_b > bps_a * 1.8,
        "redb write-amp gap collapsed: A={bps_a:.3} B={bps_b:.3}"
    );
}

/// Crash gate: a `kill -9`-class truncation of the newest bytes must leave the
/// store openable, dropping only the torn tail — never panicking, never losing
/// a durably-committed prefix.
#[test]
fn append_only_recovers_from_torn_tail() {
    let corpus = small_corpus();
    let dir = tempfile::tempdir().unwrap();

    let committed = {
        let mut a = AppendOnlyStore::open(dir.path()).unwrap();
        let n = corpus
            .replay_prefix_into(&mut a, corpus.sample_count() / 2)
            .unwrap();
        a.commit(Durability::Full).unwrap();
        // Uncommitted tail that a crash will shear off.
        corpus
            .replay_suffix_into(&mut a, corpus.sample_count() / 2)
            .unwrap();
        n
    };

    fault::truncate_newest_segment(dir.path(), 37).unwrap();

    let reopened = AppendOnlyStore::open(dir.path()).unwrap();
    let recovered = reopened.total_samples().unwrap();
    assert!(
        recovered >= committed,
        "durably-committed prefix lost: recovered {recovered} < committed {committed}"
    );
}

/// Corruption gate: a mid-file bit flip must be quarantined (that chunk skipped)
/// and the store must keep serving the rest — it must never panic the agent.
#[test]
fn append_only_quarantines_a_flipped_chunk_without_panic() {
    let corpus = small_corpus();
    let dir = tempfile::tempdir().unwrap();
    {
        let mut a = AppendOnlyStore::open(dir.path()).unwrap();
        corpus.replay_into(&mut a).unwrap();
        a.commit(Durability::Full).unwrap();
    }

    fault::flip_byte_in_newest_segment(dir.path(), 0.5).unwrap();

    let reopened = AppendOnlyStore::open(dir.path()).unwrap();
    let report = reopened.integrity_report().unwrap();
    assert!(report.quarantined_chunks >= 1, "flip not detected");
    // Still usable: a range query over an untouched series returns data.
    assert!(reopened.total_samples().unwrap() > 0);
}

/// Disk-full gate: the store must cap its footprint and fail the offending
/// append gracefully rather than filling the host disk or corrupting state.
#[test]
fn append_only_caps_footprint_under_disk_pressure() {
    let corpus = small_corpus();
    let dir = tempfile::tempdir().unwrap();
    let mut a = AppendOnlyStore::open(dir.path()).unwrap();
    a.set_byte_cap(64 * 1024);
    let outcome = corpus.replay_into(&mut a);
    // Either it evicted to stay under cap, or it returned a graceful capacity
    // error — never a panic, never an unbounded file.
    assert!(
        outcome.is_ok() || matches!(outcome, Err(edge_tsdb::TsdbError::CapacityExceeded { .. }))
    );
    assert!(a.size_on_disk().unwrap() <= 96 * 1024, "cap breached");
}

/// Time gate: an NTP step (timestamps that jump backward and far forward) must
/// round-trip losslessly and read back in timestamp order — the Gorilla
/// delta-of-delta codec handles negative deltas and the store sorts on read, so
/// a clock correction never corrupts or misorders history.
#[test]
fn ntp_style_time_jumps_round_trip_in_order() {
    let jumpy = Corpus::jumpy_series();
    let dir = tempfile::tempdir().unwrap();
    let mut a = AppendOnlyStore::open(dir.path()).unwrap();
    for s in &jumpy {
        a.append(77, *s).unwrap();
    }
    a.commit(Durability::Full).unwrap();

    let got = a.range(77, i64::MIN, i64::MAX).unwrap();
    let mut want = jumpy.clone();
    want.sort_by_key(|s: &Sample| s.ts);
    assert_eq!(got, want);
}

/// Baseline (C): confirms the no-persist candidate loses history across reopen,
/// documenting exactly what persistence buys — the reason C is only a control.
#[test]
fn baseline_is_volatile_by_design() {
    let corpus = small_corpus();
    let mut c = BaselineStore::open(std::path::Path::new(".")).unwrap();
    corpus.replay_into(&mut c).unwrap();
    assert!(c.total_samples().unwrap() > 0);
    // A "reopen" is a fresh instance: nothing survives.
    let fresh = BaselineStore::open(std::path::Path::new(".")).unwrap();
    assert_eq!(fresh.total_samples().unwrap(), 0);
}
