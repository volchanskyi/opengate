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
//!
//! A rule may compare the reading itself, how fast it is changing, or its
//! largest or mean value over a window, and may require several dimensions at
//! once. Everything it can say is data in a closed grammar — never shipped code
//! — so [`rule_cost`] answers what any rule costs to evaluate from its declared
//! fields alone, before it ever reaches an endpoint. Beside firing, every rule
//! reports what it is *doing* here: a rule this host cannot evaluate is
//! `unsupported` and counted, never quietly skipped.

//! Beside the numeric evaluator sits the system-event side of the same job.
//! Some failures never show up as a number crossing a line — a task stuck for
//! two minutes, memory reclaimed by killing a process, a disk that stopped
//! answering — and the machine reports every one of them about itself in its
//! own log. [`EventPack`] reads those records through curated rules; both
//! producers hand what they find to one [`AlertSink`], which is bounded and
//! rate-limited per device and counts everything either limit costs.

//! A rule reaching a machine also asks what it *would* have caught: the same
//! rule is re-run over the minute-by-minute history the device already holds
//! ([`RetroScan`]), so "has this happened before" is answered on the endpoint
//! rather than by shipping every device's seconds to a central recorder. Those
//! findings carry the minute they happened and land in the same bounded sink,
//! spending the same allowance as anything raised live.

//! What a rule costs is finally the machine's own decision. The pack is
//! cost-bounded before it ships, but the endpoint is what pays, and a rule can
//! reach one without having come through that gate — so a rule that touches more
//! than [`RULE_BUDGET_READINGS_PER_SEC`] readings a second here is stopped, hard
//! and by itself, and says so in its coverage. One expensive rule silencing the
//! cheap ones would turn a bad rollout into blanket blindness while still
//! looking contained.

mod evaluator;
mod event;
mod retro;
mod sink;

pub use evaluator::{
    rule_cost, AlertEvaluator, RULE_BUDGET_READINGS_PER_SEC, RULE_BUDGET_WINDOW_SECS,
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
