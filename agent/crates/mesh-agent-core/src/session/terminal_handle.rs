//! Terminal handle and key code conversion for relay sessions.

use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;

use mesh_protocol::KeyCode;
use tokio::sync::mpsc;

/// Terminal handle for sending data and managing lifecycle.
pub struct TerminalHandle {
    stdin_tx: mpsc::Sender<Vec<u8>>,
    resize_tx: mpsc::Sender<(u16, u16)>,
    shutdown: Arc<AtomicBool>,
}

impl TerminalHandle {
    /// Create a new terminal handle.
    pub fn new(
        stdin_tx: mpsc::Sender<Vec<u8>>,
        resize_tx: mpsc::Sender<(u16, u16)>,
        shutdown: Arc<AtomicBool>,
    ) -> Self {
        Self {
            stdin_tx,
            resize_tx,
            shutdown,
        }
    }

    /// Send a key press to the terminal stdin.
    pub fn send_key(&self, key: KeyCode) {
        let bytes = key_to_bytes(key);
        if !bytes.is_empty() {
            let _ = self.stdin_tx.try_send(bytes.to_vec());
        }
    }

    /// Send raw bytes to the terminal stdin (used for TerminalFrame data from browser).
    pub fn send_raw(&self, data: Vec<u8>) {
        if !data.is_empty() {
            let _ = self.stdin_tx.try_send(data);
        }
    }

    /// Resize the terminal.
    pub fn resize(&self, cols: u16, rows: u16) {
        let _ = self.resize_tx.try_send((cols, rows));
    }

    /// Signal the terminal to shut down.
    pub fn shutdown(&self) {
        self.shutdown.store(true, Ordering::Relaxed);
    }
}

/// Convert a KeyCode to terminal-compatible bytes.
pub(crate) fn key_to_bytes(key: KeyCode) -> &'static [u8] {
    use KeyCode::*;
    match key {
        // Letters (a-z)
        KeyA => b"a",
        KeyB => b"b",
        KeyC => b"c",
        KeyD => b"d",
        KeyE => b"e",
        KeyF => b"f",
        KeyG => b"g",
        KeyH => b"h",
        KeyI => b"i",
        KeyJ => b"j",
        KeyK => b"k",
        KeyL => b"l",
        KeyM => b"m",
        KeyN => b"n",
        KeyO => b"o",
        KeyP => b"p",
        KeyQ => b"q",
        KeyR => b"r",
        KeyS => b"s",
        KeyT => b"t",
        KeyU => b"u",
        KeyV => b"v",
        KeyW => b"w",
        KeyX => b"x",
        KeyY => b"y",
        KeyZ => b"z",
        // Digits (0-9)
        Digit0 => b"0",
        Digit1 => b"1",
        Digit2 => b"2",
        Digit3 => b"3",
        Digit4 => b"4",
        Digit5 => b"5",
        Digit6 => b"6",
        Digit7 => b"7",
        Digit8 => b"8",
        Digit9 => b"9",
        // Whitespace / control
        Enter => b"\r",
        Tab => b"\t",
        Escape => b"\x1b",
        Backspace => b"\x7f",
        Space => b" ",
        // Arrows & navigation
        ArrowUp => b"\x1b[A",
        ArrowDown => b"\x1b[B",
        ArrowRight => b"\x1b[C",
        ArrowLeft => b"\x1b[D",
        Home => b"\x1b[H",
        End => b"\x1b[F",
        PageUp => b"\x1b[5~",
        PageDown => b"\x1b[6~",
        Delete => b"\x1b[3~",
        Insert => b"\x1b[2~",
        // Function keys
        F1 => b"\x1bOP",
        F2 => b"\x1bOQ",
        F3 => b"\x1bOR",
        F4 => b"\x1bOS",
        F5 => b"\x1b[15~",
        F6 => b"\x1b[17~",
        F7 => b"\x1b[18~",
        F8 => b"\x1b[19~",
        F9 => b"\x1b[20~",
        F10 => b"\x1b[21~",
        F11 => b"\x1b[23~",
        F12 => b"\x1b[24~",
        // Punctuation
        Minus => b"-",
        Equal => b"=",
        BracketLeft => b"[",
        BracketRight => b"]",
        Backslash => b"\\",
        Semicolon => b";",
        Quote => b"'",
        Comma => b",",
        Period => b".",
        Slash => b"/",
        Backquote => b"`",
        // Modifiers and special keys don't produce bytes
        _ => b"",
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_key_to_bytes_letters() {
        assert_eq!(key_to_bytes(KeyCode::KeyA), b"a");
        assert_eq!(key_to_bytes(KeyCode::KeyZ), b"z");
    }

    #[test]
    fn test_key_to_bytes_special() {
        assert_eq!(key_to_bytes(KeyCode::Enter), b"\r");
        assert_eq!(key_to_bytes(KeyCode::Escape), b"\x1b");
        assert_eq!(key_to_bytes(KeyCode::ArrowUp), b"\x1b[A");
    }

    #[test]
    fn test_key_to_bytes_modifiers_empty() {
        assert!(key_to_bytes(KeyCode::ShiftLeft).is_empty());
        assert!(key_to_bytes(KeyCode::ControlLeft).is_empty());
    }

    #[test]
    fn test_terminal_handle_send_and_resize() {
        let (stdin_tx, mut stdin_rx) = mpsc::channel(8);
        let (resize_tx, mut resize_rx) = mpsc::channel(8);
        let shutdown = Arc::new(AtomicBool::new(false));

        let handle = TerminalHandle::new(stdin_tx, resize_tx, shutdown.clone());

        handle.send_key(KeyCode::KeyA);
        let data = stdin_rx.try_recv().unwrap();
        assert_eq!(data, b"a");

        handle.resize(120, 40);
        let (cols, rows) = resize_rx.try_recv().unwrap();
        assert_eq!((cols, rows), (120, 40));

        handle.shutdown();
        assert!(shutdown.load(Ordering::Relaxed));
    }
}
