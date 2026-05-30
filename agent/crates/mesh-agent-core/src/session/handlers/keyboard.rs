//! Keyboard input control-message handler.
//!
//! Owns `ControlMessage::KeyPress` dispatch. Carved out of
//! [`super::super::handler::SessionHandler`] per ADR-024.

use mesh_protocol::{KeyCode, KeyEvent, Permissions};
use tracing::warn;

use super::super::terminal_handle::TerminalHandle;
use super::ControlMessageHandler;
use crate::platform::InputInjector;

/// Handles keyboard input messages.
///
/// Unit struct with associated functions — no per-session state. The
/// injection path runs only when `permissions.input` is true; the
/// terminal echo path runs only when a terminal session is active AND
/// the key event is `pressed = true` (release events don't go to PTY
/// stdin in the existing protocol).
pub struct KeyboardHandler;

impl ControlMessageHandler for KeyboardHandler {}

impl KeyboardHandler {
    /// Process a `KeyPress` control message.
    pub fn handle_key_press(
        permissions: &Permissions,
        injector: &dyn InputInjector,
        terminal: Option<&TerminalHandle>,
        key: KeyCode,
        pressed: bool,
    ) {
        if permissions.input {
            if let Err(e) = injector.inject_key(KeyEvent { key, pressed }) {
                warn!(target: "input", error = %e, "inject_key failed");
            }
        }
        if let Some(term) = terminal {
            if pressed {
                term.send_key(key);
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::platform::InputError;
    use mesh_protocol::MouseButton;
    use std::sync::atomic::AtomicBool;
    use std::sync::Arc;
    use std::sync::Mutex;
    use tokio::sync::mpsc;

    fn perms(input: bool) -> Permissions {
        Permissions {
            desktop: true,
            terminal: true,
            file_read: true,
            file_write: false,
            input,
        }
    }

    struct Rec {
        calls: Arc<Mutex<Vec<String>>>,
        fail: Arc<Mutex<bool>>,
    }
    impl Rec {
        fn new() -> Self {
            Self {
                calls: Arc::new(Mutex::new(Vec::new())),
                fail: Arc::new(Mutex::new(false)),
            }
        }
        fn calls(&self) -> Vec<String> {
            self.calls.lock().unwrap().clone()
        }
    }
    impl InputInjector for Rec {
        fn inject_key(&self, e: KeyEvent) -> Result<(), InputError> {
            if *self.fail.lock().unwrap() {
                return Err(InputError::Backend("forced".to_string()));
            }
            self.calls
                .lock()
                .unwrap()
                .push(format!("key:{:?}:{}", e.key, e.pressed));
            Ok(())
        }
        fn inject_mouse_move(&self, _x: i32, _y: i32) -> Result<(), InputError> {
            unreachable!()
        }
        fn inject_mouse_button(&self, _b: MouseButton, _p: bool) -> Result<(), InputError> {
            unreachable!()
        }
        fn is_available(&self) -> bool {
            true
        }
    }

    fn term() -> (TerminalHandle, mpsc::Receiver<Vec<u8>>) {
        let (stdin_tx, stdin_rx) = mpsc::channel(8);
        let (resize_tx, _) = mpsc::channel(8);
        (
            TerminalHandle::new(stdin_tx, resize_tx, Arc::new(AtomicBool::new(false))),
            stdin_rx,
        )
    }

    #[tokio::test]
    async fn key_press_with_input_permitted_injects() {
        let rec = Rec::new();
        KeyboardHandler::handle_key_press(&perms(true), &rec, None, KeyCode::KeyA, true);
        assert_eq!(rec.calls(), vec!["key:KeyA:true".to_string()]);
    }

    #[tokio::test]
    async fn key_press_with_input_denied_does_not_inject() {
        let rec = Rec::new();
        KeyboardHandler::handle_key_press(&perms(false), &rec, None, KeyCode::KeyA, true);
        assert!(rec.calls().is_empty());
    }

    #[tokio::test]
    async fn key_press_with_terminal_pressed_sends_byte() {
        let rec = Rec::new();
        let (t, mut stdin_rx) = term();
        KeyboardHandler::handle_key_press(&perms(true), &rec, Some(&t), KeyCode::KeyA, true);
        assert_eq!(
            stdin_rx.try_recv().expect("terminal must receive byte"),
            b"a"
        );
    }

    #[tokio::test]
    async fn key_press_with_terminal_released_does_not_send_byte() {
        let rec = Rec::new();
        let (t, mut stdin_rx) = term();
        KeyboardHandler::handle_key_press(&perms(true), &rec, Some(&t), KeyCode::KeyA, false);
        assert!(matches!(
            stdin_rx.try_recv(),
            Err(mpsc::error::TryRecvError::Empty)
        ));
    }

    #[tokio::test]
    async fn key_press_injector_failure_does_not_panic() {
        let rec = Rec::new();
        *rec.fail.lock().unwrap() = true;
        KeyboardHandler::handle_key_press(&perms(true), &rec, None, KeyCode::KeyA, true);
    }

    #[test]
    fn keyboard_handler_implements_control_message_handler() {
        fn assert_impl<T: ControlMessageHandler>() {}
        assert_impl::<KeyboardHandler>();
    }
}
