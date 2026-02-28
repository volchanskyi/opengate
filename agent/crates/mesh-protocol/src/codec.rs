use crate::control::ControlMessage;
use crate::error::ProtocolError;
use crate::types::{DesktopFrame, FileFrame, TerminalFrame};

/// Maximum frame size: 16 MiB.
pub const MAX_FRAME_SIZE: usize = 16 * 1024 * 1024;

/// Frame type byte constants.
pub const FRAME_CONTROL: u8 = 0x01;
pub const FRAME_DESKTOP: u8 = 0x02;
pub const FRAME_TERMINAL: u8 = 0x03;
pub const FRAME_FILE: u8 = 0x04;
pub const FRAME_PING: u8 = 0x05;
pub const FRAME_PONG: u8 = 0x06;

/// A protocol frame that can be sent over the wire.
#[derive(Debug, Clone, PartialEq)]
#[non_exhaustive]
pub enum Frame {
    Control(ControlMessage),
    Desktop(DesktopFrame),
    Terminal(TerminalFrame),
    FileTransfer(FileFrame),
    Ping,
    Pong,
}

impl Frame {
    /// Encode this frame to wire format: [1-byte type][4-byte BE length][payload].
    /// Ping and Pong are encoded as a single type byte with no payload.
    pub fn encode(&self) -> Result<Vec<u8>, ProtocolError> {
        match self {
            Frame::Control(msg) => {
                let payload = rmp_serde::to_vec_named(msg)?;
                encode_frame(FRAME_CONTROL, &payload)
            }
            Frame::Desktop(frame) => {
                let payload = rmp_serde::to_vec_named(frame)?;
                encode_frame(FRAME_DESKTOP, &payload)
            }
            Frame::Terminal(frame) => {
                let payload = rmp_serde::to_vec_named(frame)?;
                encode_frame(FRAME_TERMINAL, &payload)
            }
            Frame::FileTransfer(frame) => {
                let payload = rmp_serde::to_vec_named(frame)?;
                encode_frame(FRAME_FILE, &payload)
            }
            Frame::Ping => Ok(vec![FRAME_PING]),
            Frame::Pong => Ok(vec![FRAME_PONG]),
        }
    }

    /// Decode a frame from wire format.
    /// Returns the frame and number of bytes consumed.
    pub fn decode(data: &[u8]) -> Result<(Frame, usize), ProtocolError> {
        if data.is_empty() {
            return Err(ProtocolError::IncompleteFrame { needed: 1 });
        }

        let type_byte = data[0];

        match type_byte {
            FRAME_PING => Ok((Frame::Ping, 1)),
            FRAME_PONG => Ok((Frame::Pong, 1)),
            FRAME_CONTROL | FRAME_DESKTOP | FRAME_TERMINAL | FRAME_FILE => {
                if data.len() < 5 {
                    return Err(ProtocolError::IncompleteFrame {
                        needed: 5 - data.len(),
                    });
                }
                let length =
                    u32::from_be_bytes([data[1], data[2], data[3], data[4]]) as usize;
                if length > MAX_FRAME_SIZE {
                    return Err(ProtocolError::FrameTooLarge {
                        size: length,
                        max: MAX_FRAME_SIZE,
                    });
                }
                let total = 5 + length;
                if data.len() < total {
                    return Err(ProtocolError::IncompleteFrame {
                        needed: total - data.len(),
                    });
                }
                let payload = &data[5..total];
                let frame = match type_byte {
                    FRAME_CONTROL => {
                        Frame::Control(rmp_serde::from_slice(payload)?)
                    }
                    FRAME_DESKTOP => {
                        Frame::Desktop(rmp_serde::from_slice(payload)?)
                    }
                    FRAME_TERMINAL => {
                        Frame::Terminal(rmp_serde::from_slice(payload)?)
                    }
                    FRAME_FILE => {
                        Frame::FileTransfer(rmp_serde::from_slice(payload)?)
                    }
                    _ => unreachable!(),
                };
                Ok((frame, total))
            }
            _ => Err(ProtocolError::UnknownFrameType(type_byte)),
        }
    }
}

fn encode_frame(type_byte: u8, payload: &[u8]) -> Result<Vec<u8>, ProtocolError> {
    if payload.len() > MAX_FRAME_SIZE {
        return Err(ProtocolError::FrameTooLarge {
            size: payload.len(),
            max: MAX_FRAME_SIZE,
        });
    }
    let length = payload.len() as u32;
    let mut buf = Vec::with_capacity(5 + payload.len());
    buf.push(type_byte);
    buf.extend_from_slice(&length.to_be_bytes());
    buf.extend_from_slice(payload);
    Ok(buf)
}
