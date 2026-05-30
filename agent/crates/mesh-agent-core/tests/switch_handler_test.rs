//! Integration test for `SwitchHandler` (ADR-024 §9 carve).
//!
//! Pins the externally-visible contract: SwitchAck with no active peer
//! connection silently no-ops (no panic, no frame emission). The
//! peer-present path requires a real `AgentPeerConnection` (live
//! WebRTC stack); that path is exercised at the integration-test layer.

use std::sync::Arc;

use mesh_agent_core::session::handlers::SwitchHandler;
use mesh_agent_core::webrtc::AgentPeerConnection;
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
