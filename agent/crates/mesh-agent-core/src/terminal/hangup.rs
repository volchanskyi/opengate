//! Ends a terminal as a dropped SSH connection does: the shell and its foreground job are hung
//! up, and whatever of the foreground job outlasts the grace is killed.

use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex, PoisonError};
use std::time::Duration;

use portable_pty::{Child, MasterPty, PtySize};
use tokio::sync::{mpsc, oneshot};
use tokio::task::JoinHandle;
use tracing::{debug, info, warn};

/// How long the shell and its foreground job get to act on the hang-up before they are killed.
const GRACE: Duration = Duration::from_secs(2);

/// How often the foreground job is checked during the grace.
const CHECK_EVERY: Duration = Duration::from_millis(50);

/// A hang-up request, answered once the shell and every helper thread have finished.
pub type HangupRequest = oneshot::Sender<oneshot::Sender<()>>;

/// The shell, until its waiter thread reaps it.
pub(super) type Shell = Arc<Mutex<Option<Box<dyn Child + Send + Sync>>>>;

/// Owns the controlling side of the terminal, its shell and its helper threads.
pub(super) struct Supervisor {
    pub(super) master: Box<dyn MasterPty + Send>,
    pub(super) shell: Shell,
    pub(super) shutdown: Arc<AtomicBool>,
    pub(super) reader: JoinHandle<()>,
    pub(super) writer: JoinHandle<()>,
    pub(super) waiter: JoinHandle<()>,
}

impl Supervisor {
    /// Applies resizes until a hang-up is asked for or the handle is dropped, then hangs up.
    pub(super) async fn run(
        self,
        mut resizes: mpsc::Receiver<(u16, u16)>,
        mut requests: oneshot::Receiver<oneshot::Sender<()>>,
    ) {
        let answer = loop {
            tokio::select! {
                request = &mut requests => break request.ok(),
                Some((cols, rows)) = resizes.recv() => self.resize(cols, rows),
            }
        };
        self.hang_up().await;
        if let Some(answer) = answer {
            answer.send(()).ok();
        }
    }

    fn resize(&self, cols: u16, rows: u16) {
        if let Err(e) = self.master.resize(PtySize {
            rows,
            cols,
            pixel_width: 0,
            pixel_height: 0,
        }) {
            warn!("PTY resize error: {e}");
        }
    }

    async fn hang_up(self) {
        let Self {
            master,
            shell,
            shutdown,
            reader,
            writer,
            mut waiter,
        } = self;
        shutdown.store(true, Ordering::Relaxed);
        let foreground = master.process_group_leader().filter(|group| *group > 1);
        for signal in [libc::SIGHUP, libc::SIGCONT] {
            signal_shell(&shell, signal);
            if let Some(group) = foreground {
                signal_group(group, signal);
            }
        }
        drop(master);

        let deadline = tokio::time::Instant::now() + GRACE;
        let shell_ended = tokio::time::timeout_at(deadline, &mut waiter).await.is_ok();
        if let Some(group) = foreground {
            while group_alive(group) && tokio::time::Instant::now() < deadline {
                tokio::time::sleep(CHECK_EVERY).await;
            }
            if group_alive(group) {
                info!(group, "the terminal's job ignored the hang-up, ending it");
                signal_group(group, libc::SIGKILL);
            }
        }
        if !shell_ended {
            info!("the terminal's shell ignored the hang-up, ending it");
            signal_shell(&shell, libc::SIGKILL);
            waiter.await.ok();
        }
        reader.await.ok();
        // The writer ends once its input closes; a write the shell never reads holds it.
        if tokio::time::timeout(GRACE, writer).await.is_err() {
            warn!("the terminal's input writer is still blocked, leaving it");
        }
    }
}

/// Spawns the thread that reaps the shell once it has exited.
pub(super) fn spawn_waiter(shell: Shell, pid: Option<u32>) -> JoinHandle<()> {
    tokio::task::spawn_blocking(move || {
        if let Some(pid) = pid {
            wait_for_exit(pid);
        }
        // The shell leaves the slot only once it has exited, so a signal sent while it holds the
        // slot reaches the shell and no process that took its pid after.
        let child = shell.lock().unwrap_or_else(PoisonError::into_inner).take();
        if let Some(mut child) = child {
            if let Err(e) = child.wait() {
                debug!("PTY child wait failed: {e}");
            }
        }
    })
}

