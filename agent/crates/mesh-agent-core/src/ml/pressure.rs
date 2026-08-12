//! Kernel pressure-stall information (PSI) — the five stall vitals.
//!
//! Each vital is "percent of time tasks were stalled" in `[0, 100]`, read from
//! the `avg60` field the kernel publishes in `/proc/pressure/{cpu,memory,io}`.
//! The kernel has already performed the reduction, so a stall vital costs one
//! file read and zero cardinality, and its averaging window is exactly the 60 s
//! the vitals contract publishes on.
//!
//! Five vitals ship: `stall.cpu.some`, `stall.mem.some`, `stall.mem.full`,
//! `stall.io.some` and `stall.io.full`. CPU `full` is omitted because the kernel
//! defines it as always zero, and a constant is not worth a central series.
//!
//! **An absent reading is absent, never zero.** A host whose kernel publishes no
//! pressure information reports [`PressureSupport::Unsupported`] and no vitals
//! at all: a zero would read as "never stalled", which is a claim about a
//! measurement the host cannot make. No analogue is synthesized from counters
//! that measure something else — publishing those under a `stall.*` name would
//! put two meanings behind one name.
//!
//! **A containerized agent measures itself.** When `/proc/self/cgroup` shows a
//! non-root unified cgroup, the three `*.pressure` files of that cgroup are the
//! source, so the agent reports its own pressure rather than the host's — which
//! includes every neighbouring container's. If that cgroup publishes no pressure
//! files there is no fallback to `/proc/pressure`: the answer is `Unsupported`.
//!
//! The reader resolves every path under an injectable root, so a host without
//! PSI is an ordinary fixture directory rather than a platform nobody can test
//! on. Production passes `/`.

use std::fs;
use std::path::{Path, PathBuf};

use super::cgroup::own_cgroup;

/// The line prefix carrying the share of time *some* tasks were stalled while
/// others still ran.
const SOME: &str = "some";
/// The line prefix carrying the share of time *every* runnable task was stalled.
const FULL: &str = "full";
/// The kernel field holding the 60 s average — the vitals cadence exactly.
const AVG60: &str = "avg60=";

/// Whether this host publishes pressure stall information.
///
/// The state is reported rather than implied: coverage accounting distinguishes
/// a rule that is inactive from one the host cannot support, so a gap in the
/// fleet's stall coverage is visible instead of reading as calm.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
#[non_exhaustive]
pub enum PressureSupport {
    /// The kernel publishes pressure information and the agent reads it.
    Supported,
    /// No pressure source resolved; the stall vitals are absent for this host.
    Unsupported,
}

/// The three pressure files a host reads, whether host-wide or cgroup-scoped.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PressurePaths {
    /// The CPU pressure file.
    pub cpu: PathBuf,
    /// The memory pressure file.
    pub memory: PathBuf,
    /// The I/O pressure file.
    pub io: PathBuf,
}

/// One read of the five stall vitals, each the `avg60` of its own line. A `None`
/// is a vital this host did not publish this second — never a zero standing in
/// for one.
#[derive(Debug, Clone, Copy, PartialEq, Default)]
pub struct PressureReading {
    /// Percent of the last 60 s some task was stalled on CPU.
    pub cpu_some: Option<f32>,
    /// Percent of the last 60 s some task was stalled on memory.
    pub mem_some: Option<f32>,
    /// Percent of the last 60 s every runnable task was stalled on memory.
    pub mem_full: Option<f32>,
    /// Percent of the last 60 s some task was stalled on I/O.
    pub io_some: Option<f32>,
    /// Percent of the last 60 s every runnable task was stalled on I/O.
    pub io_full: Option<f32>,
}

/// Reads the stall vitals from whichever pressure source belongs to this agent.
///
/// The source is resolved once, at construction; each [`read`](Self::read) then
/// costs three file reads.
#[derive(Debug, Clone)]
pub struct PressureReader {
    /// The resolved source, or `None` on a host that publishes no pressure.
    paths: Option<PressurePaths>,
}

impl PressureReader {
    /// Resolve this agent's pressure source under `root` — its own cgroup when
    /// containerized, the host's files otherwise. Production passes `/`; tests
    /// pass a fixture directory, which is how a host without PSI is exercised on
    /// a host that has it.
    #[must_use]
    pub fn for_root(root: &Path) -> Self {
        Self {
            paths: resolve(root),
        }
    }

    /// Whether this host publishes pressure information at all.
    #[must_use]
    pub fn support(&self) -> PressureSupport {
        match self.paths {
            Some(_) => PressureSupport::Supported,
            None => PressureSupport::Unsupported,
        }
    }

