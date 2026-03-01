/// Errors from the agent identity layer.
#[derive(Debug, thiserror::Error)]
#[non_exhaustive]
pub enum AgentError {
    /// I/O error during identity load/save.
    #[error("I/O error: {0}")]
    Io(#[from] std::io::Error),

    /// Certificate generation failed.
    #[error("certificate generation failed: {0}")]
    CertGen(String),

    /// Protocol-level error.
    #[error("protocol error: {0}")]
    Protocol(#[from] mesh_protocol::ProtocolError),
}

/// Errors from the connection layer.
#[derive(Debug, thiserror::Error)]
#[non_exhaustive]
pub enum ConnectionError {
    /// QUIC connection error.
    #[error("QUIC connection error: {0}")]
    Quic(String),

    /// Handshake failed.
    #[error("handshake failed: {0}")]
    Handshake(String),

    /// I/O error.
    #[error("I/O error: {0}")]
    Io(#[from] std::io::Error),

    /// Protocol-level error.
    #[error("protocol error: {0}")]
    Protocol(#[from] mesh_protocol::ProtocolError),

    /// Server rejected the registration.
    #[error("server rejected registration")]
    RegistrationRejected,
}