/// Blocks until `pid` has exited, leaving it for [`Child::wait`] to reap.
fn wait_for_exit(pid: u32) {
    loop {
        // SAFETY: siginfo_t is plain data that waitid fills in.
        let mut info: libc::siginfo_t = unsafe { std::mem::zeroed() };
        // SAFETY: waitid writes only into `info`; WNOWAIT leaves the child waitable.
        let rc =
            unsafe { libc::waitid(libc::P_PID, pid, &mut info, libc::WEXITED | libc::WNOWAIT) };
        if rc == 0 || std::io::Error::last_os_error().kind() != std::io::ErrorKind::Interrupted {
            return;
        }
    }
}

fn signal_shell(shell: &Shell, signal: libc::c_int) {
    let shell = shell.lock().unwrap_or_else(PoisonError::into_inner);
    let pid = shell
        .as_ref()
        .and_then(|child| child.process_id())
        .and_then(|pid| libc::pid_t::try_from(pid).ok());
    if let Some(pid) = pid {
        // SAFETY: the held lock keeps the shell unreaped, so `pid` still names it.
        unsafe {
            libc::kill(pid, signal);
        }
    }
}

/// Signals every process in `group`, which [`Supervisor::hang_up`] holds above 1.
fn signal_group(group: libc::pid_t, signal: libc::c_int) {
    // SAFETY: a negative pid addresses one process group; `group > 1` never names them all.
    unsafe {
        libc::kill(-group, signal);
    }
}

fn group_alive(group: libc::pid_t) -> bool {
    // SAFETY: signal 0 only asks whether the group has a member.
    unsafe { libc::kill(-group, 0) == 0 }
}

#[cfg(test)]
mod tests {
    use std::path::{Path, PathBuf};
    use std::sync::atomic::AtomicBool;
    use std::sync::Arc;
    use std::time::{Duration, Instant};

    use crate::session::outbox::Outbox;
    use crate::session::TerminalHandle;
    use crate::terminal::TerminalSession;

    /// A hang-up finishes within the 2 s grace plus the reader's poll and the reaping.
    const HANG_UP_LIMIT: Duration = Duration::from_secs(3);

    /// The shell reads its start-up files before the typed command runs.
    const START_LIMIT: Duration = Duration::from_secs(20);

    struct Scene {
        dir: tempfile::TempDir,
        terminal: TerminalHandle,
    }

    impl Scene {
        async fn open() -> Self {
            let (outbox, mut lanes) = Outbox::new();
            let terminal = TerminalSession::spawn(80, 24)
                .unwrap()
                .run(outbox, Arc::new(AtomicBool::new(true)))
                .await
                .unwrap();
            tokio::spawn(async move { while lanes.next().await.is_some() {} });
            Self {
                dir: tempfile::tempdir().unwrap(),
                terminal,
            }
        }

        fn path(&self, name: &str) -> PathBuf {
            self.dir.path().join(name)
        }

        /// Writes a script that records its process id in `pid` before `body` runs.
        fn script(&self, body: &str) -> PathBuf {
            let path = self.path("run.sh");
            let pid = self.path("pid");
            std::fs::write(&path, format!("echo $$ > '{}'\n{body}\n", pid.display())).unwrap();
            path
        }

        fn type_line(&self, line: &str) {
            self.terminal.send_raw(format!("{line}\r").into_bytes());
        }

        async fn pid(&self) -> i32 {
            let path = self.path("pid");
            let deadline = Instant::now() + START_LIMIT;
            loop {
                if let Some(pid) = std::fs::read_to_string(&path)
                    .ok()
                    .and_then(|s| s.trim().parse().ok())
                {
                    return pid;
                }
                assert!(Instant::now() < deadline, "the typed command never started");
                tokio::time::sleep(Duration::from_millis(20)).await;
            }
        }

        /// Hangs up and asserts the terminal, its shell and its helper threads finished in time.
        async fn hang_up(self) -> tempfile::TempDir {
            let started = Instant::now();
            tokio::time::timeout(HANG_UP_LIMIT, self.terminal.hang_up())
                .await
                .expect("the hang-up finished in time");
            assert!(started.elapsed() < HANG_UP_LIMIT);
            self.dir
        }
    }

