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
                let echo = ControlMessage::ChatMessage {
                    text,
                    sender: "agent".to_string(),
                };
                if let Err(e) = send_frame(frame_tx, &Frame::Control(echo)).await {
                    warn!("failed to echo chat message: {e}");
                }
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

#[cfg(test)]
mod tests {
    use super::*;
    use crate::file_ops::FileOpsHandler;
    use crate::platform::{InputError, InputInjector, NullInput};
    use crate::session_error::SessionError;
    use mesh_protocol::{
        ControlMessage, DesktopFrame, Frame, FrameEncoding, KeyEvent, MouseButton, Permissions,
        SessionToken, TerminalFrame,
    };
    use std::sync::{Arc, Mutex};
    use tokio::sync::mpsc;

    /// Recording injector that tracks all inject calls.
    struct RecordingInjector {
        calls: Arc<Mutex<Vec<String>>>,
    }

    impl RecordingInjector {
        fn new() -> (Self, Arc<Mutex<Vec<String>>>) {
            let calls = Arc::new(Mutex::new(Vec::new()));
            (
                Self {
                    calls: calls.clone(),
                },
                calls,
            )
        }
    }

    impl InputInjector for RecordingInjector {
        fn inject_key(&self, event: KeyEvent) -> Result<(), InputError> {
            self.calls
                .lock()
                .unwrap()
                .push(format!("key:{:?}:{}", event.key, event.pressed));
            Ok(())
        }

        fn inject_mouse_move(&self, x: i32, y: i32) -> Result<(), InputError> {
            self.calls
                .lock()
                .unwrap()
                .push(format!("mouse_move:{x},{y}"));
            Ok(())
        }

        fn inject_mouse_button(
            &self,
            button: MouseButton,
            pressed: bool,
        ) -> Result<(), InputError> {
            self.calls
                .lock()
                .unwrap()
                .push(format!("mouse_button:{button:?}:{pressed}"));
            Ok(())
        }

        fn is_available(&self) -> bool {
            true
        }
    }

    fn all_perms() -> Permissions {
        Permissions {
            desktop: true,
            terminal: true,
            file_read: true,
            file_write: false,
            input: true,
        }
    }

    fn no_input_perms() -> Permissions {
        Permissions {
            desktop: true,
            terminal: true,
            file_read: true,
            file_write: false,
            input: false,
        }
    }

    fn new_handler(perms: Permissions) -> SessionHandler {
        SessionHandler::new(SessionToken::generate(), perms)
    }

    fn new_webrtc_pc() -> Arc<tokio::sync::Mutex<Option<Arc<AgentPeerConnection>>>> {
        Arc::new(tokio::sync::Mutex::new(None))
    }

    /// Decode a frame from bytes sent on the frame_tx channel.
    fn decode_frame(data: &[u8]) -> Frame {
        let (frame, _) = Frame::decode(data).expect("failed to decode frame");
        frame
    }

    #[tokio::test]
    async fn test_handle_frame_ping_responds_pong() {
        let handler = new_handler(all_perms());
        let injector = NullInput;
        let (frame_tx, mut frame_rx) = mpsc::channel::<Vec<u8>>(64);
        let file_ops = FileOpsHandler::new(true, false);
        let webrtc_pc = new_webrtc_pc();

        handler
            .handle_frame(
                Frame::Ping,
                &injector,
                &frame_tx,
                &file_ops,
                None,
                &webrtc_pc,
            )
            .await;

        let data = frame_rx.try_recv().expect("expected pong frame");
        let frame = decode_frame(&data);
        assert_eq!(frame, Frame::Pong);
    }

