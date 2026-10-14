//! PTY-backed terminal session that forwards terminal data as `TerminalFrame`s.

use std::io::{Read, Write};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;

use mesh_protocol::{Frame, TerminalFrame};
use portable_pty::{native_pty_system, CommandBuilder, PtySize};
use tokio::sync::mpsc;
use tracing::{debug, warn};

use crate::session::outbox::Outbox;
use crate::session::TerminalHandle;
use crate::session_error::SessionError;

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

        let mut child = self
            .pair
            .slave
            .spawn_command(cmd)
            .map_err(|e| SessionError::Terminal(e.to_string()))?;

        let reader = self
            .pair
            .master
            .try_clone_reader()
            .map_err(|e| SessionError::Terminal(e.to_string()))?;

        let writer = self
            .pair
            .master
            .take_writer()
            .map_err(|e| SessionError::Terminal(e.to_string()))?;

        let (stdin_tx, stdin_rx) = mpsc::channel::<Vec<u8>>(64);
        let (resize_tx, resize_rx) = mpsc::channel::<(u16, u16)>(8);
        let shutdown = Arc::new(AtomicBool::new(false));

        let reader_running = running.clone();
        let reader_shutdown = shutdown.clone();
        tokio::task::spawn_blocking(move || {
            pty_reader_loop(reader, outbox, reader_running, reader_shutdown);
        });

        let writer_shutdown = shutdown.clone();
        tokio::task::spawn_blocking(move || {
            stdin_writer_loop(writer, stdin_rx, writer_shutdown);
        });

        let master = self.pair.master;
        let resize_shutdown = shutdown.clone();
        tokio::spawn(async move {
            resize_loop(master, resize_rx, resize_shutdown).await;
        });

        tokio::task::spawn_blocking(move || {
            if let Err(e) = child.wait() {
                debug!("PTY child wait failed: {e}");
            }
        });

        Ok(TerminalHandle::new(stdin_tx, resize_tx, shutdown))
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
            Err(e) => {
                debug!("PTY read error: {e}");
                break;
            }
        }
    }
}

pub(crate) fn stdin_writer_loop(
    mut writer: Box<dyn Write + Send>,
    mut rx: mpsc::Receiver<Vec<u8>>,
    shutdown: Arc<AtomicBool>,
) {
    while let Some(data) = rx.blocking_recv() {
        if shutdown.load(Ordering::Relaxed) {
            break;
        }
        if writer.write_all(&data).is_err() {
            break;
        }
        if let Err(e) = writer.flush() {
            debug!("PTY writer flush failed: {e}");
            break;
        }
    }
}

async fn resize_loop(
    master: Box<dyn portable_pty::MasterPty + Send>,
    mut rx: mpsc::Receiver<(u16, u16)>,
    shutdown: Arc<AtomicBool>,
) {
    while let Some((cols, rows)) = rx.recv().await {
        if shutdown.load(Ordering::Relaxed) {
            break;
        }
        if let Err(e) = master.resize(PtySize {
            rows,
            cols,
            pixel_width: 0,
            pixel_height: 0,
        }) {
            warn!("PTY resize error: {e}");
        }
    }
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
}
