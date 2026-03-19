//! Terminal session management using PTY.
//!
//! Spawns a pseudo-terminal (PTY) and bridges its I/O with the relay
//! WebSocket connection, forwarding terminal data as `TerminalFrame`s.

use std::io::{Read, Write};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;

use mesh_protocol::{Frame, TerminalFrame};
use portable_pty::{native_pty_system, CommandBuilder, PtySize};
use tokio::sync::mpsc;
use tracing::{debug, warn};

use crate::session::TerminalHandle;
use crate::session_error::SessionError;

/// Manages a PTY-backed terminal session.
pub struct TerminalSession {
    pair: portable_pty::PtyPair,
}

impl TerminalSession {
    /// Spawn a new terminal with the given dimensions.
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

    /// Start the terminal, spawning a shell and returning a handle.
    ///
    /// The terminal output is forwarded as `TerminalFrame`s via `frame_tx`.
    pub async fn run(
        self,
        frame_tx: mpsc::Sender<Vec<u8>>,
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

        // Spawn PTY reader -> frame sender
        let reader_running = running.clone();
        let reader_shutdown = shutdown.clone();
        tokio::task::spawn_blocking(move || {
            pty_reader_loop(reader, frame_tx.clone(), reader_running, reader_shutdown);
        });

        // Spawn stdin writer
        let writer_shutdown = shutdown.clone();
        tokio::task::spawn_blocking(move || {
            stdin_writer_loop(writer, stdin_rx, writer_shutdown);
        });

        // Spawn resize handler
        let master = self.pair.master;
        let resize_shutdown = shutdown.clone();
        tokio::spawn(async move {
            resize_loop(master, resize_rx, resize_shutdown).await;
        });

        // Spawn child waiter (cleanup on exit)
        tokio::task::spawn_blocking(move || {
            let _ = child.wait();
        });

        Ok(TerminalHandle::new(stdin_tx, resize_tx, shutdown))
    }
}

/// Read PTY output and send encoded terminal frames.
fn pty_reader_loop(
    mut reader: Box<dyn Read + Send>,
    frame_tx: mpsc::Sender<Vec<u8>>,
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
                    if frame_tx.blocking_send(encoded).is_err() {
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

/// Write data from the stdin channel to the PTY master writer.
fn stdin_writer_loop(
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
        let _ = writer.flush();
    }
}

/// Handle resize events from the resize channel.
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

/// Get the default shell for the current platform.
fn default_shell() -> String {
    #[cfg(unix)]
    {
        std::env::var("SHELL").unwrap_or_else(|_| "/bin/sh".to_string())
    }
    #[cfg(windows)]
    {
        std::env::var("COMSPEC").unwrap_or_else(|_| "cmd.exe".to_string())
    }
    #[cfg(not(any(unix, windows)))]
    {
        "sh".to_string()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_default_shell_is_not_empty() {
        let shell = default_shell();
        assert!(!shell.is_empty());
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
}