    #[tokio::test]
    async fn test_handle_frame_terminal_no_session() {
        let handler = new_handler(all_perms());
        let injector = NullInput;
        let (frame_tx, mut frame_rx) = mpsc::channel::<Vec<u8>>(64);
        let file_ops = FileOpsHandler::new(true, false);
        let webrtc_pc = new_webrtc_pc();

        // Send a terminal frame with no terminal session active — should not panic
        handler
            .handle_frame(
                Frame::Terminal(TerminalFrame {
                    data: b"ls -la\n".to_vec(),
                }),
                &injector,
                &frame_tx,
                &file_ops,
                None,
                &webrtc_pc,
            )
            .await;

        // No output expected
        assert!(frame_rx.try_recv().is_err());
    }

    #[tokio::test]
    async fn test_handle_frame_unexpected_type_ignored() {
        let handler = new_handler(all_perms());
        let injector = NullInput;
        let (frame_tx, mut frame_rx) = mpsc::channel::<Vec<u8>>(64);
        let file_ops = FileOpsHandler::new(true, false);
        let webrtc_pc = new_webrtc_pc();

        // Desktop frames from browser are unexpected — should be silently ignored
        handler
            .handle_frame(
                Frame::Desktop(DesktopFrame {
                    sequence: 0,
                    x: 0,
                    y: 0,
                    width: 100,
                    height: 100,
                    encoding: FrameEncoding::Raw,
                    data: vec![0; 100],
                }),
                &injector,
                &frame_tx,
                &file_ops,
                None,
                &webrtc_pc,
            )
            .await;

        assert!(frame_rx.try_recv().is_err());
    }

    #[tokio::test]
    async fn test_handle_control_mouse_move_permitted() {
        let handler = new_handler(all_perms());
        let (injector, calls) = RecordingInjector::new();
        let (frame_tx, _frame_rx) = mpsc::channel::<Vec<u8>>(64);
        let file_ops = FileOpsHandler::new(true, false);
        let webrtc_pc = new_webrtc_pc();

        handler
            .handle_frame(
                Frame::Control(ControlMessage::MouseMove { x: 100, y: 200 }),
                &injector,
                &frame_tx,
                &file_ops,
                None,
                &webrtc_pc,
            )
            .await;

        let recorded = calls.lock().unwrap();
        assert_eq!(recorded.len(), 1);
        assert_eq!(recorded[0], "mouse_move:100,200");
    }

    #[tokio::test]
    async fn test_handle_control_mouse_move_denied() {
        let handler = new_handler(no_input_perms());
        let (injector, calls) = RecordingInjector::new();
        let (frame_tx, _frame_rx) = mpsc::channel::<Vec<u8>>(64);
        let file_ops = FileOpsHandler::new(true, false);
        let webrtc_pc = new_webrtc_pc();

        handler
            .handle_frame(
                Frame::Control(ControlMessage::MouseMove { x: 100, y: 200 }),
                &injector,
                &frame_tx,
                &file_ops,
                None,
                &webrtc_pc,
            )
            .await;

        let recorded = calls.lock().unwrap();
        assert!(
            recorded.is_empty(),
            "inject should NOT be called when input is denied"
        );
    }

    #[tokio::test]
    async fn test_handle_control_file_list_success() {
        let handler = new_handler(all_perms());
        let injector = NullInput;
        let (frame_tx, mut frame_rx) = mpsc::channel::<Vec<u8>>(64);
        let file_ops = FileOpsHandler::new(true, false);
        let webrtc_pc = new_webrtc_pc();

        // Use a temp directory that definitely exists
        let dir = tempfile::tempdir().expect("create temp dir");
        let dir_path = dir.path().to_string_lossy().to_string();

        handler
            .handle_frame(
                Frame::Control(ControlMessage::FileListRequest {
                    path: dir_path.clone(),
                }),
                &injector,
                &frame_tx,
                &file_ops,
                None,
                &webrtc_pc,
            )
            .await;

        let data = frame_rx.try_recv().expect("expected file list response");
        let frame = decode_frame(&data);
        match frame {
            Frame::Control(ControlMessage::FileListResponse { path, .. }) => {
                assert_eq!(path, dir_path);
            }
            other => panic!("expected FileListResponse, got {other:?}"),
        }
    }

