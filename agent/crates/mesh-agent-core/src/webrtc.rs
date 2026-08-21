//! Agent-side WebRTC peer connection for P2P data channel upgrade.
//!
//! The browser (offerer) creates an SDP offer and sends it via the relay.
//! The agent (answerer) handles the offer, creates an answer, and exchanges
//! ICE candidates until a direct connection is established.

use std::sync::Arc;

use bytes::BytesMut;
use mesh_protocol::{ControlMessage, Frame};
use tokio::sync::{mpsc, Mutex};
use tracing::{debug, info, warn};
use webrtc::data_channel::{DataChannel, DataChannelEvent};
use webrtc::peer_connection::{
    PeerConnection, PeerConnectionBuilder, PeerConnectionEventHandler, RTCConfigurationBuilder,
    RTCIceCandidateInit, RTCIceServer, RTCPeerConnectionIceEvent, RTCPeerConnectionState,
    RTCSessionDescription,
};

use crate::session_error::SessionError;

/// Local address the peer connection binds for ICE. Port 0 lets the OS pick,
/// and the gathered host candidates are trickled to the browser from there.
const UDP_BIND_ADDR: &str = "0.0.0.0:0";

/// One of the three labelled data channels the browser opens, held from the
/// moment `on_data_channel` fires until the connection closes.
type ChannelSlot = Arc<Mutex<Option<Arc<dyn DataChannel>>>>;

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
/// Manages the peer connection lifecycle as the answerer:
/// receives browser's offer, creates answer, exchanges ICE candidates,
/// and routes data channel messages as protocol frames.
pub struct AgentPeerConnection {
    pc: Arc<dyn PeerConnection>,
    /// Receiver for outbound ICE candidates (consumed by session handler).
    ice_candidate_rx: Mutex<mpsc::Receiver<(String, String)>>,
    /// Data channels (populated when browser's channels arrive via on_data_channel).
    control_channel: ChannelSlot,
    desktop_channel: ChannelSlot,
    bulk_channel: ChannelSlot,
    /// Tracks whether remote description has been set.
    remote_desc_set: Mutex<bool>,
    /// Buffered ICE candidates received before remote description is set.
    pending_candidates: Mutex<Vec<RTCIceCandidateInit>>,
}

/// Peer-connection event sink. The driver task owns the connection and calls
/// these as ICE candidates are gathered, the connection state moves, and the
/// browser's data channels arrive.
struct AgentEventHandler {
    /// Channel for outbound ICE candidates to forward via relay.
    ice_candidate_tx: mpsc::Sender<(String, String)>,
    /// Channel for frames received on data channels.
    inbound_frame_tx: mpsc::Sender<Frame>,
    control_channel: ChannelSlot,
    desktop_channel: ChannelSlot,
    bulk_channel: ChannelSlot,
}

#[async_trait::async_trait]
impl PeerConnectionEventHandler for AgentEventHandler {
    async fn on_ice_candidate(&self, event: RTCPeerConnectionIceEvent) {
        let json = match event.candidate.to_json() {
            Ok(j) => j,
            Err(e) => {
                warn!("failed to serialize ICE candidate: {e}");
                return;
            }
        };
        let mid = json.sdp_mid.unwrap_or_default();
        if let Err(e) = self.ice_candidate_tx.send((json.candidate, mid)).await {
            debug!("ICE candidate channel closed: {e}");
        }
    }

    async fn on_connection_state_change(&self, state: RTCPeerConnectionState) {
        info!("WebRTC peer connection state: {state}");
    }

    async fn on_data_channel(&self, data_channel: Arc<dyn DataChannel>) {
        let label = match data_channel.label().await {
            Ok(l) => l,
            Err(e) => {
                warn!("failed to read data channel label: {e}");
                return;
            }
        };

        debug!(label, "received data channel from browser");

        if !AgentPeerConnection::store_channel_by_label(
            &label,
            &data_channel,
            &self.control_channel,
            &self.desktop_channel,
            &self.bulk_channel,
        )
        .await
        {
            return;
        }
        AgentPeerConnection::pump_data_channel(data_channel, self.inbound_frame_tx.clone());
    }
}

