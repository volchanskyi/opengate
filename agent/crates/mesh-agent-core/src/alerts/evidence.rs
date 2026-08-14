//! What travels with an alert, and what the size cap takes when it will not fit.
//!
//! Central keeps a 60 s average per dimension and never asks the device for
//! more, so the ten-second collapse that explains an incident exists on the
//! endpoint and nowhere else. Evidence is therefore assembled once, at fire
//! time, and shipped with the alert: what is not on the message is not recorded.
//!
//! The composition is **fixed**, not "as much as fits". Two incidents a week
//! apart are only comparable if they were assembled the same way, so the counts
//! below are the contract rather than a budget:
//!
//! | Part | Bound | Why |
//! |---|---|---|
//! | `ranked` | 8 dimensions | the ranking's useful tail; past eight the scores are noise |
//! | `series` | the top 3 ranked, ±5 min, ≤ 512 points | enough to see the shape either side of the event |
//! | `processes` | 10 at the event instant | the existing process-report rank vocabulary |
//! | `log_samples` | 20 redacted lines | bounded *before* redaction, so a flood cannot buy CPU |
//!
//! Size is the codec's business, not the composer's: how large evidence
//! compresses to is unknowable until it has been compressed. So
//! [`encode_evidence`] composes, encodes, measures, and only then gives
//! something up — least valuable first, in a fixed order, re-encoding after each
//! step. Going over the cap costs the alert its tail. It never costs the alert:
//! a machine in trouble still says so, with less behind it.

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

/// Everything the composer reads at the moment an alert fires.
///
/// Borrowed rather than owned: this runs on a machine that is already in
/// trouble, which is exactly when copying its recent history would be the wrong
/// thing to do.
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
    /// The codec that produced it, named on the message so a later one is
    /// additive.
    pub codec: &'static str,
    /// Whether the cap cost this evidence anything.
    pub truncated: bool,
}

/// Assemble the evidence for one alert at the fixed composition above.
///
/// Redaction happens here, at the edge, on every free-text field — log lines,
/// process basenames, and dimension labels alike. A label is not supposed to be
/// able to carry a secret, but "supposed to" is not a property anything checks
/// at run time, and the cost of redacting a bounded list of short strings is
/// nothing against the cost of being wrong once.
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

    // Series follow the ranking, so the three that travel are the three a
    // technician would have asked to see. A ranked dimension whose readings the
    // store has already evicted costs the alert its series and nothing else.
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

    // The cap is applied before redaction on purpose: redaction is the expensive
    // half, and a host emitting ten thousand secret-bearing lines a second must
    // not get to choose how much of the device's budget the alert spends.
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

/// Encode evidence for the wire, giving up its least valuable parts until it
/// fits [`MAX_EVIDENCE_BYTES`].
///
/// The evidence is edited in place, so the caller keeps exactly what was shipped
/// rather than what was offered — an alert whose stored evidence disagrees with
/// the blob beside it would be worse than no evidence at all.
///
/// # Errors
/// Returns the codec's error if the evidence cannot be serialized at all.
pub fn encode_evidence(evidence: &mut AlertEvidence) -> Result<EncodedEvidence, ProtocolError> {
    let mut bytes = evidence.encode()?;
    if bytes.len() <= MAX_EVIDENCE_BYTES {
        return Ok(EncodedEvidence {
            bytes,
            codec: EVIDENCE_CODEC,
            truncated: false,
        });
    }

    // Compressed size is not knowable until after encoding, so this is a second
    // pass rather than a prediction: give something up, re-encode, measure again.
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

    // Nothing left to give. An empty evidence set still encodes to a few dozen
    // bytes, so the cap holds and the alert travels; `truncated` is what says the
    // silence is the cap's doing rather than a device that saw nothing.
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

/// Readings inside the event window, capped at [`SERIES_MAX_POINTS`].
///
/// The cap keeps the readings nearest the event: whatever the window holds, the
/// end of it is where the machine was when the rule fired.
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

/// Give up one thing, from the least valuable end, and say whether there was
/// anything left to give.
///
/// The order is the composition table read bottom-up: log samples, then the
/// process list, then the readings inside each series, then whole series, then
/// the ranking — which goes last and never entirely, because it is the line a
/// technician reads first. Halving rather than dropping one at a time keeps the
/// number of re-encodes logarithmic on a machine that is already struggling.
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
