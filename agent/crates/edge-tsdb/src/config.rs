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

impl Default for TsdbConfig {
    fn default() -> Self {
        Self {
            cap_bytes: u64::MAX,
            host_free_fraction: 0.05,
            default_scale: None,
        }
    }
}
