//! Non-intrusive, read-only host auto-discovery (WS-16).
//!
//! Profiles the host — listening ports, host services, database engines,
//! containers, and installed packages — into a bounded, secret-free
//! [`DiscoveryProfile`] that serializes to a `DiscoveryReport` control message.
//! Every collector is OS/localhost introspection only (no WMI, no network
//! scanning), caps its own output, and no-ops (returns empty) on a platform
//! where its source is absent, so one call is safe on every fleet host. The
//! payload carries engine/port/version and package/service names only — never a
//! connection string, credential, or bound address.

pub mod containers;
pub mod db_engines;
pub mod packages;
pub mod ports;
pub mod services;

use std::collections::hash_map::DefaultHasher;
use std::hash::{Hash, Hasher};

use mesh_protocol::{
    ControlMessage, DiscoveredContainer, DiscoveredDbEngine, DiscoveredPackage, DiscoveredPort,
    DiscoveredService,
};

/// Per-category output caps. A busy host cannot explode the report or the
/// central inventory table; [`DiscoveryProfile::truncated`] flags any category
/// that was capped.
pub const MAX_PORTS: usize = 256;
/// Cap on host services in one report.
pub const MAX_SERVICES: usize = 512;
/// Cap on inferred database engines in one report.
pub const MAX_DB_ENGINES: usize = 64;
/// Cap on containers in one report.
pub const MAX_CONTAINERS: usize = 256;
/// Cap on installed packages in one report.
pub const MAX_PACKAGES: usize = 4096;

/// A complete, bounded host profile ready to serialize into a `DiscoveryReport`.
/// Categories are individually capped and deterministically ordered so the
/// [`DiscoveryProfile::fingerprint`] used for change-triggered re-profiling is
/// stable across collections that observed the same host state.
#[derive(Debug, Clone, Default, PartialEq)]
#[non_exhaustive]
pub struct DiscoveryProfile {
    /// Listening TCP/UDP ports with their owning process basenames.
    pub ports: Vec<DiscoveredPort>,
    /// Host services (systemd units / Windows services) and their run states.
    pub services: Vec<DiscoveredService>,
    /// Database engines inferred from listening ports.
    pub db_engines: Vec<DiscoveredDbEngine>,
    /// Containers reported by a local runtime.
    pub containers: Vec<DiscoveredContainer>,
    /// Installed OS packages.
    pub packages: Vec<DiscoveredPackage>,
    /// Set when any category hit its cap and was truncated.
    pub truncated: bool,
}

/// Truncates `values` to `max` in place, returning whether anything was dropped.
fn cap<T>(values: &mut Vec<T>, max: usize) -> bool {
    if values.len() > max {
        values.truncate(max);
        true
    } else {
        false
    }
}

impl DiscoveryProfile {
    /// Applies the per-category caps, recording whether any category was
    /// truncated. Idempotent: a profile already within the caps is unchanged.
    fn apply_caps(&mut self) {
        let mut truncated = false;
        truncated |= cap(&mut self.ports, MAX_PORTS);
        truncated |= cap(&mut self.services, MAX_SERVICES);
        truncated |= cap(&mut self.db_engines, MAX_DB_ENGINES);
        truncated |= cap(&mut self.containers, MAX_CONTAINERS);
        truncated |= cap(&mut self.packages, MAX_PACKAGES);
        self.truncated = truncated;
    }

    /// A stable content fingerprint used to suppress unchanged reports. Two
    /// profiles describing the same host state hash equal regardless of when
    /// they were collected (the wall-clock timestamp is not part of the
    /// profile), so the periodic task only ships a report when the host changed.
    pub fn fingerprint(&self) -> u64 {
        let mut hasher = DefaultHasher::new();
        for port in &self.ports {
            port.proto.hash(&mut hasher);
            port.port.hash(&mut hasher);
            port.process.hash(&mut hasher);
        }
        for service in &self.services {
            service.name.hash(&mut hasher);
            service.state.hash(&mut hasher);
        }
        for engine in &self.db_engines {
            engine.engine.hash(&mut hasher);
            engine.version.hash(&mut hasher);
            engine.port.hash(&mut hasher);
        }
        for container in &self.containers {
            container.runtime.hash(&mut hasher);
            container.image.hash(&mut hasher);
            container.name.hash(&mut hasher);
            container.state.hash(&mut hasher);
        }
        for package in &self.packages {
            package.name.hash(&mut hasher);
            package.version.hash(&mut hasher);
        }
        self.truncated.hash(&mut hasher);
        hasher.finish()
    }

    /// Serializes the profile into a `DiscoveryReport` control message stamped
    /// with `ts`. The server assigns the authoritative org, so `org_id` is left
    /// empty (the agent never asserts a tenant).
    pub fn into_report(self, ts: i64) -> ControlMessage {
        ControlMessage::DiscoveryReport {
            ts,
            org_id: String::new(),
            ports: self.ports,
            services: self.services,
            db_engines: self.db_engines,
            containers: self.containers,
            packages: self.packages,
            truncated: self.truncated,
        }
    }
}

