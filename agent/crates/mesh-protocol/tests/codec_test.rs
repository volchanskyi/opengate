use mesh_protocol::*;

#[test]
fn test_control_message_roundtrip_msgpack() {
    let messages: Vec<ControlMessage> = vec![
        ControlMessage::AgentRegister {
            capabilities: vec![AgentCapability::RemoteDesktop, AgentCapability::Terminal],
            hostname: "test-machine".to_string(),
            os: "linux".to_string(),
            arch: "amd64".to_string(),
            version: "0.1.0".to_string(),
        },
        ControlMessage::AgentHeartbeat {
            timestamp: 1700000000,
        },
        ControlMessage::AgentHealthSummary {
            ts: 1700000100,
            org_id: "00000000-0000-0000-0000-000000000002".to_string(),
            node_anomaly_rate: 0.125,
            per_family_rates: vec![
                FamilyAnomalyRate {
                    family: "cpu".to_string(),
                    rate: 0.25,
                },
                FamilyAnomalyRate {
                    family: "process".to_string(),
                    rate: 0.5,
                },
            ],
            recent_bitmask: vec![0xAA, 0x55, 0xF0],
            sampler_ver: "sysinfo-k2".to_string(),
            model_ver: "k2-baseline-v1".to_string(),
        },
        ControlMessage::AgentMetricWindow {
            ts: 1700000160,
            org_id: "00000000-0000-0000-0000-000000000002".to_string(),
            dims: vec![
                MetricDim {
                    name: "cpu.total".to_string(),
                    avg: 42.5,
                },
                MetricDim {
                    name: "mem.rss".to_string(),
                    avg: 2048.0,
                },
            ],
        },
        ControlMessage::ProcessReport {
            ts: 1700000220,
            org_id: "00000000-0000-0000-0000-000000000002".to_string(),
            top_n: vec![ProcessReportEntry {
                rank: 1,
                basename: "postgres".to_string(),
                cmdline_hash: Some("sha256:abcdef".to_string()),
                pid: 4242,
                cpu: 12.5,
                mem: 3.25,
            }],
        },
        ControlMessage::RequestHealthWindow {
            since_ts: 1700000000,
            limit: 12,
        },
        ControlMessage::HealthWindowResponse {
            summaries: vec![HealthSummary {
                ts: 1700000100,
                org_id: "00000000-0000-0000-0000-000000000002".to_string(),
                node_anomaly_rate: 0.125,
                per_family_rates: vec![FamilyAnomalyRate {
                    family: "cpu".to_string(),
                    rate: 0.25,
                }],
                recent_bitmask: vec![0xAA, 0x55, 0xF0],
                sampler_ver: "sysinfo-k2".to_string(),
                model_ver: "k2-baseline-v1".to_string(),
            }],
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
            sha256: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855".to_string(),
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
fn test_max_frame_size_is_16mib() {
    // Pins the constant; replace * with + would yield 16+1024+1024 = 2064.
    assert_eq!(MAX_FRAME_SIZE, 16 * 1024 * 1024);
    assert_eq!(MAX_FRAME_SIZE, 16_777_216);
}

#[test]
fn test_decode_single_byte_ping_returns_ping() {
    // Pins FRAME_PING match arm in Frame::decode.
    let (frame, consumed) = Frame::decode(&[0x05]).expect("decode ping");
    assert_eq!(frame, Frame::Ping);
    assert_eq!(consumed, 1);
}

#[test]
fn test_decode_single_byte_pong_returns_pong() {
    // Pins FRAME_PONG match arm in Frame::decode.
    let (frame, consumed) = Frame::decode(&[0x06]).expect("decode pong");
    assert_eq!(frame, Frame::Pong);
    assert_eq!(consumed, 1);
}

#[test]
fn test_decode_minimum_header_length_succeeds_with_empty_payload() {
    // A 5-byte header (type + length=0) is the boundary for the `data.len() < 5`
    // check. Replacing `<` with `<=` would reject this valid frame.
    let frame = Frame::Control(ControlMessage::RelayReady);
    let encoded = frame.encode().unwrap();
    assert!(encoded.len() >= 5);
    let (_, consumed) = Frame::decode(&encoded).expect("decode 5+ byte frame");
    assert_eq!(consumed, encoded.len());
}

#[test]
fn test_decode_partial_header_reports_correct_needed_bytes() {
    // Tests `needed: 5 - data.len()`. Mutating `-` to `+` would report
    // 5 + data.len() (e.g. 5+3=8 instead of 5-3=2).
    if let Err(ProtocolError::IncompleteFrame { needed }) = Frame::decode(&[0x01, 0x00, 0x00]) {
        assert_eq!(needed, 2, "needed must be 5 - 3 = 2");
    } else {
        panic!("expected IncompleteFrame");
    }
}

#[test]
fn test_decode_partial_payload_reports_correct_needed_bytes() {
    // Type=Control, length=10, but payload only 3 bytes.
    // total = 5 + 10 = 15; data.len() = 5 + 3 = 8; needed = 15 - 8 = 7.
    let mut data = vec![0x01, 0x00, 0x00, 0x00, 0x0A]; // header: control, length=10
    data.extend_from_slice(&[0x01, 0x02, 0x03]); // 3 bytes of payload
    if let Err(ProtocolError::IncompleteFrame { needed }) = Frame::decode(&data) {
        assert_eq!(needed, 7, "needed must be (5+10) - (5+3) = 7");
    } else {
        panic!("expected IncompleteFrame");
    }
}

#[test]
fn test_decode_rejects_length_above_max_frame_size() {
    // length = MAX_FRAME_SIZE + 1 must error; mutating `>` to `>=` would
    // accept exactly MAX_FRAME_SIZE; mutating to `==` would accept anything else.
    let too_big = (MAX_FRAME_SIZE + 1) as u32;
    let mut data = vec![0x01];
    data.extend_from_slice(&too_big.to_be_bytes());
    match Frame::decode(&data) {
        Err(ProtocolError::FrameTooLarge { size, max }) => {
            assert_eq!(size, MAX_FRAME_SIZE + 1);
            assert_eq!(max, MAX_FRAME_SIZE);
        }
        other => panic!("expected FrameTooLarge, got {:?}", other),
    }
}

#[test]
fn test_decode_accepts_length_equal_to_max_frame_size_header() {
    // length = MAX_FRAME_SIZE must NOT error from the >MAX check.
    // We don't have to provide the full payload; we just verify the
    // size check itself doesn't reject this boundary value.
    let exact = MAX_FRAME_SIZE as u32;
    let mut data = vec![0x01];
    data.extend_from_slice(&exact.to_be_bytes());
    // Will fail with IncompleteFrame (because we didn't supply payload),
    // but must NOT fail with FrameTooLarge.
    match Frame::decode(&data) {
        Err(ProtocolError::IncompleteFrame { .. }) => {}
        other => panic!("expected IncompleteFrame, got {:?}", other),
    }
}

#[test]
fn test_encode_frame_rejects_payload_above_max_frame_size() {
    // Build an oversized payload via Frame::Terminal (raw bytes). Mutating
    // the `>` in encode_frame to `>=` would reject exactly MAX_FRAME_SIZE.
    // Use a payload large enough to exceed MAX after MessagePack overhead.
    let frame = Frame::Terminal(TerminalFrame {
        data: vec![0u8; MAX_FRAME_SIZE + 1],
    });
    match frame.encode() {
        Err(ProtocolError::FrameTooLarge { .. }) => {}
        other => panic!("expected FrameTooLarge, got {:?}", other),
    }
}

#[test]
fn test_retired_handshake_proof_types_rejected() {
    for type_byte in [0x12, 0x13] {
        match HandshakeMessage::decode_binary(&[type_byte]) {
            Err(ProtocolError::InvalidHandshake(_)) => {}
            other => panic!("expected InvalidHandshake for retired proof type, got {other:?}"),
        }
    }
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
        Frame::decode(&data).ok();
        // Just verify no panic
    }
}

#[test]
fn test_request_device_logs_missing_fields() {
    // Simulate what Go sends when fields are empty (omitempty drops them).
    // Only "type" is present; all other fields are missing.
    use std::collections::BTreeMap;
    let mut map = BTreeMap::new();
    map.insert("type", "RequestDeviceLogs");
    let encoded = rmp_serde::to_vec_named(&map).unwrap();
    let decoded: ControlMessage = rmp_serde::from_slice(&encoded).unwrap();
    assert_eq!(
        decoded,
        ControlMessage::RequestDeviceLogs {
            log_level: String::new(),
            time_from: String::new(),
            time_to: String::new(),
            search: String::new(),
            log_offset: 0,
            log_limit: 0,
            source: String::new(),
            unit: String::new(),
        }
    );
}

#[test]
fn test_request_health_window_missing_fields() {
    // Simulate what Go sends when fields are empty (omitempty drops them).
    // Only "type" is present; all other fields are missing.
    use std::collections::BTreeMap;
    let mut map = BTreeMap::new();
    map.insert("type", "RequestHealthWindow");
    let encoded = rmp_serde::to_vec_named(&map).unwrap();
    let decoded: ControlMessage = rmp_serde::from_slice(&encoded).unwrap();
    assert_eq!(
        decoded,
        ControlMessage::RequestHealthWindow {
            since_ts: 0,
            limit: 0,
        }
    );
}

#[test]
fn test_edge_sentinel_agent_reports_tolerate_go_omitempty_zero_fields() {
    // Simulate Go encoding a flat ControlMessage where omitempty drops zero-valued
    // Edge-Sentinel fields.
    use std::collections::BTreeMap;

    let decode_type_only = |msg_type: &str| {
        let mut map = BTreeMap::new();
        map.insert("type", msg_type);
        let encoded = rmp_serde::to_vec_named(&map).unwrap();
        rmp_serde::from_slice::<ControlMessage>(&encoded).unwrap()
    };

    assert_eq!(
        decode_type_only("AgentHealthSummary"),
        ControlMessage::AgentHealthSummary {
            ts: 0,
            org_id: String::new(),
            node_anomaly_rate: 0.0,
            per_family_rates: Vec::new(),
            recent_bitmask: Vec::new(),
            sampler_ver: String::new(),
            model_ver: String::new(),
        }
    );
    assert_eq!(
        decode_type_only("AgentMetricWindow"),
        ControlMessage::AgentMetricWindow {
            ts: 0,
            org_id: String::new(),
            dims: Vec::new(),
        }
    );
    assert_eq!(
        decode_type_only("ProcessReport"),
        ControlMessage::ProcessReport {
            ts: 0,
            org_id: String::new(),
            top_n: Vec::new(),
        }
    );
}

#[test]
fn test_agent_health_summary_recent_bitmask_roundtrip() {
    let msg = ControlMessage::AgentHealthSummary {
        ts: 1700000100,
        org_id: "00000000-0000-0000-0000-000000000002".to_string(),
        node_anomaly_rate: 0.125,
        per_family_rates: vec![FamilyAnomalyRate {
            family: "cpu".to_string(),
            rate: 0.25,
        }],
        recent_bitmask: vec![0xAA, 0x55, 0xF0],
        sampler_ver: "sysinfo-k2".to_string(),
        model_ver: "k2-baseline-v1".to_string(),
    };

    let encoded = rmp_serde::to_vec_named(&msg).unwrap();
    assert!(
        encoded
            .windows(5)
            .any(|w| w == [0xC4, 0x03, 0xAA, 0x55, 0xF0]),
        "recent_bitmask must encode as a MessagePack bin blob, not an integer array"
    );
    let decoded: ControlMessage = rmp_serde::from_slice(&encoded).unwrap();
    assert_eq!(msg, decoded);
    match decoded {
        ControlMessage::AgentHealthSummary { recent_bitmask, .. } => {
            assert_eq!(recent_bitmask, vec![0xAA, 0x55, 0xF0]);
        }
        other => panic!("expected AgentHealthSummary, got {other:?}"),
    }
}

#[test]
fn test_unknown_control_tag_decodes_to_catch_all() {
    use std::collections::BTreeMap;

    let mut map = BTreeMap::new();
    map.insert("type", "FutureServerControl");
    let encoded = rmp_serde::to_vec_named(&map).unwrap();

    let decoded: ControlMessage = rmp_serde::from_slice(&encoded).unwrap();
    assert_eq!(decoded, ControlMessage::Unknown);
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
