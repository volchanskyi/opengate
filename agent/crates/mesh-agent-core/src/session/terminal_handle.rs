//! Terminal handle and key code conversion for relay sessions.

use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;

use mesh_protocol::KeyCode;
use tokio::sync::mpsc;
use tokio::sync::mpsc::error::TrySendError;
use tracing::{trace, warn};

fn log_try_send<T>(err: TrySendError<T>, target: &'static str) {
    match err {
        TrySendError::Full(_) => warn!(channel = target, "terminal channel full, dropping send"),
        TrySendError::Closed(_) => trace!(channel = target, "terminal channel closed during send"),
    }
}

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
            if let Err(e) = self.stdin_tx.try_send(bytes.to_vec()) {
                log_try_send(e, "stdin");
            }
        }
    }

    /// Send raw bytes to the terminal stdin (used for TerminalFrame data from browser).
    pub fn send_raw(&self, data: Vec<u8>) {
        if !data.is_empty() {
            if let Err(e) = self.stdin_tx.try_send(data) {
                log_try_send(e, "stdin");
            }
        }
    }

    /// Resize the terminal.
    pub fn resize(&self, cols: u16, rows: u16) {
        if let Err(e) = self.resize_tx.try_send((cols, rows)) {
            log_try_send(e, "resize");
        }
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

    /// Exhaustive table for every KeyCode -> bytes mapping. Each row pins a
    /// single match arm in `key_to_bytes`, so deleting any arm fails the test.
    #[test]
    fn key_to_bytes_table() {
        let cases: &[(KeyCode, &[u8])] = &[
            // Letters
            (KeyCode::KeyA, b"a"),
            (KeyCode::KeyB, b"b"),
            (KeyCode::KeyC, b"c"),
            (KeyCode::KeyD, b"d"),
            (KeyCode::KeyE, b"e"),
            (KeyCode::KeyF, b"f"),
            (KeyCode::KeyG, b"g"),
            (KeyCode::KeyH, b"h"),
            (KeyCode::KeyI, b"i"),
            (KeyCode::KeyJ, b"j"),
            (KeyCode::KeyK, b"k"),
            (KeyCode::KeyL, b"l"),
            (KeyCode::KeyM, b"m"),
            (KeyCode::KeyN, b"n"),
            (KeyCode::KeyO, b"o"),
            (KeyCode::KeyP, b"p"),
            (KeyCode::KeyQ, b"q"),
            (KeyCode::KeyR, b"r"),
            (KeyCode::KeyS, b"s"),
            (KeyCode::KeyT, b"t"),
            (KeyCode::KeyU, b"u"),
            (KeyCode::KeyV, b"v"),
            (KeyCode::KeyW, b"w"),
            (KeyCode::KeyX, b"x"),
            (KeyCode::KeyY, b"y"),
            (KeyCode::KeyZ, b"z"),
            // Digits
            (KeyCode::Digit0, b"0"),
            (KeyCode::Digit1, b"1"),
            (KeyCode::Digit2, b"2"),
            (KeyCode::Digit3, b"3"),
            (KeyCode::Digit4, b"4"),
            (KeyCode::Digit5, b"5"),
            (KeyCode::Digit6, b"6"),
            (KeyCode::Digit7, b"7"),
            (KeyCode::Digit8, b"8"),
            (KeyCode::Digit9, b"9"),
            // Whitespace / control
            (KeyCode::Enter, b"\r"),
            (KeyCode::Tab, b"\t"),
            (KeyCode::Escape, b"\x1b"),
            (KeyCode::Backspace, b"\x7f"),
            (KeyCode::Space, b" "),
            // Arrows & navigation
            (KeyCode::ArrowUp, b"\x1b[A"),
            (KeyCode::ArrowDown, b"\x1b[B"),
            (KeyCode::ArrowRight, b"\x1b[C"),
            (KeyCode::ArrowLeft, b"\x1b[D"),
            (KeyCode::Home, b"\x1b[H"),
            (KeyCode::End, b"\x1b[F"),
            (KeyCode::PageUp, b"\x1b[5~"),
            (KeyCode::PageDown, b"\x1b[6~"),
            (KeyCode::Delete, b"\x1b[3~"),
            (KeyCode::Insert, b"\x1b[2~"),
            // Function keys
            (KeyCode::F1, b"\x1bOP"),
            (KeyCode::F2, b"\x1bOQ"),
            (KeyCode::F3, b"\x1bOR"),
            (KeyCode::F4, b"\x1bOS"),
            (KeyCode::F5, b"\x1b[15~"),
            (KeyCode::F6, b"\x1b[17~"),
            (KeyCode::F7, b"\x1b[18~"),
            (KeyCode::F8, b"\x1b[19~"),
            (KeyCode::F9, b"\x1b[20~"),
            (KeyCode::F10, b"\x1b[21~"),
            (KeyCode::F11, b"\x1b[23~"),
            (KeyCode::F12, b"\x1b[24~"),
            // Punctuation
            (KeyCode::Minus, b"-"),
            (KeyCode::Equal, b"="),
            (KeyCode::BracketLeft, b"["),
            (KeyCode::BracketRight, b"]"),
            (KeyCode::Backslash, b"\\"),
            (KeyCode::Semicolon, b";"),
            (KeyCode::Quote, b"'"),
            (KeyCode::Comma, b","),
            (KeyCode::Period, b"."),
            (KeyCode::Slash, b"/"),
            (KeyCode::Backquote, b"`"),
        ];
        for (key, expected) in cases {
            assert_eq!(
                key_to_bytes(*key),
                *expected,
                "key_to_bytes({:?}) should be {:?}",
                key,
                expected
            );
        }
    }

    /// Modifier and unmapped keys must produce empty bytes.
    #[test]
    fn key_to_bytes_modifiers_and_unmapped_empty() {
        for k in [
            KeyCode::ShiftLeft,
            KeyCode::ShiftRight,
            KeyCode::ControlLeft,
            KeyCode::ControlRight,
            KeyCode::AltLeft,
            KeyCode::AltRight,
            KeyCode::MetaLeft,
            KeyCode::MetaRight,
            KeyCode::CapsLock,
        ] {
            assert!(
                key_to_bytes(k).is_empty(),
                "modifier {:?} should not produce bytes",
                k
            );
        }
    }

    #[test]
    fn send_key_forwards_bytes_to_stdin_channel() {
        let (stdin_tx, mut stdin_rx) = mpsc::channel(8);
        let (resize_tx, _resize_rx) = mpsc::channel(8);
        let shutdown = Arc::new(AtomicBool::new(false));

        let handle = TerminalHandle::new(stdin_tx, resize_tx, shutdown);
        handle.send_key(KeyCode::KeyA);
        assert_eq!(stdin_rx.try_recv().unwrap(), b"a");
    }

    #[test]
    fn send_key_for_modifier_does_not_send() {
        let (stdin_tx, mut stdin_rx) = mpsc::channel(8);
        let (resize_tx, _resize_rx) = mpsc::channel(8);
        let shutdown = Arc::new(AtomicBool::new(false));

        let handle = TerminalHandle::new(stdin_tx, resize_tx, shutdown);
        handle.send_key(KeyCode::ShiftLeft);
        assert!(matches!(
            stdin_rx.try_recv(),
            Err(mpsc::error::TryRecvError::Empty)
        ));
    }

    #[test]
    fn send_raw_forwards_arbitrary_bytes() {
        let (stdin_tx, mut stdin_rx) = mpsc::channel(8);
        let (resize_tx, _resize_rx) = mpsc::channel(8);
        let shutdown = Arc::new(AtomicBool::new(false));

        let handle = TerminalHandle::new(stdin_tx, resize_tx, shutdown);
        handle.send_raw(vec![1, 2, 3]);
        assert_eq!(stdin_rx.try_recv().unwrap(), vec![1, 2, 3]);
    }

    #[test]
    fn send_raw_drops_empty_payload() {
        let (stdin_tx, mut stdin_rx) = mpsc::channel(8);
        let (resize_tx, _resize_rx) = mpsc::channel(8);
        let shutdown = Arc::new(AtomicBool::new(false));

        let handle = TerminalHandle::new(stdin_tx, resize_tx, shutdown);
        handle.send_raw(vec![]);
        assert!(matches!(
            stdin_rx.try_recv(),
            Err(mpsc::error::TryRecvError::Empty)
        ));
    }

    #[test]
    fn send_key_on_full_channel_does_not_panic() {
        // capacity 1 channel filled before send
        let (stdin_tx, _stdin_rx) = mpsc::channel(1);
        let (resize_tx, _resize_rx) = mpsc::channel(1);
        let shutdown = Arc::new(AtomicBool::new(false));

        let handle = TerminalHandle::new(stdin_tx.clone(), resize_tx, shutdown);
        // Saturate then send — exercises log_try_send Full branch.
        stdin_tx.try_send(vec![0xff]).unwrap();
        handle.send_key(KeyCode::KeyA);
        handle.send_raw(vec![0x42]);
    }

    #[test]
    fn send_key_on_closed_channel_does_not_panic() {
        let (stdin_tx, stdin_rx) = mpsc::channel(8);
        let (resize_tx, resize_rx) = mpsc::channel(8);
        let shutdown = Arc::new(AtomicBool::new(false));

        // Close receivers — exercises log_try_send Closed branch.
        drop(stdin_rx);
        drop(resize_rx);

        let handle = TerminalHandle::new(stdin_tx, resize_tx, shutdown);
        handle.send_key(KeyCode::KeyA);
        handle.send_raw(vec![0x42]);
        handle.resize(80, 24);
    }

    #[test]
    fn resize_forwards_dimensions() {
        let (stdin_tx, _stdin_rx) = mpsc::channel(8);
        let (resize_tx, mut resize_rx) = mpsc::channel(8);
        let shutdown = Arc::new(AtomicBool::new(false));

        let handle = TerminalHandle::new(stdin_tx, resize_tx, shutdown);
        handle.resize(120, 40);
        assert_eq!(resize_rx.try_recv().unwrap(), (120, 40));
    }

    #[test]
    fn shutdown_sets_atomic_flag() {
        let (stdin_tx, _stdin_rx) = mpsc::channel(8);
        let (resize_tx, _resize_rx) = mpsc::channel(8);
        let shutdown = Arc::new(AtomicBool::new(false));

        let handle = TerminalHandle::new(stdin_tx, resize_tx, shutdown.clone());
        assert!(!shutdown.load(Ordering::Relaxed));
        handle.shutdown();
        assert!(shutdown.load(Ordering::Relaxed));
    }
}