    /// Whether `pid` runs; a zombie waiting for its parent counts as ended.
    fn alive(pid: i32) -> bool {
        std::fs::read_to_string(format!("/proc/{pid}/stat"))
            .ok()
            .and_then(|stat| {
                let state = stat.rsplit_once(") ")?.1.chars().next()?;
                Some(state != 'Z' && state != 'X')
            })
            .unwrap_or(false)
    }

    async fn ended(pid: i32) -> bool {
        let deadline = Instant::now() + Duration::from_secs(1);
        while alive(pid) && Instant::now() < deadline {
            tokio::time::sleep(Duration::from_millis(20)).await;
        }
        !alive(pid)
    }

    fn quoted(path: &Path) -> String {
        format!("'{}'", path.display())
    }

    #[tokio::test(flavor = "multi_thread", worker_threads = 2)]
    async fn a_hang_up_ends_the_running_program_and_every_helper() {
        let scene = Scene::open().await;
        let script = scene.script("exec tail -f /dev/null");
        scene.type_line(&format!("sh {}", quoted(&script)));
        let pid = scene.pid().await;

        scene.hang_up().await;
        assert!(ended(pid).await, "tail outlived the session");
    }

    #[tokio::test(flavor = "multi_thread", worker_threads = 2)]
    async fn a_program_that_ignores_the_hang_up_is_ended_after_the_grace() {
        let scene = Scene::open().await;
        let script = scene.script("trap '' HUP\nexec sleep 300");
        scene.type_line(&format!("sh {}", quoted(&script)));
        let pid = scene.pid().await;

        scene.hang_up().await;
        assert!(ended(pid).await, "a program ignoring the hang-up survived");
    }

    #[tokio::test(flavor = "multi_thread", worker_threads = 2)]
    async fn a_program_that_saves_on_hang_up_gets_to_save() {
        let scene = Scene::open().await;
        let saved = scene.path("saved");
        let script = scene.script(&format!(
            "trap 'echo saved > {}; exit' HUP\nsleep 300",
            quoted(&saved)
        ));
        scene.type_line(&format!("sh {}", quoted(&script)));
        let pid = scene.pid().await;

        let _dir = scene.hang_up().await;
        assert!(ended(pid).await);
        assert_eq!(std::fs::read_to_string(&saved).unwrap().trim(), "saved");
    }

    #[tokio::test(flavor = "multi_thread", worker_threads = 2)]
    async fn a_program_started_with_nohup_outlives_the_session() {
        let scene = Scene::open().await;
        let script = scene.script("exec sleep 300");
        scene.type_line(&format!("nohup sh {} >/dev/null 2>&1 &", quoted(&script)));
        let pid = scene.pid().await;

        let _dir = scene.hang_up().await;
        let survived = alive(pid);
        // SAFETY: kill(2) on the process this test started through the terminal.
        unsafe {
            libc::kill(pid, libc::SIGKILL);
        }
        assert!(survived, "nohup did not keep the program running");
    }

    #[tokio::test(flavor = "multi_thread", worker_threads = 2)]
    async fn a_hang_up_after_the_shell_exited_finishes_at_once() {
        let scene = Scene::open().await;
        scene.type_line("exit");
        tokio::time::sleep(Duration::from_millis(500)).await;

        let started = Instant::now();
        scene.hang_up().await;
        assert!(started.elapsed() < Duration::from_secs(1));
    }

    #[tokio::test(flavor = "multi_thread", worker_threads = 2)]
    async fn a_dropped_terminal_is_hung_up() {
        let scene = Scene::open().await;
        let script = scene.script("exec tail -f /dev/null");
        scene.type_line(&format!("sh {}", quoted(&script)));
        let pid = scene.pid().await;

        drop(scene.terminal);
        let deadline = Instant::now() + HANG_UP_LIMIT;
        while alive(pid) && Instant::now() < deadline {
            tokio::time::sleep(Duration::from_millis(20)).await;
        }
        assert!(!alive(pid), "tail outlived a dropped terminal");
    }
}
