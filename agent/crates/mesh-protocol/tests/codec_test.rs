use mesh_protocol::*;

#[test]
fn test_control_message_roundtrip_msgpack() {
    let messages: Vec<ControlMessage> = vec![
        ControlMessage::AgentRegister {
            capabilities: vec![AgentCapability::RemoteDesktop, AgentCapability::Terminal],
            hostname: "test-machine".to_string(),
            os: "linux".to_string(),
        },
        ControlMessage::AgentHeartbeat {
            timestamp: 1700000000,
        },
        ControlMessage::SessionAccept {
            token: SessionToken::generate(),
            relay_url: "wss://relay.example.com/abc".to_string(),
        },
        ControlMessage::SessionReject {
            token: SessionToken::generate(),
            reason: "user declined".to_string(),
        },
        ControlMessage::SessionRequest {
            token: SessionToken::generate(),
            relay_url: "wss://relay.example.com/def".to_string(),
            permissions: Permissions {
                desktop: true,
                terminal: true,
                file_read: true,
                file_write: false,
                input: true,
            },
        },
        ControlMessage::AgentUpdate {
            version: "1.2.3".to_string(),
            url: "https://update.example.com/agent-1.2.3".to_string(),
            signature: "abc123".to_string(),
        },
        ControlMessage::RelayReady,
        ControlMessage::SwitchToWebRTC {
            sdp_offer: "v=0\r\no=- ...".to_string(),
        },
        ControlMessage::SwitchAck,
        ControlMessage::IceCandidate {
            candidate: "candidate:1 1 UDP ...".to_string(),
            mid: "0".to_string(),
        },
    ];

    for msg in messages {
        let frame = Frame::Control(msg.clone());
        let encoded = frame.encode().expect("encode should succeed");
        let (decoded, consumed) = Frame::decode(&encoded).expect("decode should succeed");
        assert_eq!(consumed, encoded.len());
        match decoded {
            Frame::Control(decoded_msg) => assert_eq!(msg, decoded_msg),
            other => panic!("expected Control frame, got {:?}", other),
        }
    }
}

#[test]
fn test_desktop_frame_roundtrip() {
    let desktop = DesktopFrame {
        sequence: 42,
        x: 10,
        y: 20,
        width: 1920,
        height: 1080,
        encoding: FrameEncoding::Zstd,
        data: vec![0xDE, 0xAD, 0xBE, 0xEF, 0x01, 0x02, 0x03],
    };

    let frame = Frame::Desktop(desktop.clone());
    let encoded = frame.encode().expect("encode should succeed");
    let (decoded, consumed) = Frame::decode(&encoded).expect("decode should succeed");
    assert_eq!(consumed, encoded.len());
    match decoded {
        Frame::Desktop(decoded_frame) => {
            assert_eq!(desktop.sequence, decoded_frame.sequence);
            assert_eq!(desktop.x, decoded_frame.x);
            assert_eq!(desktop.y, decoded_frame.y);
            assert_eq!(desktop.width, decoded_frame.width);
            assert_eq!(desktop.height, decoded_frame.height);
            assert_eq!(desktop.encoding, decoded_frame.encoding);
            assert_eq!(desktop.data, decoded_frame.data);
        }
        other => panic!("expected Desktop frame, got {:?}", other),
    }
}

#[test]
fn test_terminal_frame_roundtrip() {
    let terminal = TerminalFrame {
        data: b"Hello, terminal!\n".to_vec(),
    };
    let frame = Frame::Terminal(terminal.clone());
    let encoded = frame.encode().unwrap();
    let (decoded, _) = Frame::decode(&encoded).unwrap();
    assert_eq!(Frame::Terminal(terminal), decoded);
}

#[test]
fn test_file_frame_roundtrip() {
    let file = FileFrame {
        offset: 1024,
        total_size: 1_048_576,
        data: vec![0xFF; 256],
    };
    let frame = Frame::FileTransfer(file.clone());
    let encoded = frame.encode().unwrap();
    let (decoded, _) = Frame::decode(&encoded).unwrap();
    assert_eq!(Frame::FileTransfer(file), decoded);
}

