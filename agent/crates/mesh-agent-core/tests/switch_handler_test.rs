//! Integration test for `SwitchHandler`.
//!
//! Pins the externally-visible contract: SwitchAck with no active peer
//! connection silently no-ops (no panic, no frame emission). The
//! peer-present path requires a real `AgentPeerConnection` (live
//! WebRTC stack); that path is exercised at the integration-test layer.

use std::sync::Arc;

use mesh_agent_core::session::handlers::SwitchHandler;
use mesh_agent_core::webrtc::AgentPeerConnection;
use mesh_protocol::{ControlMessage, Frame};
use tokio::sync::{mpsc, Mutex};

#[tokio::test]
async fn switch_ack_with_no_peer_conn_does_not_emit_frame() {
    let webrtc_pc: Arc<Mutex<Option<Arc<AgentPeerConnection>>>> = Arc::new(Mutex::new(None));
    let (frame_tx, mut frame_rx) = mpsc::channel::<Vec<u8>>(8);

    SwitchHandler::handle_ack(&webrtc_pc, &frame_tx).await;

    assert!(matches!(
        frame_rx.try_recv(),
        Err(mpsc::error::TryRecvError::Empty)
    ));
}

/// With an active peer connection in the slot, `handle_ack` MUST echo a
/// `SwitchAck` control frame back to the browser. This pins both the
/// `guard.is_some()` true-branch and the `handle_ack` body itself — a mutant
/// that replaces the body with `()` (uncaught at the crate-alone mutation
/// baseline) would drop the confirmation and strand the browser mid-upgrade.
/// `AgentPeerConnection::new` is offline-safe: webrtc-rs only touches the
/// network once a local description triggers ICE gathering, which this path
/// never does.
#[tokio::test]
async fn switch_ack_with_peer_emits_switch_ack_frame() {
    let (inbound_tx, _inbound_rx) = mpsc::channel(8);
    let pc = AgentPeerConnection::new(Vec::new(), inbound_tx)
        .await
        .expect("peer connection construction is offline-safe");
    let webrtc_pc: Arc<Mutex<Option<Arc<AgentPeerConnection>>>> =
        Arc::new(Mutex::new(Some(Arc::new(pc))));
    let (frame_tx, mut frame_rx) = mpsc::channel::<Vec<u8>>(8);

    SwitchHandler::handle_ack(&webrtc_pc, &frame_tx).await;

    let data = frame_rx.try_recv().expect("expected a SwitchAck frame");
    let (frame, _) = Frame::decode(&data).expect("decode SwitchAck frame");
    assert!(matches!(frame, Frame::Control(ControlMessage::SwitchAck)));
}
