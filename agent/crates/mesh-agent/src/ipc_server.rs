//! IPC server for tray communication.
//!
//! Listens on a Unix domain socket and handles JSON-over-newline requests
//! from `mesh-agent-tray`. Each connected tray client receives push events
//! when agent state changes (e.g. connection status).

use std::path::{Path, PathBuf};

use mesh_agent_ipc::{TrayEvent, TrayRequest, TrayResponse, UpdateState};
use tokio::io::{AsyncBufReadExt, AsyncWriteExt, BufReader};
use tokio::net::{UnixListener, UnixStream};
use tokio::sync::{broadcast, watch};
use tracing::{debug, error, info, warn};

/// Default socket path for the IPC channel.
const DEFAULT_SOCKET_DIR: &str = "/run/mesh-agent";
const SOCKET_NAME: &str = "tray.sock";

/// Shared agent state that the IPC server exposes to tray clients.
#[derive(Debug, Clone)]
pub struct AgentState {
    pub version: String,
    pub device_id: String,
    pub hostname: String,
    pub os: String,
    pub arch: String,
    pub server_addr: String,
    pub connected: bool,
    pub start_time: std::time::Instant,
    pub log_path: String,
}

impl AgentState {
    fn uptime_secs(&self) -> u64 {
        self.start_time.elapsed().as_secs()
    }
}

/// Handle to the running IPC server for sending state updates.
#[derive(Clone)]
pub struct IpcHandle {
    state_tx: watch::Sender<AgentState>,
    event_tx: broadcast::Sender<TrayEvent>,
    socket_path: PathBuf,
}

impl IpcHandle {
    /// Update the shared agent state. Tray clients will see this on next status request.
    pub fn update_state<F>(&self, f: F)
    where
        F: FnOnce(&mut AgentState),
    {
        self.state_tx.send_modify(f);
    }

    /// Push a connection status change event to all connected tray clients.
    pub fn notify_connection_changed(&self, connected: bool) {
        self.update_state(|s| s.connected = connected);
        let _ = self.event_tx.send(TrayEvent::ConnectionChanged { connected });
    }

    /// Push an update progress event to all connected tray clients.
    pub fn notify_update_progress(&self, percent: u8, version: String) {
        let _ = self
            .event_tx
            .send(TrayEvent::UpdateProgress { percent, version });
    }

    /// Returns the socket path.
    pub fn socket_path(&self) -> &Path {
        &self.socket_path
    }
}

/// Requests that require action from the main agent loop.
#[derive(Debug)]
pub enum IpcAction {
    /// Tray requested agent restart.
    Restart,
    /// Tray requested update check.
    CheckUpdate,
    /// Tray requested a chat token.
    RequestChatToken {
        /// Channel to send the response back to the tray client.
        reply: tokio::sync::oneshot::Sender<TrayResponse>,
    },
}

/// Start the IPC server. Returns a handle for updating state and a receiver
/// for actions that need the main loop's attention.
pub fn start(
    initial_state: AgentState,
) -> anyhow::Result<(IpcHandle, tokio::sync::mpsc::Receiver<IpcAction>)> {
    let socket_path = PathBuf::from(DEFAULT_SOCKET_DIR).join(SOCKET_NAME);

    // Ensure socket directory exists with correct permissions.
    std::fs::create_dir_all(DEFAULT_SOCKET_DIR).unwrap_or_else(|e| {
        // Non-fatal: we might not have permissions (non-root), fall back to data dir
        warn!(error = %e, "cannot create {DEFAULT_SOCKET_DIR}, IPC may not be available");
    });

    // Remove stale socket file.
    let _ = std::fs::remove_file(&socket_path);

    let listener = UnixListener::bind(&socket_path).map_err(|e| {
        anyhow::anyhow!("failed to bind IPC socket at {}: {e}", socket_path.display())
    })?;

    // Set socket permissions: owner + group readable/writable (0660).
    // If the `mesh-agent` group exists, set group ownership so desktop users
    // in that group can connect via mesh-agent-tray.
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let _ = std::fs::set_permissions(&socket_path, std::fs::Permissions::from_mode(0o660));
        set_socket_group(&socket_path);
    }

    info!(path = %socket_path.display(), "IPC server listening");

    let (state_tx, state_rx) = watch::channel(initial_state);
    let (event_tx, _) = broadcast::channel::<TrayEvent>(32);
    let (action_tx, action_rx) = tokio::sync::mpsc::channel::<IpcAction>(16);

    let handle = IpcHandle {
        state_tx,
        event_tx: event_tx.clone(),
        socket_path: socket_path.clone(),
    };

    // Spawn the accept loop.
    let state_rx_clone = state_rx;
    let action_tx_clone = action_tx;
    let event_tx_clone = event_tx;
    tokio::spawn(async move {
        accept_loop(listener, state_rx_clone, event_tx_clone, action_tx_clone).await;
    });

    Ok((handle, action_rx))
}

