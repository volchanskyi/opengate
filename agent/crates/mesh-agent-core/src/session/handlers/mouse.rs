//! Mouse input control-message handler.
//!
//! Owns `ControlMessage::MouseMove` and `ControlMessage::MouseClick`
//! dispatch. Carved out of [`super::super::handler::SessionHandler`] per
//! ADR-024.

use mesh_protocol::{MouseButton, Permissions};
use tracing::warn;

use crate::platform::InputInjector;

use super::ControlMessageHandler;

/// Handles mouse input messages.
///
/// Unit struct with associated functions — no per-session state needed.
/// `Permissions` and the `InputInjector` are threaded explicitly so the
/// handler is trivially testable in isolation.
pub struct MouseHandler;

impl ControlMessageHandler for MouseHandler {}

impl MouseHandler {
    /// Process a `MouseMove` control message.
    ///
    /// Silently drops the event if `permissions.input` is false. Failed
    /// injector calls are logged at warn but do not propagate — the relay
    /// session continues regardless.
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

    /// Process a `MouseClick` control message.
    ///
    /// Issues `inject_mouse_move` followed by `inject_mouse_button`. Same
    /// permission gate and warn-on-failure posture as `handle_mouse_move`.
    /// A failed move does not short-circuit the button — both attempts run
    /// independently, matching the pre-carve-out behavior.
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

    /// Recording injector — captures every inject call in order, with
    /// per-method failure toggles for negative-path coverage.
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
        // Pins the u16 → i32 widening at the boundary; mutating the cast to
        // `as i16` would overflow at u16::MAX and surface here.
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
        // Smoke: the production NullInput impl (headless/CI) must not panic
        // when called via MouseHandler with input permitted.
        let null = NullInput;
        MouseHandler::handle_mouse_move(&perms(true), &null, 1, 1);
        MouseHandler::handle_mouse_click(&perms(true), &null, MouseButton::Left, true, 1, 1);
    }

    #[test]
    fn mouse_handler_implements_control_message_handler() {
        // Compile-time pin: MouseHandler must be a ControlMessageHandler.
        // Catches accidental removal of the impl marker.
        fn assert_impl<T: ControlMessageHandler>() {}
        assert_impl::<MouseHandler>();
    }
}
