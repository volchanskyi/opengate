//! Agent-side WebRTC peer connection for P2P data channel upgrade.
//!
//! The browser (offerer) creates an SDP offer and sends it via the relay.
//! The agent (answerer) handles the offer, creates an answer, and exchanges
//! ICE candidates until a direct connection is established.

use std::sync::Arc;

use mesh_protocol::{ControlMessage, Frame};
use tokio::sync::{mpsc, Mutex};
use tracing::{debug, info, warn};
use webrtc::api::interceptor_registry::register_default_interceptors;
use webrtc::api::media_engine::MediaEngine;
use webrtc::api::APIBuilder;
use webrtc::data_channel::data_channel_message::DataChannelMessage;
use webrtc::data_channel::RTCDataChannel;
use webrtc::ice_transport::ice_candidate::RTCIceCandidateInit;
use webrtc::ice_transport::ice_server::RTCIceServer;
use webrtc::peer_connection::configuration::RTCConfiguration;
use webrtc::peer_connection::peer_connection_state::RTCPeerConnectionState;
use webrtc::peer_connection::sdp::session_description::RTCSessionDescription;
use webrtc::peer_connection::RTCPeerConnection;

use crate::session_error::SessionError;

/// ICE server configuration received from the server.
#[derive(Debug, Clone)]
pub struct IceServerConfig {
    /// STUN/TURN URLs.
    pub urls: Vec<String>,
    /// Optional username for TURN.
    pub username: String,
    /// Optional credential for TURN.
    pub credential: String,
}

/// Agent-side WebRTC peer connection wrapper.
///
/// Manages the RTCPeerConnection lifecycle as the answerer:
/// receives browser's offer, creates answer, exchanges ICE candidates,
/// and routes data channel messages as protocol frames.
pub struct AgentPeerConnection {
    pc: Arc<RTCPeerConnection>,
    /// Channel for outbound ICE candidates to forward via relay.
    ice_candidate_tx: mpsc::Sender<(String, String)>,
    /// Receiver for outbound ICE candidates (consumed by session handler).
    ice_candidate_rx: Mutex<mpsc::Receiver<(String, String)>>,
    /// Channel for frames received on data channels.
    inbound_frame_tx: mpsc::Sender<Frame>,
    /// Data channels (populated when browser's channels arrive via on_data_channel).
    control_channel: Arc<Mutex<Option<Arc<RTCDataChannel>>>>,
    desktop_channel: Arc<Mutex<Option<Arc<RTCDataChannel>>>>,
    bulk_channel: Arc<Mutex<Option<Arc<RTCDataChannel>>>>,
    /// Tracks whether remote description has been set.
    remote_desc_set: Arc<Mutex<bool>>,
    /// Buffered ICE candidates received before remote description is set.
    pending_candidates: Arc<Mutex<Vec<RTCIceCandidateInit>>>,
}

impl AgentPeerConnection {
    /// Create a new peer connection with the given ICE servers.
    ///
    /// The `inbound_frame_tx` channel receives frames decoded from data channel messages.
    pub async fn new(
        ice_servers: Vec<IceServerConfig>,
        inbound_frame_tx: mpsc::Sender<Frame>,
    ) -> Result<Self, SessionError> {
        let mut m = MediaEngine::default();
        m.register_default_codecs()
            .map_err(|e| SessionError::WebSocket(format!("media engine setup: {e}")))?;

        let mut registry = webrtc::interceptor::registry::Registry::new();
        registry = register_default_interceptors(registry, &mut m)
            .map_err(|e| SessionError::WebSocket(format!("interceptor setup: {e}")))?;

        let api = APIBuilder::new()
            .with_media_engine(m)
            .with_interceptor_registry(registry)
            .build();

        let config = RTCConfiguration {
            ice_servers: ice_servers
                .into_iter()
                .map(|s| RTCIceServer {
                    urls: s.urls,
                    username: s.username,
                    credential: s.credential,
                })
                .collect(),
            ..Default::default()
        };

        let pc = Arc::new(
            api.new_peer_connection(config)
                .await
                .map_err(|e| SessionError::WebSocket(format!("peer connection create: {e}")))?,
        );

        let (ice_tx, ice_rx) = mpsc::channel(32);
        let control_channel = Arc::new(Mutex::new(None));
        let desktop_channel = Arc::new(Mutex::new(None));
        let bulk_channel = Arc::new(Mutex::new(None));
        let remote_desc_set = Arc::new(Mutex::new(false));
        let pending_candidates = Arc::new(Mutex::new(Vec::new()));

        let conn = Self {
            pc: pc.clone(),
            ice_candidate_tx: ice_tx,
            ice_candidate_rx: Mutex::new(ice_rx),
            inbound_frame_tx,
            control_channel: control_channel.clone(),
            desktop_channel: desktop_channel.clone(),
            bulk_channel: bulk_channel.clone(),
            remote_desc_set,
            pending_candidates,
        };

        Self::wire_ice_callback(&pc, conn.ice_candidate_tx.clone());
        Self::wire_state_callback(&pc);
        Self::wire_data_channel_handler(
            &pc,
            conn.inbound_frame_tx.clone(),
            control_channel,
            desktop_channel,
            bulk_channel,
        );

        Ok(conn)
    }

