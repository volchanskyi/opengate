/** Frame type byte constants matching Rust/Go wire protocol. */
export const FRAME_CONTROL = 0x01;
export const FRAME_DESKTOP = 0x02;
export const FRAME_TERMINAL = 0x03;
export const FRAME_FILE = 0x04;
export const FRAME_PING = 0x05;
export const FRAME_PONG = 0x06;

/** Maximum frame payload size: 16 MiB. */
export const MAX_FRAME_SIZE = 16 * 1024 * 1024;

// ts-prune-ignore-next-line — composed into ControlMessage; part of public protocol surface
export interface Permissions {
  desktop: boolean;
  terminal: boolean;
  file_read: boolean;
  file_write: boolean;
  input: boolean;
}

// ts-prune-ignore-next-line — composed into DesktopFrame; part of public protocol surface
export type FrameEncoding = 'Raw' | 'Zlib' | 'Zstd' | 'H264Idr' | 'H264Delta' | 'Jpeg';

export interface DesktopFrame {
  sequence: number;
  x: number;
  y: number;
  width: number;
  height: number;
  encoding: FrameEncoding;
  data: Uint8Array;
}

export interface TerminalFrame {
  data: Uint8Array;
}

export interface FileFrame {
  offset: number;
  total_size: number;
  data: Uint8Array;
}

export interface ChatMessageFields {
  text: string;
  sender: string;
}

/** Mouse button names matching the Rust `MouseButton` enum. */
export type MouseButton = 'Left' | 'Right' | 'Middle' | 'Back' | 'Forward';

/** Key codes matching the Rust `KeyCode` enum. */
export type KeyCode =
  | 'KeyA' | 'KeyB' | 'KeyC' | 'KeyD' | 'KeyE' | 'KeyF' | 'KeyG'
  | 'KeyH' | 'KeyI' | 'KeyJ' | 'KeyK' | 'KeyL' | 'KeyM' | 'KeyN'
  | 'KeyO' | 'KeyP' | 'KeyQ' | 'KeyR' | 'KeyS' | 'KeyT' | 'KeyU'
  | 'KeyV' | 'KeyW' | 'KeyX' | 'KeyY' | 'KeyZ'
  | 'Digit0' | 'Digit1' | 'Digit2' | 'Digit3' | 'Digit4'
  | 'Digit5' | 'Digit6' | 'Digit7' | 'Digit8' | 'Digit9'
  | 'ShiftLeft' | 'ShiftRight' | 'ControlLeft' | 'ControlRight'
  | 'AltLeft' | 'AltRight' | 'MetaLeft' | 'MetaRight'
  | 'ArrowUp' | 'ArrowDown' | 'ArrowLeft' | 'ArrowRight'
  | 'Home' | 'End' | 'PageUp' | 'PageDown'
  | 'Backspace' | 'Delete' | 'Enter' | 'Tab' | 'Escape' | 'Space'
  | 'Insert' | 'CapsLock' | 'NumLock' | 'ScrollLock'
  | 'F1' | 'F2' | 'F3' | 'F4' | 'F5' | 'F6'
  | 'F7' | 'F8' | 'F9' | 'F10' | 'F11' | 'F12'
  | 'Minus' | 'Equal' | 'BracketLeft' | 'BracketRight' | 'Backslash'
  | 'Semicolon' | 'Quote' | 'Comma' | 'Period' | 'Slash' | 'Backquote'
  | 'Numpad0' | 'Numpad1' | 'Numpad2' | 'Numpad3' | 'Numpad4'
  | 'Numpad5' | 'Numpad6' | 'Numpad7' | 'Numpad8' | 'Numpad9'
  | 'NumpadAdd' | 'NumpadSubtract' | 'NumpadMultiply' | 'NumpadDivide'
  | 'NumpadDecimal' | 'NumpadEnter'
  | 'PrintScreen' | 'Pause';

/** Control messages, internally tagged to match Rust's `#[serde(tag = "type")]`. */
export type ControlMessage =
  | { type: 'AgentRegister'; capabilities: string[]; hostname: string; os: string; arch: string; version: string }
  | { type: 'AgentHeartbeat'; timestamp: number }
  | { type: 'SessionAccept'; token: string; relay_url: string }
  | { type: 'SessionReject'; token: string; reason: string }
  | { type: 'SessionRequest'; token: string; relay_url: string; permissions: Permissions }
  | { type: 'AgentUpdate'; version: string; url: string; signature: string }
  | { type: 'AgentUpdateAck'; version: string; success: boolean; error: string }
  | { type: 'RelayReady' }
  | { type: 'SwitchToWebRTC'; sdp_offer: string }
  | { type: 'SwitchAck' }
  | { type: 'IceCandidate'; candidate: string; mid: string }
  | { type: 'MouseMove'; x: number; y: number }
  | { type: 'MouseClick'; button: MouseButton; pressed: boolean; x: number; y: number }
  | { type: 'KeyPress'; key: KeyCode; pressed: boolean }
  | { type: 'TerminalResize'; cols: number; rows: number }
  | { type: 'FileListRequest'; path: string }
  | { type: 'FileListResponse'; path: string; entries: FileEntry[] }
  | { type: 'FileListError'; path: string; error: string }
  | { type: 'FileDownloadRequest'; path: string }
  | { type: 'FileUploadRequest'; path: string; total_size: number }
  | ({ type: 'ChatMessage' } & ChatMessageFields);

export interface FileEntry {
  name: string;
  is_dir: boolean;
  size: number;
  modified: number;
}

export type Frame =
  | { type: typeof FRAME_CONTROL; message: ControlMessage }
  | { type: typeof FRAME_DESKTOP; frame: DesktopFrame }
  | { type: typeof FRAME_TERMINAL; frame: TerminalFrame }
  | { type: typeof FRAME_FILE; frame: FileFrame }
  | { type: typeof FRAME_PING }
  | { type: typeof FRAME_PONG };
