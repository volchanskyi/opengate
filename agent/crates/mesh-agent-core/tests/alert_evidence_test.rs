//! Alert evidence has a fixed composition, and a size-cap overrun drops the least valuable
//! parts first.

use mesh_agent_core::alerts::{
    compose_evidence, encode_evidence, DimSeries, EvidenceSource, LOG_SAMPLES, PROCESS_ROWS,
    RANKED_DIMS, SERIES_DIMS, SERIES_MAX_POINTS, SERIES_SPAN_SECS,
};
use mesh_agent_core::correlate::Ranked;
use mesh_protocol::{
    AlertEvidence, EvidenceProcess, HistoryPoint, EVIDENCE_CODEC, MAX_EVIDENCE_BYTES,
};

const EVENT_TS: i64 = 1_700_000_000;

fn ranked(count: usize) -> Vec<Ranked> {
    (0..count)
        .map(|i| Ranked {
            dim: format!("dim.{i}"),
            #[allow(clippy::cast_precision_loss)]
            score: 1.0 - (i as f64) * 0.01,
            ks_statistic: 0.5,
            anomaly_rate: 0.5,
            shift_magnitude: 0.5,
            baseline_samples: 64,
            focus_samples: 64,
        })
        .collect()
}

/// Readings for `count` dimensions, one a second, wider than the window evidence keeps.
fn readings(count: usize, span_secs: i64) -> Vec<DimSeries> {
    (0..count)
        .map(|i| DimSeries {
            dim: format!("dim.{i}"),
            points: (-span_secs..=span_secs)
                .map(|offset| HistoryPoint {
                    ts: EVENT_TS + offset,
                    #[allow(clippy::cast_precision_loss)]
                    value: offset as f64,
                })
                .collect(),
        })
        .collect()
}

fn processes(count: u32) -> Vec<EvidenceProcess> {
    (0..count)
        .map(|i| EvidenceProcess {
            rank: i,
            basename: format!("worker{i}"),
            cmdline_hash: None,
            pid: 2000 + i,
            cpu_share: Some(f64::from(i)),
            mem: f64::from(i),
        })
        .collect()
}

fn log_lines(count: usize) -> Vec<String> {
    (0..count).map(|i| format!("event {i} occurred")).collect()
}

fn source<'a>(
    scores: &'a [Ranked],
    series: &'a [DimSeries],
    procs: &'a [EvidenceProcess],
    logs: &'a [String],
) -> EvidenceSource<'a> {
    EvidenceSource {
        ranked: scores,
        readings: series,
        processes: procs,
        log_lines: logs,
        event_ts: EVENT_TS,
    }
}

#[test]
fn the_composition_is_fixed_rather_than_whatever_was_available() {
    let scores = ranked(40);
    let series = readings(40, SERIES_SPAN_SECS * 4);
    let procs = processes(200);
    let logs = log_lines(500);

    let evidence = compose_evidence(&source(&scores, &series, &procs, &logs));

    assert_eq!(evidence.ranked.len(), RANKED_DIMS);
    assert_eq!(evidence.series.len(), SERIES_DIMS);
    assert_eq!(evidence.processes.len(), PROCESS_ROWS);
    assert_eq!(evidence.log_samples.len(), LOG_SAMPLES);
    assert!(
        !evidence.truncated,
        "the fixed composition fits its own cap"
    );

    for (i, series) in evidence.series.iter().enumerate() {
        assert_eq!(series.dim, evidence.ranked[i].dim);
        assert!(
            series.points.len() <= SERIES_MAX_POINTS,
            "a series carries at most {SERIES_MAX_POINTS} points, got {}",
            series.points.len()
        );
        assert!(!series.points.is_empty());
    }
}

#[test]
fn a_series_covers_the_event_window_on_both_sides() {
    let scores = ranked(4);
    let series = readings(4, SERIES_SPAN_SECS * 4);
    let evidence = compose_evidence(&source(&scores, &series, &[], &[]));

    let first = &evidence.series[0];
    let oldest = first.points.first().expect("a series carries readings").ts;
    let newest = first.points.last().expect("a series carries readings").ts;

    assert!(
        oldest >= EVENT_TS - SERIES_SPAN_SECS,
        "readings older than the window must not travel"
    );
    assert!(
        newest <= EVENT_TS + SERIES_SPAN_SECS,
        "readings newer than the window must not travel"
    );
    assert!(
        oldest < EVENT_TS && newest > EVENT_TS,
        "the window has to show both sides of the event, not just the aftermath"
    );
}

