//! Kernel pressure-stall information: five `avg60` stall vitals in `[0, 100]`, with a host that
//! publishes none reporting `Unsupported` and no vitals. A container reads its own cgroup only.

use std::fs;
use std::path::{Path, PathBuf};

use super::cgroup::own_cgroup;

/// The line prefix for the share of time some tasks were stalled.
const SOME: &str = "some";
/// The line prefix for the share of time every runnable task was stalled.
const FULL: &str = "full";
/// The kernel field holding the 60 s average.
const AVG60: &str = "avg60=";

/// Whether this host publishes pressure stall information.
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

/// One read of the five stall vitals; `None` marks an unpublished vital, never a zero.
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

/// Reads the stall vitals from the pressure source resolved once at construction.
#[derive(Debug, Clone)]
pub struct PressureReader {
    paths: Option<PressurePaths>,
}

impl PressureReader {
    /// Resolves the pressure source under `root`: the agent's own cgroup in a container, else the host.
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

    /// The resolved source files, or `None` when nothing resolved.
    #[must_use]
    pub fn paths(&self) -> Option<&PressurePaths> {
        self.paths.as_ref()
    }

    /// Reads the five vitals; a missing or malformed file loses only the vitals it carries.
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

/// A pressure file's contents, or an empty string that parses as absent when unreadable.
fn read_text(path: &Path) -> String {
    fs::read_to_string(path).unwrap_or_default()
}

/// The pressure source under `root`, or `None` when none is published.
/// A containerized agent reads its cgroup only, because host pressure includes other containers.
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
    // Any one file present means the kernel publishes pressure.
    let present = paths.cpu.exists() || paths.memory.exists() || paths.io.exists();
    present.then_some(paths)
}

/// The `avg60` value of a `some` or `full` line; `None` for a missing, non-numeric or
/// out-of-`[0, 100]` value, which is never clamped.
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

    #[test]
    fn only_the_leading_word_selects_a_line() {
        let text = "some avg10=0.00 avg60=1.23 full=nonsense total=99\n";

        assert_eq!(parse_avg60(text, SOME), Some(1.23));
        assert_eq!(parse_avg60(text, FULL), None);
    }

    #[test]
    fn no_other_averaging_window_is_mistaken_for_avg60() {
        let text = "some avg10=7.00 avg300=9.00 total=99\n";

        assert_eq!(parse_avg60(text, SOME), None);
    }
}
