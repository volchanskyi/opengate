//! Measurement runner for the WS-14a bake-off (`cargo bench -p edge-tsdb`).
//!
//! Prints the acceptance-gate table the ADR records: bytes/sample, ingest
//! throughput, commit cost, range-query latency, and crash-recovery time for
//! each persistent substrate over one shared fixture corpus. It is a plain
//! `main` (harness = false) so it is excluded from coverage and CI test timing;
//! the gate *thresholds* are asserted in `tests/gates_test.rs`, which always
//! runs.

use std::time::Instant;

use edge_tsdb::append_only::AppendOnlyStore;
use edge_tsdb::baseline::BaselineStore;
use edge_tsdb::corpus::{Corpus, CorpusConfig};
use edge_tsdb::redb_store::RedbStore;
use edge_tsdb::substrate::{Durability, Substrate};

fn measure<S: Substrate>(name: &str, corpus: &Corpus, dir: &std::path::Path) {
    let mut store = S::open(dir).unwrap();

    let t0 = Instant::now();
    corpus.replay_into(&mut store).unwrap();
    let ingest = t0.elapsed();

    let t1 = Instant::now();
    store.commit(Durability::Full).unwrap();
    let commit = t1.elapsed();

    let size = store.size_on_disk().unwrap();
    let n = corpus.sample_count();
    let bps = size as f64 / n as f64;

    // Range query: pull one series' full window and time it.
    let t2 = Instant::now();
    let got = store.range(0, i64::MIN, i64::MAX).unwrap();
    let query = t2.elapsed();

    let ingest_rate = n as f64 / ingest.as_secs_f64();
    println!(
        "{name:<12} | {bps:>7.3} | {size:>9} | {ingest_us:>10.1} | {rate:>12.0} | {commit_ms:>9.2} | {query_us:>10.1} | {pts:>7}",
        bps = bps,
        size = size,
        ingest_us = ingest.as_secs_f64() * 1e6,
        rate = ingest_rate,
        commit_ms = commit.as_secs_f64() * 1e3,
        query_us = query.as_secs_f64() * 1e6,
        pts = got.len(),
    );
}

fn measure_recovery(corpus: &Corpus, dir: &std::path::Path) {
    {
        let mut store = AppendOnlyStore::open(dir).unwrap();
        corpus.replay_into(&mut store).unwrap();
        store.commit(Durability::Full).unwrap();
        corpus
            .replay_prefix_into(&mut store, corpus.sample_count() / 4)
            .unwrap();
    }
    edge_tsdb::fault::truncate_newest_segment(dir, 128).unwrap();

    let t = Instant::now();
    let store = AppendOnlyStore::open(dir).unwrap();
    let repair = t.elapsed();
    let report = store.integrity_report().unwrap();
    println!(
        "\nrecovery: open-after-kill = {:.1} µs, durable = {}, recoverable = {}, repaired_segments = {}, quarantined = {}",
        repair.as_secs_f64() * 1e6,
        report.durable_samples,
        report.recoverable_samples,
        report.repaired_segments,
        report.quarantined_chunks,
    );
}

fn main() {
    let corpus = Corpus::generate(CorpusConfig {
        seed: 0xB00B_5EED,
        series: 40,
        duration_secs: 3_600,
        ..CorpusConfig::default()
    });
    println!(
        "WS-14a bake-off — corpus: {} series x {} s = {} samples\n",
        40,
        3_600,
        corpus.sample_count()
    );
    println!(
        "{:<12} | {:>7} | {:>9} | {:>10} | {:>12} | {:>9} | {:>10} | {:>7}",
        "substrate", "B/samp", "bytes", "ingest_us", "samples/s", "commit_ms", "query_us", "pts"
    );
    println!("{}", "-".repeat(96));

    let da = tempfile::tempdir().unwrap();
    measure::<AppendOnlyStore>("A:append", &corpus, da.path());
    let db = tempfile::tempdir().unwrap();
    measure::<RedbStore>("B:redb", &corpus, db.path());
    let dc = tempfile::tempdir().unwrap();
    measure::<BaselineStore>("C:baseline", &corpus, dc.path());

    let dr = tempfile::tempdir().unwrap();
    measure_recovery(&corpus, dr.path());
}
