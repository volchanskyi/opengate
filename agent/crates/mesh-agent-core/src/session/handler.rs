//! Frame dispatch and control message handling for relay sessions.

use std::sync::Arc;

use mesh_protocol::{ControlMessage, Frame};
use tokio::sync::mpsc;
use tracing::{debug, info, warn};

use super::handlers::{
    FileHandler, KeyboardHandler, MouseHandler, SwitchHandler, TerminalControlHandler,
    WebRTCHandler,
};
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
                MouseHandler::handle_mouse_move(&self.permissions, injector, x, y);
            }
            ControlMessage::MouseClick {
                button,
                pressed,
                x,
                y,
            } => {
                MouseHandler::handle_mouse_click(
                    &self.permissions,
                    injector,
                    button,
                    pressed,
                    x,
                    y,
                );
            }
            ControlMessage::KeyPress { key, pressed } => {
                KeyboardHandler::handle_key_press(
                    &self.permissions,
                    injector,
                    terminal,
                    key,
                    pressed,
                );
            }
            ControlMessage::TerminalResize { cols, rows } => {
                TerminalControlHandler::handle_resize(terminal, cols, rows);
            }
            ControlMessage::FileListRequest { path } => {
                info!(path, "file list requested");
                FileHandler::handle_list(file_ops, frame_tx, &path).await;
            }
            ControlMessage::FileDownloadRequest { path } => {
                info!(path, "file download requested");
                FileHandler::handle_download(file_ops, frame_tx, &path);
            }
            ControlMessage::FileUploadRequest { path, total_size } => {
                info!(
                    path,
                    total_size, "file upload request (not yet implemented)"
                );
                FileHandler::handle_upload(&path, total_size);
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
                WebRTCHandler::handle_offer(
                    self.ice_servers.clone(),
                    sdp_offer,
                    frame_tx,
                    webrtc_pc,
                )
                .await;
            }
            ControlMessage::IceCandidate { candidate, mid } => {
                WebRTCHandler::handle_candidate(webrtc_pc, &candidate, &mid).await;
            }
            ControlMessage::SwitchAck => {
                info!("WebRTC switch ack received");
                SwitchHandler::handle_ack(webrtc_pc, frame_tx).await;
            }
            _ => {
                debug!("unhandled control message in session");
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

    type TestTerminalHandle = (
        TerminalHandle,
        mpsc::Receiver<Vec<u8>>,
        mpsc::Receiver<(u16, u16)>,
        Arc<std::sync::atomic::AtomicBool>,
    );

    /// Helper: build a TerminalHandle backed by test channels we can observe.
    fn new_test_terminal_handle() -> TestTerminalHandle {
        let (stdin_tx, stdin_rx) = mpsc::channel(8);
        let (resize_tx, resize_rx) = mpsc::channel(8);
        let shutdown = Arc::new(std::sync::atomic::AtomicBool::new(false));
        let handle = TerminalHandle::new(stdin_tx, resize_tx, shutdown.clone());
        (handle, stdin_rx, resize_rx, shutdown)
    }

    /// Pin handle_frame's `Frame::Terminal` match arm: when a terminal is
    /// active, the inbound bytes must be forwarded to its stdin channel.
    /// Mutating away the Terminal arm would silently drop browser keystrokes.
    #[tokio::test]
    async fn handle_frame_terminal_forwards_bytes_when_session_active() {
        let handler = new_handler(all_perms());
        let injector = NullInput;
        let (frame_tx, _frame_rx) = mpsc::channel::<Vec<u8>>(64);
        let file_ops = FileOpsHandler::new(true, false);
        let webrtc_pc = new_webrtc_pc();
        let (term, mut stdin_rx, _resize_rx, _shutdown) = new_test_terminal_handle();

        handler
            .handle_frame(
                Frame::Terminal(TerminalFrame {
                    data: b"ls -la\n".to_vec(),
                }),
                &injector,
                &frame_tx,
                &file_ops,
                Some(&term),
                &webrtc_pc,
            )
            .await;

        let bytes = stdin_rx
            .try_recv()
            .expect("terminal stdin must receive bytes");
        assert_eq!(bytes, b"ls -la\n");
    }

    /// Pin handle_control's `ControlMessage::MouseClick` match arm and the
    /// handle_mouse_click body. Mutating either away would skip the dispatch
    /// or skip the inject calls entirely.
    #[tokio::test]
    async fn handle_control_mouse_click_dispatches_move_and_button() {
        let handler = new_handler(all_perms());
        let (injector, calls) = RecordingInjector::new();
        let (frame_tx, _frame_rx) = mpsc::channel::<Vec<u8>>(64);
        let file_ops = FileOpsHandler::new(true, false);
        let webrtc_pc = new_webrtc_pc();

        handler
            .handle_frame(
                Frame::Control(ControlMessage::MouseClick {
                    button: MouseButton::Left,
                    pressed: true,
                    x: 50,
                    y: 60,
                }),
                &injector,
                &frame_tx,
                &file_ops,
                None,
                &webrtc_pc,
            )
            .await;

        let recorded = calls.lock().unwrap();
        assert_eq!(
            *recorded,
            vec![
                "mouse_move:50,60".to_string(),
                "mouse_button:Left:true".to_string()
            ]
        );
    }

    /// Pin handle_control's `ControlMessage::KeyPress` arm and handle_key_press
    /// body. Must dispatch to injector AND (when terminal active and pressed)
    /// to the terminal stdin.
    #[tokio::test]
    async fn handle_control_key_press_dispatches_to_injector_and_terminal() {
        let handler = new_handler(all_perms());
        let (injector, calls) = RecordingInjector::new();
        let (frame_tx, _frame_rx) = mpsc::channel::<Vec<u8>>(64);
        let file_ops = FileOpsHandler::new(true, false);
        let webrtc_pc = new_webrtc_pc();
        let (term, mut stdin_rx, _resize_rx, _shutdown) = new_test_terminal_handle();

        // KeyPress with pressed=true should hit both injector and terminal.
        handler
            .handle_frame(
                Frame::Control(ControlMessage::KeyPress {
                    key: mesh_protocol::KeyCode::KeyA,
                    pressed: true,
                }),
                &injector,
                &frame_tx,
                &file_ops,
                Some(&term),
                &webrtc_pc,
            )
            .await;

        let recorded = calls.lock().unwrap();
        assert_eq!(*recorded, vec!["key:KeyA:true".to_string()]);
        drop(recorded);

        // Terminal must also receive the byte.
        let bytes = stdin_rx
            .try_recv()
            .expect("terminal must receive byte for pressed key");
        assert_eq!(bytes, b"a");
    }

    /// Pin handle_control's `ControlMessage::TerminalResize` arm: must call
    /// resize on the terminal handle.
    #[tokio::test]
    async fn handle_control_terminal_resize_forwards_dimensions() {
        let handler = new_handler(all_perms());
        let injector = NullInput;
        let (frame_tx, _frame_rx) = mpsc::channel::<Vec<u8>>(64);
        let file_ops = FileOpsHandler::new(true, false);
        let webrtc_pc = new_webrtc_pc();
        let (term, _stdin_rx, mut resize_rx, _shutdown) = new_test_terminal_handle();

        handler
            .handle_frame(
                Frame::Control(ControlMessage::TerminalResize {
                    cols: 132,
                    rows: 50,
                }),
                &injector,
                &frame_tx,
                &file_ops,
                Some(&term),
                &webrtc_pc,
            )
            .await;

        let (cols, rows) = resize_rx
            .try_recv()
            .expect("resize channel must receive dimensions");
        assert_eq!((cols, rows), (132, 50));
    }

    /// Pin handle_control's `ControlMessage::FileDownloadRequest` arm and the
    /// handle_file_download body: must spawn a background task that streams
    /// data to the frame channel.
    #[tokio::test]
    async fn handle_control_file_download_streams_to_frame_channel() {
        let handler = new_handler(all_perms());
        let injector = NullInput;
        let (frame_tx, mut frame_rx) = mpsc::channel::<Vec<u8>>(64);
        let file_ops = FileOpsHandler::new(true, false);
        let webrtc_pc = new_webrtc_pc();

        let dir = tempfile::tempdir().unwrap();
        let file_path = dir.path().join("test.txt");
        std::fs::write(&file_path, b"download payload").unwrap();

        handler
            .handle_frame(
                Frame::Control(ControlMessage::FileDownloadRequest {
                    path: file_path.to_string_lossy().into_owned(),
                }),
                &injector,
                &frame_tx,
                &file_ops,
                None,
                &webrtc_pc,
            )
            .await;

        // Background task is spawned — give it a moment to send.
        let data = tokio::time::timeout(std::time::Duration::from_secs(2), frame_rx.recv())
            .await
            .expect("download task must send within timeout")
            .expect("download task must produce a frame");
        let frame = decode_frame(&data);
        match frame {
            Frame::FileTransfer(ff) => {
                assert_eq!(ff.data, b"download payload");
                assert_eq!(ff.total_size, b"download payload".len() as u64);
            }
            other => panic!("expected FileTransfer, got {other:?}"),
        }
    }

    /// Pin handle_control's `ControlMessage::FileUploadRequest` arm: even
    /// though upload is not yet implemented, the arm must exist so the
    /// message is acknowledged silently (no panic, no rogue frame emission).
    #[tokio::test]
    async fn handle_control_file_upload_request_is_silently_acknowledged() {
        let handler = new_handler(all_perms());
        let injector = NullInput;
        let (frame_tx, mut frame_rx) = mpsc::channel::<Vec<u8>>(64);
        let file_ops = FileOpsHandler::new(true, false);
        let webrtc_pc = new_webrtc_pc();

        handler
            .handle_frame(
                Frame::Control(ControlMessage::FileUploadRequest {
                    path: "/tmp/x".to_string(),
                    total_size: 1024,
                }),
                &injector,
                &frame_tx,
                &file_ops,
                None,
                &webrtc_pc,
            )
            .await;

        // No frame should be emitted.
        assert!(matches!(
            frame_rx.try_recv(),
            Err(mpsc::error::TryRecvError::Empty)
        ));
    }

    /// Pin `ControlMessage::IceCandidate` and `ControlMessage::SwitchAck` arms
    /// when there is no active WebRTC peer connection. Both must early-return
    /// without panicking, and must not emit any frames.
    #[tokio::test]
    async fn handle_control_ice_and_switch_ack_no_op_without_peer() {
        let handler = new_handler(all_perms());
        let injector = NullInput;
        let (frame_tx, mut frame_rx) = mpsc::channel::<Vec<u8>>(64);
        let file_ops = FileOpsHandler::new(true, false);
        let webrtc_pc = new_webrtc_pc();

        handler
            .handle_frame(
                Frame::Control(ControlMessage::IceCandidate {
                    candidate: "candidate:1 1 UDP".to_string(),
                    mid: "0".to_string(),
                }),
                &injector,
                &frame_tx,
                &file_ops,
                None,
                &webrtc_pc,
            )
            .await;

        handler
            .handle_frame(
                Frame::Control(ControlMessage::SwitchAck),
                &injector,
                &frame_tx,
                &file_ops,
                None,
                &webrtc_pc,
            )
            .await;

        assert!(matches!(
            frame_rx.try_recv(),
            Err(mpsc::error::TryRecvError::Empty)
        ));
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
