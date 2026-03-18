//! Keyboard and mouse input types for injection.

use serde::{Deserialize, Serialize};

/// A keyboard key code for input injection.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[non_exhaustive]
pub enum KeyCode {
    // Letters
    KeyA,
    KeyB,
    KeyC,
    KeyD,
    KeyE,
    KeyF,
    KeyG,
    KeyH,
    KeyI,
    KeyJ,
    KeyK,
    KeyL,
    KeyM,
    KeyN,
    KeyO,
    KeyP,
    KeyQ,
    KeyR,
    KeyS,
    KeyT,
    KeyU,
    KeyV,
    KeyW,
    KeyX,
    KeyY,
    KeyZ,
    // Digits
    Digit0,
    Digit1,
    Digit2,
    Digit3,
    Digit4,
    Digit5,
    Digit6,
    Digit7,
    Digit8,
    Digit9,
    // Modifiers
    ShiftLeft,
    ShiftRight,
    ControlLeft,
    ControlRight,
    AltLeft,
    AltRight,
    MetaLeft,
    MetaRight,
    // Navigation
    ArrowUp,
    ArrowDown,
    ArrowLeft,
    ArrowRight,
    Home,
    End,
    PageUp,
    PageDown,
    // Editing
    Backspace,
    Delete,
    Enter,
    Tab,
    Escape,
    Space,
    Insert,
    CapsLock,
    NumLock,
    ScrollLock,
    // Function keys
    F1,
    F2,
    F3,
    F4,
    F5,
    F6,
    F7,
    F8,
    F9,
    F10,
    F11,
    F12,
    // Punctuation / symbols
    Minus,
    Equal,
    BracketLeft,
    BracketRight,
    Backslash,
    Semicolon,
    Quote,
    Comma,
    Period,
    Slash,
    Backquote,
    // Numpad
    Numpad0,
    Numpad1,
    Numpad2,
    Numpad3,
    Numpad4,
    Numpad5,
    Numpad6,
    Numpad7,
    Numpad8,
    Numpad9,
    NumpadAdd,
    NumpadSubtract,
    NumpadMultiply,
    NumpadDivide,
    NumpadDecimal,
    NumpadEnter,
    // Special
    PrintScreen,
    Pause,
}

/// A keyboard event for input injection.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct KeyEvent {
    /// The key code being pressed or released.
    pub key: KeyCode,
    /// True if the key is being pressed, false if released.
    pub pressed: bool,
}

/// Mouse button identifiers.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[non_exhaustive]
pub enum MouseButton {
    Left,
    Right,
    Middle,
    Back,
    Forward,
}
