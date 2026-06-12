//! Integration test for `KeyboardHandler`.

use std::sync::atomic::AtomicBool;
use std::sync::{Arc, Mutex};

use mesh_agent_core::platform::{InputError, InputInjector};
use mesh_agent_core::session::handlers::KeyboardHandler;
use mesh_agent_core::session::TerminalHandle;
use mesh_protocol::{KeyCode, KeyEvent, MouseButton, Permissions};
use tokio::sync::mpsc;

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
    fn inject_key(&self, event: KeyEvent) -> Result<(), InputError> {
        self.calls
            .lock()
            .unwrap()
            .push(format!("key:{:?}:{}", event.key, event.pressed));
        Ok(())
    }
    fn inject_mouse_move(&self, _x: i32, _y: i32) -> Result<(), InputError> {
        unreachable!("KeyboardHandler must not move the mouse")
    }
    fn inject_mouse_button(&self, _b: MouseButton, _p: bool) -> Result<(), InputError> {
        unreachable!("KeyboardHandler must not press mouse buttons")
    }
    fn is_available(&self) -> bool {
        true
    }
}

fn perms(input: bool) -> Permissions {
    Permissions {
        desktop: true,
        terminal: true,
        file_read: true,
        file_write: false,
        input,
    }
}

#[tokio::test]
async fn key_press_dispatches_to_injector_and_terminal_when_pressed() {
    let rec = CallRecorder::new();
    let (stdin_tx, mut stdin_rx) = mpsc::channel(8);
    let (resize_tx, _resize_rx) = mpsc::channel(8);
    let term = TerminalHandle::new(stdin_tx, resize_tx, Arc::new(AtomicBool::new(false)));

    KeyboardHandler::handle_key_press(&perms(true), &rec, Some(&term), KeyCode::KeyA, true);

    assert_eq!(rec.snapshot(), vec!["key:KeyA:true".to_string()]);
    assert_eq!(
        stdin_rx
            .try_recv()
            .expect("terminal must receive byte for pressed key"),
        b"a"
    );
}

#[test]
fn key_press_input_denied_silences_both_paths() {
    let rec = CallRecorder::new();
    KeyboardHandler::handle_key_press(&perms(false), &rec, None, KeyCode::KeyA, true);
    assert!(rec.snapshot().is_empty());
}
