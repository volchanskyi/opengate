//! Listening-port discovery (WS-16).
//!
//! On Linux the collector reads `/proc/net/{tcp,tcp6,udp,udp6}` — listening TCP
//! sockets (state `0A`) and unconnected bound UDP sockets — and resolves each
//! socket's owning process basename by walking `/proc/[pid]/fd` for the matching
//! `socket:[inode]`. On Windows it parses `netstat -ano`. Both are read-only,
//! localhost-only introspection: no network scanning, no bound address ever
//! leaves the device — only the transport, port number, and process basename.

use std::collections::{HashMap, HashSet};

use mesh_protocol::DiscoveredPort;

/// Hex TCP state for a listening socket in `/proc/net/tcp{,6}`.
const TCP_LISTEN: &str = "0A";

/// One row parsed from a `/proc/net/*` table: the local port and the socket
/// inode used to map it back to an owning process.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(crate) struct ProcNetEntry {
    pub port: u16,
    pub inode: u64,
}

/// Parses the hex `IP:PORT` local-address column into a port number.
fn hex_local_port(addr: &str) -> Option<u16> {
    let (_, port) = addr.rsplit_once(':')?;
    u16::from_str_radix(port, 16).ok()
}

/// Parses the hex `IP:PORT` remote-address column into a port number.
fn hex_remote_port(addr: &str) -> Option<u16> {
    hex_local_port(addr)
}

/// Parses a `/proc/net/tcp{,6}` or `/proc/net/udp{,6}` table. For TCP only
/// sockets in the `LISTEN` state are kept; for UDP only unconnected bound
/// sockets (remote port `0`) are kept, which is the closest UDP analogue to a
/// listener. Malformed rows are skipped so one bad line never aborts the scan.
pub(crate) fn parse_proc_net(content: &str, is_tcp: bool) -> Vec<ProcNetEntry> {
    let mut out = Vec::new();
    for line in content.lines().skip(1) {
        let fields: Vec<&str> = line.split_whitespace().collect();
        // sl local rem st ... inode is field index 9.
        if fields.len() < 10 {
            continue;
        }
        let keep = if is_tcp {
            fields[3] == TCP_LISTEN
        } else {
            hex_remote_port(fields[2]) == Some(0)
        };
        if !keep {
            continue;
        }
        let (Some(port), Ok(inode)) = (hex_local_port(fields[1]), fields[9].parse::<u64>()) else {
            continue;
        };
        out.push(ProcNetEntry { port, inode });
    }
    out
}

/// Correlates parsed socket rows for one transport with an inode → process
/// basename map, de-duplicating by port (a socket bound on both IPv4 and IPv6
/// reports once). Ports whose inode is unresolved carry an empty process.
pub(crate) fn resolve_ports(
    entries: &[ProcNetEntry],
    proto: &str,
    inode_to_proc: &HashMap<u64, String>,
) -> Vec<DiscoveredPort> {
    let mut seen = HashSet::new();
    let mut out = Vec::new();
    for entry in entries {
        if !seen.insert(entry.port) {
            continue;
        }
        out.push(DiscoveredPort {
            proto: proto.to_string(),
            port: entry.port,
            process: inode_to_proc.get(&entry.inode).cloned().unwrap_or_default(),
        });
    }
    out
}

/// Parses a `socket:[12345]` symlink target into its inode number.
#[cfg(target_os = "linux")]
fn parse_socket_inode(target: &str) -> Option<u64> {
    let inner = target.strip_prefix("socket:[")?.strip_suffix(']')?;
    inner.parse::<u64>().ok()
}

/// Reads the most recent listening ports on the host, bounded and de-duplicated.
/// Returns an empty vector on any platform where the source is absent, so one
/// call is safe on every fleet host.
pub fn collect_ports() -> Vec<DiscoveredPort> {
    #[cfg(target_os = "linux")]
    {
        collect_ports_linux()
    }
    #[cfg(target_os = "windows")]
    {
        collect_ports_windows()
    }
    #[cfg(not(any(target_os = "linux", target_os = "windows")))]
    {
        Vec::new()
    }
}

