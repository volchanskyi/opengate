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

#[tokio::test]
async fn ping_is_answered_before_the_session_ends() {
    let (url, listener) = bind_relay_stub().await;
    let relay = tokio::spawn(async move {
        let (stream, _) = listener.accept().await.expect("relay stub accept");
        let mut ws = tokio_tungstenite::accept_async(stream)
            .await
            .expect("relay stub handshake");

        ws.send(Message::Ping(vec![0xB0, 0xA7].into()))
            .await
            .expect("relay stub ping");
        // The answer is read before closing so teardown cannot race it away.
        let answer = ws.next().await.expect("agent answer").expect("agent frame");
        ws.send(Message::Close(None))
            .await
            .expect("relay stub close");
        while ws.next().await.is_some() {}
        answer
    });

    run_session(&url, no_permissions())
        .await
        .expect("session should end cleanly after a ping");

    let answer = relay.await.expect("relay stub task");
    assert_eq!(
        answer.into_data().to_vec(),
        vec![0xB0, 0xA7],
        "the agent must echo the ping payload back to the relay"
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
