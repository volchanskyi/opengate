//! Hardware inventory types reported by the agent.

use serde::{Deserialize, Serialize};

/// Network interface information reported by the agent.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct NetworkInterface {
    /// Interface name (e.g. "eth0", "wlan0").
    pub name: String,
    /// MAC address.
    pub mac: String,
    /// IPv4 addresses assigned to this interface.
    pub ipv4: Vec<String>,
    /// IPv6 addresses assigned to this interface.
    pub ipv6: Vec<String>,
}