/// Runs every collector once and assembles a bounded [`DiscoveryProfile`]. Each
/// collector independently no-ops on an unsupported platform, so this returns a
/// partial (possibly empty) profile rather than failing. Database engines are
/// inferred from the collected ports, so no additional host access is needed.
pub fn collect_profile() -> DiscoveryProfile {
    let ports = ports::collect_ports();
    let db_engines = db_engines::infer_db_engines(&ports);
    let mut profile = DiscoveryProfile {
        ports,
        services: services::collect_services(),
        db_engines,
        containers: containers::collect_containers(),
        packages: packages::collect_packages(),
        truncated: false,
    };
    profile.apply_caps();
    profile
}

#[cfg(test)]
mod tests {
    use super::*;

    fn sample_profile() -> DiscoveryProfile {
        DiscoveryProfile {
            ports: vec![DiscoveredPort {
                proto: "tcp".into(),
                port: 5432,
                process: "postgres".into(),
            }],
            services: vec![DiscoveredService {
                name: "nginx.service".into(),
                state: "running".into(),
            }],
            db_engines: vec![DiscoveredDbEngine {
                engine: "postgres".into(),
                version: String::new(),
                port: 5432,
            }],
            containers: vec![DiscoveredContainer {
                runtime: "docker".into(),
                image: "redis:7".into(),
                name: "cache".into(),
                state: "running".into(),
            }],
            packages: vec![DiscoveredPackage {
                name: "openssl".into(),
                version: "3.0.13".into(),
            }],
            truncated: false,
        }
    }

    /// The wall-clock timestamp is not part of the profile, so two reports of
    /// the same host state at different times share a fingerprint (the
    /// change-detection invariant) yet still carry their own `ts`.
    #[test]
    fn fingerprint_is_timestamp_independent() {
        let profile = sample_profile();
        let fp = profile.clone().fingerprint();
        let report_a = profile.clone().into_report(1000);
        let report_b = profile.clone().into_report(2000);
        assert_eq!(fp, sample_profile().fingerprint());
        match (report_a, report_b) {
            (
                ControlMessage::DiscoveryReport { ts: ts_a, .. },
                ControlMessage::DiscoveryReport { ts: ts_b, .. },
            ) => {
                assert_eq!(ts_a, 1000);
                assert_eq!(ts_b, 2000);
            }
            _ => panic!("expected DiscoveryReport"),
        }
    }

    /// Any content change moves the fingerprint, so a changed host re-ships.
    #[test]
    fn fingerprint_changes_with_content() {
        let base = sample_profile().fingerprint();
        let mut changed = sample_profile();
        changed.services[0].state = "failed".into();
        assert_ne!(base, changed.fingerprint());
    }

    /// Over-cap categories are truncated and the report is flagged truncated.
    #[test]
    fn apply_caps_truncates_and_flags() {
        let mut profile = DiscoveryProfile {
            packages: (0..MAX_PACKAGES + 10)
                .map(|i| DiscoveredPackage {
                    name: format!("pkg{i}"),
                    version: "1".into(),
                })
                .collect(),
            ..Default::default()
        };
        profile.apply_caps();
        assert_eq!(profile.packages.len(), MAX_PACKAGES);
        assert!(profile.truncated, "hitting a cap sets truncated");
    }

    /// A profile within every cap is not flagged truncated.
    #[test]
    fn apply_caps_within_bounds_not_flagged() {
        let mut profile = sample_profile();
        profile.apply_caps();
        assert!(!profile.truncated);
    }

    /// The report leaves `org_id` empty (the server assigns the tenant) and
    /// carries every category through unchanged.
    #[test]
    fn into_report_leaves_org_empty_and_preserves_categories() {
        let report = sample_profile().into_report(1_700_000_000);
        match report {
            ControlMessage::DiscoveryReport {
                ts,
                org_id,
                ports,
                services,
                db_engines,
                containers,
                packages,
                truncated,
            } => {
                assert_eq!(ts, 1_700_000_000);
                assert!(org_id.is_empty(), "agent must not assert an org");
                assert_eq!(ports.len(), 1);
                assert_eq!(services.len(), 1);
                assert_eq!(db_engines.len(), 1);
                assert_eq!(containers.len(), 1);
                assert_eq!(packages.len(), 1);
                assert!(!truncated);
            }
            _ => panic!("expected DiscoveryReport"),
        }
    }

    /// `collect_profile` never panics and returns a within-caps profile on any
    /// host (categories may be empty when their source is absent, e.g. in CI).
    #[test]
    fn collect_profile_is_bounded_and_safe() {
        let profile = collect_profile();
        assert!(profile.ports.len() <= MAX_PORTS);
        assert!(profile.services.len() <= MAX_SERVICES);
        assert!(profile.db_engines.len() <= MAX_DB_ENGINES);
        assert!(profile.containers.len() <= MAX_CONTAINERS);
        assert!(profile.packages.len() <= MAX_PACKAGES);
    }
}