/// Accept loop: accepts tray client connections and spawns a handler for each.
async fn accept_loop(
    listener: UnixListener,
    state_rx: watch::Receiver<AgentState>,
    event_tx: broadcast::Sender<TrayEvent>,
    action_tx: tokio::sync::mpsc::Sender<IpcAction>,
) {
    loop {
        match listener.accept().await {
            Ok((stream, _addr)) => {
                debug!("tray client connected");
                let state_rx = state_rx.clone();
                let event_tx = event_tx.subscribe();
                let action_tx = action_tx.clone();
                tokio::spawn(async move {
                    if let Err(e) = handle_client(stream, state_rx, event_tx, action_tx).await {
                        debug!(error = %e, "tray client disconnected");
                    }
                });
            }
            Err(e) => {
                error!(error = %e, "failed to accept IPC connection");
                tokio::time::sleep(std::time::Duration::from_secs(1)).await;
            }
        }
    }
}

/// Handle a single tray client connection.
async fn handle_client(
    stream: UnixStream,
    state_rx: watch::Receiver<AgentState>,
    mut event_rx: broadcast::Receiver<TrayEvent>,
    action_tx: tokio::sync::mpsc::Sender<IpcAction>,
) -> anyhow::Result<()> {
    let (reader, mut writer) = stream.into_split();
    let mut lines = BufReader::new(reader).lines();

    loop {
        tokio::select! {
            line = lines.next_line() => {
                let line = match line? {
                    Some(l) => l,
                    None => return Ok(()), // client disconnected
                };

                let request: TrayRequest = match serde_json::from_str(&line) {
                    Ok(r) => r,
                    Err(e) => {
                        let resp = TrayResponse::Error { message: format!("invalid request: {e}") };
                        let buf = mesh_agent_ipc::encode_line(&resp)?;
                        writer.write_all(&buf).await?;
                        continue;
                    }
                };

                let response = handle_request(request, &state_rx, &action_tx).await;
                let buf = mesh_agent_ipc::encode_line(&response)?;
                writer.write_all(&buf).await?;
            }
            event = event_rx.recv() => {
                match event {
                    Ok(evt) => {
                        let buf = mesh_agent_ipc::encode_line(&evt)?;
                        writer.write_all(&buf).await?;
                    }
                    Err(broadcast::error::RecvError::Lagged(n)) => {
                        debug!(skipped = n, "tray client lagged behind on events");
                    }
                    Err(broadcast::error::RecvError::Closed) => return Ok(()),
                }
            }
        }
    }
}

/// Process a single tray request and return the response.
async fn handle_request(
    request: TrayRequest,
    state_rx: &watch::Receiver<AgentState>,
    action_tx: &tokio::sync::mpsc::Sender<IpcAction>,
) -> TrayResponse {
    let state = state_rx.borrow().clone();

    match request {
        TrayRequest::Status => TrayResponse::Status {
            connected: state.connected,
            version: state.version.clone(),
            server_addr: state.server_addr.clone(),
            uptime_secs: state.uptime_secs(),
        },
        TrayRequest::GetInfo => TrayResponse::Info {
            version: state.version.clone(),
            device_id: state.device_id.clone(),
            hostname: state.hostname.clone(),
            os: state.os.clone(),
            arch: state.arch.clone(),
            server_addr: state.server_addr.clone(),
            connected: state.connected,
            uptime_secs: state.uptime_secs(),
            log_path: state.log_path.clone(),
        },
        TrayRequest::Restart => {
            let _ = action_tx.send(IpcAction::Restart).await;
            TrayResponse::RestartAck
        }
        TrayRequest::CheckUpdate => {
            let _ = action_tx.send(IpcAction::CheckUpdate).await;
            TrayResponse::UpdateStatus {
                status: UpdateState::Checking,
                version: state.version.clone(),
            }
        }
        TrayRequest::RequestChatToken => {
            let (reply_tx, reply_rx) = tokio::sync::oneshot::channel();
            if action_tx
                .send(IpcAction::RequestChatToken { reply: reply_tx })
                .await
                .is_err()
            {
                return TrayResponse::Error {
                    message: "agent shutting down".to_string(),
                };
            }
            match tokio::time::timeout(std::time::Duration::from_secs(10), reply_rx).await {
                Ok(Ok(resp)) => resp,
                Ok(Err(_)) => TrayResponse::Error {
                    message: "agent did not respond".to_string(),
                },
                Err(_) => TrayResponse::Error {
                    message: "chat token request timed out".to_string(),
                },
            }
        }
        TrayRequest::GetLogs { lines: count } => {
            // Read the last N lines from the log file.
            match read_log_tail(&state.log_path, count).await {
                Ok(lines) => TrayResponse::Logs { lines },
                Err(e) => TrayResponse::Error {
                    message: format!("failed to read logs: {e}"),
                },
            }
        }
        _ => TrayResponse::Error {
            message: "unsupported request".to_string(),
        },
    }
}