impl AgentPeerConnection {
    /// Create a new peer connection with the given ICE servers.
    ///
    /// The `inbound_frame_tx` channel receives frames decoded from data channel messages.
    pub async fn new(
        ice_servers: Vec<IceServerConfig>,
        inbound_frame_tx: mpsc::Sender<Frame>,
    ) -> Result<Self, SessionError> {
        let config = RTCConfigurationBuilder::new()
            .with_ice_servers(
                ice_servers
                    .into_iter()
                    .map(|s| RTCIceServer {
                        urls: s.urls,
                        username: s.username,
                        credential: s.credential,
                    })
                    .collect(),
            )
            .build();

        let (ice_tx, ice_rx) = mpsc::channel(32);
        let control_channel: ChannelSlot = Arc::new(Mutex::new(None));
        let desktop_channel: ChannelSlot = Arc::new(Mutex::new(None));
        let bulk_channel: ChannelSlot = Arc::new(Mutex::new(None));

        let handler = Arc::new(AgentEventHandler {
            ice_candidate_tx: ice_tx,
            inbound_frame_tx,
            control_channel: control_channel.clone(),
            desktop_channel: desktop_channel.clone(),
            bulk_channel: bulk_channel.clone(),
        });

        let pc = PeerConnectionBuilder::new()
            .with_configuration(config)
            .with_handler(handler)
            .with_udp_addrs(vec![UDP_BIND_ADDR.to_string()])
            .build()
            .await
            .map_err(|e| SessionError::WebSocket(format!("peer connection create: {e}")))?;

        Ok(Self {
            pc: Arc::new(pc),
            ice_candidate_rx: Mutex::new(ice_rx),
            control_channel,
            desktop_channel,
            bulk_channel,
            remote_desc_set: Mutex::new(false),
            pending_candidates: Mutex::new(Vec::new()),
        })
    }

    pub(crate) async fn store_channel_by_label(
        label: &str,
        d: &Arc<dyn DataChannel>,
        cc: &ChannelSlot,
        dc: &ChannelSlot,
        bc: &ChannelSlot,
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

    /// Drain one data channel's events for the life of the channel, decoding
    /// every binary message into a protocol frame for the session handler.
    fn pump_data_channel(d: Arc<dyn DataChannel>, frame_tx: mpsc::Sender<Frame>) {
        tokio::spawn(async move {
            while let Some(event) = d.poll().await {
                match event {
                    DataChannelEvent::OnMessage(msg) => match Frame::decode(&msg.data) {
                        Ok((frame, _)) => {
                            if let Err(e) = frame_tx.send(frame).await {
                                debug!("WebRTC inbound frame channel closed: {e}");
                                return;
                            }
                        }
                        Err(e) => {
                            warn!("data channel frame decode error: {e}");
                        }
                    },
                    DataChannelEvent::OnClose => return,
                    _ => {}
                }
            }
        });
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

        ch.send(BytesMut::from(&encoded[..]))
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

    /// Event sink for the throwaway peer connection the channel-routing test
    /// builds; every event it receives is irrelevant to that test.
    struct NoopEventHandler;

    #[async_trait::async_trait]
    impl PeerConnectionEventHandler for NoopEventHandler {}

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

    /// Pin store_channel_by_label match arms. Each label routes to a
    /// distinct slot; an unknown label returns false. Mutating any arm
    /// (or the bool return) breaks WebRTC channel routing.
    #[tokio::test]
    async fn store_channel_by_label_routes_each_label_to_correct_slot() {
        // Build a real data channel via a throwaway PeerConnection. We only
        // need an Arc<dyn DataChannel> to put into the slots; the channel
        // itself is never opened.
        let pc = PeerConnectionBuilder::new()
            .with_handler(Arc::new(NoopEventHandler))
            .with_udp_addrs(vec![UDP_BIND_ADDR.to_string()])
            .build()
            .await
            .unwrap();
        let dc = pc.create_data_channel("placeholder", None).await.unwrap();

        let cc: ChannelSlot = Arc::new(Mutex::new(None));
        let dch: ChannelSlot = Arc::new(Mutex::new(None));
        let bc: ChannelSlot = Arc::new(Mutex::new(None));

        // "control" routes to cc.
        assert!(AgentPeerConnection::store_channel_by_label("control", &dc, &cc, &dch, &bc).await);
        assert!(cc.lock().await.is_some());
        assert!(dch.lock().await.is_none());
        assert!(bc.lock().await.is_none());

        // "desktop" routes to dch.
        assert!(AgentPeerConnection::store_channel_by_label("desktop", &dc, &cc, &dch, &bc).await);
        assert!(dch.lock().await.is_some());
        assert!(bc.lock().await.is_none());

        // "bulk" routes to bc.
        assert!(AgentPeerConnection::store_channel_by_label("bulk", &dc, &cc, &dch, &bc).await);
        assert!(bc.lock().await.is_some());

        // Unknown label returns false; previously-set slots remain.
        let cc2: ChannelSlot = Arc::new(Mutex::new(None));
        let dch2: ChannelSlot = Arc::new(Mutex::new(None));
        let bc2: ChannelSlot = Arc::new(Mutex::new(None));
        assert!(
            !AgentPeerConnection::store_channel_by_label("unknown-label", &dc, &cc2, &dch2, &bc2)
                .await
        );
        assert!(cc2.lock().await.is_none());
        assert!(dch2.lock().await.is_none());
        assert!(bc2.lock().await.is_none());

        if let Err(e) = pc.close().await {
            eprintln!("test cleanup: pc.close failed: {e}");
        }
    }
}
