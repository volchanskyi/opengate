//! WebRTC switch-ack control-message handler.
//!
//! Owns `ControlMessage::SwitchAck` dispatch so switch acknowledgements remain
//! isolated from the [`super::super::handler::SessionHandler`] multiplexer.

use std::sync::Arc;

use mesh_protocol::{ControlMessage, Frame};
use tokio::sync::{mpsc, Mutex};
use tracing::{info, warn};

use super::super::relay::send_frame;
use super::ControlMessageHandler;
use crate::webrtc::AgentPeerConnection;

/// Handles the WebRTC switch-ack message that confirms the browser has
/// accepted a WebRTC upgrade. Silently drops the ack when no peer
/// connection is active (legitimate transient state during teardown).
pub struct SwitchHandler;

impl ControlMessageHandler for SwitchHandler {}

impl SwitchHandler {
    /// Process a `SwitchAck` control message. Confirms the WebRTC upgrade
    /// to the browser by echoing back a SwitchAck frame; no-op when no
    /// peer connection is currently held.
    pub async fn handle_ack(
        webrtc_pc: &Arc<Mutex<Option<Arc<AgentPeerConnection>>>>,
        frame_tx: &mpsc::Sender<Vec<u8>>,
    ) {
        let guard = webrtc_pc.lock().await;
        if guard.is_some() {
            info!("WebRTC switch acknowledged by browser");
            if let Err(e) = send_frame(frame_tx, &Frame::Control(ControlMessage::SwitchAck)).await {
                warn!("failed to send switch ack: {e}");
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn ack_without_peer_does_not_emit_frame() {
        let webrtc_pc: Arc<Mutex<Option<Arc<AgentPeerConnection>>>> = Arc::new(Mutex::new(None));
        let (frame_tx, mut frame_rx) = mpsc::channel::<Vec<u8>>(8);

        SwitchHandler::handle_ack(&webrtc_pc, &frame_tx).await;

        assert!(matches!(
            frame_rx.try_recv(),
            Err(mpsc::error::TryRecvError::Empty)
        ));
    }

    #[test]
    fn switch_handler_implements_control_message_handler() {
        fn assert_impl<T: ControlMessageHandler>() {}
        assert_impl::<SwitchHandler>();
    }
}
