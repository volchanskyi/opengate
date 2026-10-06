//! What travels with an alert: a fixed composition assembled once at fire time, packed by
//! [`encode_evidence`], which gives up the least valuable parts until the size cap holds.

use mesh_protocol::{
    AlertEvidence, EvidenceSeries, HistoryPoint, ProcessReportEntry, ProtocolError, RankedDim,
    EVIDENCE_CODEC, MAX_EVIDENCE_BYTES,
};

use crate::correlate::Ranked;
use crate::ml::redact::{redact_cmdline, redact_log_line};

/// Dimensions whose scores travel with an alert.
pub const RANKED_DIMS: usize = 8;

/// Ranked dimensions that also carry their readings.
pub const SERIES_DIMS: usize = 3;

/// How far either side of the event a series reaches, in seconds.
pub const SERIES_SPAN_SECS: i64 = 300;

/// Readings one series may carry across that window.
pub const SERIES_MAX_POINTS: usize = 512;

/// Process rows taken at the event instant.
pub const PROCESS_ROWS: usize = 10;

/// Host log lines sampled from the event window.
pub const LOG_SAMPLES: usize = 20;

/// One dimension's readings as the local store holds them.
#[derive(Debug, Clone, PartialEq)]
pub struct DimSeries {
    /// The dimension's stable label.
    pub dim: String,
    /// Readings, oldest first.
    pub points: Vec<HistoryPoint>,
}

/// Everything the composer reads at the moment an alert fires, borrowed to avoid copying history.
#[derive(Debug, Clone, Copy)]
pub struct EvidenceSource<'a> {
    /// The correlation engine's ranking, most anomalous first.
    pub ranked: &'a [Ranked],
    /// Readings the local store holds, by dimension.
    pub readings: &'a [DimSeries],
    /// What was running at the event instant, most significant first.
    pub processes: &'a [ProcessReportEntry],
    /// Host log lines from the event window, in the order they were read.
    pub log_lines: &'a [String],
    /// The instant the rule fired, in seconds.
    pub event_ts: i64,
}

/// Evidence encoded for the wire.
#[derive(Debug, Clone, PartialEq, Eq)]
#[non_exhaustive]
pub struct EncodedEvidence {
    /// The compressed blob.
    pub bytes: Vec<u8>,
    /// The codec that produced the blob, named on the message.
    pub codec: &'static str,
    /// Whether the cap cost this evidence anything.
    pub truncated: bool,
}

/// Assembles the evidence for one alert, redacting every free-text field: log lines, process
/// basenames and dimension labels.
#[must_use]
pub fn compose_evidence(source: &EvidenceSource<'_>) -> AlertEvidence {
    let ranked: Vec<RankedDim> = source
        .ranked
        .iter()
        .take(RANKED_DIMS)
        .map(|r| RankedDim {
            dim: redact_log_line(&r.dim),
            score: r.score,
        })
        .collect();

    // Series follow the ranking; a ranked dimension whose readings were evicted has no series.
    let series: Vec<EvidenceSeries> = source
        .ranked
        .iter()
        .take(SERIES_DIMS)
        .filter_map(|r| {
            let readings = source.readings.iter().find(|s| s.dim == r.dim)?;
            let points = window_points(&readings.points, source.event_ts);
            if points.is_empty() {
                return None;
            }
            Some(EvidenceSeries {
                dim: redact_log_line(&r.dim),
                points,
            })
        })
        .collect();

    let processes: Vec<ProcessReportEntry> = source
        .processes
        .iter()
        .take(PROCESS_ROWS)
        .map(|p| ProcessReportEntry {
            basename: redact_cmdline(&p.basename),
            ..p.clone()
        })
        .collect();

    // The cap applies before redaction, bounding the redaction cost.
    let log_samples: Vec<String> = source
        .log_lines
        .iter()
        .take(LOG_SAMPLES)
        .map(|line| redact_log_line(line))
        .collect();

    AlertEvidence {
        ranked,
        series,
        processes,
        log_samples,
        truncated: false,
    }
}

