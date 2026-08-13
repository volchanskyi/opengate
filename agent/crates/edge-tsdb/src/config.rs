//! Production store configuration and the durability contract.
//!
//! Kept in its own always-compiled module so the shipped store surface does not
//! depend on the bake-off comparison layer (`substrate`, `append_only`, …),
//! which is gated behind the `bakeoff` feature.

/// Durability requested at commit time.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
#[non_exhaustive]
pub enum Durability {
    /// Flush and fsync — survives power loss. The bounded-loss boundary.
    Full,
    /// Buffer only; a later `Full` commit or clean close makes it durable. The
    /// fast path for hot writes, with a bounded loss window on crash.
    None,
}

/// Footprint and precision policy for a [`LocalTsdb`](crate::store::LocalTsdb).
///
/// The store is host-disk-courteous: it never fills the host disk and the agent
/// never crashes on a full disk. The effective cap is
/// `min(cap_bytes, host_free × host_free_fraction)` recomputed as the disk fills,
/// and eviction (coarsest tier first) keeps the store under it.
#[derive(Debug, Clone, Copy)]
pub struct TsdbConfig {
    /// Hard upper bound on the store's on-disk footprint, in bytes.
    pub cap_bytes: u64,
    /// Fraction of currently-free host disk the store may additionally borrow
    /// against. The effective cap is the smaller of `cap_bytes` and
    /// `free × host_free_fraction`. `0.0` disables the host-pressure backoff and
    /// uses `cap_bytes` alone.
    pub host_free_fraction: f64,
    /// Default per-metric fixed-point scale applied to series without an
    /// explicit [`set_scale`](crate::store::LocalTsdb::set_scale). `None` keeps
    /// the adaptive float32 / integer path (no fixed-point quantization).
    pub default_scale: Option<i64>,
}

impl TsdbConfig {
    /// The cap actually in force given `host_free` currently-free host bytes:
    /// the configured cap until the host disk gets tight, then the fraction of
    /// what is left. `None` (nobody has reported the disk) means the configured
    /// cap, and a zero fraction disables the backoff.
    ///
    /// Public because it is also the store's disk-pressure geometry: anything
    /// that must stand down *before* eviction changes what it keeps reads its
    /// own threshold from this function, so the two cannot drift apart.
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
