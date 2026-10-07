//! Listening-port discovery from `/proc/net` and `/proc/[pid]/fd`; only the transport, port and
//! process basename are reported, never a bound address.

use std::collections::{HashMap, HashSet};

use mesh_protocol::DiscoveredPort;

/// Hex TCP state for a listening socket in `/proc/net/tcp{,6}`.
const TCP_LISTEN: &str = "0A";

/// One `/proc/net` row: the local port and the socket inode that maps it to a process.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(crate) struct ProcNetEntry {
    pub port: u16,
    pub inode: u64,
}

fn hex_local_port(addr: &str) -> Option<u16> {
    let (_, port) = addr.rsplit_once(':')?;
    u16::from_str_radix(port, 16).ok()
}

fn hex_remote_port(addr: &str) -> Option<u16> {
    hex_local_port(addr)
}

/// Parses a `/proc/net` table, keeping TCP listeners and UDP sockets with remote port 0.
pub(crate) fn parse_proc_net(content: &str, is_tcp: bool) -> Vec<ProcNetEntry> {
    let mut out = Vec::new();
    for line in content.lines().skip(1) {
        let fields: Vec<&str> = line.split_whitespace().collect();
        // The inode is field index 9.
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

/// Maps socket rows to process basenames, deduplicating by port; an unresolved inode gives none.
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

#[cfg(target_os = "linux")]
fn parse_socket_inode(target: &str) -> Option<u64> {
    let inner = target.strip_prefix("socket:[")?.strip_suffix(']')?;
    inner.parse::<u64>().ok()
}

/// Lists listening ports; empty where the `/proc` source is absent.
pub fn collect_ports() -> Vec<DiscoveredPort> {
    #[cfg(target_os = "linux")]
    {
        collect_ports_linux()
    }
    #[cfg(not(target_os = "linux"))]
    {
        Vec::new()
    }
}

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

/// Maps wanted socket inodes to process basenames via `/proc/[pid]/fd`, stopping once all resolve.
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

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parse_proc_net_tcp_keeps_only_listeners() {
        // Rows 0 and 2 are LISTEN (0A); row 1 is ESTABLISHED (01).
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

    #[test]
    fn parse_proc_net_keeps_the_shortest_complete_row() {
        let table = concat!(
            "header line ignored\n",
            "   0: 00000000:1F90 00000000:0000 0A 0 0 0 0 0 12345\n",
            "   1: 00000000:0016 00000000:0000 0A 0 0 0 0 0\n",
        );
        let entries = parse_proc_net(table, true);
        assert_eq!(entries.len(), 1, "the nine-column row has no inode");
        assert_eq!(
            entries[0],
            ProcNetEntry {
                port: 8080,
                inode: 12345
            }
        );
    }

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

    #[test]
    fn collect_ports_reads_the_platform_or_reports_nothing() {
        let ports = collect_ports();
        #[cfg(not(target_os = "linux"))]
        assert!(ports.is_empty(), "no socket table to read");
        let mut seen = HashSet::new();
        for port in &ports {
            assert!(
                port.proto == "tcp" || port.proto == "udp",
                "unexpected transport {}",
                port.proto
            );
            assert!(
                seen.insert((port.proto.clone(), port.port)),
                "{}:{} reported twice",
                port.proto,
                port.port
            );
        }
    }

    #[cfg(target_os = "linux")]
    #[test]
    fn parse_socket_inode_extracts_number() {
        assert_eq!(parse_socket_inode("socket:[12345]"), Some(12345));
        assert_eq!(parse_socket_inode("anon_inode:[eventpoll]"), None);
        assert_eq!(parse_socket_inode("/dev/null"), None);
    }
}
