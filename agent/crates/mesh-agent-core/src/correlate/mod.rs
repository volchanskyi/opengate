//! Ranks the host dimensions whose 1 s local readings in an alert's event window broke pattern
//! against the stretch before it, within the bounds of [`CorrelationLimits`].

mod ks;
mod rank;
mod window;

pub use ks::{anomaly_rate, ks_statistic, mean_std_dev, shift_magnitude};
pub use rank::{rank_dimensions, CorrelationLimits, DimWindows, Ranked, Ranking};
pub use window::{correlate_snapshot, CorrelationWindow};
