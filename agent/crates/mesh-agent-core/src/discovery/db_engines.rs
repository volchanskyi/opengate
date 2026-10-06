//! Database-engine inference from listening ports, with no connection opened and no version probed.

use std::collections::HashSet;

use mesh_protocol::{DiscoveredDbEngine, DiscoveredPort};

/// A well-known database engine, its default TCP port and its process basenames.
struct KnownEngine {
    engine: &'static str,
    port: u16,
    procs: &'static [&'static str],
}

/// Process names catch non-standard ports; a default port needs an unknown or matching process.
const KNOWN_ENGINES: &[KnownEngine] = &[
    KnownEngine {
        engine: "postgres",
        port: 5432,
        procs: &["postgres", "postmaster"],
    },
    KnownEngine {
        engine: "mysql",
        port: 3306,
        procs: &["mysqld", "mariadbd", "mysql"],
    },
    KnownEngine {
        engine: "mongodb",
        port: 27017,
        procs: &["mongod"],
    },
    KnownEngine {
        engine: "redis",
        port: 6379,
        procs: &["redis-server", "valkey-server"],
    },
    KnownEngine {
        engine: "mssql",
        port: 1433,
        procs: &["sqlservr"],
    },
    KnownEngine {
        engine: "cockroachdb",
        port: 26257,
        procs: &["cockroach"],
    },
    KnownEngine {
        engine: "elasticsearch",
        port: 9200,
        procs: &["elasticsearch"],
    },
    KnownEngine {
        engine: "cassandra",
        port: 9042,
        procs: &["cassandra"],
    },
    KnownEngine {
        engine: "clickhouse",
        port: 9000,
        procs: &["clickhouse-serv"],
    },
];

/// Infers database engines from listening ports, each once at its first port, with no version.
pub fn infer_db_engines(ports: &[DiscoveredPort]) -> Vec<DiscoveredDbEngine> {
    let mut out = Vec::new();
    let mut seen = HashSet::new();
    for port in ports {
        for known in KNOWN_ENGINES {
            let by_proc = !port.process.is_empty() && known.procs.contains(&port.process.as_str());
            let by_port = port.proto == "tcp"
                && port.port == known.port
                && (port.process.is_empty() || known.procs.contains(&port.process.as_str()));
            if (by_proc || by_port) && seen.insert(known.engine) {
                out.push(DiscoveredDbEngine {
                    engine: known.engine.to_string(),
                    version: String::new(),
                    port: port.port,
                });
            }
        }
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    fn port(proto: &str, port: u16, process: &str) -> DiscoveredPort {
        DiscoveredPort {
            proto: proto.to_string(),
            port,
            process: process.to_string(),
        }
    }

    #[test]
    fn infers_by_process_on_nonstandard_port() {
        let ports = vec![port("tcp", 6544, "postgres")];
        let engines = infer_db_engines(&ports);
        assert_eq!(engines.len(), 1);
        assert_eq!(engines[0].engine, "postgres");
        assert_eq!(engines[0].port, 6544);
        assert!(engines[0].version.is_empty(), "version is never probed");
    }

    #[test]
    fn infers_by_wellknown_port_when_process_unknown() {
        let ports = vec![port("tcp", 3306, "")];
        let engines = infer_db_engines(&ports);
        assert_eq!(engines.len(), 1);
        assert_eq!(engines[0].engine, "mysql");
    }

    #[test]
    fn does_not_infer_when_process_contradicts_port() {
        let ports = vec![port("tcp", 5432, "haproxy")];
        assert!(
            infer_db_engines(&ports).is_empty(),
            "haproxy on 5432 is not postgres"
        );
    }

    #[test]
    fn dedups_engine_across_ports() {
        let ports = vec![port("tcp", 5432, "postgres"), port("tcp", 5433, "postgres")];
        let engines = infer_db_engines(&ports);
        assert_eq!(engines.len(), 1, "postgres reported once");
        assert_eq!(engines[0].port, 5432, "first matching port wins");
    }

    #[test]
    fn ignores_udp_ports() {
        let ports = vec![port("udp", 5432, "")];
        assert!(infer_db_engines(&ports).is_empty());
    }

    #[test]
    fn infers_multiple_engines() {
        let ports = vec![
            port("tcp", 5432, "postgres"),
            port("tcp", 6379, "redis-server"),
            port("tcp", 27017, ""),
        ];
        let engines = infer_db_engines(&ports);
        let names: HashSet<&str> = engines.iter().map(|e| e.engine.as_str()).collect();
        assert_eq!(names.len(), 3);
        assert!(names.contains("postgres"));
        assert!(names.contains("redis"));
        assert!(names.contains("mongodb"));
    }
}
