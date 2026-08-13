//! WS-19 declarative edge threshold-alert evaluation.
//!
//! A tenant-scoped set of [`mesh_protocol::ThresholdRule`]s is evaluated locally
//! against sampler dimensions every window, alongside the WS-2 ML anomaly
//! detector. A breach must sustain continuously for the rule's `sustain_secs`
//! before it fires (rising-edge flap suppression), and each rule's hysteresis
//! `clear` boundary keeps it firing until the metric recovers past that boundary
//! (falling-edge flap suppression). The evaluator is pure and allocation-light;
//! it emits a breach signal per firing rule and leaves delivery to the caller.
//! Delivery stays investigation-aid only until the FPR soak.

//! Beside the numeric evaluator sits the system-event side of the same job.
//! Some failures never show up as a number crossing a line — a task stuck for
//! two minutes, memory reclaimed by killing a process, a disk that stopped
//! answering — and the machine reports every one of them about itself in its
//! own log. [`EventPack`] reads those records through curated rules; both
//! producers hand what they find to one [`AlertSink`], which is bounded and
//! rate-limited per device and counts everything either limit costs.

mod evaluator;
mod event;
mod sink;

pub use evaluator::AlertEvaluator;
pub use event::{EventLevel, EventMatcher, EventPack, EventRule, HostEvent, ServiceErrorRule};
pub use sink::{
    AlertSeverity, AlertSink, EdgeAlert, PushOutcome, SinkStats, DEFAULT_CAPACITY,
    DEVICE_HOURLY_CEILING,
};