#[test]
fn a_thin_device_composes_what_it_has_without_inventing_the_rest() {
    let scores = ranked(2);
    let series = readings(2, 30);
    let procs = processes(1);
    let evidence = compose_evidence(&source(&scores, &series, &procs, &[]));

    assert_eq!(evidence.ranked.len(), 2);
    assert_eq!(evidence.series.len(), 2);
    assert_eq!(evidence.processes.len(), 1);
    assert!(evidence.log_samples.is_empty());
    assert!(!evidence.truncated);
}

#[test]
fn a_ranked_dimension_with_no_readings_still_ranks() {
    // A ranked dimension can have readings already evicted from the local store.
    let scores = ranked(3);
    let series = readings(1, 60);
    let evidence = compose_evidence(&source(&scores, &series, &[], &[]));

    assert_eq!(evidence.ranked.len(), 3);
    assert_eq!(
        evidence.series.len(),
        1,
        "only the dimension whose readings survived carries a series"
    );
    assert_eq!(evidence.series[0].dim, "dim.0");
}

#[test]
fn the_log_sample_cap_is_taken_before_redaction() {
    // The cap applies before redaction, so redaction work stays bounded.
    let logs: Vec<String> = (0..5_000)
        .map(|i| format!("attempt {i} password=hunter2"))
        .collect();
    let evidence = compose_evidence(&source(&[], &[], &[], &logs));

    assert_eq!(evidence.log_samples.len(), LOG_SAMPLES);
    for line in &evidence.log_samples {
        assert!(!line.contains("hunter2"), "the cap must not skip redaction");
    }
}

#[test]
fn no_field_carries_a_secret_off_the_device() {
    // Redaction happens at the edge; the server's own guard is defence in depth.
    let hostile = [
        "GET /v1 Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.eyJhIjoxfQ.sig",
        "aws_key AKIAIOSFODNN7EXAMPLE rotated",
        "login password=hunter2 ok",
        "mail to alice@example.com queued",
        "mount //host/share user:s3cr3t@fileserver failed",
        "psql postgres://admin:letmein@db:5432/app refused",
    ];
    let logs: Vec<String> = hostile.iter().map(|s| (*s).to_string()).collect();

    let scores = vec![Ranked {
        dim: "password=hunter2".to_string(),
        score: 1.0,
        ks_statistic: 0.0,
        anomaly_rate: 0.0,
        shift_magnitude: 0.0,
        baseline_samples: 2,
        focus_samples: 2,
    }];
    let series = vec![DimSeries {
        dim: "password=hunter2".to_string(),
        points: vec![HistoryPoint {
            ts: EVENT_TS,
            value: 1.0,
        }],
    }];
    let procs = vec![EvidenceProcess {
        rank: 0,
        basename: "backup --token=s3cr3t".to_string(),
        cmdline_hash: None,
        pid: 42,
        cpu_share: Some(1.0),
        mem: 1.0,
    }];

    let evidence = compose_evidence(&source(&scores, &series, &procs, &logs));

    let forbidden = [
        "hunter2",
        "s3cr3t",
        "letmein",
        "AKIAIOSFODNN7EXAMPLE",
        "eyJhbGciOiJIUzI1NiJ9",
    ];
    for secret in forbidden {
        for line in &evidence.log_samples {
            assert!(!line.contains(secret), "log sample leaked {secret}: {line}");
        }
        for dim in &evidence.ranked {
            assert!(
                !dim.dim.contains(secret),
                "ranked label leaked {secret}: {}",
                dim.dim
            );
        }
        for series in &evidence.series {
            assert!(
                !series.dim.contains(secret),
                "series label leaked {secret}: {}",
                series.dim
            );
        }
        for process in &evidence.processes {
            assert!(
                !process.basename.contains(secret),
                "process basename leaked {secret}: {}",
                process.basename
            );
        }
    }
}

