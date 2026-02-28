/// Errors that can occur during protocol operations.
#[derive(Debug, thiserror::Error)]
#[non_exhaustive]
pub enum ProtocolError {
    /// Frame exceeds maximum allowed size.
    #[error("frame too large: {size} bytes (max {max})")]
    FrameTooLarge { size: usize, max: usize },

    /// Unknown frame type byte encountered.
    #[error("unknown frame type: 0x{0:02x}")]
    UnknownFrameType(u8),

    /// Not enough data to decode a complete frame.
    #[error("incomplete frame: need {needed} more bytes")]
    IncompleteFrame { needed: usize },

    /// MessagePack encoding/decoding error.
    #[error("msgpack error: {0}")]
    MsgpackEncode(#[from] rmp_serde::encode::Error),

    /// MessagePack decoding error.
    #[error("msgpack decode error: {0}")]
    MsgpackDecode(#[from] rmp_serde::decode::Error),

    /// Invalid handshake message.
    #[error("invalid handshake: {0}")]
    InvalidHandshake(String),

    /// Invalid session token format.
    #[error("invalid session token")]
    InvalidSessionToken,
}
