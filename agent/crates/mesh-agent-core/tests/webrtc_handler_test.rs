//! Integration test for `WebRTCHandler` (ADR-024 §9 carve).
//!
//! Covers the no-peer-conn ICE path. The full offer/answer dance and
//! ICE-with-peer paths require a live webrtc-rs stack and are exercised
//! at the integration-test layer via the existing E2E suite.

use std::sync::Arc;

use mesh_agent_core::session::handlers::WebRTCHandler;
use mesh_agent_core::webrtc::AgentPeerConnection;
use tokio::sync::Mutex;

#[tokio::test]
async fn handle_candidate_with_no_peer_does_not_panic() {
    let webrtc_pc: Arc<Mutex<Option<Arc<AgentPeerConnection>>>> = Arc::new(Mutex::new(None));

    WebRTCHandler::handle_candidate(&webrtc_pc, "candidate:1 1 UDP", "0").await;
}
