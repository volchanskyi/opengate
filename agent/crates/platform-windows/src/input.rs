//! Input injection implementations for Windows.
//!
//! On Windows, uses Win32 SendInput for keyboard and mouse injection.
//! On non-Windows targets, this module is empty.

#[cfg(windows)]
mod win32 {
    use mesh_agent_core::{InputError, InputInjector};
    use mesh_protocol::{KeyEvent, MouseButton};

    /// Win32 SendInput-based input injector.
    pub struct Win32Input;

    impl Win32Input {
        /// Create a new Win32 input injector.
        pub fn new() -> Self {
            Self
        }
    }

    impl Default for Win32Input {
        fn default() -> Self {
            Self::new()
        }
    }

    impl InputInjector for Win32Input {
        fn inject_key(&self, _event: KeyEvent) -> Result<(), InputError> {
            // TODO: Map KeyCode to virtual key code, call SendInput
            Err(InputError::Backend(
                "Win32 input not yet implemented".to_string(),
            ))
        }

        fn inject_mouse_move(&self, _x: i32, _y: i32) -> Result<(), InputError> {
            // TODO: Call SendInput with MOUSEINPUT
            Err(InputError::Backend(
                "Win32 input not yet implemented".to_string(),
            ))
        }

        fn inject_mouse_button(
            &self,
            _button: MouseButton,
            _pressed: bool,
        ) -> Result<(), InputError> {
            // TODO: Call SendInput with MOUSEINPUT button flags
            Err(InputError::Backend(
                "Win32 input not yet implemented".to_string(),
            ))
        }

        fn is_available(&self) -> bool {
            true
        }
    }
}

#[cfg(windows)]
pub use win32::Win32Input;
