//! The session's features and the dispatcher that hands them each delivered frame in order.

use mesh_protocol::{ControlMessage, Frame, Permissions};
use tracing::{debug, info, warn};

use super::handlers::{
    FileHandler, FileTasks, KeyboardHandler, MouseHandler, TerminalControlHandler,
};
use super::outbox::Outbox;
use super::outlet::OutletReader;
use super::terminal_handle::TerminalHandle;
use crate::file_ops::FileOpsHandler;
use crate::platform::InputInjector;

/// Everything a session's frames act on.
pub(crate) struct Features {
    pub(crate) permissions: Permissions,
    pub(crate) injector: Box<dyn InputInjector>,
    pub(crate) outbox: Outbox,
    pub(crate) file_ops: FileOpsHandler,
    pub(crate) terminal: Option<TerminalHandle>,
    pub(crate) files: FileTasks,
}

/// Runs each delivered frame against the features, in order, until the core is gone; then stops
/// the session's file tasks and its terminal.
pub(crate) async fn dispatch(mut features: Features, mut frames: OutletReader) {
    while let Some(frame) = frames.next().await {
        features.handle(&frame).await;
        frames.written();
    }
    features.files.stop().await;
    if let Some(terminal) = &features.terminal {
        terminal.shutdown();
    }
}

impl Features {
    /// Decodes one delivered frame and acts on it; an undecodable one is logged and dropped.
    pub(crate) async fn handle(&mut self, bytes: &[u8]) {
        match Frame::decode(bytes) {
            Ok((frame, _)) => self.handle_frame(frame).await,
            Err(e) => warn!("frame decode error: {e}"),
        }
    }

    async fn handle_frame(&mut self, frame: Frame) {
        match frame {
            Frame::Control(msg) => self.handle_control(msg).await,
            Frame::Terminal(term_frame) => match &self.terminal {
                Some(term) => term.send_raw(term_frame.data),
                None => debug!("terminal frame received but no terminal session active"),
            },
            Frame::Ping => {
                if let Err(e) = self.outbox.send(&Frame::Pong).await {
                    warn!("failed to send pong: {e}");
                }
            }
            _ => debug!("ignoring unexpected frame type from browser"),
        }
    }

