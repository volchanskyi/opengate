//! PTY-backed terminal session that forwards terminal data as `TerminalFrame`s.

mod hangup;

use std::io::{Read, Write};
use std::os::fd::{AsRawFd, BorrowedFd};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

use mesh_protocol::{Frame, TerminalFrame};
use portable_pty::{native_pty_system, CommandBuilder, MasterPty, PtySize};
use tokio::sync::{mpsc, oneshot};
use tracing::debug;

pub use hangup::HangupRequest;

use crate::session::outbox::Outbox;
use crate::session::TerminalHandle;
use crate::session_error::SessionError;

/// How long the reader waits for output before checking whether the terminal was hung up.
const READ_POLL_MS: libc::c_int = 100;

/// Manages a PTY-backed terminal session.
pub struct TerminalSession {
    pair: portable_pty::PtyPair,
}

impl TerminalSession {
    /// Opens a PTY with the given dimensions in character cells.
    pub fn spawn(cols: u16, rows: u16) -> Result<Self, SessionError> {
        let pty_system = native_pty_system();
        let pair = pty_system
            .openpty(PtySize {
                rows,
                cols,
                pixel_width: 0,
                pixel_height: 0,
            })
            .map_err(|e| SessionError::Terminal(e.to_string()))?;

        Ok(Self { pair })
    }

    /// Spawns a shell, queues its output as `TerminalFrame`s in the outbox, and returns a handle.
    pub async fn run(
        self,
        outbox: Outbox,
        running: Arc<AtomicBool>,
    ) -> Result<TerminalHandle, SessionError> {
        let shell = default_shell();
        let mut cmd = CommandBuilder::new(&shell);
        cmd.env("TERM", "xterm-256color");

        let portable_pty::PtyPair { master, slave } = self.pair;
        let child = slave
            .spawn_command(cmd)
            .map_err(|e| SessionError::Terminal(e.to_string()))?;
        // The shell holds the only open slave, so its exit ends the terminal.
        drop(slave);

        let reader = PollingReader::new(&*master)?;
        let writer = master
            .take_writer()
            .map_err(|e| SessionError::Terminal(e.to_string()))?;

        let (stdin_tx, stdin_rx) = mpsc::channel::<Vec<u8>>(64);
        let (resize_tx, resize_rx) = mpsc::channel::<(u16, u16)>(8);
        let (hangup_tx, hangup_rx) = oneshot::channel();
        let shutdown = Arc::new(AtomicBool::new(false));

        let reader_shutdown = shutdown.clone();
        let reader = tokio::task::spawn_blocking(move || {
            pty_reader_loop(Box::new(reader), outbox, running, reader_shutdown);
        });

        let writer_shutdown = shutdown.clone();
        let writer = tokio::task::spawn_blocking(move || {
            stdin_writer_loop(writer, stdin_rx, writer_shutdown);
        });

        let shell_pid = child.process_id();
        let shell: hangup::Shell = Arc::new(Mutex::new(Some(child)));
        let waiter = hangup::spawn_waiter(shell.clone(), shell_pid);
        let supervisor = hangup::Supervisor {
            master,
            shell,
            shutdown,
            reader,
            writer,
            waiter,
        };
        tokio::spawn(supervisor.run(resize_rx, hangup_rx));

        Ok(TerminalHandle::new(stdin_tx, resize_tx, hangup_tx))
    }
}

/// Reads the controlling side of the terminal, waking every [`READ_POLL_MS`] with
/// [`std::io::ErrorKind::WouldBlock`] so its reader can notice a hang-up.
struct PollingReader(std::fs::File);

impl PollingReader {
    fn new(master: &dyn MasterPty) -> Result<Self, SessionError> {
        let fd = master
            .as_raw_fd()
            .ok_or_else(|| SessionError::Terminal("the terminal has no descriptor".to_string()))?;
        // SAFETY: `master` keeps `fd` open for this call, and the clone is the reader's own.
        let owned = unsafe { BorrowedFd::borrow_raw(fd) }
            .try_clone_to_owned()
            .map_err(|e| SessionError::Terminal(e.to_string()))?;
        Ok(Self(std::fs::File::from(owned)))
    }
}

impl Read for PollingReader {
    fn read(&mut self, buf: &mut [u8]) -> std::io::Result<usize> {
        let mut ready = libc::pollfd {
            fd: self.0.as_raw_fd(),
            events: libc::POLLIN,
            revents: 0,
        };
        // SAFETY: one pollfd that outlives the call.
        match unsafe { libc::poll(&mut ready, 1, READ_POLL_MS) } {
            0 => Err(std::io::ErrorKind::WouldBlock.into()),
            n if n < 0 => {
                let e = std::io::Error::last_os_error();
                match e.kind() {
                    std::io::ErrorKind::Interrupted => Err(std::io::ErrorKind::WouldBlock.into()),
                    _ => Err(e),
                }
            }
            _ => self.0.read(buf),
        }
    }
}