/// Read the last `count` lines from the log file.
async fn read_log_tail(log_path: &str, count: u32) -> anyhow::Result<Vec<String>> {
    let content = tokio::fs::read_to_string(log_path).await?;
    let all_lines: Vec<&str> = content.lines().collect();
    let start = all_lines.len().saturating_sub(count as usize);
    Ok(all_lines[start..].iter().map(|s| s.to_string()).collect())
}

/// Set group ownership of the socket to `mesh-agent` if the group exists.
/// This allows non-root desktop users (in the mesh-agent group) to connect.
#[cfg(unix)]
fn set_socket_group(socket_path: &Path) {
    use std::ffi::CString;

    let group_name = CString::new("mesh-agent").unwrap();
    // SAFETY: getgrnam is standard POSIX. We pass a valid C string.
    let grp = unsafe { libc::getgrnam(group_name.as_ptr()) };
    if grp.is_null() {
        debug!("mesh-agent group not found, socket accessible only to root");
        return;
    }
    let gid = unsafe { (*grp).gr_gid };

    let path_c = match CString::new(socket_path.to_string_lossy().as_bytes()) {
        Ok(p) => p,
        Err(_) => return,
    };
    // SAFETY: chown is standard POSIX. -1 means "don't change owner".
    let ret = unsafe { libc::chown(path_c.as_ptr(), u32::MAX, gid) };
    if ret == 0 {
        debug!(gid, "set socket group to mesh-agent");
    } else {
        warn!("failed to chown socket to mesh-agent group");
    }
}

/// Clean up the IPC socket file on shutdown.
pub fn cleanup(socket_path: &Path) {
    let _ = std::fs::remove_file(socket_path);
    debug!(path = %socket_path.display(), "IPC socket removed");
}

#[cfg(test)]
mod tests {
    use super::*;
    use tokio::io::AsyncWriteExt;
    use tokio::net::UnixStream;

    fn test_state() -> AgentState {
        AgentState {
            version: "0.15.4".to_string(),
            device_id: "test-device-123".to_string(),
            hostname: "test-host".to_string(),
            os: "Ubuntu 24.04".to_string(),
            arch: "x86_64".to_string(),
            server_addr: "server:4433".to_string(),
            connected: true,
            start_time: std::time::Instant::now(),
            log_path: "/tmp/test-agent.log".to_string(),
        }
    }

    #[tokio::test]
    async fn test_ipc_status_request() {
        let dir = tempfile::tempdir().unwrap();
        let sock_path = dir.path().join("test.sock");

        let listener = UnixListener::bind(&sock_path).unwrap();
        let (_state_tx, state_rx) = watch::channel(test_state());
        let (event_tx, _) = broadcast::channel::<TrayEvent>(32);
        let (action_tx, _action_rx) = tokio::sync::mpsc::channel::<IpcAction>(16);

        // Spawn server handler for one connection
        tokio::spawn(async move {
            let (stream, _) = listener.accept().await.unwrap();
            let evt_rx = event_tx.subscribe();
            handle_client(stream, state_rx, evt_rx, action_tx)
                .await
                .ok();
        });

        // Connect as tray client
        let mut client = UnixStream::connect(&sock_path).await.unwrap();
        let req = mesh_agent_ipc::encode_line(&TrayRequest::Status).unwrap();
        client.write_all(&req).await.unwrap();

        let mut buf_reader = BufReader::new(client);
        let mut line = String::new();
        buf_reader.read_line(&mut line).await.unwrap();

        let resp: TrayResponse = serde_json::from_str(&line).unwrap();
        match resp {
            TrayResponse::Status {
                connected,
                version,
                server_addr,
                ..
            } => {
                assert!(connected);
                assert_eq!(version, "0.15.4");
                assert_eq!(server_addr, "server:4433");
            }
            other => panic!("expected Status, got {other:?}"),
        }
    }

