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

mod evaluator;

pub use evaluator::AlertEvaluator;
