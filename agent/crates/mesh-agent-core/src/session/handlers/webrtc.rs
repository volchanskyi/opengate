//! WebRTC control-message handler.
//!
//! Owns `ControlMessage::SwitchToWebRTC` (offer/answer SDP exchange +
//! peer-connection setup + ICE candidate forwarding) and
//! `ControlMessage::IceCandidate` dispatch so WebRTC negotiation remains
//! isolated from the [`super::super::handler::SessionHandler`] multiplexer.

use std::sync::Arc;

use mesh_protocol::{ControlMessage, Frame};
use tokio::sync::{mpsc, Mutex};
use tracing::{debug, info, warn};

use super::super::relay::send_frame;
use super::ControlMessageHandler;
use crate::webrtc::{AgentPeerConnection, IceServerConfig};

/// Handles WebRTC signaling messages: offer/answer setup (SwitchToWebRTC)
/// and incremental ICE candidate exchange. Spawns background tasks to
/// forward ICE candidates and inbound data-channel frames back through
/// the relay frame channel.
pub struct WebRTCHandler;

impl ControlMessageHandler for WebRTCHandler {}

impl WebRTCHandler {
    /// Process a `SwitchToWebRTC` control message. Creates a peer
    /// connection against the provided ICE server config, applies the
    /// browser's SDP offer, sends back the answer, and spawns background
    /// tasks for ICE forwarding + inbound data-channel frame relay.
    pub async fn handle_offer(
        ice_servers: Vec<IceServerConfig>,
        sdp_offer: String,
        frame_tx: &mpsc::Sender<Vec<u8>>,
        webrtc_pc: &Arc<Mutex<Option<Arc<AgentPeerConnection>>>>,
    ) {
        info!("received WebRTC offer, creating answer");
        let tx = frame_tx.clone();
        let pc_slot = webrtc_pc.clone();

        let (inbound_tx, mut inbound_rx) = mpsc::channel::<Frame>(64);
        match AgentPeerConnection::new(ice_servers, inbound_tx).await {
            Ok(pc) => {
                let pc = Arc::new(pc);
                *pc_slot.lock().await = Some(pc.clone());

                match pc.handle_offer(&sdp_offer).await {
                    Ok(answer_sdp) => {
                        if let Err(e) = send_frame(
                            &tx,
                            &Frame::Control(ControlMessage::SwitchToWebRTC {
                                sdp_offer: answer_sdp,
                            }),
                        )
                        .await
                        {
                            warn!("failed to send WebRTC answer: {e}");
                        }

                        // ICE candidate forwarding task.
                        let pc_ice = pc.clone();
                        let tx_ice = tx.clone();
                        tokio::spawn(async move {
                            while let Some((candidate, mid)) = pc_ice.next_ice_candidate().await {
                                if let Err(e) = send_frame(
                                    &tx_ice,
                                    &Frame::Control(ControlMessage::IceCandidate {
                                        candidate,
                                        mid,
                                    }),
                                )
                                .await
                                {
                                    warn!("failed to forward ICE candidate: {e}");
                                }
                            }
                        });

                        // Inbound data-channel frame relay task.
                        let tx_inbound = tx.clone();
                        tokio::spawn(async move {
                            while let Some(frame) = inbound_rx.recv().await {
                                let encoded = match frame.encode() {
                                    Ok(e) => e,
                                    Err(e) => {
                                        warn!("frame encode error from WebRTC: {e}");
                                        continue;
                                    }
                                };
                                if tx_inbound.send(encoded).await.is_err() {
                                    warn!("inbound WebRTC frame channel closed");
                                    break;
                                }
                            }
                        });
                    }
                    Err(e) => {
                        warn!("failed to handle WebRTC offer: {e}");
                        *pc_slot.lock().await = None;
                    }
                }
            }
            Err(e) => {
                warn!("failed to create WebRTC peer connection: {e}");
            }
        }
    }

    /// Process an `IceCandidate` control message. Forwards the candidate
    /// to the active peer connection; silently no-ops when no peer
    /// connection is held (legitimate transient state during teardown).
    pub async fn handle_candidate(
        webrtc_pc: &Arc<Mutex<Option<Arc<AgentPeerConnection>>>>,
        candidate: &str,
        mid: &str,
    ) {
        let guard = webrtc_pc.lock().await;
        if let Some(ref pc) = *guard {
            if let Err(e) = pc.add_ice_candidate(candidate, mid).await {
                warn!("failed to add ICE candidate: {e}");
            }
        } else {
            debug!("ICE candidate received but no WebRTC connection active");
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn candidate_with_no_peer_silently_drops() {
        let webrtc_pc: Arc<Mutex<Option<Arc<AgentPeerConnection>>>> = Arc::new(Mutex::new(None));
        WebRTCHandler::handle_candidate(&webrtc_pc, "candidate:1 1 UDP", "0").await;
    }

    #[test]
    fn webrtc_handler_implements_control_message_handler() {
        fn assert_impl<T: ControlMessageHandler>() {}
        assert_impl::<WebRTCHandler>();
    }
}