    async fn handle_control(&mut self, msg: ControlMessage) {
        match msg {
            ControlMessage::MouseMove { x, y } => {
                MouseHandler::handle_mouse_move(&self.permissions, &*self.injector, x, y);
            }
            ControlMessage::MouseClick {
                button,
                pressed,
                x,
                y,
            } => {
                MouseHandler::handle_mouse_click(
                    &self.permissions,
                    &*self.injector,
                    button,
                    pressed,
                    x,
                    y,
                );
            }
            ControlMessage::KeyPress { key, pressed } => {
                KeyboardHandler::handle_key_press(
                    &self.permissions,
                    &*self.injector,
                    self.terminal.as_ref(),
                    key,
                    pressed,
                );
            }
            ControlMessage::TerminalResize { cols, rows } => {
                TerminalControlHandler::handle_resize(self.terminal.as_ref(), cols, rows);
            }
            ControlMessage::FileListRequest { path } => {
                info!(path, "file list requested");
                FileHandler::handle_list(&self.file_ops, &self.outbox, &mut self.files, &path);
            }
            ControlMessage::FileDownloadRequest { path } => {
                info!(path, "file download requested");
                FileHandler::handle_download(&self.file_ops, &self.outbox, &mut self.files, &path)
                    .await;
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
                if let Err(e) = self.outbox.send(&Frame::Control(echo)).await {
                    warn!("failed to echo chat message: {e}");
                }
            }
            _ => debug!("unhandled control message in session"),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::platform::{InputError, NullInput};
    use crate::session::outbox::{Lanes, INTERACTIVE_FRAME_CAP};
    use mesh_protocol::{
        DesktopFrame, FrameEncoding, KeyCode, KeyEvent, MouseButton, TerminalFrame,
    };
    use std::sync::{Arc, Mutex};
    use std::time::Duration;
    use tokio::sync::mpsc;

    struct RecordingInjector {
        calls: Arc<Mutex<Vec<String>>>,
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

    fn perms(input: bool) -> Permissions {
        Permissions {
            desktop: true,
            terminal: true,
            file_read: true,
            file_write: false,
            input,
        }
    }

    fn features(input: bool) -> (Features, Lanes, Arc<Mutex<Vec<String>>>) {
        let (outbox, lanes) = Outbox::new();
        let calls = Arc::new(Mutex::new(Vec::new()));
        let features = Features {
            permissions: perms(input),
            injector: Box::new(RecordingInjector {
                calls: calls.clone(),
            }),
            outbox,
            file_ops: FileOpsHandler::new(true, false),
            terminal: None,
            files: FileTasks::default(),
        };
        (features, lanes, calls)
    }

    type TestTerminal = (
        TerminalHandle,
        mpsc::Receiver<Vec<u8>>,
        mpsc::Receiver<(u16, u16)>,
    );

    fn terminal() -> TestTerminal {
        let (stdin_tx, stdin_rx) = mpsc::channel(8);
        let (resize_tx, resize_rx) = mpsc::channel(8);
        let shutdown = Arc::new(std::sync::atomic::AtomicBool::new(false));
        (
            TerminalHandle::new(stdin_tx, resize_tx, shutdown),
            stdin_rx,
            resize_rx,
        )
    }

    async fn handle(features: &mut Features, frame: Frame) {
        features.handle(&frame.encode().unwrap()).await;
    }

    async fn next(lanes: &mut Lanes) -> Frame {
        tokio::time::timeout(Duration::from_secs(5), async {
            loop {
                if let Some(bytes) = lanes.try_next() {
                    return Frame::decode(&bytes).unwrap().0;
                }
                tokio::time::sleep(Duration::from_millis(2)).await;
            }
        })
        .await
        .expect("a frame arrives")
    }

    #[tokio::test]
    async fn a_ping_is_answered_with_a_pong() {
        let (mut f, mut lanes, _) = features(true);
        handle(&mut f, Frame::Ping).await;
        assert_eq!(next(&mut lanes).await, Frame::Pong);
    }

    #[tokio::test]
    async fn terminal_bytes_reach_the_shell_and_are_dropped_without_one() {
        let (mut f, mut lanes, _) = features(true);
        handle(
            &mut f,
            Frame::Terminal(TerminalFrame {
                data: b"ls\n".to_vec(),
            }),
        )
        .await;
        assert!(lanes.try_next().is_none());

        let (term, mut stdin, _resize) = terminal();
        f.terminal = Some(term);
        handle(
            &mut f,
            Frame::Terminal(TerminalFrame {
                data: b"ls -la\n".to_vec(),
            }),
        )
        .await;
        assert_eq!(stdin.try_recv().unwrap(), b"ls -la\n");
    }

    #[tokio::test]
    async fn a_screen_picture_from_the_browser_is_ignored() {
        let (mut f, mut lanes, calls) = features(true);
        let picture = Frame::Desktop(DesktopFrame {
            sequence: 0,
            x: 0,
            y: 0,
            width: 1,
            height: 1,
            encoding: FrameEncoding::Raw,
            data: vec![0; 4],
        });
        handle(&mut f, picture).await;
        assert!(lanes.try_next().is_none());
        assert!(calls.lock().unwrap().is_empty());
    }

    #[tokio::test]
    async fn input_is_injected_only_with_the_input_permission() {
        let (mut f, _lanes, calls) = features(true);
        handle(
            &mut f,
            Frame::Control(ControlMessage::MouseMove { x: 100, y: 200 }),
        )
        .await;
        handle(
            &mut f,
            Frame::Control(ControlMessage::MouseClick {
                button: MouseButton::Left,
                pressed: true,
                x: 50,
                y: 60,
            }),
        )
        .await;
        handle(
            &mut f,
            Frame::Control(ControlMessage::KeyPress {
                key: KeyCode::KeyA,
                pressed: true,
            }),
        )
        .await;
        assert_eq!(
            *calls.lock().unwrap(),
            vec![
                "mouse_move:100,200",
                "mouse_move:50,60",
                "mouse_button:Left:true",
                "key:KeyA:true"
            ]
        );

        let (mut denied, _lanes, denied_calls) = features(false);
        handle(
            &mut denied,
            Frame::Control(ControlMessage::MouseMove { x: 1, y: 2 }),
        )
        .await;
        assert!(denied_calls.lock().unwrap().is_empty());
    }

    #[tokio::test]
    async fn a_key_press_also_reaches_an_open_terminal_and_a_resize_its_size() {
        let (mut f, _lanes, _) = features(true);
        let (term, mut stdin, mut resize) = terminal();
        f.terminal = Some(term);
        handle(
            &mut f,
            Frame::Control(ControlMessage::KeyPress {
                key: KeyCode::KeyA,
                pressed: true,
            }),
        )
        .await;
        assert_eq!(stdin.try_recv().unwrap(), b"a");
        handle(
            &mut f,
            Frame::Control(ControlMessage::TerminalResize {
                cols: 132,
                rows: 50,
            }),
        )
        .await;
        assert_eq!(resize.try_recv().unwrap(), (132, 50));
    }

    #[tokio::test]
    async fn a_chat_message_is_echoed_as_the_agent() {
        let (mut f, mut lanes, _) = features(true);
        for text in ["", "héllo 🌍", "line1\nline2"] {
            handle(
                &mut f,
                Frame::Control(ControlMessage::ChatMessage {
                    text: text.to_string(),
                    sender: "browser".to_string(),
                }),
            )
            .await;
            assert_eq!(
                next(&mut lanes).await,
                Frame::Control(ControlMessage::ChatMessage {
                    text: text.to_string(),
                    sender: "agent".to_string(),
                })
            );
        }
    }

    #[tokio::test]
    async fn a_chat_message_too_large_to_echo_is_refused_and_the_session_carries_on() {
        let (mut f, mut lanes, _) = features(true);
        handle(
            &mut f,
            Frame::Control(ControlMessage::ChatMessage {
                text: "x".repeat(INTERACTIVE_FRAME_CAP),
                sender: "browser".to_string(),
            }),
        )
        .await;
        assert!(
            lanes.try_next().is_none(),
            "an echo past one piece is never sent"
        );
        handle(&mut f, Frame::Ping).await;
        assert_eq!(next(&mut lanes).await, Frame::Pong);
    }

    #[tokio::test]
    async fn file_requests_reach_the_file_handler() {
        let (mut f, mut lanes, _) = features(true);
        let dir = tempfile::tempdir().unwrap();
        std::fs::write(dir.path().join("note.txt"), b"hello").unwrap();
        let path = dir.path().to_string_lossy().to_string();

        handle(
            &mut f,
            Frame::Control(ControlMessage::FileListRequest { path: path.clone() }),
        )
        .await;
        match next(&mut lanes).await {
            Frame::Control(ControlMessage::FileListResponse { entries, more, .. }) => {
                assert_eq!(entries.len(), 1);
                assert!(!more);
            }
            other => panic!("expected a listing, got {other:?}"),
        }

        let file = format!("{path}/note.txt");
        handle(
            &mut f,
            Frame::Control(ControlMessage::FileDownloadRequest { path: file }),
        )
        .await;
        match next(&mut lanes).await {
            Frame::FileTransfer(piece) => assert_eq!(piece.data, b"hello"),
            other => panic!("expected file data, got {other:?}"),
        }
        f.files.stop().await;
    }

    #[tokio::test]
    async fn an_undecodable_frame_is_dropped() {
        let (mut f, mut lanes, calls) = features(true);
        f.handle(&[0xff, 0xff]).await;
        assert!(lanes.try_next().is_none());
        assert!(calls.lock().unwrap().is_empty());
        let (mut null, _l, _) = features(true);
        null.injector = Box::new(NullInput);
        handle(
            &mut null,
            Frame::Control(ControlMessage::MouseMove { x: 1, y: 1 }),
        )
        .await;
    }
}