pub(crate) fn pty_reader_loop(
    mut reader: Box<dyn Read + Send>,
    outbox: Outbox,
    running: Arc<AtomicBool>,
    shutdown: Arc<AtomicBool>,
) {
    let mut buf = [0u8; 4096];
    loop {
        if !running.load(Ordering::Relaxed) || shutdown.load(Ordering::Relaxed) {
            break;
        }
        match reader.read(&mut buf) {
            Ok(0) => break,
            Ok(n) => {
                let frame = Frame::Terminal(TerminalFrame {
                    data: buf[..n].to_vec(),
                });
                if let Ok(encoded) = frame.encode() {
                    if outbox.blocking_send_encoded(encoded).is_err() {
                        break;
                    }
                }
            }
            Err(e) if e.kind() == std::io::ErrorKind::WouldBlock => {}
            Err(e) => {
                debug!("PTY read error: {e}");
                break;
            }
        }
    }
}

/// How long the shell's input stays quiet after a cursor-position answer reaches it. Busybox's line
/// editor reads 50 ms past an answer like `ESC[1;5R` and drops what follows with it.
const CURSOR_ANSWER_QUIET: Duration = Duration::from_millis(150);

/// Writes browser input to the terminal, holding the keys after a cursor-position answer until
/// the shell has had [`CURSOR_ANSWER_QUIET`] to read the answer alone.
pub(crate) fn stdin_writer_loop(
    mut writer: Box<dyn Write + Send>,
    mut rx: mpsc::Receiver<Vec<u8>>,
    shutdown: Arc<AtomicBool>,
) {
    let mut quiet_until: Option<Instant> = None;
    while let Some(data) = rx.blocking_recv() {
        if shutdown.load(Ordering::Relaxed) {
            break;
        }
        for (part, answered) in split_after_cursor_answers(&data) {
            if let Some(until) = quiet_until.take() {
                std::thread::sleep(until.saturating_duration_since(Instant::now()));
            }
            if writer.write_all(part).is_err() {
                return;
            }
            if let Err(e) = writer.flush() {
                debug!("PTY writer flush failed: {e}");
                return;
            }
            if answered {
                quiet_until = Some(Instant::now() + CURSOR_ANSWER_QUIET);
            }
        }
    }
}

/// Cuts input after each cursor-position answer it carries, marking the parts that end with one.
fn split_after_cursor_answers(data: &[u8]) -> Vec<(&[u8], bool)> {
    let mut parts = Vec::new();
    let (mut start, mut at) = (0, 0);
    while let Some(offset) = data[at..].iter().position(|&b| b == 0x1b) {
        let escape = at + offset;
        match cursor_answer_len(&data[escape..]) {
            Some(len) => {
                parts.push((&data[start..escape + len], true));
                start = escape + len;
                at = start;
            }
            None => at = escape + 1,
        }
    }
    if start < data.len() {
        parts.push((&data[start..], false));
    }
    parts
}

/// The length of the cursor-position answer `ESC [ row ; column R` that `bytes` starts with.
fn cursor_answer_len(bytes: &[u8]) -> Option<usize> {
    let digits = |b: &[u8]| b.iter().take_while(|c| c.is_ascii_digit()).count();
    let after_bracket = bytes.strip_prefix(b"\x1b[")?;
    let row = digits(after_bracket);
    let after_row = after_bracket[row..].strip_prefix(b";")?;
    let column = digits(after_row);
    (row > 0 && column > 0 && after_row.get(column) == Some(&b'R')).then_some(row + column + 4)
}

