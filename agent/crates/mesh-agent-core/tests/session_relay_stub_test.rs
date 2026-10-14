//! Session run-loop coverage driven by an in-process relay stub on a local `TcpListener`,
//! with `NullCapture` and `NullInput`.

use std::collections::HashSet;
use std::time::Duration;

use futures_util::{SinkExt, StreamExt};
use mesh_agent_core::session::handlers::DOWNLOAD_RUNNING;
use mesh_agent_core::session::outbox::{FILE_PIECE, INTERACTIVE_FRAME_CAP};
use mesh_agent_core::{NullCapture, NullInput, SessionError, SessionHandler};
use mesh_protocol::{ControlMessage, FileFrame, Frame, Permissions, SessionToken};
use tokio::net::{TcpListener, TcpStream};
use tokio::task::JoinHandle;
use tokio_tungstenite::tungstenite::Message;
use tokio_tungstenite::WebSocketStream;

fn no_permissions() -> Permissions {
    Permissions {
        desktop: false,
        terminal: false,
        file_read: false,
        file_write: false,
        input: false,
    }
}

async fn bind_relay_stub() -> (String, TcpListener) {
    let listener = TcpListener::bind("127.0.0.1:0")
        .await
        .expect("bind relay stub");
    let addr = listener.local_addr().expect("relay stub address");
    (format!("ws://{addr}/ws/relay/session-token"), listener)
}

fn spawn_scripted_relay(listener: TcpListener, script: Vec<Message>) -> JoinHandle<Vec<Message>> {
    tokio::spawn(async move {
        let (stream, _) = listener.accept().await.expect("relay stub accept");
        let mut ws = tokio_tungstenite::accept_async(stream)
            .await
            .expect("relay stub handshake");

        for msg in script {
            ws.send(msg).await.expect("relay stub send");
        }
        ws.send(Message::Close(None))
            .await
            .expect("relay stub close");

        let mut received = Vec::new();
        while let Some(Ok(msg)) = ws.next().await {
            received.push(msg);
        }
        received
    })
}

async fn run_session(url: &str, permissions: Permissions) -> Result<(), SessionError> {
    SessionHandler::new(SessionToken::generate(), permissions)
        .run(url, Box::new(NullCapture), Box::new(NullInput))
        .await
}

#[tokio::test]
async fn relay_close_ends_the_session_cleanly() {
    let (url, listener) = bind_relay_stub().await;
    let relay = spawn_scripted_relay(listener, Vec::new());

    run_session(&url, no_permissions())
        .await
        .expect("session should end cleanly when the relay closes");

    relay.await.expect("relay stub task");
}

#[tokio::test]
async fn empty_and_undecodable_frames_keep_the_session_alive() {
    let (url, listener) = bind_relay_stub().await;
    let relay = spawn_scripted_relay(
        listener,
        vec![
            Message::Binary(Vec::new().into()),
            Message::Binary(vec![0xFF, 0xFF, 0xFF, 0xFF].into()),
            Message::Text("not a frame".into()),
        ],
    );

    run_session(&url, no_permissions())
        .await
        .expect("session should survive junk frames");

    relay.await.expect("relay stub task");
}

/// Sends each payload as a ping and reads one answer per ping, then closes and keeps the rest.
fn spawn_pinging_relay(
    listener: TcpListener,
    payloads: Vec<Vec<u8>>,
) -> JoinHandle<(Vec<Message>, Vec<Message>)> {
    tokio::spawn(async move {
        let (stream, _) = listener.accept().await.expect("relay stub accept");
        let mut ws = tokio_tungstenite::accept_async(stream)
            .await
            .expect("relay stub handshake");

        let mut answers = Vec::new();
        for payload in payloads {
            ws.send(Message::Ping(payload.into()))
                .await
                .expect("relay stub ping");
            // Each answer is read before the next ping, so a stray data message takes a pong's slot.
            let answer = tokio::time::timeout(std::time::Duration::from_secs(5), ws.next())
                .await
                .expect("the agent answers a ping without other traffic")
                .expect("agent answer")
                .expect("agent frame");
            answers.push(answer);
        }
        ws.send(Message::Close(None))
            .await
            .expect("relay stub close");

        let mut rest = Vec::new();
        while let Some(Ok(msg)) = ws.next().await {
            rest.push(msg);
        }
        (answers, rest)
    })
}

fn data_messages(messages: &[Message]) -> Vec<&Message> {
    messages
        .iter()
        .filter(|m| matches!(m, Message::Binary(_) | Message::Text(_)))
        .collect()
}

#[tokio::test]
async fn ping_is_answered_with_a_pong_and_no_data_message() {
    let (url, listener) = bind_relay_stub().await;
    let relay = spawn_pinging_relay(listener, vec![vec![0xB0, 0xA7]]);

    run_session(&url, no_permissions())
        .await
        .expect("session should end cleanly after a ping");

    let (answers, rest) = relay.await.expect("relay stub task");
    assert_eq!(answers, vec![Message::Pong(vec![0xB0, 0xA7].into())]);
    assert!(
        data_messages(&rest).is_empty(),
        "a ping must not reach the browser as data: {rest:?}"
    );
}