    #[tokio::test]
    async fn test_ipc_info_request() {
        let dir = tempfile::tempdir().unwrap();
        let sock_path = dir.path().join("test.sock");

        let listener = UnixListener::bind(&sock_path).unwrap();
        let (_state_tx, state_rx) = watch::channel(test_state());
        let (event_tx, _) = broadcast::channel::<TrayEvent>(32);
        let (action_tx, _action_rx) = tokio::sync::mpsc::channel::<IpcAction>(16);

        tokio::spawn(async move {
            let (stream, _) = listener.accept().await.unwrap();
            let evt_rx = event_tx.subscribe();
            handle_client(stream, state_rx, evt_rx, action_tx)
                .await
                .ok();
        });

        let mut client = UnixStream::connect(&sock_path).await.unwrap();
        let req = mesh_agent_ipc::encode_line(&TrayRequest::GetInfo).unwrap();
        client.write_all(&req).await.unwrap();

        let mut buf_reader = BufReader::new(client);
        let mut line = String::new();
        buf_reader.read_line(&mut line).await.unwrap();

        let resp: TrayResponse = serde_json::from_str(&line).unwrap();
        match resp {
            TrayResponse::Info {
                device_id,
                hostname,
                os,
                arch,
                ..
            } => {
                assert_eq!(device_id, "test-device-123");
                assert_eq!(hostname, "test-host");
                assert_eq!(os, "Ubuntu 24.04");
                assert_eq!(arch, "x86_64");
            }
            other => panic!("expected Info, got {other:?}"),
        }
    }

    #[tokio::test]
    async fn test_ipc_restart_request() {
        let dir = tempfile::tempdir().unwrap();
        let sock_path = dir.path().join("test.sock");

        let listener = UnixListener::bind(&sock_path).unwrap();
        let (_state_tx, state_rx) = watch::channel(test_state());
        let (event_tx, _) = broadcast::channel::<TrayEvent>(32);
        let (action_tx, mut action_rx) = tokio::sync::mpsc::channel::<IpcAction>(16);

        tokio::spawn(async move {
            let (stream, _) = listener.accept().await.unwrap();
            let evt_rx = event_tx.subscribe();
            handle_client(stream, state_rx, evt_rx, action_tx)
                .await
                .ok();
        });

        let mut client = UnixStream::connect(&sock_path).await.unwrap();
        let req = mesh_agent_ipc::encode_line(&TrayRequest::Restart).unwrap();
        client.write_all(&req).await.unwrap();

        // Verify RestartAck response
        let mut buf_reader = BufReader::new(client);
        let mut line = String::new();
        buf_reader.read_line(&mut line).await.unwrap();
        let resp: TrayResponse = serde_json::from_str(&line).unwrap();
        assert_eq!(resp, TrayResponse::RestartAck);

        // Verify action was dispatched
        let action = action_rx.recv().await.unwrap();
        assert!(matches!(action, IpcAction::Restart));
    }

    #[tokio::test]
    async fn test_ipc_invalid_json() {
        let dir = tempfile::tempdir().unwrap();
        let sock_path = dir.path().join("test.sock");

        let listener = UnixListener::bind(&sock_path).unwrap();
        let (_state_tx, state_rx) = watch::channel(test_state());
        let (event_tx, _) = broadcast::channel::<TrayEvent>(32);
        let (action_tx, _action_rx) = tokio::sync::mpsc::channel::<IpcAction>(16);

        tokio::spawn(async move {
            let (stream, _) = listener.accept().await.unwrap();
            let evt_rx = event_tx.subscribe();
            handle_client(stream, state_rx, evt_rx, action_tx)
                .await
                .ok();
        });

        let mut client = UnixStream::connect(&sock_path).await.unwrap();
        client.write_all(b"garbage\n").await.unwrap();

        let mut buf_reader = BufReader::new(client);
        let mut line = String::new();
        buf_reader.read_line(&mut line).await.unwrap();
        let resp: TrayResponse = serde_json::from_str(&line).unwrap();
        match resp {
            TrayResponse::Error { message } => {
                assert!(message.contains("invalid request"));
            }
            other => panic!("expected Error, got {other:?}"),
        }
    }

