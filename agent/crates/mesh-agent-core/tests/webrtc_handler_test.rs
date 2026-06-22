//! Integration test for `WebRTCHandler`.
//!
//! Covers the no-peer-conn ICE path. The full offer/answer dance and
//! ICE-with-peer paths require a live webrtc-rs stack and are exercised
//! at the integration-test layer via the existing E2E suite.

use std::sync::Arc;

use mesh_agent_core::session::handlers::{RealWebRtcDispatch, WebRTCHandler, WebRtcDispatch};
use mesh_agent_core::webrtc::AgentPeerConnection;
use tokio::sync::Mutex;

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