#[test]
fn oversized_evidence_is_truncated_and_still_travels() {
    let scores = ranked(RANKED_DIMS);
    let series = readings(RANKED_DIMS, SERIES_SPAN_SECS);
    let procs = processes(PROCESS_ROWS as u32);
    // Incompressible lines reach the cap by content, not repetition.
    let logs: Vec<String> = (0..LOG_SAMPLES).map(|i| noisy_line(i, 8_000)).collect();

    let mut evidence = compose_evidence(&source(&scores, &series, &procs, &logs));
    // The codec enforces size, since compressed size is known only after encoding.
    assert_eq!(evidence.log_samples.len(), LOG_SAMPLES);

    let encoded = encode_evidence(&mut evidence).expect("evidence must encode");
    assert_eq!(encoded.codec, EVIDENCE_CODEC);
    assert!(
        encoded.bytes.len() <= MAX_EVIDENCE_BYTES,
        "encoded evidence must respect its cap: {} bytes",
        encoded.bytes.len()
    );
    assert!(encoded.truncated, "a truncated alert must say it was");
    assert!(
        evidence.truncated,
        "the evidence handed to the wire must carry the flag, not just the caller"
    );

    assert!(
        evidence.log_samples.len() < LOG_SAMPLES,
        "log samples are the first thing the cap takes"
    );
    assert!(
        !evidence.log_samples.is_empty(),
        "the samples are halved until they fit, not discarded wholesale"
    );
    assert_eq!(
        evidence.ranked.len(),
        RANKED_DIMS,
        "the ranking is the last thing the cap takes"
    );

    let decoded = AlertEvidence::decode(&encoded.bytes, encoded.codec).expect("decodes");
    assert!(decoded.truncated);
    assert_eq!(decoded.ranked.len(), RANKED_DIMS);
}

#[test]
fn truncation_is_the_same_two_runs_running() {
    let build = || {
        let scores = ranked(RANKED_DIMS);
        let series = readings(RANKED_DIMS, SERIES_SPAN_SECS);
        let procs = processes(PROCESS_ROWS as u32);
        let logs: Vec<String> = (0..LOG_SAMPLES).map(|i| noisy_line(i, 8_000)).collect();
        let mut evidence = compose_evidence(&source(&scores, &series, &procs, &logs));
        let encoded = encode_evidence(&mut evidence).expect("evidence must encode");
        (evidence, encoded.bytes)
    };

    let (first, first_bytes) = build();
    let (second, second_bytes) = build();
    assert_eq!(
        first, second,
        "the same evidence must truncate the same way"
    );
    assert_eq!(first_bytes, second_bytes);
}

#[test]
fn evidence_that_fits_is_left_alone() {
    let scores = ranked(RANKED_DIMS);
    let series = readings(RANKED_DIMS, SERIES_SPAN_SECS);
    let procs = processes(PROCESS_ROWS as u32);
    let logs = log_lines(LOG_SAMPLES);

    let mut evidence = compose_evidence(&source(&scores, &series, &procs, &logs));
    let before = evidence.clone();
    let encoded = encode_evidence(&mut evidence).expect("evidence must encode");

    assert!(!encoded.truncated);
    assert_eq!(
        evidence, before,
        "nothing may be dropped from evidence that fits"
    );
    assert_eq!(
        AlertEvidence::decode(&encoded.bytes, encoded.codec).unwrap(),
        before
    );
}

/// A fixed-seed, effectively incompressible line, so the cap is reached by compressed content.
/// The seed is mixed so that no two lines share one stream.
fn noisy_line(index: usize, len: usize) -> String {
    const ALPHABET: &[u8] = b"abcdefghijklmnopqrstuvwxyz0123456789";
    const STEP: u64 = 0x9E37_79B9_7F4A_7C15;

    let mut state = mix((index as u64 + 1).wrapping_mul(0x2545_F491_4F6C_DD1D));
    let noise: String = (0..len)
        .map(|_| {
            state = state.wrapping_add(STEP);
            ALPHABET[(mix(state) % ALPHABET.len() as u64) as usize] as char
        })
        .collect();
    format!("line {index} {noise}")
}

/// The SplitMix64 finalizer, a bijection that scatters a counter into structureless bits.
fn mix(mut z: u64) -> u64 {
    z = (z ^ (z >> 30)).wrapping_mul(0xBF58_476D_1CE4_E5B9);
    z = (z ^ (z >> 27)).wrapping_mul(0x94D0_49BB_1331_11EB);
    z ^ (z >> 31)
}

use mesh_protocol::{EvidenceSeries, RankedDim};

/// `count` incompressible log samples of `bytes` each.
fn heavy_logs(count: usize, bytes: usize) -> Vec<String> {
    (0..count).map(|i| noisy_line(i, bytes)).collect()
}

