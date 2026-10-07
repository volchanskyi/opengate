//! Integration test for `SwitchHandler` acknowledgements.

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
