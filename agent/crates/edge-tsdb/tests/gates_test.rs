#![cfg(feature = "bakeoff")]

use edge_tsdb::corpus::{Corpus, CorpusConfig};
use edge_tsdb::fault;
use edge_tsdb::sample::Sample;
use edge_tsdb::substrate::{Durability, Substrate};
use edge_tsdb::{append_only::AppendOnlyStore, baseline::BaselineStore, redb_store::RedbStore};

fn small_corpus() -> Corpus {
    Corpus::generate(CorpusConfig {
        seed: 0xED9E_5E77,
        series: 8,
        duration_secs: 3_600,
        ..CorpusConfig::default()
    })
}

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

    assert!(
        bps_b > bps_a * 1.8,
        "redb write-amp gap collapsed: A={bps_a:.3} B={bps_b:.3}"
    );
}

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
    assert!(reopened.total_samples().unwrap() > 0);
}

#[test]
fn append_only_caps_footprint_under_disk_pressure() {
    let corpus = small_corpus();
    let dir = tempfile::tempdir().unwrap();
    let mut a = AppendOnlyStore::open(dir.path()).unwrap();
    a.set_byte_cap(64 * 1024);
    let outcome = corpus.replay_into(&mut a);
    assert!(
        outcome.is_ok() || matches!(outcome, Err(edge_tsdb::TsdbError::CapacityExceeded { .. }))
    );
    assert!(a.size_on_disk().unwrap() <= 96 * 1024, "cap breached");
}

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

#[test]
fn baseline_is_volatile_by_design() {
    let corpus = small_corpus();
    let mut c = BaselineStore::open(std::path::Path::new(".")).unwrap();
    corpus.replay_into(&mut c).unwrap();
    assert!(c.total_samples().unwrap() > 0);
    let fresh = BaselineStore::open(std::path::Path::new(".")).unwrap();
    assert_eq!(fresh.total_samples().unwrap(), 0);
}
