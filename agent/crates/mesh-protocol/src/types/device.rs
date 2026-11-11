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
    /// Gates the server → agent backfill grant, defer and ack control messages and
    /// `RequestLocalHistory`.
    Backfill,
    /// Gates the agent → server `DiscoveryReport`, the read-only host discovery profile.
    Discovery,
    /// Gates the server → agent `PushAlertRules` message carrying the agent's tenant-scoped
    /// ruleset.
    ThresholdAlerts,
    /// Gates the agent → server `AgentAlert` transport, which carries the evidence.
    Alerts,
    /// The direct link takes a channel per job (typing, screen, files) as well as one shared
    /// channel; a browser opens the three only for an agent that lists this.
    DirectChannels,
    /// Gates the server → agent `AlertAnswer`, which tells the agent what became of each alert.
    AlertAnswers,
    /// Gates the server → agent `LiveWindowStored`, which confirms each stored live window.
    LiveConfirmations,
    /// Gates a `DeferBackfill` carrying `since_ts`, which asks the agent to send history again.
    ResendRequests,
}

/// Current status of a device.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[non_exhaustive]
pub enum DeviceStatus {
    Online,
    Offline,
    Connecting,
}
