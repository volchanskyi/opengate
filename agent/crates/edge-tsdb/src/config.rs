//! Store configuration and the durability contract, compiled without the `bakeoff` feature.

/// Durability requested at commit time.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
#[non_exhaustive]
pub enum Durability {
    /// Flush and fsync, so the commit survives power loss.
    Full,
    /// Buffer only; a later `Full` commit makes it durable.
    None,
}

/// Footprint and precision policy for a [`LocalTsdb`](crate::store::LocalTsdb); the effective
/// cap is `min(cap_bytes, host_free × host_free_fraction)`.
#[derive(Debug, Clone, Copy)]
pub struct TsdbConfig {
    /// Hard upper bound on the store's on-disk footprint, in bytes.
    pub cap_bytes: u64,
    /// Fraction of free host disk the cap may borrow against; `0.0` uses `cap_bytes` alone.
    pub host_free_fraction: f64,
    /// Fixed-point scale for series without a [`set_scale`](crate::store::LocalTsdb::set_scale);
    /// `None` selects the adaptive float32 path.
    pub default_scale: Option<i64>,
}

impl TsdbConfig {
    /// The cap in force given `host_free` bytes; `None` or a zero fraction yields `cap_bytes`.
    /// Callers that act before eviction read their threshold here so both agree.
    #[must_use]
    pub fn effective_cap(&self, host_free: Option<u64>) -> u64 {
        match host_free {
            Some(free) if self.host_free_fraction > 0.0 => {
                let borrow = (free as f64 * self.host_free_fraction) as u64;
                self.cap_bytes.min(borrow)
            }
            _ => self.cap_bytes,
        }
    }
}

impl Default for TsdbConfig {
    fn default() -> Self {
        Self {
            cap_bytes: u64::MAX,
            host_free_fraction: 0.05,
            default_scale: None,
        }
    }
}
