//! Maintenance-mode gate shared between the agent control loop and the
//! Edge-Sentinel collectors.
//!
//! Maintenance is a server-authoritative desired state pushed over the control
//! channel. While in maintenance the sampler and discovery collectors suppress
//! all work — no sampling, local-store writes, discovery sweeps, or alert-breach
//! evaluation — so an admin's disruptive host work never pollutes the anomaly
//! baseline or pages anyone. The QUIC control channel and the remote-management
//! paths stay live so the server can still tell the agent to leave maintenance
//! and the admin can keep working through the agent.

use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;

/// Shared maintenance-mode gate. The control loop flips it on `SetMaintenanceMode`
/// (and clears it to Active on every registration); each collector holds a clone
/// and consults [`in_maintenance`](Self::in_maintenance) to decide whether to
/// suppress its work this cycle.
#[derive(Clone, Debug, Default)]
pub struct MaintenanceGate {
    on: Arc<AtomicBool>,
}

impl MaintenanceGate {
    /// A fresh gate in the Active (not-in-maintenance) state.
    pub fn new() -> Self {
        Self::default()
    }

    /// Set the desired maintenance state: `true` enters maintenance (collectors
    /// suppress), `false` returns to Active (collectors resume).
    pub fn set(&self, enabled: bool) {
        self.on.store(enabled, Ordering::Relaxed);
    }

    /// True when the device is in maintenance and collectors must suppress.
    pub fn in_maintenance(&self) -> bool {
        self.on.load(Ordering::Relaxed)
    }
}

/// Detects the maintenance→Active edge for the sampler. Leaving maintenance is
/// the moment to re-baseline anomaly detection so the post-change footprint
/// becomes the new normal instead of alerting on every intended change.
#[derive(Clone, Debug, Default)]
pub struct MaintenanceTransition {
    was_in_maintenance: bool,
}

impl MaintenanceTransition {
    /// A tracker starting from the Active state.
    pub fn new() -> Self {
        Self::default()
    }

    /// Record the current maintenance state and return `true` exactly on a
    /// maintenance→Active transition — the tick on which the caller should
    /// re-baseline. Entering maintenance, staying in either state, and staying
    /// Active all return `false`.
    pub fn just_exited(&mut self, in_maintenance: bool) -> bool {
        let exited = self.was_in_maintenance && !in_maintenance;
        self.was_in_maintenance = in_maintenance;
        exited
    }
}
