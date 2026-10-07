//! Integration tests for `WebRTCHandler` and `AgentPeerConnection` paths that need no live peer.

use std::sync::Arc;

use mesh_agent_core::session::handlers::{RealWebRtcDispatch, WebRTCHandler, WebRtcDispatch};
use mesh_agent_core::webrtc::AgentPeerConnection;
use mesh_protocol::Frame;
use tokio::sync::{mpsc, Mutex};

#[tokio::test]
async fn handle_candidate_with_no_peer_does_not_panic() {
    let webrtc_pc: Arc<Mutex<Option<Arc<AgentPeerConnection>>>> = Arc::new(Mutex::new(None));

    WebRTCHandler::handle_candidate(&webrtc_pc, "candidate:1 1 UDP", "0").await;
}

#[tokio::test]
async fn real_dispatch_candidate_with_no_peer_does_not_panic() {
    let webrtc_pc: Arc<Mutex<Option<Arc<AgentPeerConnection>>>> = Arc::new(Mutex::new(None));

    let dispatch = RealWebRtcDispatch;
    dispatch
        .candidate(&webrtc_pc, "candidate:1 1 UDP", "0")
        .await;
}

#[tokio::test]
async fn send_frame_before_any_data_channel_reports_closed_channel() {
    let (frame_tx, _frame_rx) = mpsc::channel(4);
    let pc = AgentPeerConnection::new(Vec::new(), frame_tx)
        .await
        .expect("build peer connection");

    let err = pc
        .send_frame(&Frame::Ping)
        .await
        .expect_err("no data channel is open yet");
    assert!(
        err.to_string().contains("data channel not open"),
        "unexpected error: {err}"
    );

    pc.close().await;
}

#[tokio::test]
async fn ice_candidate_before_the_offer_is_buffered() {
    let (frame_tx, _frame_rx) = mpsc::channel(4);
    let pc = AgentPeerConnection::new(Vec::new(), frame_tx)
        .await
        .expect("build peer connection");

    pc.add_ice_candidate("candidate:1 1 UDP 2130706431 192.0.2.1 30000 typ host", "0")
        .await
        .expect("candidate is buffered until the offer lands");

    pc.close().await;
}

#[tokio::test]
async fn malformed_offer_is_rejected() {
    let (frame_tx, _frame_rx) = mpsc::channel(4);
    let pc = AgentPeerConnection::new(Vec::new(), frame_tx)
        .await
        .expect("build peer connection");

    let err = pc
        .handle_offer("this is not an SDP offer")
        .await
        .expect_err("malformed SDP must not be accepted");
    assert!(
        err.to_string().contains("invalid offer SDP"),
        "unexpected error: {err}"
    );

    pc.close().await;
}