/// Encodes evidence, shrinking it in place until it fits [`MAX_EVIDENCE_BYTES`].
/// Fails with the codec's error when the evidence cannot be serialized.
pub fn encode_evidence(evidence: &mut AlertEvidence) -> Result<EncodedEvidence, ProtocolError> {
    let mut bytes = evidence.encode()?;
    if bytes.len() <= MAX_EVIDENCE_BYTES {
        return Ok(EncodedEvidence {
            bytes,
            codec: EVIDENCE_CODEC,
            truncated: false,
        });
    }

    // Compressed size is known only after encoding: give something up, re-encode, measure again.
    evidence.truncated = true;
    while shrink(evidence) {
        bytes = evidence.encode()?;
        if bytes.len() <= MAX_EVIDENCE_BYTES {
            return Ok(EncodedEvidence {
                bytes,
                codec: EVIDENCE_CODEC,
                truncated: true,
            });
        }
    }

    // Nothing left to give: the empty set still encodes under the cap and `truncated` stays set.
    *evidence = AlertEvidence {
        truncated: true,
        ..AlertEvidence::default()
    };
    Ok(EncodedEvidence {
        bytes: evidence.encode()?,
        codec: EVIDENCE_CODEC,
        truncated: true,
    })
}

/// Composes and packs in one step; evidence that will not serialize becomes an empty blob that
/// names no codec.
#[must_use]
pub fn pack_evidence(source: &EvidenceSource<'_>) -> EncodedEvidence {
    pack(compose_evidence(source))
}

/// Packs one dimension's own readings for a finding raised over stored history, which carries a
/// series and no ranking.
#[must_use]
pub fn pack_metric_evidence(dim: &str, points: &[HistoryPoint], event_ts: i64) -> EncodedEvidence {
    let windowed = window_points(points, event_ts);
    let series = if windowed.is_empty() {
        Vec::new()
    } else {
        vec![EvidenceSeries {
            // The label is redacted like every free-text field.
            dim: redact_log_line(dim),
            points: windowed,
        }]
    };
    pack(AlertEvidence {
        series,
        ..AlertEvidence::default()
    })
}

/// Encodes what was composed, answering with an empty blob when it will not encode.
fn pack(mut evidence: AlertEvidence) -> EncodedEvidence {
    encode_evidence(&mut evidence).unwrap_or(EncodedEvidence {
        bytes: Vec::new(),
        codec: "",
        truncated: false,
    })
}

/// Readings inside the event window, capped at [`SERIES_MAX_POINTS`] by keeping the newest.
fn window_points(points: &[HistoryPoint], event_ts: i64) -> Vec<HistoryPoint> {
    let (from, to) = (
        event_ts.saturating_sub(SERIES_SPAN_SECS),
        event_ts.saturating_add(SERIES_SPAN_SECS),
    );
    let inside: Vec<HistoryPoint> = points
        .iter()
        .filter(|p| p.ts >= from && p.ts <= to)
        .cloned()
        .collect();
    let overflow = inside.len().saturating_sub(SERIES_MAX_POINTS);
    inside[overflow..].to_vec()
}

/// Gives up one part, least valuable first, and reports whether anything was left to give.
/// The order is log samples, processes, readings within series, whole series, then the ranking.
fn shrink(evidence: &mut AlertEvidence) -> bool {
    if !evidence.log_samples.is_empty() {
        evidence
            .log_samples
            .truncate(evidence.log_samples.len() / 2);
        return true;
    }
    if !evidence.processes.is_empty() {
        evidence.processes.truncate(evidence.processes.len() / 2);
        return true;
    }
    if evidence.series.iter().any(|s| !s.points.is_empty()) {
        for series in &mut evidence.series {
            let keep = series.points.len() / 2;
            let dropped = series.points.len() - keep;
            series.points.drain(..dropped);
        }
        return true;
    }
    if !evidence.series.is_empty() {
        evidence.series.clear();
        return true;
    }
    if evidence.ranked.len() > 1 {
        evidence.ranked.truncate(evidence.ranked.len() / 2);
        return true;
    }
    false
}
