//! Edge-Sentinel local anomaly primitives.
//!
//! The module is intentionally agent-local: it samples host metrics, computes
//! anomaly bits, and leaves protocol/reporting decisions to later workstreams.

pub mod backfill;
pub mod ensemble;
pub mod kmeans;
pub mod log_rate;
pub mod redact;
pub mod sampler;
pub mod store_sink;
pub mod window;
