//! Tests for the maintenance gate shared with collectors and the exit-edge tracker.

use mesh_agent_core::maintenance::{MaintenanceGate, MaintenanceTransition};

#[test]
fn gate_defaults_to_active() {
    let gate = MaintenanceGate::new();
    assert!(
        !gate.in_maintenance(),
        "a fresh gate defaults to Active (not in maintenance)"
    );
}

#[test]
fn gate_set_toggles_state() {
    let gate = MaintenanceGate::new();
    gate.set(true);
    assert!(gate.in_maintenance(), "set(true) enters maintenance");
    gate.set(false);
    assert!(!gate.in_maintenance(), "set(false) returns to Active");
}

#[test]
fn gate_clones_share_state() {
    let gate = MaintenanceGate::new();
    let collector_view = gate.clone();
    gate.set(true);
    assert!(
        collector_view.in_maintenance(),
        "a clone observes a flip through the original handle"
    );
    collector_view.set(false);
    assert!(
        !gate.in_maintenance(),
        "a flip through the clone is observed by the original"
    );
}

#[test]
fn transition_no_exit_while_active() {
    let mut t = MaintenanceTransition::new();
    assert!(!t.just_exited(false), "staying Active is not an exit");
    assert!(!t.just_exited(false), "still Active is not an exit");
}

#[test]
fn transition_entering_is_not_an_exit() {
    let mut t = MaintenanceTransition::new();
    assert!(
        !t.just_exited(true),
        "Active→maintenance is an entry, not an exit"
    );
    assert!(
        !t.just_exited(true),
        "staying in maintenance is not an exit"
    );
}

#[test]
fn transition_reports_exit_once() {
    let mut t = MaintenanceTransition::new();
    t.just_exited(true);
    assert!(
        t.just_exited(false),
        "maintenance→Active is the re-baseline edge"
    );
    assert!(
        !t.just_exited(false),
        "the exit edge fires once, not on every subsequent Active tick"
    );
}

#[test]
fn transition_detects_repeated_cycles() {
    let mut t = MaintenanceTransition::new();
    t.just_exited(true);
    assert!(t.just_exited(false), "first exit");
    t.just_exited(true);
    assert!(t.just_exited(false), "second cycle exit re-fires");
}
