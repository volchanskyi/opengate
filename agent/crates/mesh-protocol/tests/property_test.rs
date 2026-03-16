use mesh_protocol::*;
use proptest::prelude::*;

proptest! {
    #![proptest_config(ProptestConfig::with_cases(10_000))]

    #[test]
    fn test_codec_never_panics_on_arbitrary_bytes(data: Vec<u8>) {
        // Frame::decode must return Err, never panic
        let _ = Frame::decode(&data);
    }
}

// Strategy to generate arbitrary ControlMessage values
fn arb_session_token() -> impl Strategy<Value = SessionToken> {
    Just(SessionToken::generate())
}

fn arb_capabilities() -> impl Strategy<Value = Vec<AgentCapability>> {
    prop::collection::vec(
        prop_oneof![
            Just(AgentCapability::RemoteDesktop),
            Just(AgentCapability::Terminal),
            Just(AgentCapability::FileManager),
            Just(AgentCapability::InputInjection),
            Just(AgentCapability::ProcessManager),
        ],
        0..5,
    )
}

fn arb_permissions() -> impl Strategy<Value = Permissions> {
    (
        any::<bool>(),
        any::<bool>(),
        any::<bool>(),
        any::<bool>(),
        any::<bool>(),
    )
        .prop_map(
            |(desktop, terminal, file_read, file_write, input)| Permissions {
                desktop,
                terminal,
                file_read,
                file_write,
                input,
            },
        )
}

fn arb_control_message() -> impl Strategy<Value = ControlMessage> {
    prop_oneof![
        (arb_capabilities(), ".*", ".*", ".*", ".*").prop_map(
            |(capabilities, hostname, os, arch, version)| {
                ControlMessage::AgentRegister {
                    capabilities,
                    hostname,
                    os,
                    arch,
                    version,
                }
            },
        ),
        any::<i64>().prop_map(|timestamp| ControlMessage::AgentHeartbeat { timestamp }),
        (arb_session_token(), ".*")
            .prop_map(|(token, relay_url)| { ControlMessage::SessionAccept { token, relay_url } }),
        (arb_session_token(), ".*")
            .prop_map(|(token, reason)| { ControlMessage::SessionReject { token, reason } }),
        (arb_session_token(), ".*", arb_permissions()).prop_map(
            |(token, relay_url, permissions)| {
                ControlMessage::SessionRequest {
                    token,
                    relay_url,
                    permissions,
                }
            }
        ),
        (".*", ".*", ".*").prop_map(|(version, url, signature)| {
            ControlMessage::AgentUpdate {
                version,
                url,
                signature,
            }
        }),
        Just(ControlMessage::RelayReady),
        ".*".prop_map(|sdp_offer| ControlMessage::SwitchToWebRTC { sdp_offer }),
        Just(ControlMessage::SwitchAck),
        (".*", ".*").prop_map(|(candidate, mid)| ControlMessage::IceCandidate { candidate, mid }),
    ]
}

proptest! {
    #![proptest_config(ProptestConfig::with_cases(1_000))]

    #[test]
    fn test_control_message_encode_decode_identity(msg in arb_control_message()) {
        let frame = Frame::Control(msg.clone());
        let encoded = frame.encode().unwrap();
        let (decoded, consumed) = Frame::decode(&encoded).unwrap();
        assert_eq!(consumed, encoded.len());
        match decoded {
            Frame::Control(decoded_msg) => assert_eq!(msg, decoded_msg),
            other => panic!("expected Control frame, got {:?}", other),
        }
    }
}
