//! Relay session handler for agent-side session management.
//!
//! When the server sends a `SessionRequest`, the agent connects to the relay
//! WebSocket and streams desktop/terminal/file data to the browser.

use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::Arc;
use std::time::Duration;

use futures_util::stream::SplitSink;
use futures_util::{SinkExt, StreamExt};
use mesh_protocol::{
    ControlMessage, DesktopFrame, Frame, FrameEncoding, Permissions, SessionToken,
};
use tokio::sync::mpsc;
use tokio_tungstenite::tungstenite::Message;
use tokio_tungstenite::{connect_async, MaybeTlsStream};
use tracing::{debug, info, warn};

use crate::file_ops::FileOpsHandler;
use crate::platform::{InputInjector, ScreenCapture};
use crate::session_error::SessionError;
use crate::terminal::TerminalSession;
use crate::webrtc::{AgentPeerConnection, IceServerConfig};

type WsStream = tokio_tungstenite::WebSocketStream<MaybeTlsStream<tokio::net::TcpStream>>;

/// Manages one relay session between the agent and a browser.
pub struct SessionHandler {
    token: SessionToken,
    permissions: Permissions,
    ice_servers: Vec<IceServerConfig>,
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

        let (ws, _response) = connect_async(&url).await?;
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
                    let _ = frame_tx.send(Message::Pong(payload).into_data()).await;
                    continue;
                }
                _ => continue,
            };

            if data.is_empty() {
                continue;
            }

            match Frame::decode(&data) {
                Ok((frame, _consumed)) => {
                    self.handle_frame(
                        frame,
                        &*injector,
                        &frame_tx,
                        &file_ops,
                        terminal.as_ref(),
                        &webrtc_pc,
                    )
                    .await;
                }
                Err(e) => {
                    warn!("frame decode error: {e}");
                }
            }
        }

        info!(token = %self.token.as_str(), "session ended");
        running.store(false, Ordering::Relaxed);

        // Clean up tasks
        if let Some(h) = capture_handle {
            h.abort();
        }
        if let Some(ref t) = terminal {
            t.shutdown();
        }
        writer_handle.abort();

        // Clean up WebRTC
        if let Some(ref pc) = *webrtc_pc.lock().await {
            pc.close().await;
        }

        Ok(())
    }

    async fn handle_frame(
        &self,
        frame: Frame,
        injector: &dyn InputInjector,
        frame_tx: &mpsc::Sender<Vec<u8>>,
        file_ops: &FileOpsHandler,
        terminal: Option<&TerminalHandle>,
        webrtc_pc: &Arc<tokio::sync::Mutex<Option<Arc<AgentPeerConnection>>>>,
    ) {
        match frame {
            Frame::Control(msg) => {
                self.handle_control(msg, injector, frame_tx, file_ops, terminal, webrtc_pc)
                    .await;
            }
            Frame::Ping => {
                let _ = send_frame(frame_tx, &Frame::Pong).await;
            }
            _ => {
                debug!("ignoring unexpected frame type from browser");
            }
        }
    }

    async fn handle_control(
        &self,
        msg: ControlMessage,
        injector: &dyn InputInjector,
        frame_tx: &mpsc::Sender<Vec<u8>>,
        file_ops: &FileOpsHandler,
        terminal: Option<&TerminalHandle>,
        webrtc_pc: &Arc<tokio::sync::Mutex<Option<Arc<AgentPeerConnection>>>>,
    ) {
        match msg {
            ControlMessage::MouseMove { x, y } => {
                if self.permissions.input {
                    let _ = injector.inject_mouse_move(x as i32, y as i32);
                }
            }
            ControlMessage::MouseClick {
                button,
                pressed,
                x,
                y,
            } => {
                if self.permissions.input {
                    let _ = injector.inject_mouse_move(x as i32, y as i32);
                    let _ = injector.inject_mouse_button(button, pressed);
                }
            }
            ControlMessage::KeyPress { key, pressed } => {
                if self.permissions.input {
                    let _ = injector.inject_key(mesh_protocol::KeyEvent { key, pressed });
                }
                // Also forward to terminal if present
                if let Some(term) = terminal {
                    if pressed {
                        term.send_key(key);
                    }
                }
            }
            ControlMessage::TerminalResize { cols, rows } => {
                if let Some(term) = terminal {
                    term.resize(cols, rows);
                }
            }
            ControlMessage::FileListRequest { path } => match file_ops.list_directory(&path) {
                Ok(response) => {
                    let _ = send_frame(frame_tx, &Frame::Control(response)).await;
                }
                Err(e) => warn!("file list error: {e}"),
            },
            ControlMessage::FileDownloadRequest { path } => {
                let tx = frame_tx.clone();
                let file_ops = file_ops.clone();
                tokio::spawn(async move {
                    if let Err(e) = file_ops.stream_download(&path, &tx).await {
                        warn!("file download error: {e}");
                    }
                });
            }
            ControlMessage::FileUploadRequest { path, total_size } => {
                debug!(
                    path,
                    total_size, "file upload request (not yet implemented)"
                );
            }
            ControlMessage::ChatMessage { text, sender } => {
                debug!(sender, text, "chat message received");
            }
            ControlMessage::SwitchToWebRTC { sdp_offer } => {
                info!("received WebRTC offer, creating answer");
                let ice_servers = self.ice_servers.clone();
                let tx = frame_tx.clone();
                let pc_slot = webrtc_pc.clone();

                // Create peer connection and handle offer
                let (inbound_tx, mut inbound_rx) = mpsc::channel::<Frame>(64);
                match AgentPeerConnection::new(ice_servers, inbound_tx).await {
                    Ok(pc) => {
                        let pc = Arc::new(pc);
                        *pc_slot.lock().await = Some(pc.clone());

                        match pc.handle_offer(&sdp_offer).await {
                            Ok(answer_sdp) => {
                                let _ = send_frame(
                                    &tx,
                                    &Frame::Control(ControlMessage::SwitchToWebRTC {
                                        sdp_offer: answer_sdp,
                                    }),
                                )
                                .await;

                                // Spawn ICE candidate forwarding task
                                let pc_ice = pc.clone();
                                let tx_ice = tx.clone();
                                tokio::spawn(async move {
                                    while let Some((candidate, mid)) =
                                        pc_ice.next_ice_candidate().await
                                    {
                                        let _ = send_frame(
                                            &tx_ice,
                                            &Frame::Control(ControlMessage::IceCandidate {
                                                candidate,
                                                mid,
                                            }),
                                        )
                                        .await;
                                    }
                                });

                                // Spawn inbound data channel frame handler
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
                                        // Forward to relay as fallback path
                                        let _ = tx_inbound.send(encoded).await;
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
            ControlMessage::IceCandidate { candidate, mid } => {
                let guard = webrtc_pc.lock().await;
                if let Some(ref pc) = *guard {
                    if let Err(e) = pc.add_ice_candidate(&candidate, &mid).await {
                        warn!("failed to add ICE candidate: {e}");
                    }
                } else {
                    debug!("ICE candidate received but no WebRTC connection active");
                }
            }
            ControlMessage::SwitchAck => {
                let guard = webrtc_pc.lock().await;
                if guard.is_some() {
                    info!("WebRTC switch acknowledged by browser");
                    // Send our own ack back
                    let _ = send_frame(frame_tx, &Frame::Control(ControlMessage::SwitchAck)).await;
                }
            }
            _ => {
                debug!("unhandled control message in session");
            }
        }
    }
}

/// Terminal handle for sending data and managing lifecycle.
pub struct TerminalHandle {
    stdin_tx: mpsc::Sender<Vec<u8>>,
    resize_tx: mpsc::Sender<(u16, u16)>,
    shutdown: Arc<AtomicBool>,
}

impl TerminalHandle {
    /// Create a new terminal handle.
    pub fn new(
        stdin_tx: mpsc::Sender<Vec<u8>>,
        resize_tx: mpsc::Sender<(u16, u16)>,
        shutdown: Arc<AtomicBool>,
    ) -> Self {
        Self {
            stdin_tx,
            resize_tx,
            shutdown,
        }
    }

    /// Send a key press to the terminal stdin.
    pub fn send_key(&self, key: mesh_protocol::KeyCode) {
        // Convert key code to terminal bytes
        let bytes = key_to_bytes(key);
        if !bytes.is_empty() {
            let _ = self.stdin_tx.try_send(bytes);
        }
    }

    /// Resize the terminal.
    pub fn resize(&self, cols: u16, rows: u16) {
        let _ = self.resize_tx.try_send((cols, rows));
    }

    /// Signal the terminal to shut down.
    pub fn shutdown(&self) {
        self.shutdown.store(true, Ordering::Relaxed);
    }
}

/// Build relay URL with ?side=agent query parameter.
fn build_relay_url(relay_url: &str) -> Result<String, SessionError> {
    let mut parsed = url::Url::parse(relay_url)
        .map_err(|e| SessionError::WebSocket(format!("invalid relay URL: {e}")))?;
    parsed.query_pairs_mut().append_pair("side", "agent");
    Ok(parsed.to_string())
}

/// WebSocket writer loop: sends encoded frames from the channel.
async fn ws_writer_loop(
    mut ws_tx: SplitSink<WsStream, Message>,
    mut frame_rx: mpsc::Receiver<Vec<u8>>,
    running: Arc<AtomicBool>,
) {
    while let Some(data) = frame_rx.recv().await {
        if !running.load(Ordering::Relaxed) {
            break;
        }
        if let Err(e) = ws_tx.send(Message::Binary(data)).await {
            debug!("WebSocket send error: {e}");
            break;
        }
    }
    let _ = ws_tx.close().await;
}

/// Desktop capture loop: captures frames and sends them to the relay.
async fn capture_loop(
    capture: &mut dyn ScreenCapture,
    frame_tx: mpsc::Sender<Vec<u8>>,
    running: Arc<AtomicBool>,
) {
    let sequence = AtomicU64::new(0);
    let frame_interval = Duration::from_millis(33); // ~30 FPS

    while running.load(Ordering::Relaxed) {
        match capture.next_frame().await {
            Ok(raw_frame) => {
                let seq = sequence.fetch_add(1, Ordering::Relaxed);
                let desktop_frame = DesktopFrame {
                    sequence: seq,
                    x: 0,
                    y: 0,
                    width: raw_frame.width as u16,
                    height: raw_frame.height as u16,
                    encoding: FrameEncoding::Raw,
                    data: raw_frame.data,
                };
                let frame = Frame::Desktop(desktop_frame);
                if send_frame(&frame_tx, &frame).await.is_err() {
                    break;
                }
            }
            Err(e) => {
                debug!("capture error: {e}");
                tokio::time::sleep(frame_interval).await;
            }
        }

        tokio::time::sleep(frame_interval).await;
    }
}

/// Encode a frame and send it via the channel.
async fn send_frame(tx: &mpsc::Sender<Vec<u8>>, frame: &Frame) -> Result<(), SessionError> {
    let encoded = frame.encode()?;
    tx.send(encoded)
        .await
        .map_err(|_| SessionError::WebSocket("send channel closed".to_string()))
}

/// Convert a KeyCode to terminal-compatible bytes.
fn key_to_bytes(key: mesh_protocol::KeyCode) -> Vec<u8> {
    use mesh_protocol::KeyCode::*;
    match key {
        KeyA => b"a".to_vec(),
        KeyB => b"b".to_vec(),
        KeyC => b"c".to_vec(),
        KeyD => b"d".to_vec(),
        KeyE => b"e".to_vec(),
        KeyF => b"f".to_vec(),
        KeyG => b"g".to_vec(),
        KeyH => b"h".to_vec(),
        KeyI => b"i".to_vec(),
        KeyJ => b"j".to_vec(),
        KeyK => b"k".to_vec(),
        KeyL => b"l".to_vec(),
        KeyM => b"m".to_vec(),
        KeyN => b"n".to_vec(),
        KeyO => b"o".to_vec(),
        KeyP => b"p".to_vec(),
        KeyQ => b"q".to_vec(),
        KeyR => b"r".to_vec(),
        KeyS => b"s".to_vec(),
        KeyT => b"t".to_vec(),
        KeyU => b"u".to_vec(),
        KeyV => b"v".to_vec(),
        KeyW => b"w".to_vec(),
        KeyX => b"x".to_vec(),
        KeyY => b"y".to_vec(),
        KeyZ => b"z".to_vec(),
        Digit0 => b"0".to_vec(),
        Digit1 => b"1".to_vec(),
        Digit2 => b"2".to_vec(),
        Digit3 => b"3".to_vec(),
        Digit4 => b"4".to_vec(),
        Digit5 => b"5".to_vec(),
        Digit6 => b"6".to_vec(),
        Digit7 => b"7".to_vec(),
        Digit8 => b"8".to_vec(),
        Digit9 => b"9".to_vec(),
        Enter => b"\r".to_vec(),
        Tab => b"\t".to_vec(),
        Escape => b"\x1b".to_vec(),
        Backspace => b"\x7f".to_vec(),
        Space => b" ".to_vec(),
        ArrowUp => b"\x1b[A".to_vec(),
        ArrowDown => b"\x1b[B".to_vec(),
        ArrowRight => b"\x1b[C".to_vec(),
        ArrowLeft => b"\x1b[D".to_vec(),
        Home => b"\x1b[H".to_vec(),
        End => b"\x1b[F".to_vec(),
        PageUp => b"\x1b[5~".to_vec(),
        PageDown => b"\x1b[6~".to_vec(),
        Delete => b"\x1b[3~".to_vec(),
        Insert => b"\x1b[2~".to_vec(),
        F1 => b"\x1bOP".to_vec(),
        F2 => b"\x1bOQ".to_vec(),
        F3 => b"\x1bOR".to_vec(),
        F4 => b"\x1bOS".to_vec(),
        F5 => b"\x1b[15~".to_vec(),
        F6 => b"\x1b[17~".to_vec(),
        F7 => b"\x1b[18~".to_vec(),
        F8 => b"\x1b[19~".to_vec(),
        F9 => b"\x1b[20~".to_vec(),
        F10 => b"\x1b[21~".to_vec(),
        F11 => b"\x1b[23~".to_vec(),
        F12 => b"\x1b[24~".to_vec(),
        Minus => b"-".to_vec(),
        Equal => b"=".to_vec(),
        BracketLeft => b"[".to_vec(),
        BracketRight => b"]".to_vec(),
        Backslash => b"\\".to_vec(),
        Semicolon => b";".to_vec(),
        Quote => b"'".to_vec(),
        Comma => b",".to_vec(),
        Period => b".".to_vec(),
        Slash => b"/".to_vec(),
        Backquote => b"`".to_vec(),
        _ => Vec::new(), // Modifiers and special keys don't produce bytes
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_build_relay_url_adds_side_param() {
        let url = build_relay_url("wss://localhost/ws/relay/abc123").unwrap();
        assert!(url.contains("side=agent"));
        assert!(url.contains("abc123"));
    }

    #[test]
    fn test_build_relay_url_preserves_existing_params() {
        let url = build_relay_url("wss://localhost/ws/relay/abc?foo=bar").unwrap();
        assert!(url.contains("foo=bar"));
        assert!(url.contains("side=agent"));
    }

    #[test]
    fn test_build_relay_url_invalid() {
        let result = build_relay_url("not a url");
        assert!(result.is_err());
    }

    #[test]
    fn test_key_to_bytes_letters() {
        assert_eq!(key_to_bytes(mesh_protocol::KeyCode::KeyA), b"a");
        assert_eq!(key_to_bytes(mesh_protocol::KeyCode::KeyZ), b"z");
    }

    #[test]
    fn test_key_to_bytes_special() {
        assert_eq!(key_to_bytes(mesh_protocol::KeyCode::Enter), b"\r");
        assert_eq!(key_to_bytes(mesh_protocol::KeyCode::Escape), b"\x1b");
        assert_eq!(key_to_bytes(mesh_protocol::KeyCode::ArrowUp), b"\x1b[A");
    }

    #[test]
    fn test_key_to_bytes_modifiers_empty() {
        assert!(key_to_bytes(mesh_protocol::KeyCode::ShiftLeft).is_empty());
        assert!(key_to_bytes(mesh_protocol::KeyCode::ControlLeft).is_empty());
    }

    #[test]
    fn test_terminal_handle_send_and_resize() {
        let (stdin_tx, mut stdin_rx) = mpsc::channel(8);
        let (resize_tx, mut resize_rx) = mpsc::channel(8);
        let shutdown = Arc::new(AtomicBool::new(false));

        let handle = TerminalHandle::new(stdin_tx, resize_tx, shutdown.clone());

        handle.send_key(mesh_protocol::KeyCode::KeyA);
        let data = stdin_rx.try_recv().unwrap();
        assert_eq!(data, b"a");

        handle.resize(120, 40);
        let (cols, rows) = resize_rx.try_recv().unwrap();
        assert_eq!((cols, rows), (120, 40));

        handle.shutdown();
        assert!(shutdown.load(Ordering::Relaxed));
    }
}
