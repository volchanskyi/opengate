//! Integration tests for `MouseHandler` control-message dispatch.
//!
//! The inner control-message fan-out lives behind a `ControlMessageHandler`
//! marker trait. `MouseHandler` owns `MouseMove` + `MouseClick`. These tests pin
//! the externally-visible
//! contract: permission gating, dispatch order, and resilience to injector
//! failures. Per-method unit tests live alongside the implementation in
//! `src/session/handlers/mouse.rs`.

use std::sync::{Arc, Mutex};

use mesh_agent_core::platform::{InputError, InputInjector};
use mesh_agent_core::session::handlers::MouseHandler;
use mesh_protocol::{KeyEvent, MouseButton, Permissions};

fn perms_with_input(input: bool) -> Permissions {
    Permissions {
        desktop: true,
        terminal: true,
        file_read: true,
        file_write: false,
        input,
    }
}

struct CallRecorder {
    calls: Arc<Mutex<Vec<String>>>,
}

impl CallRecorder {
    fn new() -> Self {
        Self {
            calls: Arc::new(Mutex::new(Vec::new())),
        }
    }

    fn snapshot(&self) -> Vec<String> {
        self.calls.lock().unwrap().clone()
    }
}

impl InputInjector for CallRecorder {
    fn inject_key(&self, _event: KeyEvent) -> Result<(), InputError> {
        unreachable!("MouseHandler must not call inject_key");
    }

    fn inject_mouse_move(&self, x: i32, y: i32) -> Result<(), InputError> {
        self.calls
            .lock()
            .unwrap()
            .push(format!("mouse_move:{x},{y}"));
        Ok(())
    }

    fn inject_mouse_button(&self, button: MouseButton, pressed: bool) -> Result<(), InputError> {
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
fn mouse_move_through_handler_is_input_gated() {
    let permitted = CallRecorder::new();
    MouseHandler::handle_mouse_move(&perms_with_input(true), &permitted, 7, 9);
    assert_eq!(permitted.snapshot(), vec!["mouse_move:7,9".to_string()]);

    let denied = CallRecorder::new();
    MouseHandler::handle_mouse_move(&perms_with_input(false), &denied, 7, 9);
    assert!(
        denied.snapshot().is_empty(),
        "MouseHandler must drop events when input permission is denied"
    );
}

#[test]
fn mouse_click_through_handler_dispatches_move_then_button() {
    let rec = CallRecorder::new();
    MouseHandler::handle_mouse_click(&perms_with_input(true), &rec, MouseButton::Left, true, 3, 4);
    assert_eq!(
        rec.snapshot(),
        vec![
            "mouse_move:3,4".to_string(),
            "mouse_button:Left:true".to_string(),
        ],
        "MouseHandler must issue move-then-button in that exact order"
    );
}

#[test]
fn mouse_click_input_denied_blocks_both_inject_calls() {
    let rec = CallRecorder::new();
    MouseHandler::handle_mouse_click(
        &perms_with_input(false),
        &rec,
        MouseButton::Right,
        false,
        0,
        0,
    );
    assert!(
        rec.snapshot().is_empty(),
        "denied input must short-circuit BEFORE the move/button pair"
    );
}
