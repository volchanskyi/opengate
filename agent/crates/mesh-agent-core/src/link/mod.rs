//! The host's end of the direct link: the peer connection and the run-time send limit.

pub mod pacing;
pub(crate) mod peer;

pub use peer::{CHANNEL_LABEL, CHECK_EVERY, SILENCE_LIMIT};
