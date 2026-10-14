//! The direct link's rules, free of I/O: pieces, the hand-off between relay and link, and the
//! host's bound on the candidates it checks.

pub mod candidates;
pub mod handoff;
pub mod pieces;

pub use candidates::CandidateFilter;
pub use handoff::{Config, Handoff, Instruction, LeaveReason, Outcome, Path};
pub use pieces::{Reassembler, ReassemblyError, MAX_LINK_FRAME, MAX_PIECE};
