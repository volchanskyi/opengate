//! WebRTC switch-ack control-message handler.

use std::sync::Arc;

use mesh_protocol::{ControlMessage, Frame};
use tokio::sync::{mpsc, Mutex};
use tracing::{info, warn};

use super::super::relay::send_frame;
use super::ControlMessageHandler;
use crate::webrtc::AgentPeerConnection;

/// Handles the switch-ack that confirms the browser accepted a WebRTC upgrade.
pub struct SwitchHandler;

impl ControlMessageHandler for SwitchHandler {}

impl SwitchHandler {
    /// Echoes a `SwitchAck` frame to the browser while a peer connection is held.
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
}
