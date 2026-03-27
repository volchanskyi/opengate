//! IPC client for communicating with the `mesh-agent` service.
//!
//! Connects to the agent's Unix domain socket and exchanges
//! JSON-over-newline messages. Automatically reconnects on failure.

use std::path::PathBuf;

use mesh_agent_ipc::{TrayEvent, TrayRequest, TrayResponse};
use tokio::io::{AsyncBufReadExt, AsyncWriteExt, BufReader};
use tokio::net::UnixStream;
use tokio::sync::mpsc;
use tracing::{debug, info, warn};

/// Default socket path matching the agent's IPC server.
const DEFAULT_SOCKET_PATH: &str = "/run/mesh-agent/tray.sock";

/// Reconnect delay when the agent socket is unavailable.
const RECONNECT_DELAY: std::time::Duration = std::time::Duration::from_secs(5);

/// IPC client that maintains a persistent connection to the agent.
pub struct IpcClient {
    socket_path: PathBuf,
}

/// Messages the IPC reader task sends to the main tray loop.
#[derive(Debug)]
pub enum IpcMessage {
    /// A response to a request we sent.
    Response(TrayResponse),
    /// A push event from the agent.
    Event(TrayEvent),
    /// The connection was lost.
    Disconnected,
    /// Successfully connected to agent.
    Connected,
}

impl IpcClient {
    pub fn new() -> Self {
        Self {
            socket_path: PathBuf::from(DEFAULT_SOCKET_PATH),
        }
    }

    /// Spawn the IPC connection manager. Returns channels for sending requests
    /// and receiving responses/events.
    ///
    /// The manager automatically reconnects when the connection drops.
    pub fn spawn(self) -> (mpsc::Sender<TrayRequest>, mpsc::Receiver<IpcMessage>) {
        let (req_tx, req_rx) = mpsc::channel::<TrayRequest>(16);
        let (msg_tx, msg_rx) = mpsc::channel::<IpcMessage>(64);

        tokio::spawn(connection_loop(self.socket_path, req_rx, msg_tx));

        (req_tx, msg_rx)
    }
}

/// Main connection loop: connect → exchange messages → reconnect on failure.
async fn connection_loop(
    socket_path: PathBuf,
    mut req_rx: mpsc::Receiver<TrayRequest>,
    msg_tx: mpsc::Sender<IpcMessage>,
) {
    loop {
        match UnixStream::connect(&socket_path).await {
            Ok(stream) => {
                info!(path = %socket_path.display(), "connected to agent");
                let _ = msg_tx.send(IpcMessage::Connected).await;

                if let Err(e) = handle_connection(stream, &mut req_rx, &msg_tx).await {
                    debug!(error = %e, "IPC connection error");
                }

                let _ = msg_tx.send(IpcMessage::Disconnected).await;
                info!("disconnected from agent, reconnecting...");
            }
            Err(e) => {
                debug!(error = %e, path = %socket_path.display(), "cannot connect to agent");
            }
        }

        tokio::time::sleep(RECONNECT_DELAY).await;
    }
}

