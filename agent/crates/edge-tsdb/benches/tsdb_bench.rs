//! The local metrics store's hot paths as the agent drives them: a commit window of the sampler's
//! readings written, an hour of one series read back, and the open a restarted agent pays.

use criterion::{criterion_group, criterion_main, Criterion};
use edge_tsdb::corpus::{Corpus, CorpusConfig};
use edge_tsdb::{Durability, LocalTsdb, Sample, TsdbConfig};
use std::hint::black_box;

/// The series the agent's sampler writes on every tick.
const SERIES: u32 = 13;

/// The ticks between the agent's commits, one sample per series per tick.
const COMMIT_WINDOW: i64 = 60;

/// The history a populated store holds for the read and open benchmarks.
const HISTORY_SECS: i64 = 6 * 3_600;

fn corpus(duration_secs: i64) -> Corpus {
    Corpus::generate(CorpusConfig {
        seed: 0xB00B_5EED,
        series: SERIES,
        duration_secs,
        ..CorpusConfig::default()
    })
}

/// A store under `dir` holding the corpus, committed.
fn populated(dir: &std::path::Path, corpus: &Corpus) -> LocalTsdb {
    let mut db = LocalTsdb::open(dir, TsdbConfig::default()).expect("open store");
    for (series, samples) in (0..).zip(corpus.series()) {
        for sample in samples {
            db.append(series, *sample, false).expect("append");
        }
    }
    db.commit(Durability::Full).expect("commit");
    db
}

// The commit skips the fsync, so the figure is the store's own work on the disk's page cache.
fn bench_commit_window(c: &mut Criterion) {
    let dir = tempfile::tempdir().expect("temp dir");
    let mut db = LocalTsdb::open(dir.path(), TsdbConfig::default()).expect("open store");
    let readings = corpus(COMMIT_WINDOW);
    let mut ts = 0;

    c.bench_function("tsdb_commit_window", |b| {
        b.iter(|| {
            for (series, samples) in (0..).zip(readings.series()) {
                for (offset, sample) in (0..).zip(samples) {
                    db.append(series, Sample::new(ts + offset, sample.value), false)
                        .expect("append");
                }
            }
            ts += COMMIT_WINDOW;
            db.commit(Durability::None).expect("commit");
        })
    });
}

fn bench_range_hour(c: &mut Criterion) {
    let history = corpus(HISTORY_SECS);
    let dir = tempfile::tempdir().expect("temp dir");
    let db = populated(dir.path(), &history);
    let end = history.series()[0].last().expect("a sample").ts;

    c.bench_function("tsdb_range_raw_hour", |b| {
        b.iter(|| {
            let points = db.range_raw(0, end - 3_600, end).expect("range");
            black_box(points.len())
        })
    });
}

fn bench_reopen(c: &mut Criterion) {
    let history = corpus(HISTORY_SECS);
    let dir = tempfile::tempdir().expect("temp dir");
    drop(populated(dir.path(), &history));

    c.bench_function("tsdb_reopen", |b| {
        b.iter(|| black_box(LocalTsdb::open(dir.path(), TsdbConfig::default()).expect("open")))
    });
}

criterion_group! {
    name = benches;
    config = Criterion::default().sample_size(20);
    targets = bench_commit_window, bench_range_hour, bench_reopen
}
criterion_main!(benches);
