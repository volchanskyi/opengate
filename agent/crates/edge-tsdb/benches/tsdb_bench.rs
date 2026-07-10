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
use edge_tsdb::compact::encode_compact;
use edge_tsdb::corpus::{Corpus, CorpusConfig};
use edge_tsdb::gorilla::encode_block;
use edge_tsdb::redb_compact::RedbCompactStore;
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
    let dbp = tempfile::tempdir().unwrap();
    measure::<RedbCompactStore>("B+:redb32", &corpus, dbp.path());
    let dc = tempfile::tempdir().unwrap();
    measure::<BaselineStore>("C:baseline", &corpus, dc.path());

    let dr = tempfile::tempdir().unwrap();
    measure_recovery(&corpus, dr.path());

    codec_bakeoff(&corpus);
    redb_scale_sweep();
}

/// Pure-encoding comparison: identical per-series streams through lossless f64
/// Gorilla vs the compact codec, with the float32 error the compact path costs.
fn codec_bakeoff(corpus: &Corpus) {
    let mut f64_bytes = 0usize;
    let mut compact_bytes = 0usize;
    let mut compact_anom_bytes = 0usize;
    let mut n = 0usize;
    let mut max_abs = 0.0f64;
    let mut max_rel = 0.0f64;
    let mut lossless_series = 0usize;

    for series in corpus.series() {
        let no_anom = vec![false; series.len()];
        // ~1% anomalous samples to price the inline bit realistically.
        let anom: Vec<bool> = (0..series.len()).map(|i| i % 97 == 0).collect();
        f64_bytes += encode_block(series).len();
        compact_bytes += encode_compact(series, &no_anom, 1).len();
        compact_anom_bytes += encode_compact(series, &anom, 1).len();
        n += series.len();

        let (decoded, _) =
            edge_tsdb::compact::decode_compact(&encode_compact(series, &no_anom, 1)).unwrap();
        let mut exact = true;
        for (d, o) in decoded.iter().zip(series) {
            let abs = (d.value - o.value).abs();
            let rel = abs / o.value.abs().max(1.0);
            max_abs = max_abs.max(abs);
            max_rel = max_rel.max(rel);
            if d.value.to_bits() != o.value.to_bits() {
                exact = false;
            }
        }
        if exact {
            lossless_series += 1;
        }
    }

    let f64_bps = f64_bytes as f64 / n as f64;
    let compact_bps = compact_bytes as f64 / n as f64;
    let anom_overhead = (compact_anom_bytes - compact_bytes) as f64 / n as f64;
    println!("\n== codec bake-off (pure encoding, {n} samples) ==");
    println!("  f64 Gorilla (lossless)   : {f64_bps:.3} B/sample");
    println!(
        "  compact float32+implicit : {compact_bps:.3} B/sample  ({:.2}x denser)",
        f64_bps / compact_bps
    );
    println!("  anomaly-bit overhead (~1%): +{anom_overhead:.4} B/sample");
    println!("  float32 error: max_abs={max_abs:.2e}, max_rel={max_rel:.2e}");
    println!(
        "  bit-exact series (integral/counters): {lossless_series} / {}",
        corpus.series().len()
    );
}

/// Characterise redb's file size vs actual data volume across scales — the
/// fixed ~1 MB floor dominates until data outgrows it, so bytes/sample is only
/// meaningful at steady state. Compares small-block f64 vs big-block compact.
fn redb_scale_sweep() {
    println!("\n== redb file size vs data volume (steady-state check) ==");
    println!(
        "  {:>10} | {:>12} | {:>12} | {:>12}",
        "samples", "B+ file", "B+ B/samp", "B B/samp"
    );
    for &hours in &[1i64, 6, 24, 96] {
        let c = Corpus::generate(CorpusConfig {
            seed: 0x5CA1E,
            series: 40,
            duration_secs: hours * 3_600,
            ..CorpusConfig::default()
        });
        let n = c.sample_count();

        let dbp = tempfile::tempdir().unwrap();
        let mut bp = RedbCompactStore::open(dbp.path()).unwrap();
        c.replay_into(&mut bp).unwrap();
        bp.commit(Durability::Full).unwrap();
        let bp_size = bp.size_on_disk().unwrap();

        let db = tempfile::tempdir().unwrap();
        let mut b = RedbStore::open(db.path()).unwrap();
        c.replay_into(&mut b).unwrap();
        b.commit(Durability::Full).unwrap();
        let b_size = b.size_on_disk().unwrap();

        println!(
            "  {:>10} | {:>10} B | {:>10.3} | {:>10.3}",
            n,
            bp_size,
            bp_size as f64 / n as f64,
            b_size as f64 / n as f64
        );
    }
}