#[tokio::test]
async fn an_idle_session_answers_each_keep_alive_with_its_own_pong() {
    let (url, listener) = bind_relay_stub().await;
    // The server's keep-alive payload is a counter written as text.
    let relay = spawn_pinging_relay(listener, vec![b"37".to_vec(), b"38".to_vec()]);

    run_session(&url, no_permissions())
        .await
        .expect("session should end cleanly after two keep-alives");

    let (answers, rest) = relay.await.expect("relay stub task");
    assert_eq!(
        answers,
        vec![
            Message::Pong(b"37".to_vec().into()),
            Message::Pong(b"38".to_vec().into()),
        ]
    );
    assert!(
        data_messages(&rest).is_empty(),
        "a keep-alive must not reach the browser as data: {rest:?}"
    );
}

#[tokio::test]
async fn a_dropped_transport_ends_the_session() {
    let (url, listener) = bind_relay_stub().await;
    // Dropping the socket without a close handshake surfaces as a receive error on the agent.
    let relay = tokio::spawn(async move {
        let (stream, _) = listener.accept().await.expect("relay stub accept");
        let ws = tokio_tungstenite::accept_async(stream)
            .await
            .expect("relay stub handshake");
        drop(ws);
    });

    run_session(&url, no_permissions())
        .await
        .expect("session should end when the transport disappears");

    relay.await.expect("relay stub task");
}

#[tokio::test]
async fn desktop_permission_starts_a_capture_task() {
    let (url, listener) = bind_relay_stub().await;
    let relay = spawn_scripted_relay(listener, Vec::new());

    let mut permissions = no_permissions();
    permissions.desktop = true;

    // NullCapture reports no display, so the capture task ends on its own.
    run_session(&url, permissions)
        .await
        .expect("session with desktop permission should end cleanly");

    relay.await.expect("relay stub task");
}

#[tokio::test]
async fn terminal_permission_starts_a_terminal_session() {
    let (url, listener) = bind_relay_stub().await;
    let relay = spawn_scripted_relay(listener, Vec::new());

    let mut permissions = no_permissions();
    permissions.terminal = true;

    // A host without a usable PTY logs and continues without a terminal.
    run_session(&url, permissions)
        .await
        .expect("session with terminal permission should end cleanly");

    relay.await.expect("relay stub task");
}

#[tokio::test]
async fn an_unparseable_relay_url_fails_before_connecting() {
    let err = run_session("::not a url::", no_permissions())
        .await
        .expect_err("an unparseable relay URL must not reach the transport");
    assert!(
        matches!(err, SessionError::WebSocket(_)),
        "expected a WebSocket error, got {err:?}"
    );
}

#[tokio::test]
async fn an_unreachable_relay_surfaces_the_transport_error() {
    // The dropped listener leaves the port closed, so the connect attempt fails.
    let (url, listener) = bind_relay_stub().await;
    drop(listener);

    let err = run_session(&url, no_permissions())
        .await
        .expect_err("connecting to a closed port must fail");
    assert!(
        matches!(err, SessionError::WebSocket(_)),
        "expected a WebSocket error, got {err:?}"
    );
}

#[tokio::test]
async fn a_handler_with_helpers_still_runs_a_session() {
    let (url, listener) = bind_relay_stub().await;
    let relay = spawn_scripted_relay(listener, Vec::new());

    SessionHandler::new(SessionToken::generate(), no_permissions())
        .with_helpers(vec!["stun:stun.example.com:3478".to_string()])
        .run(&url, Box::new(NullCapture), Box::new(NullInput))
        .await
        .expect("configured helpers must not affect the session lifecycle");

    relay.await.expect("relay stub task");
}

type Relay = WebSocketStream<TcpStream>;

/// Starts a session against a relay stub and returns the browser's end of the relay.
async fn open_session(permissions: Permissions) -> (Relay, JoinHandle<Result<(), SessionError>>) {
    let (url, listener) = bind_relay_stub().await;
    let session = tokio::spawn(async move { run_session(&url, permissions).await });
    let (stream, _) = listener.accept().await.expect("relay stub accept");
    let ws = tokio_tungstenite::accept_async(stream)
        .await
        .expect("relay stub handshake");
    (ws, session)
}

async fn send(ws: &mut Relay, frame: Frame) {
    ws.send(Message::Binary(frame.encode().expect("encode").into()))
        .await
        .expect("relay stub send");
}

/// The host's next frame on the relay, with its encoded size.
async fn next_frame(ws: &mut Relay) -> (Frame, usize) {
    loop {
        let msg = tokio::time::timeout(Duration::from_secs(10), ws.next())
            .await
            .expect("the host answers")
            .expect("the relay stays open")
            .expect("a relay message");
        if let Message::Binary(data) = msg {
            return (Frame::decode(&data).expect("a whole frame").0, data.len());
        }
    }
}

