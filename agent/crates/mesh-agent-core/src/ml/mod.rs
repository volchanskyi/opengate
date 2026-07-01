//! Edge-Sentinel local anomaly primitives.
//!
//! The module is intentionally agent-local: it samples host metrics, computes
//! anomaly bits, and leaves protocol/reporting decisions to later workstreams.

pub mod ensemble;
pub mod kmeans;
pub mod redact;
pub mod sampler;
pub mod window;
