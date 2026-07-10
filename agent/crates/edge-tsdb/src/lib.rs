//! # edge-tsdb — WS-14a local-TSDB substrate bake-off (spike)
//!
//! A local persistent time-series store is a storage-engine project, not a
//! feature slice. This crate is the evidence harness that decides which
//! substrate WS-14b builds on:
//!
//! - a deterministic [`corpus`] of realistic host telemetry, replayable in CI;
//! - a shared [`gorilla`] compression layer + [`tier`] rollups + [`frame`]
//!   framing (the bespoke tiering+compression layer reused by substrates A/B);
//! - three [`substrate`]s behind one trait — [`append_only`] (A, bespoke files),
//!   [`redb_store`] (B, redb), and [`baseline`] (C, no-persist control);
//! - a [`fault`] injection harness (torn write, bit-rot, disk-full, clock jump).
//!
//! Nothing in the shipped agent depends on this crate; it exists to produce the
//! measured acceptance-gate evidence recorded in ADR-051.

pub mod append_only;
pub mod baseline;
pub mod bitio;
pub mod compact;
pub mod corpus;
pub mod crc;
pub mod error;
pub mod fault;
pub mod frame;
pub mod gorilla;
pub mod redb_compact;
pub mod redb_store;
pub mod sample;
pub mod substrate;
pub mod tier;

pub use error::{Result, TsdbError};
pub use sample::{Sample, SeriesId};
pub use substrate::{Durability, Substrate};