/// Closes the relay and waits for the session to end cleanly.
async fn end(mut ws: Relay, session: JoinHandle<Result<(), SessionError>>) {
    ws.close(None).await.expect("relay stub close");
    while let Some(Ok(_)) = ws.next().await {}
    tokio::time::timeout(Duration::from_secs(10), session)
        .await
        .expect("the session ends when its relay closes")
        .expect("session task")
        .expect("a clean end");
}

fn file_permissions() -> Permissions {
    Permissions {
        file_read: true,
        ..no_permissions()
    }
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn a_second_offer_is_refused_and_the_session_carries_on_over_the_relay() {
    let (mut ws, session) = open_session(no_permissions()).await;
    for _ in 0..2 {
        let offer = ControlMessage::DirectOffer {
            sdp: "v=0 not an offer".to_string(),
        };
        send(&mut ws, Frame::Control(offer)).await;
    }
    send(&mut ws, Frame::Ping).await;

    let mut refusals = 0;
    loop {
        match next_frame(&mut ws).await.0 {
            Frame::Control(ControlMessage::DirectRefused { reason }) => {
                assert!(!reason.is_empty());
                refusals += 1;
            }
            Frame::Control(ControlMessage::DirectFallback { received }) => assert_eq!(received, 0),
            Frame::Pong => break,
            other => panic!("unexpected frame from the host: {other:?}"),
        }
    }
    assert_eq!(refusals, 1, "the second offer, and only it, is refused");
    end(ws, session).await;
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn a_download_streams_through_the_session_and_a_second_is_refused_while_it_runs() {
    let dir = tempfile::tempdir().expect("tempdir");
    let big = dir.path().join("backup.tar");
    let data: Vec<u8> = (0..16 * FILE_PIECE + 17).map(|i| (i % 251) as u8).collect();
    std::fs::write(&big, &data).expect("write the big file");
    let small = dir.path().join("hosts");
    std::fs::write(&small, b"127.0.0.1 localhost\n").expect("write the small file");
    let (big, small) = (
        big.to_string_lossy().to_string(),
        small.to_string_lossy().to_string(),
    );

    let (mut ws, session) = open_session(file_permissions()).await;
    for path in [&big, &small] {
        let request = ControlMessage::FileDownloadRequest { path: path.clone() };
        send(&mut ws, Frame::Control(request)).await;
    }

    let mut received = Vec::new();
    let mut refused = false;
    while received.len() < data.len() {
        match next_frame(&mut ws).await {
            (
                Frame::FileTransfer(FileFrame {
                    offset,
                    total_size,
                    data: piece,
                }),
                len,
            ) => {
                assert!(
                    len <= FILE_PIECE + 64,
                    "a file frame of {len} bytes is past one piece"
                );
                assert_eq!(offset as usize, received.len(), "pieces arrive in order");
                assert_eq!(total_size as usize, data.len());
                received.extend(piece);
            }
            (Frame::Control(ControlMessage::FileDownloadError { path, error }), _) => {
                assert_eq!(path, small);
                assert_eq!(error, DOWNLOAD_RUNNING);
                refused = true;
            }
            (other, _) => panic!("unexpected frame from the host: {other:?}"),
        }
    }
    assert!(refused, "a second download is refused while one runs");
    assert_eq!(received, data, "the download matches its source");

    let request = ControlMessage::FileDownloadRequest { path: small };
    send(&mut ws, Frame::Control(request)).await;
    match next_frame(&mut ws).await.0 {
        Frame::FileTransfer(piece) => assert_eq!(piece.data, b"127.0.0.1 localhost\n"),
        other => panic!("the next download runs once the first is done, got {other:?}"),
    }
    end(ws, session).await;
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn a_large_folder_lists_in_pages_and_a_ping_goes_ahead_of_them() {
    let dir = tempfile::tempdir().expect("tempdir");
    for i in 0..5_000 {
        let name = format!("event-{i:05}-collected-from-the-host-journal.log");
        std::fs::write(dir.path().join(name), b"").expect("write a file");
    }
    let path = dir.path().to_string_lossy().to_string();

    let (mut ws, session) = open_session(file_permissions()).await;
    send(
        &mut ws,
        Frame::Control(ControlMessage::FileListRequest { path }),
    )
    .await;
    send(&mut ws, Frame::Ping).await;

    let mut names = HashSet::new();
    let mut pages = 0;
    let mut ponged_at = None;
    loop {
        match next_frame(&mut ws).await {
            (Frame::Control(ControlMessage::FileListResponse { entries, more, .. }), len) => {
                assert!(
                    len <= INTERACTIVE_FRAME_CAP,
                    "a page of {len} bytes is past one piece"
                );
                pages += 1;
                for entry in entries {
                    assert!(names.insert(entry.name), "each entry is listed once");
                }
                if !more {
                    break;
                }
            }
            (Frame::Pong, _) => ponged_at = Some(pages),
            (other, _) => panic!("unexpected frame from the host: {other:?}"),
        }
    }
    assert_eq!(names.len(), 5_000);
    assert!(pages > 1, "the folder goes out in pages");
    let ponged_at = ponged_at.expect("the ping is answered");
    assert!(
        ponged_at < pages,
        "the pong goes ahead of the last page, at {ponged_at} of {pages}"
    );
    end(ws, session).await;
}