/// Handle a single connected session: read responses/events and write requests.
async fn handle_connection(
    stream: UnixStream,
    req_rx: &mut mpsc::Receiver<TrayRequest>,
    msg_tx: &mpsc::Sender<IpcMessage>,
) -> anyhow::Result<()> {
    let (reader, mut writer) = stream.into_split();
    let mut lines = BufReader::new(reader).lines();

    loop {
        tokio::select! {
            // Incoming: response or push event from agent
            line = lines.next_line() => {
                let line = match line? {
                    Some(l) => l,
                    None => return Ok(()), // agent closed connection
                };

                // Try to parse as TrayResponse first, then as TrayEvent.
                if let Ok(resp) = serde_json::from_str::<TrayResponse>(&line) {
                    msg_tx.send(IpcMessage::Response(resp)).await?;
                } else if let Ok(evt) = serde_json::from_str::<TrayEvent>(&line) {
                    msg_tx.send(IpcMessage::Event(evt)).await?;
                } else {
                    warn!(line = %line, "unrecognized IPC message from agent");
                }
            }
            // Outgoing: request from tray to agent
            request = req_rx.recv() => {
                let request = match request {
                    Some(r) => r,
                    None => return Ok(()), // tray shutting down
                };
                let buf = mesh_agent_ipc::encode_line(&request)?;
                writer.write_all(&buf).await?;
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use tokio::net::UnixListener;

    #[tokio::test]
    async fn test_ipc_client_connects_and_receives_status() {
        let dir = tempfile::tempdir().unwrap();
        let sock_path = dir.path().join("test.sock");

        let listener = UnixListener::bind(&sock_path).unwrap();

        // Spawn a mock agent that responds to Status requests
        tokio::spawn(async move {
            let (stream, _) = listener.accept().await.unwrap();
            let (reader, mut writer) = stream.into_split();
            let mut lines = BufReader::new(reader).lines();

            if let Ok(Some(line)) = lines.next_line().await {
                let req: TrayRequest = serde_json::from_str(&line).unwrap();
                assert!(matches!(req, TrayRequest::Status));

                let resp = TrayResponse::Status {
                    connected: true,
                    version: "0.15.4".to_string(),
                    server_addr: "test:4433".to_string(),
                    uptime_secs: 100,
                };
                let buf = mesh_agent_ipc::encode_line(&resp).unwrap();
                writer.write_all(&buf).await.unwrap();
            }
        });

        let client = IpcClient {
            socket_path: sock_path,
        };
        let (req_tx, mut msg_rx) = client.spawn();

        // Wait for Connected message
        let msg = msg_rx.recv().await.unwrap();
        assert!(matches!(msg, IpcMessage::Connected));

        // Send a status request
        req_tx.send(TrayRequest::Status).await.unwrap();

        // Receive response
        let msg = msg_rx.recv().await.unwrap();
        match msg {
            IpcMessage::Response(TrayResponse::Status { version, .. }) => {
                assert_eq!(version, "0.15.4");
            }
            other => panic!("expected Status response, got {other:?}"),
        }
    }

    #[tokio::test]
    async fn test_ipc_client_receives_push_event() {
        let dir = tempfile::tempdir().unwrap();
        let sock_path = dir.path().join("test.sock");

        let listener = UnixListener::bind(&sock_path).unwrap();

        tokio::spawn(async move {
            let (stream, _) = listener.accept().await.unwrap();
            let (_, mut writer) = stream.into_split();

            // Send a push event
            let evt = TrayEvent::ConnectionChanged { connected: false };
            let buf = mesh_agent_ipc::encode_line(&evt).unwrap();
            writer.write_all(&buf).await.unwrap();
        });

        let client = IpcClient {
            socket_path: sock_path,
        };
        let (_req_tx, mut msg_rx) = client.spawn();

        // Connected
        let msg = msg_rx.recv().await.unwrap();
        assert!(matches!(msg, IpcMessage::Connected));

        // Push event
        let msg = msg_rx.recv().await.unwrap();
        match msg {
            IpcMessage::Event(TrayEvent::ConnectionChanged { connected }) => {
                assert!(!connected);
            }
            other => panic!("expected ConnectionChanged event, got {other:?}"),
        }
    }

    #[tokio::test]
    async fn test_ipc_client_reconnects_on_disconnect() {
        let dir = tempfile::tempdir().unwrap();
        let sock_path = dir.path().join("test.sock");

        // No listener → client should get Disconnected and keep trying
        let client = IpcClient {
            socket_path: sock_path.clone(),
        };
        let (_req_tx, mut msg_rx) = client.spawn();

        // Give the client time to fail connection attempts
        tokio::time::sleep(std::time::Duration::from_millis(100)).await;

        // Now start a listener
        let listener = UnixListener::bind(&sock_path).unwrap();
        tokio::spawn(async move {
            let (stream, _) = listener.accept().await.unwrap();
            // Keep connection open briefly
            tokio::time::sleep(std::time::Duration::from_secs(1)).await;
            drop(stream);
        });

        // Eventually we should get a Connected message
        let msg = tokio::time::timeout(std::time::Duration::from_secs(10), msg_rx.recv())
            .await
            .unwrap()
            .unwrap();
        assert!(matches!(msg, IpcMessage::Connected));
    }
}