#[test]
fn test_frame_type_byte_prefix() {
    // Control frames must have type prefix 0x01
    let control = Frame::Control(ControlMessage::RelayReady);
    let encoded = control.encode().unwrap();
    assert_eq!(encoded[0], 0x01);

    // Desktop frames must have type prefix 0x02
    let desktop = Frame::Desktop(DesktopFrame {
        sequence: 0,
        x: 0,
        y: 0,
        width: 1,
        height: 1,
        encoding: FrameEncoding::Raw,
        data: vec![0],
    });
    let encoded = desktop.encode().unwrap();
    assert_eq!(encoded[0], 0x02);

    // Terminal frames must have type prefix 0x03
    let terminal = Frame::Terminal(TerminalFrame { data: vec![b'A'] });
    let encoded = terminal.encode().unwrap();
    assert_eq!(encoded[0], 0x03);

    // File frames must have type prefix 0x04
    let file = Frame::FileTransfer(FileFrame {
        offset: 0,
        total_size: 1,
        data: vec![0],
    });
    let encoded = file.encode().unwrap();
    assert_eq!(encoded[0], 0x04);

    // Ping must be exactly [0x05]
    let ping = Frame::Ping;
    let encoded = ping.encode().unwrap();
    assert_eq!(encoded, vec![0x05]);

    // Pong must be exactly [0x06]
    let pong = Frame::Pong;
    let encoded = pong.encode().unwrap();
    assert_eq!(encoded, vec![0x06]);
}

#[test]
fn test_handshake_binary_encoding() {
    let nonce = [0xAA; 32];
    let cert_hash = [0xBB; 48];

    let msg = HandshakeMessage::ServerHello { nonce, cert_hash };
    let encoded = msg.encode_binary();
    // 1 byte type + 32 byte nonce + 48 byte cert_hash = 81 bytes
    assert_eq!(encoded.len(), 81);
    assert_eq!(encoded[0], 0x10);
    assert_eq!(&encoded[1..33], &nonce);
    assert_eq!(&encoded[33..81], &cert_hash);

    // AgentHello: same structure, different type byte
    let msg = HandshakeMessage::AgentHello {
        nonce,
        agent_cert_hash: cert_hash,
    };
    let encoded = msg.encode_binary();
    assert_eq!(encoded.len(), 81);
    assert_eq!(encoded[0], 0x11);
}

#[test]
fn test_handshake_binary_roundtrip() {
    let nonce = [0xAA; 32];
    let cert_hash = [0xBB; 48];

    let messages = vec![
        HandshakeMessage::ServerHello { nonce, cert_hash },
        HandshakeMessage::AgentHello {
            nonce,
            agent_cert_hash: cert_hash,
        },
        HandshakeMessage::ServerProof {
            signature: vec![1, 2, 3, 4, 5],
        },
        HandshakeMessage::AgentProof {
            signature: vec![6, 7, 8, 9],
            device_id: DeviceId::new(),
        },
        HandshakeMessage::SkipAuth {
            cached_cert_hash: cert_hash,
        },
        HandshakeMessage::ExpectHash { cert_hash },
    ];

    for msg in messages {
        let encoded = msg.encode_binary();
        let decoded = HandshakeMessage::decode_binary(&encoded).expect("decode should succeed");
        assert_eq!(msg, decoded);
    }
}

#[test]
fn test_session_token_is_32_byte_hex() {
    let token = SessionToken::generate();
    assert_eq!(token.as_str().len(), 64); // 32 bytes = 64 hex chars
    assert!(token.as_str().chars().all(|c| c.is_ascii_hexdigit()));
}

#[test]
fn test_session_token_uniqueness() {
    let t1 = SessionToken::generate();
    let t2 = SessionToken::generate();
    assert_ne!(t1, t2);
}

#[test]
fn test_session_token_entropy() {
    use std::collections::HashSet;
    // Generate 100 tokens; all must be unique.
    let tokens: HashSet<String> = (0..100)
        .map(|_| SessionToken::generate().as_str().to_string())
        .collect();
    assert_eq!(tokens.len(), 100, "all 100 tokens must be unique");

    // Verify byte diversity: 32 random bytes should not all be the same value.
    let token = SessionToken::generate();
    let hex = token.as_str();
    let bytes: Vec<u8> = (0..32)
        .map(|i| u8::from_str_radix(&hex[i * 2..i * 2 + 2], 16).unwrap())
        .collect();
    let unique_bytes: HashSet<u8> = bytes.iter().copied().collect();
    assert!(
        unique_bytes.len() > 8,
        "32 random bytes should have diverse values"
    );
}

