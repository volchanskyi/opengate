//! Integration test for `WebRTCHandler`.
//!
//! Covers the no-peer-conn ICE path. The full offer/answer dance and
//! ICE-with-peer paths require a live webrtc-rs stack and are exercised
//! at the integration-test layer via the existing E2E suite.

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

/// The production `RealWebRtcDispatch` must delegate `candidate` to
/// `WebRTCHandler::handle_candidate`, which silently no-ops when no peer
/// connection is held. Exercising it through the trait pins the delegation
/// wiring used by `SessionHandler` in production.
#[tokio::test]
async fn real_dispatch_candidate_with_no_peer_does_not_panic() {
    let webrtc_pc: Arc<Mutex<Option<Arc<AgentPeerConnection>>>> = Arc::new(Mutex::new(None));

    let dispatch = RealWebRtcDispatch;
    dispatch
        .candidate(&webrtc_pc, "candidate:1 1 UDP", "0")
        .await;
}

/// A peer connection is built before the browser opens any data channel, so
/// every send has nowhere to go until `on_data_channel` fires. The send must
/// report that rather than drop the frame: the session handler logs the error
/// and keeps the relay path, and a silent drop would strand the session on a
/// half-built peer connection.
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

/// The browser trickles ICE candidates as soon as it gathers them, which can
/// beat its own offer through the relay. A candidate that arrives before the
/// remote description is buffered and accepted, never rejected.
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

/// An offer that is not SDP is rejected with an error the handler can log,
/// leaving the peer-connection slot to be cleared instead of half-negotiated.
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
