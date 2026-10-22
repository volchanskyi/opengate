//! Application error codes a QUIC connection between agent and server closes with.

/// A normal close by either side.
pub const BYE: u32 = 0;
/// The server could not accept the control stream.
pub const STREAM_ACCEPT_FAILED: u32 = 1;
/// The server refused the handshake; on the fast path this means a stale master-key fingerprint.
pub const HANDSHAKE_REFUSED: u32 = 2;
/// The device was deleted; the agent treats it exactly like an `AgentDeregistered` frame.
pub const DEVICE_DELETED: u32 = 3;
/// A restricted session ended, because it renewed or its time ran out.
pub const RESTRICTED_SESSION_ENDED: u32 = 4;
/// The certificate was refused: too old, superseded, unvouched, or never issued by the server.
pub const CERTIFICATE_REFUSED: u32 = 5;
/// A newer connection for the same device replaced this one.
pub const REPLACED_BY_NEWER_CONNECTION: u32 = 6;