fn default_shell() -> String {
    #[cfg(unix)]
    {
        std::env::var("SHELL").unwrap_or_else(|_| "/bin/sh".to_string())
    }
    #[cfg(not(unix))]
    {
        "sh".to_string()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::session::outbox::Lanes;

    #[test]
    fn test_default_shell_is_not_empty() {
        let shell = default_shell();
        assert!(!shell.is_empty());
    }

    #[test]
    fn default_shell_follows_the_environment_on_unix() {
        let shell = default_shell();
        #[cfg(unix)]
        match std::env::var("SHELL") {
            Ok(configured) => assert_eq!(shell, configured),
            Err(_) => assert_eq!(shell, "/bin/sh"),
        }
        #[cfg(not(unix))]
        assert_eq!(shell, "sh");
    }

    #[test]
    fn test_spawn_terminal() {
        let term = TerminalSession::spawn(80, 24);
        assert!(term.is_ok());
    }

    #[test]
    fn test_spawn_terminal_custom_size() {
        let term = TerminalSession::spawn(120, 40);
        assert!(term.is_ok());
    }

    /// Runs the reader on a blocking thread, as the session does, and returns its lanes.
    async fn read_all(reader: Box<dyn Read + Send>, running: bool, shutdown: bool) -> Lanes {
        let (outbox, lanes) = Outbox::new();
        let running = Arc::new(AtomicBool::new(running));
        let shutdown = Arc::new(AtomicBool::new(shutdown));
        tokio::task::spawn_blocking(move || pty_reader_loop(reader, outbox, running, shutdown))
            .await
            .expect("pty_reader_loop must return");
        lanes
    }

    #[tokio::test(flavor = "multi_thread", worker_threads = 2)]
    async fn pty_reader_loop_breaks_on_eof() {
        let mut lanes = read_all(Box::new(std::io::Cursor::new(b"hello")), true, false).await;

        let data = lanes.try_next().expect("expected one frame");
        match mesh_protocol::Frame::decode(&data).unwrap().0 {
            mesh_protocol::Frame::Terminal(tf) => assert_eq!(tf.data, b"hello"),
            other => panic!("expected Terminal frame, got {other:?}"),
        }
        assert!(lanes.try_next().is_none());
    }

    #[tokio::test(flavor = "multi_thread", worker_threads = 2)]
    async fn pty_reader_loop_breaks_when_shutdown_alone_set() {
        // The reader still holds data, so only the flags can end the loop.
        let reader = Box::new(std::io::repeat(0xAB).take(1_000_000));
        let mut lanes = read_all(reader, true, true).await;
        assert!(lanes.try_next().is_none());
    }

    #[tokio::test(flavor = "multi_thread", worker_threads = 2)]
    async fn pty_reader_loop_breaks_when_running_false_alone() {
        let reader = Box::new(std::io::repeat(0x42).take(1_000_000));
        let mut lanes = read_all(reader, false, false).await;
        assert!(lanes.try_next().is_none());
    }

    #[tokio::test(flavor = "multi_thread", worker_threads = 2)]
    async fn pty_reader_loop_breaks_on_io_error() {
        struct ErrReader;
        impl Read for ErrReader {
            fn read(&mut self, _buf: &mut [u8]) -> std::io::Result<usize> {
                Err(std::io::Error::other("simulated PTY error"))
            }
        }
        let mut lanes = read_all(Box::new(ErrReader), true, false).await;
        assert!(lanes.try_next().is_none());
    }

    #[tokio::test(flavor = "multi_thread", worker_threads = 2)]
    async fn pty_reader_loop_keeps_reading_through_quiet_polls() {
        struct QuietThenData(u8);
        impl Read for QuietThenData {
            fn read(&mut self, buf: &mut [u8]) -> std::io::Result<usize> {
                self.0 += 1;
                match self.0 {
                    1 | 2 => Err(std::io::ErrorKind::WouldBlock.into()),
                    3 => {
                        buf[..2].copy_from_slice(b"ok");
                        Ok(2)
                    }
                    _ => Ok(0),
                }
            }
        }
        let mut lanes = read_all(Box::new(QuietThenData(0)), true, false).await;
        let data = lanes.try_next().expect("the output after the quiet polls");
        match mesh_protocol::Frame::decode(&data).unwrap().0 {
            mesh_protocol::Frame::Terminal(tf) => assert_eq!(tf.data, b"ok"),
            other => panic!("expected Terminal frame, got {other:?}"),
        }
    }

    #[tokio::test(flavor = "multi_thread", worker_threads = 2)]
    async fn terminal_output_frames_each_fit_the_interactive_lane() {
        let reader = Box::new(std::io::repeat(b'x').take(64 * 1024));
        let mut lanes = read_all(reader, true, false).await;
        let mut total = 0;
        while let Some(frame) = lanes.try_next() {
            assert!(frame.len() <= crate::session::outbox::INTERACTIVE_FRAME_CAP);
            total += frame.len();
        }
        assert!(total >= 64 * 1024);
    }

    #[test]
    fn stdin_writer_loop_writes_to_underlying_writer() {
        let captured = Arc::new(std::sync::Mutex::new(Vec::<u8>::new()));
        struct CapturingWriter(Arc<std::sync::Mutex<Vec<u8>>>);
        impl Write for CapturingWriter {
            fn write(&mut self, buf: &[u8]) -> std::io::Result<usize> {
                self.0.lock().unwrap().extend_from_slice(buf);
                Ok(buf.len())
            }
            fn flush(&mut self) -> std::io::Result<()> {
                Ok(())
            }
        }
        let writer: Box<dyn Write + Send> = Box::new(CapturingWriter(captured.clone()));
        let (tx, rx) = mpsc::channel::<Vec<u8>>(4);
        let shutdown = Arc::new(AtomicBool::new(false));

        let handle = std::thread::spawn(move || {
            stdin_writer_loop(writer, rx, shutdown);
        });
        tx.blocking_send(b"abc".to_vec()).unwrap();
        tx.blocking_send(b"def".to_vec()).unwrap();
        drop(tx);
        handle.join().unwrap();

        let data = captured.lock().unwrap();
        assert_eq!(*data, b"abcdef");
    }

    #[test]
    fn stdin_writer_loop_respects_shutdown_flag() {
        let captured = Arc::new(std::sync::Mutex::new(Vec::<u8>::new()));
        struct CapturingWriter(Arc<std::sync::Mutex<Vec<u8>>>);
        impl Write for CapturingWriter {
            fn write(&mut self, buf: &[u8]) -> std::io::Result<usize> {
                self.0.lock().unwrap().extend_from_slice(buf);
                Ok(buf.len())
            }
            fn flush(&mut self) -> std::io::Result<()> {
                Ok(())
            }
        }
        let writer: Box<dyn Write + Send> = Box::new(CapturingWriter(captured.clone()));
        let (tx, rx) = mpsc::channel::<Vec<u8>>(4);
        let shutdown = Arc::new(AtomicBool::new(true));

        let handle = std::thread::spawn(move || {
            stdin_writer_loop(writer, rx, shutdown);
        });
        tx.blocking_send(b"should-not-write".to_vec()).unwrap();
        drop(tx);
        handle.join().unwrap();

        let data = captured.lock().unwrap();
        assert!(
            data.is_empty(),
            "writer must not receive bytes after shutdown is set"
        );
    }

    type Writes = Arc<std::sync::Mutex<Vec<(std::time::Instant, Vec<u8>)>>>;

    /// Feeds chunks through the writer loop and returns each write with the moment it happened.
    fn timed_writes(chunks: &[&[u8]]) -> Vec<(std::time::Instant, Vec<u8>)> {
        struct TimedWriter(Writes);
        impl Write for TimedWriter {
            fn write(&mut self, buf: &[u8]) -> std::io::Result<usize> {
                let at = std::time::Instant::now();
                self.0.lock().unwrap().push((at, buf.to_vec()));
                Ok(buf.len())
            }
            fn flush(&mut self) -> std::io::Result<()> {
                Ok(())
            }
        }
        let writes: Writes = Arc::default();
        let writer: Box<dyn Write + Send> = Box::new(TimedWriter(writes.clone()));
        let (tx, rx) = mpsc::channel::<Vec<u8>>(16);
        for chunk in chunks {
            tx.try_send(chunk.to_vec()).unwrap();
        }
        drop(tx);
        stdin_writer_loop(writer, rx, Arc::new(AtomicBool::new(false)));
        let taken = std::mem::take(&mut *writes.lock().unwrap());
        taken
    }

    fn bytes_of(writes: &[(std::time::Instant, Vec<u8>)]) -> Vec<&[u8]> {
        writes.iter().map(|(_, b)| b.as_slice()).collect()
    }

    #[test]
    fn keys_after_the_terminal_cursor_answer_wait_until_the_shell_has_settled() {
        let writes = timed_writes(&[b"\x1b[1;5R", b"echo", b" opengate"]);

        assert_eq!(
            bytes_of(&writes),
            [&b"\x1b[1;5R"[..], b"echo", b" opengate"]
        );
        let held = writes[1].0.duration_since(writes[0].0);
        assert!(
            held >= CURSOR_ANSWER_QUIET,
            "the first key reached the shell {held:?} after the cursor answer"
        );
    }

    #[test]
    fn a_cursor_answer_inside_a_chunk_is_written_apart_from_the_keys_after_it() {
        let writes = timed_writes(&[b"ls\x1b[12;40Rcd /tmp\n"]);

        assert_eq!(bytes_of(&writes), [&b"ls\x1b[12;40R"[..], b"cd /tmp\n"]);
        assert!(writes[1].0.duration_since(writes[0].0) >= CURSOR_ANSWER_QUIET);
    }

    #[test]
    fn keys_with_no_cursor_answer_are_written_as_they_come() {
        // Ctrl+Up, an unfinished sequence and an answer missing its column each start like one.
        let writes = timed_writes(&[b"a\x1b[1;5A", b"\x1b[", b"3;R", b"b"]);

        assert_eq!(bytes_of(&writes).concat(), b"a\x1b[1;5A\x1b[3;Rb");
        let spread = writes.last().unwrap().0.duration_since(writes[0].0);
        assert!(
            spread < CURSOR_ANSWER_QUIET,
            "plain keys took {spread:?} to reach the shell"
        );
    }
}
