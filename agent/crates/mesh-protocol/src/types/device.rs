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
    HardwareInventory,
    DeviceLogs,
    HealthWindow,
    /// Reconnect backfill + on-demand deep-history pull (WS-15). Gates the
    /// server → agent GrantBackfill/DeferBackfill/MetricBackfillAck/
    /// RequestLocalHistory control messages.
    Backfill,
    /// Non-intrusive auto-discovery profiling (WS-16). Advertised when the
    /// periodic discovery task is enabled; the agent ships tenant-scoped
    /// `DiscoveryReport`s (ports/services/DB engines/containers/packages).
    Discovery,
    /// Declarative edge threshold alerts (WS-19). Advertised when the sampler is
    /// enabled; gates the server → agent `PushAlertRules` control message that
    /// delivers the connecting agent's tenant-scoped ruleset.
    ThresholdAlerts,
}

/// Current status of a device.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[non_exhaustive]
pub enum DeviceStatus {
    Online,
    Offline,
    Connecting,
}
