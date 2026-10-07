//! Session run-loop coverage driven by an in-process relay stub on a local `TcpListener`,
//! with `NullCapture` and `NullInput`.

use futures_util::{SinkExt, StreamExt};
use mesh_agent_core::webrtc::IceServerConfig;
use mesh_agent_core::{NullCapture, NullInput, SessionError, SessionHandler};
use mesh_protocol::{Permissions, SessionToken};
use tokio::net::TcpListener;
use tokio::task::JoinHandle;
use tokio_tungstenite::tungstenite::Message;

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
async fn a_handler_with_ice_servers_still_runs_a_session() {
    let (url, listener) = bind_relay_stub().await;
    let relay = spawn_scripted_relay(listener, Vec::new());

    SessionHandler::new(SessionToken::generate(), no_permissions())
        .with_ice_servers(vec![IceServerConfig {
            urls: vec!["stun:stun.example.com:3478".to_string()],
            username: String::new(),
            credential: String::new(),
        }])
        .run(&url, Box::new(NullCapture), Box::new(NullInput))
        .await
        .expect("configured ICE servers must not affect the session lifecycle");

    relay.await.expect("relay stub task");
}
