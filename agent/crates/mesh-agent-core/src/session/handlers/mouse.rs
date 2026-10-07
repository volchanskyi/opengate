//! Mouse input control-message handler.

use mesh_protocol::{MouseButton, Permissions};
use tracing::warn;

use crate::platform::InputInjector;

use super::ControlMessageHandler;

/// Handles mouse input messages, gated by `Permissions::input`.
pub struct MouseHandler;

impl ControlMessageHandler for MouseHandler {}

impl MouseHandler {
    /// Injects a mouse move when `permissions.input` is set; injector failures are logged.
    pub fn handle_mouse_move(
        permissions: &Permissions,
        injector: &dyn InputInjector,
        x: u16,
        y: u16,
    ) {
        if !permissions.input {
            return;
        }
        if let Err(e) = injector.inject_mouse_move(x as i32, y as i32) {
            warn!(target: "input", error = %e, "inject_mouse_move failed");
        }
    }

    /// Injects a move then a button event; each runs even when the other fails.
    pub fn handle_mouse_click(
        permissions: &Permissions,
        injector: &dyn InputInjector,
        button: MouseButton,
        pressed: bool,
        x: u16,
        y: u16,
    ) {
        if !permissions.input {
            return;
        }
        if let Err(e) = injector.inject_mouse_move(x as i32, y as i32) {
            warn!(target: "input", error = %e, "inject_mouse_move failed");
        }
        if let Err(e) = injector.inject_mouse_button(button, pressed) {
            warn!(target: "input", error = %e, "inject_mouse_button failed");
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::platform::{InputError, NullInput};
    use mesh_protocol::KeyEvent;
    use std::sync::{Arc, Mutex};

    fn perms(input: bool) -> Permissions {
        Permissions {
            desktop: true,
            terminal: true,
            file_read: true,
            file_write: false,
            input,
        }
    }

    struct RecordingInjector {
        calls: Arc<Mutex<Vec<String>>>,
        fail_move: Arc<Mutex<bool>>,
        fail_button: Arc<Mutex<bool>>,
    }

    impl RecordingInjector {
        fn new() -> Self {
            Self {
                calls: Arc::new(Mutex::new(Vec::new())),
                fail_move: Arc::new(Mutex::new(false)),
                fail_button: Arc::new(Mutex::new(false)),
            }
        }

        fn calls(&self) -> Vec<String> {
            self.calls.lock().unwrap().clone()
        }
    }

    impl InputInjector for RecordingInjector {
        fn inject_key(&self, _event: KeyEvent) -> Result<(), InputError> {
            unreachable!("MouseHandler must not call inject_key");
        }

        fn inject_mouse_move(&self, x: i32, y: i32) -> Result<(), InputError> {
            if *self.fail_move.lock().unwrap() {
                return Err(InputError::Backend("forced".to_string()));
            }
            self.calls
                .lock()
                .unwrap()
                .push(format!("mouse_move:{x},{y}"));
            Ok(())
        }

        fn inject_mouse_button(
            &self,
            button: MouseButton,
            pressed: bool,
        ) -> Result<(), InputError> {
            if *self.fail_button.lock().unwrap() {
                return Err(InputError::Backend("forced".to_string()));
            }
            self.calls
                .lock()
                .unwrap()
                .push(format!("mouse_button:{button:?}:{pressed}"));
            Ok(())
        }

        fn is_available(&self) -> bool {
            true
        }
    }

    #[test]
    fn mouse_move_dispatches_when_input_permitted() {
        let inj = RecordingInjector::new();
        MouseHandler::handle_mouse_move(&perms(true), &inj, 100, 200);
        assert_eq!(inj.calls(), vec!["mouse_move:100,200".to_string()]);
    }

    #[test]
    fn mouse_move_silently_dropped_when_input_denied() {
        let inj = RecordingInjector::new();
        MouseHandler::handle_mouse_move(&perms(false), &inj, 100, 200);
        assert!(inj.calls().is_empty());
    }

    #[test]
    fn mouse_move_injector_failure_does_not_panic() {
        let inj = RecordingInjector::new();
        *inj.fail_move.lock().unwrap() = true;
        MouseHandler::handle_mouse_move(&perms(true), &inj, 1, 2);
        assert!(inj.calls().is_empty());
    }

    #[test]
    fn mouse_move_boundary_u16_max() {
        let inj = RecordingInjector::new();
        MouseHandler::handle_mouse_move(&perms(true), &inj, u16::MAX, u16::MAX);
        assert_eq!(
            inj.calls(),
            vec![format!(
                "mouse_move:{},{}",
                u16::MAX as i32,
                u16::MAX as i32
            )]
        );
    }

    #[test]
    fn mouse_click_dispatches_move_then_button_in_order() {
        let inj = RecordingInjector::new();
        MouseHandler::handle_mouse_click(&perms(true), &inj, MouseButton::Left, true, 10, 20);
        assert_eq!(
            inj.calls(),
            vec![
                "mouse_move:10,20".to_string(),
                "mouse_button:Left:true".to_string(),
            ]
        );
    }

    #[test]
    fn mouse_click_silently_dropped_when_input_denied() {
        let inj = RecordingInjector::new();
        MouseHandler::handle_mouse_click(&perms(false), &inj, MouseButton::Right, false, 5, 5);
        assert!(inj.calls().is_empty());
    }

    #[test]
    fn mouse_click_continues_after_move_failure() {
        let inj = RecordingInjector::new();
        *inj.fail_move.lock().unwrap() = true;
        MouseHandler::handle_mouse_click(&perms(true), &inj, MouseButton::Middle, true, 0, 0);
        assert_eq!(inj.calls(), vec!["mouse_button:Middle:true".to_string()]);
    }

    #[test]
    fn mouse_click_button_failure_does_not_panic() {
        let inj = RecordingInjector::new();
        *inj.fail_button.lock().unwrap() = true;
        MouseHandler::handle_mouse_click(&perms(true), &inj, MouseButton::Left, false, 7, 8);
        assert_eq!(inj.calls(), vec!["mouse_move:7,8".to_string()]);
    }

    #[test]
    fn mouse_click_button_variants_all_dispatch() {
        for button in [MouseButton::Left, MouseButton::Right, MouseButton::Middle] {
            let inj = RecordingInjector::new();
            MouseHandler::handle_mouse_click(&perms(true), &inj, button, true, 1, 2);
            let calls = inj.calls();
            assert_eq!(calls.len(), 2, "expected 2 calls for {button:?}");
            assert!(
                calls[1].starts_with(&format!("mouse_button:{button:?}:")),
                "expected button {button:?} in second call, got {}",
                calls[1]
            );
        }
    }

    #[test]
    fn null_injector_accepts_mouse_calls() {
        let null = NullInput;
        MouseHandler::handle_mouse_move(&perms(true), &null, 1, 1);
        MouseHandler::handle_mouse_click(&perms(true), &null, MouseButton::Left, true, 1, 1);
    }
}
