//! Error texts the agent answers a request with when it does not run it; the server reads them.

/// A request arriving while another of its kind still runs.
pub const BUSY: &str = "busy";
/// A request the agent stopped at its own deadline.
pub const TIMED_OUT: &str = "timed out";