    fn wire_ice_callback(pc: &Arc<RTCPeerConnection>, ice_tx: mpsc::Sender<(String, String)>) {
        pc.on_ice_candidate(Box::new(move |candidate| {
            let tx = ice_tx.clone();
            Box::pin(async move {
                let Some(c) = candidate else {
                    return;
                };
                let json = match c.to_json() {
                    Ok(j) => j,
                    Err(e) => {
                        warn!("failed to serialize ICE candidate: {e}");
                        return;
                    }
                };
                let mid = json.sdp_mid.unwrap_or_default();
                if let Err(e) = tx.send((json.candidate, mid)).await {
                    debug!("ICE candidate channel closed: {e}");
                }
            })
        }));
    }

    fn wire_state_callback(pc: &Arc<RTCPeerConnection>) {
        pc.on_peer_connection_state_change(Box::new(move |state: RTCPeerConnectionState| {
            info!("WebRTC peer connection state: {state}");
            Box::pin(async {})
        }));
    }

    fn wire_data_channel_handler(
        pc: &Arc<RTCPeerConnection>,
        frame_tx: mpsc::Sender<Frame>,
        cc: Arc<Mutex<Option<Arc<RTCDataChannel>>>>,
        dc: Arc<Mutex<Option<Arc<RTCDataChannel>>>>,
        bc: Arc<Mutex<Option<Arc<RTCDataChannel>>>>,
    ) {
        pc.on_data_channel(Box::new(move |d: Arc<RTCDataChannel>| {
            let label = d.label().to_owned();
            let frame_tx = frame_tx.clone();
            let cc = cc.clone();
            let dc = dc.clone();
            let bc = bc.clone();

            debug!(label, "received data channel from browser");

            Box::pin(async move {
                if !Self::store_channel_by_label(&label, &d, &cc, &dc, &bc).await {
                    return;
                }
                Self::wire_data_channel_messages(&d, frame_tx);
            })
        }));
    }

    async fn store_channel_by_label(
        label: &str,
        d: &Arc<RTCDataChannel>,
        cc: &Arc<Mutex<Option<Arc<RTCDataChannel>>>>,
        dc: &Arc<Mutex<Option<Arc<RTCDataChannel>>>>,
        bc: &Arc<Mutex<Option<Arc<RTCDataChannel>>>>,
    ) -> bool {
        match label {
            "control" => {
                *cc.lock().await = Some(d.clone());
                true
            }
            "desktop" => {
                *dc.lock().await = Some(d.clone());
                true
            }
            "bulk" => {
                *bc.lock().await = Some(d.clone());
                true
            }
            other => {
                warn!(channel = other, "unknown data channel label");
                false
            }
        }
    }

    fn wire_data_channel_messages(d: &Arc<RTCDataChannel>, frame_tx: mpsc::Sender<Frame>) {
        d.on_message(Box::new(move |msg: DataChannelMessage| {
            let ftx = frame_tx.clone();
            Box::pin(async move {
                match Frame::decode(&msg.data) {
                    Ok((frame, _)) => {
                        if let Err(e) = ftx.send(frame).await {
                            debug!("WebRTC inbound frame channel closed: {e}");
                        }
                    }
                    Err(e) => {
                        warn!("data channel frame decode error: {e}");
                    }
                }
            })
        }));
    }

