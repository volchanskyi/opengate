//! Edge alerting: threshold rules, curated system-event rules ([`EventPack`]) and retroactive scans
//! ([`RetroScan`]) feed one bounded [`AlertSink`], each alert carrying its [`compose_evidence`].

mod evaluator;
mod event;
mod evidence;
mod retro;
mod sink;
mod transport;

pub use evidence::{
    compose_evidence, encode_evidence, pack_evidence, pack_metric_evidence, DimSeries,
    EncodedEvidence, EvidenceSource, LOG_SAMPLES, PROCESS_ROWS, RANKED_DIMS, SERIES_DIMS,
    SERIES_MAX_POINTS, SERIES_SPAN_SECS,
};

pub use evaluator::{
    rule_cost, AlertEvaluator, Firing, RULE_BUDGET_READINGS_PER_SEC, RULE_BUDGET_WINDOW_SECS,
};
pub use event::{EventLevel, EventMatcher, EventPack, EventRule, HostEvent, ServiceErrorRule};
pub use retro::{
    retro_hold, RetroBucket, RetroBudget, RetroConditions, RetroCursor, RetroError, RetroHistory,
    RetroHold, RetroPlan, RetroScan, RetroStats, RetroStep, RetroUnsupported, RETRO_BUCKET_SECS,
    RETRO_IDLE_CPU_PERCENT,
};
pub use sink::{
    AlertOrigin, AlertSeverity, AlertSink, EdgeAlert, PushOutcome, SinkStats, DEFAULT_CAPACITY,
    DEVICE_HOURLY_CEILING,
};
pub use transport::alert_message;

/// How evidence leaving this machine is packed, named on every alert that carries any.
pub use mesh_protocol::EVIDENCE_CODEC;