/// Reads and correlates `/proc/net/*` with the live socket-inode → process map.
#[cfg(target_os = "linux")]
fn collect_ports_linux() -> Vec<DiscoveredPort> {
    let read = |path: &str| std::fs::read_to_string(path).unwrap_or_default();
    let mut tcp = parse_proc_net(&read("/proc/net/tcp"), true);
    tcp.extend(parse_proc_net(&read("/proc/net/tcp6"), true));
    let mut udp = parse_proc_net(&read("/proc/net/udp"), false);
    udp.extend(parse_proc_net(&read("/proc/net/udp6"), false));

    let inodes: HashSet<u64> = tcp.iter().chain(udp.iter()).map(|e| e.inode).collect();
    let inode_to_proc = build_inode_proc_map(&inodes);

    let mut out = resolve_ports(&tcp, "tcp", &inode_to_proc);
    out.extend(resolve_ports(&udp, "udp", &inode_to_proc));
    out
}

/// Builds a socket-inode → process-basename map by walking `/proc/[pid]/fd`.
/// Only inodes in `wanted` are recorded, and the scan stops early once every
/// wanted inode is resolved, so a busy host does not pay for a full fd sweep.
#[cfg(target_os = "linux")]
fn build_inode_proc_map(wanted: &HashSet<u64>) -> HashMap<u64, String> {
    let mut map = HashMap::new();
    if wanted.is_empty() {
        return map;
    }
    let Ok(proc_dir) = std::fs::read_dir("/proc") else {
        return map;
    };
    for entry in proc_dir.flatten() {
        let pid_name = entry.file_name();
        let pid = pid_name.to_string_lossy();
        if !pid.chars().all(|c| c.is_ascii_digit()) {
            continue;
        }
        let comm = std::fs::read_to_string(entry.path().join("comm"))
            .map(|s| s.trim().to_string())
            .unwrap_or_default();
        let Ok(fds) = std::fs::read_dir(entry.path().join("fd")) else {
            continue;
        };
        for fd in fds.flatten() {
            let Ok(target) = std::fs::read_link(fd.path()) else {
                continue;
            };
            if let Some(inode) = parse_socket_inode(&target.to_string_lossy()) {
                if wanted.contains(&inode) {
                    map.entry(inode).or_insert_with(|| comm.clone());
                }
            }
        }
        if map.len() >= wanted.len() {
            break;
        }
    }
    map
}

/// Runs `netstat -ano` and parses listening TCP + bound UDP ports. Empty on any
/// failure path (missing binary, non-zero exit).
#[cfg(target_os = "windows")]
fn collect_ports_windows() -> Vec<DiscoveredPort> {
    let output = std::process::Command::new("netstat")
        .args(["-ano"])
        .output();
    match output {
        Ok(output) if output.status.success() => {
            parse_netstat(&String::from_utf8_lossy(&output.stdout))
        }
        _ => Vec::new(),
    }
}

