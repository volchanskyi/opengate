//! # edge-tsdb — the agent-local multi-tier persistent time-series store.
//!
//! Because central VictoriaMetrics keeps `avg` only (the cardinality decision),
//! this store is the **sole** home for each device's min/max/last + 1 s raw
//! history — load-bearing, and fetched on demand by WS-15. It was chosen and
//! de-risked by the WS-14a bake-off (ADR-051) and built in WS-14b.
//!
//! ## Production surface (always compiled)
//!
//! - [`store::LocalTsdb`] — the multi-tier store (T0 1 s raw + anomaly bits,
//!   T1 1 min, T2 1 hr) on `redb`, one atomic transaction per commit, with a
//!   durable backfill cursor, coarsest-first disk-cap eviction, format
//!   migration, MVCC snapshot reads, and a deprovision purge.
//! - [`compact`] — the block codec: fixed-point-per-metric (lossless to 1/scale)
//!   or adaptive float32, implicit fixed-step timestamps, and an inline anomaly
//!   bit.
//! - [`tier`] — the min/max/avg/last rollups, keyed by sample timestamp.
//! - [`deflate`] — optional cold-tier DEFLATE for sealed T1/T2 blocks.
//!
//! ## Bake-off reference (behind the `bakeoff` feature)
//!
//! The WS-14a comparison substrates — bespoke [`append_only`] files (A) and the
//! no-persist [`baseline`] (C), plus the small-block [`redb_store`] and
//! big-block [`redb_compact`] references — are retained behind the `bakeoff`
//! feature as the measured off-ramp reference and the `cargo bench` corpus. They
//! are not compiled into the shipped agent.

pub mod bitio;
pub mod compact;
pub mod config;
#[cfg(feature = "cold-deflate")]
pub mod deflate;
pub mod error;
pub mod gorilla;
pub mod sample;
pub mod store;
pub mod tier;

pub mod corpus;

#[cfg(feature = "bakeoff")]
pub mod append_only;
#[cfg(feature = "bakeoff")]
pub mod baseline;
#[cfg(feature = "bakeoff")]
pub mod crc;
#[cfg(feature = "bakeoff")]
pub mod fault;
#[cfg(feature = "bakeoff")]
pub mod frame;
#[cfg(feature = "bakeoff")]
pub mod redb_backend;
#[cfg(feature = "bakeoff")]
pub mod redb_compact;
#[cfg(feature = "bakeoff")]
pub mod redb_store;
#[cfg(feature = "bakeoff")]
pub mod substrate;

pub use config::{Durability, TsdbConfig};
pub use error::{Result, TsdbError};
pub use sample::{Sample, SeriesId};
pub use store::{LocalTsdb, Tier};

#[cfg(feature = "bakeoff")]
pub use substrate::Substrate;