    /// Handle an SDP offer from the browser. Returns the SDP answer string.
    pub async fn handle_offer(&self, sdp_offer: &str) -> Result<String, SessionError> {
        let offer = RTCSessionDescription::offer(sdp_offer.to_string())
            .map_err(|e| SessionError::WebSocket(format!("invalid offer SDP: {e}")))?;

        self.pc
            .set_remote_description(offer)
            .await
            .map_err(|e| SessionError::WebSocket(format!("set remote description: {e}")))?;

        // Flush buffered ICE candidates
        {
            *self.remote_desc_set.lock().await = true;
            let mut pending = self.pending_candidates.lock().await;
            for candidate in pending.drain(..) {
                if let Err(e) = self.pc.add_ice_candidate(candidate).await {
                    warn!("failed to add buffered ICE candidate: {e}");
                }
            }
        }

        let answer = self
            .pc
            .create_answer(None)
            .await
            .map_err(|e| SessionError::WebSocket(format!("create answer: {e}")))?;

        self.pc
            .set_local_description(answer)
            .await
            .map_err(|e| SessionError::WebSocket(format!("set local description: {e}")))?;

        let local_desc =
            self.pc.local_description().await.ok_or_else(|| {
                SessionError::WebSocket("no local description after set".to_string())
            })?;

        Ok(local_desc.sdp)
    }

    /// Add a remote ICE candidate from the browser.
    ///
    /// If the remote description hasn't been set yet, the candidate is buffered.
    pub async fn add_ice_candidate(&self, candidate: &str, mid: &str) -> Result<(), SessionError> {
        let init = RTCIceCandidateInit {
            candidate: candidate.to_string(),
            sdp_mid: Some(mid.to_string()),
            ..Default::default()
        };

        if !*self.remote_desc_set.lock().await {
            self.pending_candidates.lock().await.push(init);
            return Ok(());
        }

        self.pc
            .add_ice_candidate(init)
            .await
            .map_err(|e| SessionError::WebSocket(format!("add ICE candidate: {e}")))?;
        Ok(())
    }

    /// Take the next outbound ICE candidate (candidate, mid) to forward via relay.
    ///
    /// Returns `None` when the channel is closed.
    pub async fn next_ice_candidate(&self) -> Option<(String, String)> {
        self.ice_candidate_rx.lock().await.recv().await
    }

    /// Send a frame on the appropriate data channel.
    ///
    /// Control frames go to the control channel, desktop frames to the desktop
    /// channel, and terminal/file frames to the bulk channel.
    pub async fn send_frame(&self, frame: &Frame) -> Result<(), SessionError> {
        let encoded = frame.encode()?;

        let channel = match frame {
            Frame::Control(_) | Frame::Ping | Frame::Pong => self.control_channel.lock().await,
            Frame::Desktop(_) => self.desktop_channel.lock().await,
            Frame::Terminal(_) | Frame::FileTransfer(_) => self.bulk_channel.lock().await,
            _ => {
                return Err(SessionError::WebSocket(
                    "unsupported frame type".to_string(),
                ))
            }
        };

        let ch = channel
            .as_ref()
            .ok_or_else(|| SessionError::WebSocket("data channel not open".to_string()))?;

        ch.send(&bytes::Bytes::from(encoded))
            .await
            .map_err(|e| SessionError::WebSocket(format!("data channel send: {e}")))?;

        Ok(())
    }

    /// Send a control message via the control data channel.
    pub async fn send_control(&self, msg: ControlMessage) -> Result<(), SessionError> {
        self.send_frame(&Frame::Control(msg)).await
    }

    /// Close the peer connection and all data channels.
    pub async fn close(&self) {
        if let Err(e) = self.pc.close().await {
            debug!("error closing peer connection: {e}");
        }
    }
}

/// Convert protocol ICE server configs to the agent format.
pub fn ice_servers_from_strings(urls: Vec<Vec<String>>) -> Vec<IceServerConfig> {
    urls.into_iter()
        .map(|u| IceServerConfig {
            urls: u,
            username: String::new(),
            credential: String::new(),
        })
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_ice_server_config_creation() {
        let config = IceServerConfig {
            urls: vec!["stun:stun.l.google.com:19302".to_string()],
            username: String::new(),
            credential: String::new(),
        };
        assert_eq!(config.urls.len(), 1);
        assert!(config.username.is_empty());
    }

    #[test]
    fn test_ice_servers_from_strings() {
        let servers = ice_servers_from_strings(vec![
            vec!["stun:stun1.example.com:3478".to_string()],
            vec![
                "turn:turn1.example.com:3478".to_string(),
                "turn:turn2.example.com:3478".to_string(),
            ],
        ]);
        assert_eq!(servers.len(), 2);
        assert_eq!(servers[0].urls.len(), 1);
        assert_eq!(servers[1].urls.len(), 2);
    }

    #[test]
    fn test_ice_servers_from_strings_empty() {
        let servers = ice_servers_from_strings(vec![]);
        assert!(servers.is_empty());
    }
}
