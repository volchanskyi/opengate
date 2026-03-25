//! Frame dispatch and control message handling for relay sessions.

use std::sync::Arc;

use mesh_protocol::{ControlMessage, Frame, KeyCode, MouseButton};
use tokio::sync::mpsc;
use tracing::{debug, info, warn};

use super::relay::send_frame;
use super::terminal_handle::TerminalHandle;
use super::SessionHandler;
use crate::file_ops::FileOpsHandler;
use crate::platform::InputInjector;
use crate::webrtc::AgentPeerConnection;

impl SessionHandler {
    pub(crate) async fn handle_frame(
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
            Frame::Terminal(term_frame) => {
                if let Some(term) = terminal {
                    term.send_raw(term_frame.data);
                } else {
                    debug!("terminal frame received but no terminal session active");
                }
            }
            Frame::Ping => {
                if let Err(e) = send_frame(frame_tx, &Frame::Pong).await {
                    warn!("failed to send pong: {e}");
                }
            }
            _ => {
                debug!("ignoring unexpected frame type from browser");
            }
        }
    }

    pub(crate) async fn handle_control(
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
                self.handle_mouse_move(injector, x, y);
            }
            ControlMessage::MouseClick {
                button,
                pressed,
                x,
                y,
            } => {
                self.handle_mouse_click(injector, button, pressed, x, y);
            }
            ControlMessage::KeyPress { key, pressed } => {
                self.handle_key_press(injector, terminal, key, pressed);
            }
            ControlMessage::TerminalResize { cols, rows } => {
                info!(cols, rows, "terminal resize requested");
                if let Some(term) = terminal {
                    term.resize(cols, rows);
                }
            }
            ControlMessage::FileListRequest { path } => {
                info!(path, "file list requested");
                Self::handle_file_list(file_ops, frame_tx, &path).await;
            }
            ControlMessage::FileDownloadRequest { path } => {
                info!(path, "file download requested");
                Self::handle_file_download(file_ops, frame_tx, &path);
            }
            ControlMessage::FileUploadRequest { path, total_size } => {
                info!(
                    path,
                    total_size, "file upload request (not yet implemented)"
                );
            }
            ControlMessage::ChatMessage { text, sender } => {
                info!(sender, text, "chat message received");
            }
            ControlMessage::SwitchToWebRTC { sdp_offer } => {
                info!("WebRTC switch requested");
                self.handle_webrtc_offer(sdp_offer, frame_tx, webrtc_pc)
                    .await;
            }
            ControlMessage::IceCandidate { candidate, mid } => {
                Self::handle_ice_candidate(webrtc_pc, &candidate, &mid).await;
            }
            ControlMessage::SwitchAck => {
                info!("WebRTC switch ack received");
                Self::handle_switch_ack(webrtc_pc, frame_tx).await;
            }
            _ => {
                debug!("unhandled control message in session");
            }
        }
    }

    fn handle_mouse_move(&self, injector: &dyn InputInjector, x: u16, y: u16) {
        if self.permissions.input {
            let _ = injector.inject_mouse_move(x as i32, y as i32);
        }
    }

    fn handle_mouse_click(
        &self,
        injector: &dyn InputInjector,
        button: MouseButton,
        pressed: bool,
        x: u16,
        y: u16,
    ) {
        if self.permissions.input {
            let _ = injector.inject_mouse_move(x as i32, y as i32);
            let _ = injector.inject_mouse_button(button, pressed);
        }
    }

    fn handle_key_press(
        &self,
        injector: &dyn InputInjector,
        terminal: Option<&TerminalHandle>,
        key: KeyCode,
        pressed: bool,
    ) {
        if self.permissions.input {
            let _ = injector.inject_key(mesh_protocol::KeyEvent { key, pressed });
        }
        if let Some(term) = terminal {
            if pressed {
                term.send_key(key);
            }
        }
    }

    async fn handle_file_list(
        file_ops: &FileOpsHandler,
        frame_tx: &mpsc::Sender<Vec<u8>>,
        path: &str,
    ) {
        match file_ops.list_directory(path) {
            Ok(response) => {
                if let Err(e) = send_frame(frame_tx, &Frame::Control(response)).await {
                    warn!("failed to send file list response: {e}");
                }
            }
            Err(e) => {
                warn!("file list error: {e}");
                if let Err(e) = send_frame(
                    frame_tx,
                    &Frame::Control(ControlMessage::FileListError {
                        path: path.to_string(),
                        error: e.to_string(),
                    }),
                )
                .await
                {
                    warn!("failed to send file list error: {e}");
                }
            }
        }
    }

    fn handle_file_download(
        file_ops: &FileOpsHandler,
        frame_tx: &mpsc::Sender<Vec<u8>>,
        path: &str,
    ) {
        let tx = frame_tx.clone();
        let file_ops = file_ops.clone();
        let path = path.to_owned();
        tokio::spawn(async move {
            if let Err(e) = file_ops.stream_download(&path, &tx).await {
                warn!("file download error: {e}");
            }
        });
    }

    async fn handle_ice_candidate(
        webrtc_pc: &Arc<tokio::sync::Mutex<Option<Arc<AgentPeerConnection>>>>,
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

    async fn handle_switch_ack(
        webrtc_pc: &Arc<tokio::sync::Mutex<Option<Arc<AgentPeerConnection>>>>,
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

    async fn handle_webrtc_offer(
        &self,
        sdp_offer: String,
        frame_tx: &mpsc::Sender<Vec<u8>>,
        webrtc_pc: &Arc<tokio::sync::Mutex<Option<Arc<AgentPeerConnection>>>>,
    ) {
        info!("received WebRTC offer, creating answer");
        let ice_servers = self.ice_servers.clone();
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

                        // Spawn ICE candidate forwarding task
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
}
