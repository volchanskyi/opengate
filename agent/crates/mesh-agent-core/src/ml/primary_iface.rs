//! Primary (default-route) network-interface resolver.
//!
//! The net telemetry dims report throughput on the host's *primary* interface —
//! the one carrying the default route — rather than a sum across every
//! interface. This module resolves that interface, degrading silently where the
//! platform has no supported lookup: on Linux it reads the default route from
//! `/proc/net/route`; everywhere else (and when the route is unavailable) it
//! falls back to the busiest non-loopback interface. When only loopback exists
//! it yields `None`, and the caller emits no net sample rather than a wrong one.

use sysinfo::Networks;

/// Parse the default-route interface from `/proc/net/route` contents. The
/// default route is the row whose hex destination is `00000000` (0.0.0.0); its
/// interface name is the first whitespace-delimited column. Returns the first
/// matching interface, or `None` when the table has no default route.
#[must_use]
pub fn default_route_iface(route_contents: &str) -> Option<String> {
    for line in route_contents.lines().skip(1) {
        let mut cols = line.split_whitespace();
        let iface = cols.next()?;
        let Some(dest) = cols.next() else { continue };
        if dest == "00000000" {
            return Some(iface.to_string());
        }
    }
    None
}

/// Whether an interface name denotes a loopback device (excluded from the
/// busiest-interface fallback so idle hosts never report loopback traffic).
#[must_use]
pub fn is_loopback(name: &str) -> bool {
    name == "lo" || name.to_ascii_lowercase().contains("loopback")
}

/// The busiest non-loopback interface from `(name, total_bytes)` pairs, used as
/// the fallback when the default route is unavailable. `None` when every
/// interface is loopback (or the iterator is empty).
#[must_use]
pub fn busiest_iface<'a>(ifaces: impl Iterator<Item = (&'a str, u64)>) -> Option<String> {
    ifaces
        .filter(|(name, _)| !is_loopback(name))
        .max_by_key(|(_, traffic)| *traffic)
        .map(|(name, _)| name.to_string())
}

/// Resolve the primary interface against a live `sysinfo::Networks` snapshot:
/// the default route when it names an interface `sysinfo` is tracking, else the
/// busiest non-loopback interface, else `None`.
#[must_use]
pub fn resolve_primary_iface(networks: &Networks) -> Option<String> {
    #[cfg(target_os = "linux")]
    {
        if let Ok(contents) = std::fs::read_to_string("/proc/net/route") {
            if let Some(iface) = default_route_iface(&contents) {
                if networks.iter().any(|(name, _)| name.as_str() == iface) {
                    return Some(iface);
                }
            }
        }
    }
    busiest_iface(networks.iter().map(|(name, data)| {
        (
            name.as_str(),
            data.total_received()
                .saturating_add(data.total_transmitted()),
        )
    }))
}

#[cfg(test)]
mod tests {
    use super::*;

    // A trimmed `/proc/net/route` with a default route (destination 00000000) on
    // eth0 and a specific-subnet route on eth1.
    const ROUTE_WITH_DEFAULT: &str = "\
Iface\tDestination\tGateway\tFlags\tRefCnt\tUse\tMetric\tMask\tMTU\tWindow\tIRTT
eth1\t0000A8C0\t00000000\t0001\t0\t0\t0\t00FFFFFF\t0\t0\t0
eth0\t00000000\t0100A8C0\t0003\t0\t0\t0\t00000000\t0\t0\t0";

    const ROUTE_NO_DEFAULT: &str = "\
Iface\tDestination\tGateway\tFlags\tRefCnt\tUse\tMetric\tMask\tMTU\tWindow\tIRTT
eth1\t0000A8C0\t00000000\t0001\t0\t0\t0\t00FFFFFF\t0\t0\t0";

    #[test]
    fn default_route_picks_the_zero_destination_iface() {
        assert_eq!(
            default_route_iface(ROUTE_WITH_DEFAULT).as_deref(),
            Some("eth0")
        );
    }

    #[test]
    fn no_default_route_yields_none() {
        assert_eq!(default_route_iface(ROUTE_NO_DEFAULT), None);
    }

    #[test]
    fn empty_or_header_only_route_yields_none() {
        assert_eq!(default_route_iface(""), None);
        assert_eq!(default_route_iface("Iface\tDestination\tGateway"), None);
    }

    #[test]
    fn loopback_is_recognized_case_insensitively() {
        assert!(is_loopback("lo"));
        assert!(is_loopback("Loopback Pseudo-Interface 1"));
        assert!(!is_loopback("eth0"));
        assert!(!is_loopback("wlan0"));
    }

    #[test]
    fn busiest_iface_ignores_loopback_and_picks_the_max() {
        let ifaces = [("lo", 999_999), ("eth0", 100), ("wlan0", 5_000)];
        assert_eq!(busiest_iface(ifaces.into_iter()).as_deref(), Some("wlan0"));
    }

    #[test]
    fn busiest_iface_is_none_when_only_loopback_exists() {
        let ifaces = [("lo", 42)];
        assert_eq!(busiest_iface(ifaces.into_iter()), None);
        let empty: [(&str, u64); 0] = [];
        assert_eq!(busiest_iface(empty.into_iter()), None);
    }
}
