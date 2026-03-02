use serde::{Deserialize, Serialize};

/// Unique identifier for a device/agent.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub struct DeviceId(pub uuid::Uuid);

impl DeviceId {
    /// Create a new random DeviceId.
    pub fn new() -> Self {
        Self(uuid::Uuid::new_v4())
    }
}

impl Default for DeviceId {
    fn default() -> Self {
        Self::new()
    }
}

/// Unique identifier for a device group.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub struct GroupId(pub uuid::Uuid);

impl GroupId {
    /// Create a new random GroupId.
    pub fn new() -> Self {
        Self(uuid::Uuid::new_v4())
    }
}

impl Default for GroupId {
    fn default() -> Self {
        Self::new()
    }
}

/// Session token for relay connections. 32 random bytes encoded as 64 hex chars.
#[derive(Debug, Clone, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub struct SessionToken(String);

impl SessionToken {
    /// Generate a new random session token (32 bytes = 64 hex chars).
    pub fn generate() -> Self {
        use std::fmt::Write;
        let bytes: [u8; 32] = rand_bytes();
        let mut hex = String::with_capacity(64);
        for b in &bytes {
            write!(hex, "{b:02x}").expect("hex formatting cannot fail");
        }
        Self(hex)
    }

    /// Get the token as a string slice.
    pub fn as_str(&self) -> &str {
        &self.0
    }
}

fn rand_bytes() -> [u8; 32] {
    let mut buf = [0u8; 32];
    getrandom(&mut buf);
    buf
}

fn getrandom(buf: &mut [u8]) {
    use std::collections::hash_map::RandomState;
    use std::hash::{BuildHasher, Hasher};
    for chunk in buf.chunks_mut(8) {
        let s = RandomState::new();
        let val = s.build_hasher().finish().to_le_bytes();
        let len = chunk.len().min(8);
        chunk[..len].copy_from_slice(&val[..len]);
    }
}

/// Capabilities an agent can advertise.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[non_exhaustive]
pub enum AgentCapability {
    RemoteDesktop,
    Terminal,
    FileManager,
    InputInjection,
    ProcessManager,
}

/// Current status of a device.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[non_exhaustive]
pub enum DeviceStatus {
    Online,
    Offline,
    Connecting,
}

/// Permissions granted for a session.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct Permissions {
    pub desktop: bool,
    pub terminal: bool,
    pub file_read: bool,
    pub file_write: bool,
    pub input: bool,
}

/// Encoding format for desktop frame data.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[non_exhaustive]
pub enum FrameEncoding {
    Raw,
    Zlib,
    Zstd,
    H264Idr,
    H264Delta,
}

/// A desktop video frame.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct DesktopFrame {
    pub sequence: u64,
    pub x: u16,
    pub y: u16,
    pub width: u16,
    pub height: u16,
    pub encoding: FrameEncoding,
    #[serde(with = "serde_bytes")]
    pub data: Vec<u8>,
}

/// A terminal data frame.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct TerminalFrame {
    #[serde(with = "serde_bytes")]
    pub data: Vec<u8>,
}

/// A file transfer data frame.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct FileFrame {
    pub offset: u64,
    pub total_size: u64,
    #[serde(with = "serde_bytes")]
    pub data: Vec<u8>,
}

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

/// Handshake messages for agent–server authentication.
/// These use binary encoding (not msgpack), so no Serialize/Deserialize derive.
#[derive(Debug, Clone, PartialEq, Eq)]
#[non_exhaustive]
pub enum HandshakeMessage {
    ServerHello {
        nonce: [u8; 32],
        cert_hash: [u8; 48],
    },
    AgentHello {
        nonce: [u8; 32],
        agent_cert_hash: [u8; 48],
    },
    ServerProof {
        signature: Vec<u8>,
    },
    AgentProof {
        signature: Vec<u8>,
        device_id: DeviceId,
    },
    SkipAuth {
        cached_cert_hash: [u8; 48],
    },
    ExpectHash {
        cert_hash: [u8; 48],
    },
}

/// Handshake message type bytes for binary encoding.
impl HandshakeMessage {
    /// Get the type byte for this handshake message.
    pub fn type_byte(&self) -> u8 {
        match self {
            Self::ServerHello { .. } => 0x10,
            Self::AgentHello { .. } => 0x11,
            Self::ServerProof { .. } => 0x12,
            Self::AgentProof { .. } => 0x13,
            Self::SkipAuth { .. } => 0x14,
            Self::ExpectHash { .. } => 0x15,
        }
    }

