use std::path::PathBuf;

/// Configuration for the agent's connection to the server.
#[derive(Debug, Clone, serde::Deserialize)]
pub struct AgentConfig {
    /// The QUIC address of the agentapi server, e.g. "192.168.1.1:9090".
    pub server_addr: String,
    /// The server's CA certificate in PEM format.
    pub server_ca_pem: String,
    /// Directory where agent identity files are stored.
    pub data_dir: PathBuf,
}
