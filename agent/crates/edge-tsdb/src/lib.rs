//! The agent-local multi-tier persistent time-series store [`store::LocalTsdb`] on `redb`.
//! The `bakeoff` feature adds the comparison substrates, which the shipped agent omits.

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
