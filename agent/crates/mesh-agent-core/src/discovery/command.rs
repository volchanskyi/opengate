//! Runs one inventory command under a time limit and a stop request, so a hung tool costs the
//! sweep one part of its report and never the agent's shutdown.

use std::io::Read;
use std::process::{Child, Command, Stdio};
use std::time::{Duration, Instant};

use tracing::{debug, warn};

/// How long one inventory command may run before it is ended and its part reported empty.
pub const COMMAND_LIMIT: Duration = Duration::from_secs(30);

/// How often a running command is checked for having exited.
const POLL: Duration = Duration::from_millis(20);

/// A stop request an inventory command waits on.
pub trait StopSignal {
    /// Waits up to `duration`, returning `true` as soon as a stop is requested.
    fn wait(&self, duration: Duration) -> bool;
}

/// Runs `command`, returning its standard output when it exits successfully within `limit`, and
/// `None` when it cannot start, fails, outruns `limit` or meets a stop request, which ends it.
pub fn stdout_within(
    command: &mut Command,
    limit: Duration,
    stop: &dyn StopSignal,
) -> Option<String> {
    if stop.wait(Duration::ZERO) {
        return None;
    }
    let mut child = command
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::null())
        .spawn()
        .ok()?;
    // The output is read as it arrives, so a listing larger than the pipe holds never stalls.
    let Some(mut pipe) = child.stdout.take() else {
        end(&mut child);
        return None;
    };
    let reader = std::thread::spawn(move || {
        let mut out = Vec::new();
        pipe.read_to_end(&mut out).map(|_| out)
    });
    let started = Instant::now();
    loop {
        match child.try_wait() {
            Ok(Some(status)) if status.success() => {
                let out = reader.join().ok()?.ok()?;
                return Some(String::from_utf8_lossy(&out).into_owned());
            }
            Ok(Some(_)) => return None,
            Ok(None) => {}
            Err(_) => {
                end(&mut child);
                return None;
            }
        }
        let left = limit.saturating_sub(started.elapsed());
        if left.is_zero() {
            warn!(
                command = ?command.get_program(),
                limit = ?limit,
                "inventory command ran past its limit and was ended"
            );
            end(&mut child);
            return None;
        }
        if stop.wait(POLL.min(left)) {
            debug!(
                command = ?command.get_program(),
                "inventory command ended by a stop request"
            );
            end(&mut child);
            return None;
        }
    }
}

/// Ends a running command and reaps it; a kill fails only on a command that already exited.
fn end(child: &mut Child) {
    if let Err(e) = child.kill() {
        debug!(error = %e, "inventory command had already exited");
    }
    if let Err(e) = child.wait() {
        debug!(error = %e, "inventory command could not be reaped");
    }
}

/// Never stops on its own; waits out each duration.
#[cfg(test)]
pub(crate) struct NeverStops;

#[cfg(test)]
impl StopSignal for NeverStops {
    fn wait(&self, duration: Duration) -> bool {
        std::thread::sleep(duration);
        false
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::path::Path;

    /// Requests the stop once `at` has passed.
    struct StopAt(Instant);

    impl StopSignal for StopAt {
        fn wait(&self, duration: Duration) -> bool {
            let left = self.0.saturating_duration_since(Instant::now());
            std::thread::sleep(duration.min(left));
            Instant::now() >= self.0
        }
    }

    fn sh(script: &str) -> Command {
        let mut command = Command::new("sh");
        command.args(["-c", script]);
        command
    }

    fn pid_in(path: &Path) -> u32 {
        let deadline = Instant::now() + Duration::from_secs(5);
        loop {
            if let Some(pid) = std::fs::read_to_string(path)
                .ok()
                .and_then(|s| s.trim().parse().ok())
            {
                return pid;
            }
            assert!(
                Instant::now() < deadline,
                "the command never named its process"
            );
            std::thread::sleep(Duration::from_millis(10));
        }
    }

    fn alive(pid: u32) -> bool {
        Path::new(&format!("/proc/{pid}")).exists()
    }

    #[test]
    fn a_command_that_exits_hands_back_its_output() {
        let out = stdout_within(&mut sh("printf 'a\\tb\\n'"), COMMAND_LIMIT, &NeverStops);
        assert_eq!(out.as_deref(), Some("a\tb\n"));
    }

    #[test]
    fn a_command_that_fails_reports_nothing() {
        let out = stdout_within(
            &mut sh("printf partial; exit 3"),
            COMMAND_LIMIT,
            &NeverStops,
        );
        assert_eq!(out, None);
    }

    #[test]
    fn a_command_that_cannot_start_reports_nothing() {
        let mut missing = Command::new("/nonexistent/opengate-inventory-tool");
        assert_eq!(
            stdout_within(&mut missing, COMMAND_LIMIT, &NeverStops),
            None
        );
    }

    #[test]
    fn output_larger_than_a_pipe_holds_is_read_whole() {
        let out = stdout_within(
            &mut sh("head -c 300000 /dev/zero | tr '\\0' a"),
            COMMAND_LIMIT,
            &NeverStops,
        )
        .expect("the command exits successfully");
        assert_eq!(out.len(), 300_000);
    }

    #[test]
    fn a_command_past_its_limit_is_ended_and_reports_nothing() {
        let dir = tempfile::tempdir().unwrap();
        let pid_file = dir.path().join("pid");
        let script = format!("echo $$ >'{}'; exec sleep 600", pid_file.display());
        let started = Instant::now();

        let out = stdout_within(&mut sh(&script), Duration::from_millis(300), &NeverStops);

        assert_eq!(out, None);
        assert!(started.elapsed() >= Duration::from_millis(300));
        assert!(
            started.elapsed() < Duration::from_secs(5),
            "ended at the limit"
        );
        assert!(!alive(pid_in(&pid_file)), "the command outlived its limit");
    }

    #[test]
    fn a_stop_request_ends_a_running_command_at_once() {
        let dir = tempfile::tempdir().unwrap();
        let pid_file = dir.path().join("pid");
        let script = format!("echo $$ >'{}'; exec sleep 600", pid_file.display());
        let started = Instant::now();
        let stop = StopAt(started + Duration::from_millis(200));

        let out = stdout_within(&mut sh(&script), COMMAND_LIMIT, &stop);

        assert_eq!(out, None);
        assert!(
            started.elapsed() < Duration::from_secs(5),
            "ended on the stop"
        );
        assert!(!alive(pid_in(&pid_file)), "the command outlived the stop");
    }

    #[test]
    fn a_stop_requested_before_the_command_starts_runs_nothing() {
        let dir = tempfile::tempdir().unwrap();
        let marker = dir.path().join("ran");
        let script = format!("touch '{}'", marker.display());

        let out = stdout_within(&mut sh(&script), COMMAND_LIMIT, &StopAt(Instant::now()));

        assert_eq!(out, None);
        assert!(!marker.exists(), "a stopped sweep started a command");
    }
}