/// A series of `points` readings, one a second, ending the second before the event.
fn series_of(dim: &str, points: usize) -> EvidenceSeries {
    #[allow(clippy::cast_possible_wrap)]
    let span = points as i64;
    EvidenceSeries {
        dim: dim.to_string(),
        points: (0..points)
            .map(|i| HistoryPoint {
                #[allow(clippy::cast_possible_wrap)]
                ts: EVENT_TS - span + i as i64,
                #[allow(clippy::cast_precision_loss)]
                value: i as f64,
            })
            .collect(),
    }
}

fn ranked_dims(count: usize) -> Vec<RankedDim> {
    (0..count)
        .map(|i| RankedDim {
            dim: format!("dim.{i}"),
            #[allow(clippy::cast_precision_loss)]
            score: 1.0 - (i as f64) * 0.01,
        })
        .collect()
}

fn full_evidence() -> AlertEvidence {
    AlertEvidence {
        ranked: ranked_dims(RANKED_DIMS),
        series: (0..SERIES_DIMS)
            .map(|i| series_of(&format!("dim.{i}"), SERIES_MAX_POINTS))
            .collect(),
        processes: processes(PROCESS_ROWS as u32),
        log_samples: log_lines(LOG_SAMPLES),
        truncated: false,
    }
}

#[test]
fn log_samples_are_the_first_thing_given_up_and_nothing_else_is_touched() {
    let before = full_evidence();
    let mut evidence = AlertEvidence {
        log_samples: heavy_logs(LOG_SAMPLES, 16_000),
        ..before.clone()
    };

    let encoded = encode_evidence(&mut evidence).expect("evidence must encode");

    assert!(encoded.truncated, "the cap cost this evidence something");
    assert!(
        evidence.log_samples.len() < LOG_SAMPLES,
        "log samples are the first thing the cap takes"
    );
    assert!(
        !evidence.log_samples.is_empty(),
        "the samples are halved until they fit, not discarded wholesale"
    );
    assert_eq!(
        evidence.processes, before.processes,
        "the process list is not touched while log samples remain to give"
    );
    assert_eq!(
        evidence.series, before.series,
        "the readings are not touched while log samples remain to give"
    );
    assert_eq!(
        evidence.ranked, before.ranked,
        "the ranking is not touched while log samples remain to give"
    );
}

#[test]
fn the_process_list_goes_next_and_the_readings_stay_whole() {
    let before = full_evidence();
    let mut evidence = AlertEvidence {
        log_samples: Vec::new(),
        processes: (0..PROCESS_ROWS)
            .map(|i| EvidenceProcess {
                #[allow(clippy::cast_possible_truncation)]
                rank: i as u32,
                basename: noisy_line(i, 16_000),
                cmdline_hash: None,
                #[allow(clippy::cast_possible_truncation)]
                pid: 2000 + i as u32,
                cpu_share: None,
                mem: 1.0,
            })
            .collect(),
        ..before.clone()
    };

    let encoded = encode_evidence(&mut evidence).expect("evidence must encode");

    assert!(encoded.truncated);
    assert!(
        evidence.processes.len() < PROCESS_ROWS,
        "the process list is given up once there are no log samples left"
    );
    assert!(
        !evidence.processes.is_empty(),
        "the process list is halved until it fits, not discarded wholesale"
    );
    assert_eq!(
        evidence.series, before.series,
        "the readings outlive the process list"
    );
    assert_eq!(
        evidence.ranked, before.ranked,
        "the ranking outlives the process list"
    );
}

#[test]
fn readings_are_thinned_from_the_far_end_before_a_series_is_dropped() {
    let deep = 20_000;
    let mut evidence = AlertEvidence {
        ranked: ranked_dims(RANKED_DIMS),
        series: (0..SERIES_DIMS)
            .map(|i| series_of(&format!("dim.{i}"), deep))
            .collect(),
        processes: Vec::new(),
        log_samples: Vec::new(),
        truncated: false,
    };

    let encoded = encode_evidence(&mut evidence).expect("evidence must encode");

    assert!(encoded.truncated);
    assert_eq!(
        evidence.series.len(),
        SERIES_DIMS,
        "every series is thinned before any of them is dropped"
    );
    for series in &evidence.series {
        assert!(
            series.points.len() < deep,
            "an oversized series is thinned, got {} points",
            series.points.len()
        );
        assert!(
            series.points.len() > 1,
            "thinning halves a series; it does not cut it to a remainder"
        );
        let first = series
            .points
            .first()
            .expect("a thinned series keeps readings");
        let last = series
            .points
            .last()
            .expect("a thinned series keeps readings");
        assert_eq!(
            last.ts,
            EVENT_TS - 1,
            "the readings kept are the ones nearest the event"
        );
        #[allow(clippy::cast_possible_wrap)]
        let kept = series.points.len() as i64;
        assert_eq!(
            last.ts - first.ts + 1,
            kept,
            "what is kept is one unbroken run ending at the event"
        );
    }
    assert_eq!(
        evidence.ranked.len(),
        RANKED_DIMS,
        "the ranking outlives the readings"
    );
}