    #[tokio::test]
    async fn test_handle_control_file_list_error() {
        let handler = new_handler(all_perms());
        let injector = NullInput;
        let (frame_tx, mut frame_rx) = mpsc::channel::<Vec<u8>>(64);
        let file_ops = FileOpsHandler::new(true, false);
        let webrtc_pc = new_webrtc_pc();

        handler
            .handle_frame(
                Frame::Control(ControlMessage::FileListRequest {
                    path: "/nonexistent_abc123_xyz789".to_string(),
                }),
                &injector,
                &frame_tx,
                &file_ops,
                None,
                &webrtc_pc,
            )
            .await;

        let data = frame_rx.try_recv().expect("expected file list error");
        let frame = decode_frame(&data);
        match frame {
            Frame::Control(ControlMessage::FileListError { path, error }) => {
                assert_eq!(path, "/nonexistent_abc123_xyz789");
                assert!(!error.is_empty());
            }
            other => panic!("expected FileListError, got {other:?}"),
        }
    }

    #[tokio::test]
    async fn test_handle_control_chat_echoes_back() {
        let handler = new_handler(all_perms());
        let injector = NullInput;
        let (frame_tx, mut frame_rx) = mpsc::channel::<Vec<u8>>(64);
        let file_ops = FileOpsHandler::new(true, false);
        let webrtc_pc = new_webrtc_pc();

        handler
            .handle_frame(
                Frame::Control(ControlMessage::ChatMessage {
                    text: "hello".to_string(),
                    sender: "browser".to_string(),
                }),
                &injector,
                &frame_tx,
                &file_ops,
                None,
                &webrtc_pc,
            )
            .await;

        let data = frame_rx.try_recv().expect("expected chat echo frame");
        let frame = decode_frame(&data);
        match frame {
            Frame::Control(ControlMessage::ChatMessage { text, sender }) => {
                assert_eq!(sender, "agent");
                assert_eq!(text, "hello");
            }
            other => panic!("expected ChatMessage, got {other:?}"),
        }
    }

    #[tokio::test]
    async fn test_handle_control_chat_preserves_text() {
        let handler = new_handler(all_perms());
        let injector = NullInput;
        let (frame_tx, mut frame_rx) = mpsc::channel::<Vec<u8>>(64);
        let file_ops = FileOpsHandler::new(true, false);
        let webrtc_pc = new_webrtc_pc();

        // Test unicode and empty string
        for text in ["", "héllo 🌍", "日本語テスト", "line1\nline2\ttab"] {
            handler
                .handle_frame(
                    Frame::Control(ControlMessage::ChatMessage {
                        text: text.to_string(),
                        sender: "user".to_string(),
                    }),
                    &injector,
                    &frame_tx,
                    &file_ops,
                    None,
                    &webrtc_pc,
                )
                .await;

            let data = frame_rx.try_recv().expect("expected chat echo frame");
            let frame = decode_frame(&data);
            match frame {
                Frame::Control(ControlMessage::ChatMessage {
                    text: echoed,
                    sender,
                }) => {
                    assert_eq!(sender, "agent");
                    assert_eq!(echoed, text);
                }
                other => panic!("expected ChatMessage, got {other:?}"),
            }
        }
    }

    #[tokio::test]
    async fn test_send_frame_closed_channel() {
        let (frame_tx, frame_rx) = mpsc::channel::<Vec<u8>>(1);
        // Drop the receiver to close the channel
        drop(frame_rx);

        let result = send_frame(&frame_tx, &Frame::Pong).await;
        assert!(result.is_err());
        match result.unwrap_err() {
            SessionError::WebSocket(msg) => {
                assert!(msg.contains("closed"), "expected 'closed' in error: {msg}");
            }
            other => panic!("expected WebSocket error, got {other:?}"),
        }
    }
}