    #[tokio::test]
    async fn test_ipc_get_logs() {
        let dir = tempfile::tempdir().unwrap();
        let log_file = dir.path().join("test.log");
        tokio::fs::write(
            &log_file,
            "line1\nline2\nline3\nline4\nline5\n",
        )
        .await
        .unwrap();

        let sock_path = dir.path().join("test.sock");
        let listener = UnixListener::bind(&sock_path).unwrap();

        let mut state = test_state();
        state.log_path = log_file.to_string_lossy().to_string();
        let (_state_tx, state_rx) = watch::channel(state);
        let (event_tx, _) = broadcast::channel::<TrayEvent>(32);
        let (action_tx, _action_rx) = tokio::sync::mpsc::channel::<IpcAction>(16);

        tokio::spawn(async move {
            let (stream, _) = listener.accept().await.unwrap();
            let evt_rx = event_tx.subscribe();
            handle_client(stream, state_rx, evt_rx, action_tx)
                .await
                .ok();
        });

        let mut client = UnixStream::connect(&sock_path).await.unwrap();
        let req = mesh_agent_ipc::encode_line(&TrayRequest::GetLogs { lines: 3 }).unwrap();
        client.write_all(&req).await.unwrap();

        let mut buf_reader = BufReader::new(client);
        let mut line = String::new();
        buf_reader.read_line(&mut line).await.unwrap();
        let resp: TrayResponse = serde_json::from_str(&line).unwrap();
        match resp {
            TrayResponse::Logs { lines } => {
                assert_eq!(lines.len(), 3);
                assert_eq!(lines[0], "line3");
                assert_eq!(lines[1], "line4");
                assert_eq!(lines[2], "line5");
            }
            other => panic!("expected Logs, got {other:?}"),
        }
    }

    #[tokio::test]
    async fn test_ipc_push_event() {
        let dir = tempfile::tempdir().unwrap();
        let sock_path = dir.path().join("test.sock");

        let listener = UnixListener::bind(&sock_path).unwrap();
        let (_state_tx, state_rx) = watch::channel(test_state());
        let (event_tx, _) = broadcast::channel::<TrayEvent>(32);
        let (action_tx, _action_rx) = tokio::sync::mpsc::channel::<IpcAction>(16);

        let event_tx_clone = event_tx.clone();
        tokio::spawn(async move {
            let (stream, _) = listener.accept().await.unwrap();
            let evt_rx = event_tx_clone.subscribe();
            handle_client(stream, state_rx, evt_rx, action_tx)
                .await
                .ok();
        });

        let client = UnixStream::connect(&sock_path).await.unwrap();
        // Give the handler a moment to set up
        tokio::time::sleep(std::time::Duration::from_millis(50)).await;

        // Send a push event
        let _ = event_tx.send(TrayEvent::ConnectionChanged { connected: false });

        let mut buf_reader = BufReader::new(client);
        let mut line = String::new();
        buf_reader.read_line(&mut line).await.unwrap();
        let evt: TrayEvent = serde_json::from_str(&line).unwrap();
        assert_eq!(
            evt,
            TrayEvent::ConnectionChanged { connected: false }
        );
    }

    #[tokio::test]
    async fn test_read_log_tail_fewer_lines_than_requested() {
        let dir = tempfile::tempdir().unwrap();
        let log_file = dir.path().join("test.log");
        tokio::fs::write(&log_file, "only\ntwo\n").await.unwrap();

        let lines = read_log_tail(&log_file.to_string_lossy(), 100)
            .await
            .unwrap();
        assert_eq!(lines.len(), 2);
    }

    #[tokio::test]
    async fn test_read_log_tail_missing_file() {
        let result = read_log_tail("/nonexistent/file.log", 10).await;
        assert!(result.is_err());
    }
}