#[test]
fn whole_series_go_before_the_ranking_does() {
    // Series with no readings leave nothing to thin, so only their labels overflow.
    let mut evidence = AlertEvidence {
        ranked: ranked_dims(RANKED_DIMS),
        series: (0..SERIES_DIMS)
            .map(|i| EvidenceSeries {
                dim: noisy_line(i, 60_000),
                points: Vec::new(),
            })
            .collect(),
        processes: Vec::new(),
        log_samples: Vec::new(),
        truncated: false,
    };

    let encoded = encode_evidence(&mut evidence).expect("evidence must encode");

    assert!(encoded.truncated);
    assert!(
        evidence.series.is_empty(),
        "series with nothing left to thin are cleared whole"
    );
    assert_eq!(
        evidence.ranked.len(),
        RANKED_DIMS,
        "the ranking outlives the series"
    );
}

#[test]
fn the_ranking_is_halved_last_and_never_entirely() {
    let mut evidence = AlertEvidence {
        ranked: (0..RANKED_DIMS)
            .map(|i| RankedDim {
                dim: noisy_line(i, 30_000),
                #[allow(clippy::cast_precision_loss)]
                score: 1.0 - (i as f64) * 0.01,
            })
            .collect(),
        series: Vec::new(),
        processes: Vec::new(),
        log_samples: Vec::new(),
        truncated: false,
    };

    let encoded = encode_evidence(&mut evidence).expect("evidence must encode");

    assert!(encoded.truncated);
    assert!(
        encoded.bytes.len() <= MAX_EVIDENCE_BYTES,
        "the cap holds even when the ranking is all there is"
    );
    assert!(
        evidence.ranked.len() < RANKED_DIMS,
        "the ranking is thinned when it is the only thing left"
    );
    assert!(
        !evidence.ranked.is_empty(),
        "the ranking is never given up entirely — it is what a technician reads first"
    );
}

#[test]
fn evidence_that_cannot_fit_at_all_still_travels_and_says_so() {
    // One label larger than the cap exhausts every truncation step.
    let mut evidence = AlertEvidence {
        ranked: vec![RankedDim {
            dim: noisy_line(0, MAX_EVIDENCE_BYTES * 4),
            score: 1.0,
        }],
        series: Vec::new(),
        processes: Vec::new(),
        log_samples: Vec::new(),
        truncated: false,
    };

    let encoded = encode_evidence(&mut evidence).expect("evidence must encode");

    assert!(encoded.bytes.len() <= MAX_EVIDENCE_BYTES);
    assert!(encoded.truncated);
    assert_eq!(
        evidence,
        AlertEvidence {
            truncated: true,
            ..AlertEvidence::default()
        },
        "the evidence handed on is empty and flagged, so silence is never mistaken \
         for a machine that saw nothing"
    );
    let decoded = AlertEvidence::decode(&encoded.bytes, encoded.codec).expect("decodes");
    assert!(decoded.truncated);
}

#[test]
fn the_ranking_stops_at_one_dimension_rather_than_at_none() {
    // Labels sized so that one dimension fits and two do not.
    let mut evidence = AlertEvidence {
        ranked: (0..RANKED_DIMS)
            .map(|i| RankedDim {
                dim: noisy_line(i, 90_000),
                #[allow(clippy::cast_precision_loss)]
                score: 1.0 - (i as f64) * 0.01,
            })
            .collect(),
        series: Vec::new(),
        processes: Vec::new(),
        log_samples: Vec::new(),
        truncated: false,
    };

    let encoded = encode_evidence(&mut evidence).expect("evidence must encode");

    assert!(encoded.truncated);
    assert!(encoded.bytes.len() <= MAX_EVIDENCE_BYTES);
    assert_eq!(
        evidence.ranked.len(),
        1,
        "the ranking is thinned down to its most anomalous dimension and no further"
    );
    assert_eq!(
        evidence.ranked[0].score, 1.0,
        "the dimension that survives is the most anomalous one"
    );
}
