//! Server-pushed maintenance state shared by the control loop and collectors, which suppress work
//! while it is on; the control channel and remote management stay live.

use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;

/// Shared maintenance flag: the control loop sets it and each collector holds a clone to read it.
#[derive(Clone, Debug, Default)]
pub struct MaintenanceGate {
    on: Arc<AtomicBool>,
}

impl MaintenanceGate {
    /// Creates a gate in the Active state.
    pub fn new() -> Self {
        Self::default()
    }

    /// Sets the maintenance state: `true` enters maintenance, `false` returns to Active.
    pub fn set(&self, enabled: bool) {
        self.on.store(enabled, Ordering::Relaxed);
    }

    /// Reports whether the device is in maintenance.
    pub fn in_maintenance(&self) -> bool {
        self.on.load(Ordering::Relaxed)
    }
}

/// Detects the maintenance→Active edge, where the sampler re-baselines anomaly detection.
#[derive(Clone, Debug, Default)]
pub struct MaintenanceTransition {
    was_in_maintenance: bool,
}

impl MaintenanceTransition {
    /// Creates a tracker starting in the Active state.
    pub fn new() -> Self {
        Self::default()
    }

    /// Records the current state and returns `true` only on a maintenance→Active transition.
    pub fn just_exited(&mut self, in_maintenance: bool) -> bool {
        let exited = self.was_in_maintenance && !in_maintenance;
        self.was_in_maintenance = in_maintenance;
        exited
    }
}
