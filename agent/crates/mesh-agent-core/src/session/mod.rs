//! Relay session handler for agent-side session management.
//!
//! When the server sends a `SessionRequest`, the agent connects to the relay
//! WebSocket and streams desktop/terminal/file data to the browser.

mod handler;
pub(crate) mod relay;
pub(crate) mod terminal_handle;

use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;

use futures_util::StreamExt;
use mesh_protocol::{Frame, Permissions, SessionToken};
use tokio::sync::mpsc;
use tokio_tungstenite::tungstenite::Message;
use tracing::{debug, info, warn};

pub use terminal_handle::TerminalHandle;

use crate::file_ops::FileOpsHandler;
use crate::platform::{InputInjector, ScreenCapture};
use crate::session_error::SessionError;
use crate::terminal::TerminalSession;
use crate::webrtc::{AgentPeerConnection, IceServerConfig};
use relay::{build_relay_url, capture_loop, ws_writer_loop};

/// Manages one relay session between the agent and a browser.
pub struct SessionHandler {
    token: SessionToken,
    pub(crate) permissions: Permissions,
    pub(crate) ice_servers: Vec<IceServerConfig>,
}

impl SessionHandler {
    /// Create a new session handler.
    pub fn new(token: SessionToken, permissions: Permissions) -> Self {
        Self {
            token,
            permissions,
            ice_servers: Vec::new(),
        }
    }

    /// Set ICE servers for WebRTC upgrade capability.
    pub fn with_ice_servers(mut self, servers: Vec<IceServerConfig>) -> Self {
        self.ice_servers = servers;
        self
    }

    /// Connect to the relay WebSocket and run the session loop.
    ///
    /// This method blocks until the session ends (either side disconnects).
    pub async fn run(
        self,
        relay_url: &str,
        mut capture: Box<dyn ScreenCapture>,
        injector: Box<dyn InputInjector>,
    ) -> Result<(), SessionError> {
        let url = build_relay_url(relay_url)?;
        info!(token = %self.token.as_str(), url = %url, "connecting to relay");

        let (ws, _response) = tokio_tungstenite::connect_async(&url).await?;
        let (ws_tx, mut ws_rx) = ws.split();

        info!(token = %self.token.as_str(), "connected to relay");

        // Channel for sending frames to the WebSocket
        let (frame_tx, frame_rx) = mpsc::channel::<Vec<u8>>(64);
        let running = Arc::new(AtomicBool::new(true));

        // Spawn WebSocket writer task
        let writer_running = running.clone();
        let writer_handle = tokio::spawn(ws_writer_loop(ws_tx, frame_rx, writer_running));

        // Spawn desktop capture task if permitted
        let capture_handle = if self.permissions.desktop {
            let tx = frame_tx.clone();
            let r = running.clone();
            Some(tokio::spawn(async move {
                capture_loop(&mut *capture, tx, r).await;
            }))
        } else {
            None
        };

        // Spawn terminal session if permitted
        let terminal = if self.permissions.terminal {
            match TerminalSession::spawn(80, 24) {
                Ok(term) => {
                    let tx = frame_tx.clone();
                    let r = running.clone();
                    Some(term.run(tx, r).await?)
                }
                Err(e) => {
                    warn!("failed to spawn terminal: {e}");
                    None
                }
            }
        } else {
            None
        };

        let file_ops = FileOpsHandler::new(self.permissions.file_read, self.permissions.file_write);

        // WebRTC peer connection (created on first SwitchToWebRTC offer)
        let webrtc_pc: Arc<tokio::sync::Mutex<Option<Arc<AgentPeerConnection>>>> =
            Arc::new(tokio::sync::Mutex::new(None));

        // Main receive loop: read frames from browser via relay
        self.receive_loop(
            &mut ws_rx,
            &frame_tx,
            &*injector,
            &file_ops,
            terminal.as_ref(),
            &webrtc_pc,
        )
        .await;

        info!(token = %self.token.as_str(), "session ended");
        running.store(false, Ordering::Relaxed);

        Self::cleanup(capture_handle, terminal.as_ref(), writer_handle, &webrtc_pc).await;
        Ok(())
    }

    /// Process incoming WebSocket messages until the connection closes.
    async fn receive_loop(
        &self,
        ws_rx: &mut futures_util::stream::SplitStream<
            tokio_tungstenite::WebSocketStream<
                tokio_tungstenite::MaybeTlsStream<tokio::net::TcpStream>,
            >,
        >,
        frame_tx: &mpsc::Sender<Vec<u8>>,
        injector: &dyn InputInjector,
        file_ops: &FileOpsHandler,
        terminal: Option<&TerminalHandle>,
        webrtc_pc: &Arc<tokio::sync::Mutex<Option<Arc<AgentPeerConnection>>>>,
    ) {
        while let Some(msg) = ws_rx.next().await {
            let msg = match msg {
                Ok(m) => m,
                Err(e) => {
                    debug!("WebSocket receive error: {e}");
                    break;
                }
            };

            let data = match msg {
                Message::Binary(data) => data,
                Message::Close(_) => break,
                Message::Ping(payload) => {
                    let _ = frame_tx.send(Message::Pong(payload).into_data().to_vec()).await;
                    continue;
                }
                _ => continue,
            };

            if data.is_empty() {
                continue;
            }

            match Frame::decode(&data) {
                Ok((frame, _consumed)) => {
                    self.handle_frame(frame, injector, frame_tx, file_ops, terminal, webrtc_pc)
                        .await;
                }
                Err(e) => {
                    warn!("frame decode error: {e}");
                }
            }
        }
    }

    /// Clean up all spawned tasks and WebRTC connections.
    async fn cleanup(
        capture_handle: Option<tokio::task::JoinHandle<()>>,
        terminal: Option<&TerminalHandle>,
        writer_handle: tokio::task::JoinHandle<()>,
        webrtc_pc: &Arc<tokio::sync::Mutex<Option<Arc<AgentPeerConnection>>>>,
    ) {
        if let Some(h) = capture_handle {
            h.abort();
        }
        if let Some(t) = terminal {
            t.shutdown();
        }
        writer_handle.abort();
        if let Some(ref pc) = *webrtc_pc.lock().await {
            pc.close().await;
        }
    }
}
