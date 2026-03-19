//! Frame dispatch and control message handling for relay sessions.

use std::sync::Arc;

use mesh_protocol::{ControlMessage, Frame};
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
                let _ = send_frame(frame_tx, &Frame::Pong).await;
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
                self.handle_webrtc_offer(sdp_offer, frame_tx, webrtc_pc)
                    .await;
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
                    let _ = send_frame(frame_tx, &Frame::Control(ControlMessage::SwitchAck)).await;
                }
            }
            _ => {
                debug!("unhandled control message in session");
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
                            while let Some((candidate, mid)) = pc_ice.next_ice_candidate().await {
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
}