/// Parses `netstat -ano` output into listening TCP and bound UDP ports. The
/// process column is a PID, which is not a basename, so `process` is left empty
/// (the port itself is the discovery signal). Exposed for tests on all
/// platforms.
#[cfg(any(target_os = "windows", test))]
pub(crate) fn parse_netstat(content: &str) -> Vec<DiscoveredPort> {
    let mut seen = HashSet::new();
    let mut out = Vec::new();
    for line in content.lines() {
        let fields: Vec<&str> = line.split_whitespace().collect();
        if fields.len() < 4 {
            continue;
        }
        let proto = fields[0].to_ascii_lowercase();
        let is_tcp = proto == "tcp";
        let is_udp = proto == "udp";
        if !is_tcp && !is_udp {
            continue;
        }
        // TCP listeners carry a LISTENING state in the 4th column; UDP rows have
        // no state and a `*:*` foreign address.
        if is_tcp && !fields.iter().any(|f| f.eq_ignore_ascii_case("LISTENING")) {
            continue;
        }
        let Some(port) = fields[1]
            .rsplit_once(':')
            .and_then(|(_, p)| p.parse::<u16>().ok())
        else {
            continue;
        };
        if !seen.insert((is_tcp, port)) {
            continue;
        }
        out.push(DiscoveredPort {
            proto: if is_tcp { "tcp" } else { "udp" }.to_string(),
            port,
            process: String::new(),
        });
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    /// A `/proc/net/tcp` table yields only the listening (state `0A`) sockets,
    /// with the local port decoded from hex and the inode captured.
    #[test]
    fn parse_proc_net_tcp_keeps_only_listeners() {
        // Port 0x1F90 = 8080 (LISTEN 0A) and 0x0016 = 22 (LISTEN); the middle
        // row is an ESTABLISHED (01) connection and must be dropped.
        let table = concat!(
            "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n",
            "   0: 00000000:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 12345 1 0000\n",
            "   1: 0100007F:8000 0100007F:1234 01 00000000:00000000 00:00000000 00000000  1000        0 22222 1 0000\n",
            "   2: 00000000:0016 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 33333 1 0000\n",
        );
        let entries = parse_proc_net(table, true);
        assert_eq!(entries.len(), 2, "only the two LISTEN rows survive");
        assert_eq!(
            entries[0],
            ProcNetEntry {
                port: 8080,
                inode: 12345
            }
        );
        assert_eq!(
            entries[1],
            ProcNetEntry {
                port: 22,
                inode: 33333
            }
        );
    }

    /// A `/proc/net/udp` table keeps only unconnected bound sockets (remote
    /// port 0); a connected UDP socket (remote port set) is dropped.
    #[test]
    fn parse_proc_net_udp_keeps_bound_sockets() {
        let table = concat!(
            "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n",
            "   0: 00000000:0035 00000000:0000 07 00000000:00000000 00:00000000 00000000     0        0 44444 2 0000\n",
            "   1: 0100007F:E1FF 08080808:0035 07 00000000:00000000 00:00000000 00000000  1000        0 55555 2 0000\n",
        );
        let entries = parse_proc_net(table, false);
        assert_eq!(entries.len(), 1, "connected UDP socket is excluded");
        assert_eq!(entries[0].port, 53, "0x0035 = 53 (DNS)");
        assert_eq!(entries[0].inode, 44444);
    }

    /// Malformed and short rows are skipped without aborting the scan.
    #[test]
    fn parse_proc_net_skips_malformed_rows() {
        let table = concat!(
            "header line ignored\n",
            "too few columns\n",
            "   0: NOTHEX:XXXX 00000000:0000 0A 0 0 0 0 0 66666 1 0000\n",
            "   1: 00000000:0050 00000000:0000 0A 00000000:00000000 00:00000000 00000000 0 0 77777 1 0000\n",
        );
        let entries = parse_proc_net(table, true);
        assert_eq!(entries.len(), 1, "only the one well-formed LISTEN row");
        assert_eq!(entries[0].port, 80);
    }

    /// Ports resolve their owning process via the inode map and de-duplicate by
    /// port; an unresolved inode yields an empty process, not a dropped row.
    #[test]
    fn resolve_ports_maps_process_and_dedups() {
        let entries = vec![
            ProcNetEntry {
                port: 5432,
                inode: 100,
            },
            ProcNetEntry {
                port: 5432,
                inode: 101,
            }, // dup port (v4 + v6)
            ProcNetEntry {
                port: 6379,
                inode: 999,
            }, // inode not in map
        ];
        let mut map = HashMap::new();
        map.insert(100u64, "postgres".to_string());
        let ports = resolve_ports(&entries, "tcp", &map);
        assert_eq!(ports.len(), 2, "duplicate port collapses to one");
        assert_eq!(ports[0].proto, "tcp");
        assert_eq!(ports[0].port, 5432);
        assert_eq!(ports[0].process, "postgres");
        assert_eq!(ports[1].port, 6379);
        assert!(
            ports[1].process.is_empty(),
            "unresolved inode → empty process"
        );
    }

    /// `netstat -ano` parsing keeps listening TCP and bound UDP ports and
    /// de-duplicates per transport; established/foreign rows are dropped.
    #[test]
    fn parse_netstat_keeps_listeners_and_udp() {
        let out = concat!(
            "\n  Proto  Local Address          Foreign Address        State           PID\n",
            "  TCP    0.0.0.0:445            0.0.0.0:0              LISTENING       4\n",
            "  TCP    10.0.0.5:52000         93.184.216.34:443     ESTABLISHED     1200\n",
            "  UDP    0.0.0.0:53             *:*                                   900\n",
            "  TCP    0.0.0.0:445            0.0.0.0:0              LISTENING       4\n",
        );
        let ports = parse_netstat(out);
        assert_eq!(ports.len(), 2, "one TCP listener (deduped) + one UDP");
        assert_eq!(ports[0].proto, "tcp");
        assert_eq!(ports[0].port, 445);
        assert!(ports[0].process.is_empty());
        assert_eq!(ports[1].proto, "udp");
        assert_eq!(ports[1].port, 53);
    }

    #[cfg(target_os = "linux")]
    #[test]
    fn parse_socket_inode_extracts_number() {
        assert_eq!(parse_socket_inode("socket:[12345]"), Some(12345));
        assert_eq!(parse_socket_inode("anon_inode:[eventpoll]"), None);
        assert_eq!(parse_socket_inode("/dev/null"), None);
    }
}