    /// Encode to binary format (type byte + fixed fields).
    pub fn encode_binary(&self) -> Vec<u8> {
        let mut buf = Vec::new();
        buf.push(self.type_byte());
        match self {
            Self::ServerHello { nonce, cert_hash } => {
                buf.extend_from_slice(nonce);
                buf.extend_from_slice(cert_hash);
            }
            Self::AgentHello {
                nonce,
                agent_cert_hash,
            } => {
                buf.extend_from_slice(nonce);
                buf.extend_from_slice(agent_cert_hash);
            }
            Self::ServerProof { signature } => {
                buf.extend_from_slice(signature);
            }
            Self::AgentProof {
                signature,
                device_id,
            } => {
                let id_bytes = device_id.0.as_bytes();
                buf.extend_from_slice(id_bytes);
                buf.extend_from_slice(signature);
            }
            Self::SkipAuth { cached_cert_hash } => {
                buf.extend_from_slice(cached_cert_hash);
            }
            Self::ExpectHash { cert_hash } => {
                buf.extend_from_slice(cert_hash);
            }
        }
        buf
    }

    /// Decode from binary format.
    pub fn decode_binary(data: &[u8]) -> Result<Self, crate::ProtocolError> {
        if data.is_empty() {
            return Err(crate::ProtocolError::InvalidHandshake(
                "empty data".to_string(),
            ));
        }
        let type_byte = data[0];
        let payload = &data[1..];

        match type_byte {
            0x10 => {
                if payload.len() != 80 {
                    return Err(crate::ProtocolError::InvalidHandshake(format!(
                        "ServerHello payload must be 80 bytes, got {}",
                        payload.len()
                    )));
                }
                let mut nonce = [0u8; 32];
                let mut cert_hash = [0u8; 48];
                nonce.copy_from_slice(&payload[..32]);
                cert_hash.copy_from_slice(&payload[32..80]);
                Ok(Self::ServerHello { nonce, cert_hash })
            }
            0x11 => {
                if payload.len() != 80 {
                    return Err(crate::ProtocolError::InvalidHandshake(format!(
                        "AgentHello payload must be 80 bytes, got {}",
                        payload.len()
                    )));
                }
                let mut nonce = [0u8; 32];
                let mut agent_cert_hash = [0u8; 48];
                nonce.copy_from_slice(&payload[..32]);
                agent_cert_hash.copy_from_slice(&payload[32..80]);
                Ok(Self::AgentHello {
                    nonce,
                    agent_cert_hash,
                })
            }
            0x12 => Ok(Self::ServerProof {
                signature: payload.to_vec(),
            }),
            0x13 => {
                if payload.len() < 16 {
                    return Err(crate::ProtocolError::InvalidHandshake(
                        "AgentProof too short".to_string(),
                    ));
                }
                let device_id = DeviceId(uuid::Uuid::from_bytes(
                    payload[..16].try_into().map_err(|_| {
                        crate::ProtocolError::InvalidHandshake(
                            "invalid device_id bytes".to_string(),
                        )
                    })?,
                ));
                let signature = payload[16..].to_vec();
                Ok(Self::AgentProof {
                    signature,
                    device_id,
                })
            }
            0x14 => {
                if payload.len() != 48 {
                    return Err(crate::ProtocolError::InvalidHandshake(format!(
                        "SkipAuth payload must be 48 bytes, got {}",
                        payload.len()
                    )));
                }
                let mut cached_cert_hash = [0u8; 48];
                cached_cert_hash.copy_from_slice(payload);
                Ok(Self::SkipAuth { cached_cert_hash })
            }
            0x15 => {
                if payload.len() != 48 {
                    return Err(crate::ProtocolError::InvalidHandshake(format!(
                        "ExpectHash payload must be 48 bytes, got {}",
                        payload.len()
                    )));
                }
                let mut cert_hash = [0u8; 48];
                cert_hash.copy_from_slice(payload);
                Ok(Self::ExpectHash { cert_hash })
            }
            _ => Err(crate::ProtocolError::InvalidHandshake(format!(
                "unknown handshake type: 0x{type_byte:02x}"
            ))),
        }
    }
}