    /// The resolved source files, or `None` when nothing resolved. Which files
    /// were chosen is the whole answer to "whose pressure is this" — a
    /// containerized agent reading `/proc/pressure` would report its
    /// neighbours' stalls as its own.
    #[must_use]
    pub fn paths(&self) -> Option<&PressurePaths> {
        self.paths.as_ref()
    }

    /// Read the five vitals now. A file that disappears, or whose contents do
    /// not have the shape the kernel documents, costs only the vitals it carries
    /// — the rest of the read still lands.
    #[must_use]
    pub fn read(&self) -> PressureReading {
        let Some(paths) = &self.paths else {
            return PressureReading::default();
        };
        let cpu = read_text(&paths.cpu);
        let memory = read_text(&paths.memory);
        let io = read_text(&paths.io);
        PressureReading {
            cpu_some: parse_avg60(&cpu, SOME),
            mem_some: parse_avg60(&memory, SOME),
            mem_full: parse_avg60(&memory, FULL),
            io_some: parse_avg60(&io, SOME),
            io_full: parse_avg60(&io, FULL),
        }
    }
}

/// A pressure file's contents, or an empty string when it cannot be read. An
/// unreadable file carries no vitals, which the parser reports as absent.
fn read_text(path: &Path) -> String {
    fs::read_to_string(path).unwrap_or_default()
}

/// The pressure source for an agent rooted at `root`, or `None` when this host
/// publishes none.
///
/// A non-root unified cgroup means the agent is containerized, and then that
/// cgroup is the only honest source: there is deliberately no fallback to the
/// host's files, because host-wide pressure is not this container's pressure.
fn resolve(root: &Path) -> Option<PressurePaths> {
    let paths = match own_cgroup(root) {
        Some(dir) => PressurePaths {
            cpu: dir.join("cpu.pressure"),
            memory: dir.join("memory.pressure"),
            io: dir.join("io.pressure"),
        },
        None => PressurePaths {
            cpu: root.join("proc/pressure/cpu"),
            memory: root.join("proc/pressure/memory"),
            io: root.join("proc/pressure/io"),
        },
    };
    // Any one of the three present means the kernel publishes pressure; a
    // resource whose file is missing simply carries no vitals.
    let present = paths.cpu.exists() || paths.memory.exists() || paths.io.exists();
    present.then_some(paths)
}

/// The `avg60` value of the `some` or `full` line of a pressure file.
///
/// `None` for every shape that is not a percentage the kernel measured: a
/// missing line, a missing or empty field, a non-numeric value, and anything
/// outside `[0, 100]` — NaN and both infinities fail that range test too. A
/// stall vital is a share of time, so a value that is not one is no reading at
/// all; clamping it into range would publish a number the kernel never
/// measured, and 0 is exactly the "never stalled" answer this reader must never
/// invent.
fn parse_avg60(text: &str, kind: &str) -> Option<f32> {
    let line = text
        .lines()
        .find(|line| line.split_whitespace().next() == Some(kind))?;
    let value: f32 = line
        .split_whitespace()
        .find_map(|field| field.strip_prefix(AVG60))?
        .parse()
        .ok()?;
    (0.0..=100.0).contains(&value).then_some(value)
}

#[cfg(test)]
mod tests {
    use super::{parse_avg60, FULL, SOME};

    #[test]
    fn parses_the_sixty_second_average_of_each_line() {
        let text = "some avg10=0.00 avg60=1.23 avg300=0.45 total=99\n\
                    full avg10=0.00 avg60=4.56 avg300=0.12 total=42\n";

        assert_eq!(parse_avg60(text, SOME), Some(1.23));
        assert_eq!(parse_avg60(text, FULL), Some(4.56));
    }

    /// The line is matched on its first whitespace-separated word, so a field
    /// whose value happens to contain the word is never mistaken for the line.
    #[test]
    fn only_the_leading_word_selects_a_line() {
        let text = "some avg10=0.00 avg60=1.23 full=nonsense total=99\n";

        assert_eq!(parse_avg60(text, SOME), Some(1.23));
        assert_eq!(parse_avg60(text, FULL), None);
    }

    /// `avg10` and `avg300` are different windows. Matching a prefix loosely
    /// would publish a 10 s or 300 s average under a name that promises 60 s.
    #[test]
    fn no_other_averaging_window_is_mistaken_for_avg60() {
        let text = "some avg10=7.00 avg300=9.00 total=99\n";

        assert_eq!(parse_avg60(text, SOME), None);
    }
}
