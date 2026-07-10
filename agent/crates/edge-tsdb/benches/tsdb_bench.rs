//! Measurement runner for the edge-tsdb store (`cargo bench -p edge-tsdb`).
//!
//! The production section (always compiled) records the WS-14b store's footprint
//! (bytes/sample, before and after cold-tier DEFLATE), ingest/commit cost,
//! range-query latency, and crash-recovery open time — the numbers ADR-051 and
//! the WS-14b build spec track. The WS-14a bake-off comparison (append-only vs
//! redb substrates, the codec bake-off, the redb scale sweep) is retained behind
//! the `bakeoff` feature. It is a plain `main` (harness = false), so gate
//! thresholds are asserted in `tests/` (always run), not here.

use std::time::Instant;

use edge_tsdb::corpus::{Corpus, CorpusConfig};

fn main() {
    let corpus = Corpus::generate(CorpusConfig {
        seed: 0xB00B_5EED,
        series: 40,
        duration_secs: 6 * 3_600,
        ..CorpusConfig::default()
    });
    println!(
        "WS-14b store — corpus: 40 series x 6h = {} samples\n",
        corpus.sample_count()
    );
    measure_local_tsdb(&corpus);

    #[cfg(feature = "bakeoff")]
    bakeoff::run(&corpus);
}

/// Production `LocalTsdb` footprint + recovery measurement.
fn measure_local_tsdb(corpus: &Corpus) {
    use edge_tsdb::store::LocalTsdb;
    use edge_tsdb::{Durability, TsdbConfig};

    let dir = tempfile::tempdir().unwrap();
    let mut db = LocalTsdb::open(dir.path(), TsdbConfig::default()).unwrap();
    for sid in 0..corpus.series().len() {
        db.set_scale(sid as u32, 100); // centi fixed-point; adaptive keeps counters int-DoD
    }

    let t0 = Instant::now();
    for (sid, series) in corpus.series().iter().enumerate() {
        for s in series {
            db.append(sid as u32, *s, false).unwrap();
        }
    }
    let ingest = t0.elapsed();

    let tc = Instant::now();
    db.commit(Durability::Full).unwrap();
    let commit = tc.elapsed();

    let n = corpus.sample_count();
    let file = db.size_on_disk().unwrap();
    let logical = db.logical_bytes();

    db.compact_cold_tiers().unwrap();
    let logical_cold = db.logical_bytes();

    let tq = Instant::now();
    let got = db.range_raw(0, i64::MIN, i64::MAX).unwrap();
    let query = tq.elapsed();
    drop(db);

    let tr = Instant::now();
    let reopened = LocalTsdb::open(dir.path(), TsdbConfig::default()).unwrap();
    let recover = tr.elapsed();
    let recovered = reopened.range_raw(0, i64::MIN, i64::MAX).unwrap().len();

    println!("== LocalTsdb (redb + fixed-point compact, T0/T1/T2) ==");
    println!("  samples              : {n}");
    println!(
        "  logical B/sample     : {:.3}  (cold-tier deflate: {:.3})",
        logical as f64 / n as f64,
        logical_cold as f64 / n as f64
    );
    println!(
        "  file B/sample        : {:.3}  ({file} B)",
        file as f64 / n as f64
    );
    println!(
        "  ingest               : {:.0} samp/s",
        n as f64 / ingest.as_secs_f64()
    );
    println!(
        "  commit (fsync)       : {:.2} ms",
        commit.as_secs_f64() * 1e3
    );
    println!(
        "  range query          : {:.1} µs ({} pts)",
        query.as_secs_f64() * 1e6,
        got.len()
    );
    println!(
        "  crash-recovery open  : {:.1} µs (recovered {recovered} pts)",
        recover.as_secs_f64() * 1e6
    );
}

/// The WS-14a substrate bake-off, retained as the measured off-ramp reference.
#[cfg(feature = "bakeoff")]
mod bakeoff {
    use super::Instant;
    use edge_tsdb::append_only::AppendOnlyStore;
    use edge_tsdb::baseline::BaselineStore;
    use edge_tsdb::compact::encode_compact;
    use edge_tsdb::corpus::{Corpus, CorpusConfig};
    use edge_tsdb::gorilla::encode_block;
    use edge_tsdb::redb_compact::RedbCompactStore;
    use edge_tsdb::redb_store::RedbStore;
    use edge_tsdb::substrate::{Durability, Substrate};

    pub fn run(corpus: &Corpus) {
        println!("\n== WS-14a substrate bake-off (reference) ==");
        println!(
            "{:<12} | {:>7} | {:>9} | {:>10} | {:>12} | {:>9} | {:>10} | {:>7}",
            "substrate",
            "B/samp",
            "bytes",
            "ingest_us",
            "samples/s",
            "commit_ms",
            "query_us",
            "pts"
        );
        println!("{}", "-".repeat(96));
        let da = tempfile::tempdir().unwrap();
        measure::<AppendOnlyStore>("A:append", corpus, da.path());
        let db = tempfile::tempdir().unwrap();
        measure::<RedbStore>("B:redb", corpus, db.path());
        let dbp = tempfile::tempdir().unwrap();
        measure::<RedbCompactStore>("B+:redb32", corpus, dbp.path());
        let dc = tempfile::tempdir().unwrap();
        measure::<BaselineStore>("C:baseline", corpus, dc.path());

        let dr = tempfile::tempdir().unwrap();
        measure_recovery(corpus, dr.path());
        codec_bakeoff(corpus);
        redb_scale_sweep();
    }

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
        let t2 = Instant::now();
        let got = store.range(0, i64::MIN, i64::MAX).unwrap();
        let query = t2.elapsed();
        let ingest_rate = n as f64 / ingest.as_secs_f64();
        println!(
            "{name:<12} | {bps:>7.3} | {size:>9} | {ingest_us:>10.1} | {rate:>12.0} | {commit_ms:>9.2} | {query_us:>10.1} | {pts:>7}",
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
            "\nappend-only recovery: open-after-kill = {:.1} µs, durable = {}, recoverable = {}, repaired = {}, quarantined = {}",
            repair.as_secs_f64() * 1e6,
            report.durable_samples,
            report.recoverable_samples,
            report.repaired_segments,
            report.quarantined_chunks,
        );
    }

    fn codec_bakeoff(corpus: &Corpus) {
        let mut f64_bytes = 0usize;
        let mut compact_bytes = 0usize;
        let mut n = 0usize;
        for series in corpus.series() {
            let no_anom = vec![false; series.len()];
            f64_bytes += encode_block(series).len();
            compact_bytes += encode_compact(series, &no_anom, 1).len();
            n += series.len();
        }
        let f64_bps = f64_bytes as f64 / n as f64;
        let compact_bps = compact_bytes as f64 / n as f64;
        println!("\n== codec bake-off (pure encoding, {n} samples) ==");
        println!("  f64 Gorilla (lossless)   : {f64_bps:.3} B/sample");
        println!(
            "  compact float32+implicit : {compact_bps:.3} B/sample  ({:.2}x denser)",
            f64_bps / compact_bps
        );
    }

    fn redb_scale_sweep() {
        println!("\n== redb file size vs data volume (steady-state check) ==");
        println!(
            "  {:>10} | {:>12} | {:>12}",
            "samples", "B+ B/samp", "B B/samp"
        );
        for &hours in &[1i64, 6, 24] {
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
                "  {:>10} | {:>10.3} | {:>10.3}",
                n,
                bp_size as f64 / n as f64,
                b_size as f64 / n as f64
            );
        }
    }
}
