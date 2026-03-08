/// Errors from session handling.
#[derive(Debug, thiserror::Error)]
#[non_exhaustive]
pub enum SessionError {
    /// WebSocket connection error.
    #[error("WebSocket error: {0}")]
    WebSocket(String),

    /// Protocol-level error.
    #[error("protocol error: {0}")]
    Protocol(#[from] mesh_protocol::ProtocolError),

    /// Screen capture error.
    #[error("capture error: {0}")]
    Capture(#[from] crate::platform::CaptureError),

    /// I/O error.
    #[error("I/O error: {0}")]
    Io(#[from] std::io::Error),

    /// Permission denied for the requested operation.
    #[error("permission denied: {0}")]
    PermissionDenied(String),

    /// Terminal error.
    #[error("terminal error: {0}")]
    Terminal(String),
}

impl From<tokio_tungstenite::tungstenite::Error> for SessionError {
    fn from(e: tokio_tungstenite::tungstenite::Error) -> Self {
        Self::WebSocket(e.to_string())
    }
}
