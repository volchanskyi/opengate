//! Ranking which of a machine's own dimensions broke pattern.
//!
//! When a rule fires on a file server, the useful question is not "what
//! crossed a line" — the rule already said that — but "what else changed at the
//! same time". This module answers it against the agent's own full-resolution
//! history: it compares each host dimension's readings inside the event window
//! against the stretch before it, and orders them by how badly each broke
//! pattern. The result travels with the alert, so the technician who opens the
//! incident already reads `disk.await_ms 0.91, cpu.total 0.84` — nothing was
//! asked of the operator, and nothing was asked of the network.
//!
//! Running it here is also the only place it can see the detail: the agent
//! keeps 1 s readings locally, while what reaches the centre is a 60 s average
//! per dimension. A ten-second I/O collapse is the whole story on the machine
//! and is barely a bump by the time it is averaged.
//!
//! Three signals decide the order, blended into one score in `[0, 1]`:
//! how much the distribution changed shape, how many focus readings fell
//! outside the baseline's normal band, and how far the mean moved relative to
//! the baseline's own scale. The third is what keeps a service time that went
//! from 0.40 ms to 0.44 ms from outranking one that went from 0.4 ms to 40 ms —
//! the first two saturate on any clean separation, however small.
//!
//! Every part of the run is bounded ([`CorrelationLimits`]): how many
//! dimensions are looked at, how many readings each window carries, and how
//! long the whole thing may take. A rule firing repeatedly on a machine that is
//! already in trouble is exactly when this code runs, so it can never be the
//! reason the machine got worse.

mod ks;
mod rank;
mod window;

pub use ks::{anomaly_rate, ks_statistic, mean_std_dev, shift_magnitude};
pub use rank::{rank_dimensions, CorrelationLimits, DimWindows, Ranked, Ranking};
pub use window::{correlate_snapshot, CorrelationWindow};
