//! Handshake message types for agent–server authentication.
//!
//! These use binary encoding (not msgpack), so no Serialize/Deserialize derive.

use super::device::DeviceId;

/// Handshake messages for agent–server authentication.
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
        let cap = match self {
            Self::ServerHello { .. } | Self::AgentHello { .. } => 81,
            Self::SkipAuth { .. } | Self::ExpectHash { .. } => 49,
            Self::ServerProof { signature } => 1 + signature.len(),
            Self::AgentProof { signature, .. } => 17 + signature.len(),
        };
        let mut buf = Vec::with_capacity(cap);
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