#[test]
fn test_device_id_stable_across_serialization() {
    let id = DeviceId::new();
    let serialized = rmp_serde::to_vec_named(&id).unwrap();
    let deserialized: DeviceId = rmp_serde::from_slice(&serialized).unwrap();
    assert_eq!(id, deserialized);

    // Also test JSON roundtrip for good measure
    let json = serde_json::to_string(&id).unwrap();
    let from_json: DeviceId = serde_json::from_str(&json).unwrap();
    assert_eq!(id, from_json);
}

#[test]
fn test_frame_decode_incomplete() {
    // Empty data
    let result = Frame::decode(&[]);
    assert!(matches!(result, Err(ProtocolError::IncompleteFrame { .. })));

    // Just a type byte, no length
    let result = Frame::decode(&[0x01]);
    assert!(matches!(result, Err(ProtocolError::IncompleteFrame { .. })));

    // Type byte + partial length
    let result = Frame::decode(&[0x01, 0x00, 0x00]);
    assert!(matches!(result, Err(ProtocolError::IncompleteFrame { .. })));
}

#[test]
fn test_frame_decode_unknown_type() {
    let result = Frame::decode(&[0xFF, 0x00, 0x00, 0x00, 0x01, 0x00]);
    assert!(matches!(result, Err(ProtocolError::UnknownFrameType(0xFF))));
}

#[test]
fn test_codec_never_panics_on_arbitrary_bytes() {
    // Quick manual fuzz with known problematic patterns
    let test_cases: Vec<Vec<u8>> = vec![
        vec![],
        vec![0x00],
        vec![0x01],
        vec![0x01, 0xFF, 0xFF, 0xFF, 0xFF],
        vec![0x02, 0x00, 0x00, 0x00, 0x01, 0x00],
        vec![0x05, 0x06],
        vec![0xFF; 100],
        vec![0x01, 0x00, 0x00, 0x00, 0x00], // Control with 0-byte payload
    ];

    for data in test_cases {
        let _ = Frame::decode(&data);
        // Just verify no panic
    }
}

#[test]
fn test_key_event_msgpack_roundtrip() {
    let events = vec![
        KeyEvent {
            key: KeyCode::KeyA,
            pressed: true,
        },
        KeyEvent {
            key: KeyCode::Enter,
            pressed: false,
        },
        KeyEvent {
            key: KeyCode::F12,
            pressed: true,
        },
        KeyEvent {
            key: KeyCode::NumpadEnter,
            pressed: false,
        },
    ];

    for event in events {
        let encoded = rmp_serde::to_vec_named(&event).expect("encode KeyEvent");
        let decoded: KeyEvent = rmp_serde::from_slice(&encoded).expect("decode KeyEvent");
        assert_eq!(event, decoded);
    }
}

#[test]
fn test_mouse_button_msgpack_roundtrip() {
    let buttons = vec![
        MouseButton::Left,
        MouseButton::Right,
        MouseButton::Middle,
        MouseButton::Back,
        MouseButton::Forward,
    ];

    for button in buttons {
        let encoded = rmp_serde::to_vec_named(&button).expect("encode MouseButton");
        let decoded: MouseButton = rmp_serde::from_slice(&encoded).expect("decode MouseButton");
        assert_eq!(button, decoded);
    }
}

#[test]
fn test_key_code_all_variants_serializable() {
    let codes = vec![
        KeyCode::KeyA,
        KeyCode::KeyZ,
        KeyCode::Digit0,
        KeyCode::Digit9,
        KeyCode::ShiftLeft,
        KeyCode::ControlRight,
        KeyCode::AltLeft,
        KeyCode::MetaRight,
        KeyCode::ArrowUp,
        KeyCode::Home,
        KeyCode::PageDown,
        KeyCode::Backspace,
        KeyCode::Delete,
        KeyCode::Enter,
        KeyCode::Tab,
        KeyCode::Escape,
        KeyCode::Space,
        KeyCode::CapsLock,
        KeyCode::F1,
        KeyCode::F12,
        KeyCode::Minus,
        KeyCode::Backslash,
        KeyCode::Backquote,
        KeyCode::Numpad0,
        KeyCode::NumpadEnter,
        KeyCode::NumpadDivide,
        KeyCode::PrintScreen,
        KeyCode::Pause,
    ];

    for code in codes {
        let event = KeyEvent {
            key: code,
            pressed: true,
        };
        let encoded = rmp_serde::to_vec_named(&event).expect("encode");
        let decoded: KeyEvent = rmp_serde::from_slice(&encoded).expect("decode");
        assert_eq!(event, decoded);
    }
}
