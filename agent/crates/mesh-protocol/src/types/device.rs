//! Device and group identity types.

use serde::{Deserialize, Serialize};

/// Unique identifier for a device/agent.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub struct DeviceId(pub uuid::Uuid);

impl DeviceId {
    /// Create a new random DeviceId.
    pub fn new() -> Self {
        Self(uuid::Uuid::new_v4())
    }
}

impl Default for DeviceId {
    fn default() -> Self {
        Self::new()
    }
}

/// Unique identifier for a device group.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub struct GroupId(pub uuid::Uuid);

impl GroupId {
    /// Create a new random GroupId.
    pub fn new() -> Self {
        Self(uuid::Uuid::new_v4())
    }
}

impl Default for GroupId {
    fn default() -> Self {
        Self::new()
    }
}

/// Capabilities an agent can advertise.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[non_exhaustive]
pub enum AgentCapability {
    RemoteDesktop,
    Terminal,
    FileManager,
    InputInjection,
    ProcessManager,
}

/// Current status of a device.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[non_exhaustive]
pub enum DeviceStatus {
    Online,
    Offline,
    Connecting,
}
