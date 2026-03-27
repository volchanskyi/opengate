//! OpenGate system tray agent.
//!
//! Connects to the `mesh-agent` service via Unix domain socket IPC
//! and provides a system tray icon with context menu for agent management.

mod ipc;
mod logs;
mod menu;
mod notifications;
mod tray;

use mesh_agent_ipc::{TrayEvent, TrayRequest, TrayResponse, UpdateState};
use tracing::{info, warn};

fn main() {
    tracing_subscriber::fmt()
        .with_env_filter(
            tracing_subscriber::EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| tracing_subscriber::EnvFilter::new("info")),
        )
        .init();

    info!(version = env!("TRAY_VERSION"), "mesh-agent-tray starting");

    // Build the context menu.
    let (muda_menu, menu_ids) = menu::build_menu();

    // Create the tray icon (must happen on the main thread for platform compat).
    let tray_icon = tray::create_tray_icon(
        &muda_menu,
        tray::IconState::Unavailable,
        env!("TRAY_VERSION"),
    );

    // Spawn the tokio runtime on a background thread for async IPC.
    let (menu_action_tx, menu_action_rx) = std::sync::mpsc::channel::<menu::MenuAction>();
    let (icon_state_tx, icon_state_rx) = std::sync::mpsc::channel::<(tray::IconState, String)>();

    let _rt_handle = std::thread::spawn(move || {
        let rt = tokio::runtime::Builder::new_current_thread()
            .enable_all()
            .build()
            .expect("failed to create tokio runtime");

        rt.block_on(async_main(menu_action_rx, icon_state_tx));
    });

    // Main thread: run the platform event loop for tray-icon + muda.
    let menu_channel = muda::MenuEvent::receiver();

    loop {
        // Process pending icon state updates (non-blocking).
        while let Ok((state, ver)) = icon_state_rx.try_recv() {
            tray::update_icon(&tray_icon, state, &ver);
        }

        // Process menu events (non-blocking).
        if let Ok(event) = menu_channel.try_recv() {
            if let Some(action) = menu::resolve_action(&event, &menu_ids) {
                if action == menu::MenuAction::Quit {
                    info!("quit requested, exiting");
                    break;
                }
                let _ = menu_action_tx.send(action);
            }
        }

        // Sleep briefly to avoid busy-waiting. Platform event loops typically
        // use native waits, but tray-icon's model requires polling.
        std::thread::sleep(std::time::Duration::from_millis(50));
    }

    drop(tray_icon);
}

/// Async main loop running on the tokio runtime thread.
/// Handles IPC communication and dispatches menu actions.
async fn async_main(
    menu_action_rx: std::sync::mpsc::Receiver<menu::MenuAction>,
    icon_state_tx: std::sync::mpsc::Sender<(tray::IconState, String)>,
) {
    let client = ipc::IpcClient::new();
    let (req_tx, mut msg_rx) = client.spawn();

    let version = env!("TRAY_VERSION").to_string();
    let mut agent_connected = false;
    let mut agent_log_path = String::from("/var/log/mesh-agent");

    // Poll for menu actions (non-blocking, converted to async).
    let mut menu_poll = tokio::time::interval(std::time::Duration::from_millis(50));

    loop {
        tokio::select! {
            _ = menu_poll.tick() => {
                // Check for menu actions from the main thread.
                while let Ok(action) = menu_action_rx.try_recv() {
                    handle_menu_action(action, &req_tx, &agent_log_path, agent_connected).await;
                }
            }
            msg = msg_rx.recv() => {
                let msg = match msg {
                    Some(m) => m,
                    None => break, // channel closed
                };

                match msg {
                    ipc::IpcMessage::Connected => {
                        info!("IPC connected to agent");
                        // Request status immediately.
                        let _ = req_tx.send(TrayRequest::Status).await;
                    }
                    ipc::IpcMessage::Disconnected => {
                        info!("IPC disconnected from agent");
                        agent_connected = false;
                        let _ = icon_state_tx.send((tray::IconState::Unavailable, version.clone()));
                    }
                    ipc::IpcMessage::Response(resp) => {
                        handle_response(
                            resp,
                            &mut agent_connected,
                            &mut agent_log_path,
                            &icon_state_tx,
                            &version,
                        );
                    }
                    ipc::IpcMessage::Event(evt) => {
                        handle_event(
                            evt,
                            &mut agent_connected,
                            &icon_state_tx,
                            &version,
                        );
                    }
                }
            }
        }
    }
}

/// Handle a menu action by sending the appropriate IPC request.
async fn handle_menu_action(
    action: menu::MenuAction,
    req_tx: &tokio::sync::mpsc::Sender<TrayRequest>,
    log_path: &str,
    connected: bool,
) {
    match action {
        menu::MenuAction::Restart => {
            if !connected {
                notifications::notify("OpenGate Agent", "Agent is not running");
                return;
            }
            info!("restart requested");
            let _ = req_tx.send(TrayRequest::Restart).await;
        }
        menu::MenuAction::CheckUpdate => {
            if !connected {
                notifications::notify("OpenGate Agent", "Agent is not running");
                return;
            }
            info!("update check requested");
            let _ = req_tx.send(TrayRequest::CheckUpdate).await;
        }
        menu::MenuAction::OpenChat => {
            if !connected {
                notifications::notify("OpenGate Agent", "Agent is not connected to server");
                return;
            }
            info!("chat requested");
            let _ = req_tx.send(TrayRequest::RequestChatToken).await;
        }
        menu::MenuAction::OpenLogFile => {
            logs::open_log_file(log_path);
        }
        menu::MenuAction::TailLiveLogs => {
            logs::tail_live_logs(log_path);
        }
        menu::MenuAction::Journalctl => {
            logs::open_journalctl();
        }
        menu::MenuAction::BuildInfo => {
            info!("build info requested");
            let _ = req_tx.send(TrayRequest::GetInfo).await;
        }
        menu::MenuAction::Quit => {
            // Handled in the main thread event loop.
        }
    }
}

/// Handle an IPC response from the agent.
fn handle_response(
    resp: TrayResponse,
    agent_connected: &mut bool,
    agent_log_path: &mut String,
    icon_state_tx: &std::sync::mpsc::Sender<(tray::IconState, String)>,
    _version: &str,
) {
    match resp {
        TrayResponse::Status {
            connected,
            version: agent_ver,
            ..
        } => {
            *agent_connected = connected;
            let state = if connected {
                tray::IconState::Connected
            } else {
                tray::IconState::Reconnecting
            };
            let _ = icon_state_tx.send((state, agent_ver));
        }
        TrayResponse::Info { ref log_path, connected, .. } => {
            *agent_connected = connected;
            *agent_log_path = log_path.clone();
            notifications::show_build_info(&resp);
        }
        TrayResponse::RestartAck => {
            notifications::notify("OpenGate Agent", "Restarting agent...");
        }
        TrayResponse::UpdateStatus { status, version: ver } => {
            match status {
                UpdateState::NoUpdate => {
                    notifications::notify(
                        "OpenGate Agent",
                        &format!("Already on latest version ({ver})"),
                    );
                }
                UpdateState::Applied => {
                    notifications::notify(
                        "OpenGate Agent",
                        &format!("Updated to v{ver}, restarting..."),
                    );
                }
                UpdateState::Failed => {
                    notifications::notify("OpenGate Agent", "Update failed");
                }
                UpdateState::Checking => {
                    notifications::notify("OpenGate Agent", "Checking for updates...");
                }
                UpdateState::Downloading => {
                    let _ = icon_state_tx.send((tray::IconState::Updating, ver));
                }
                _ => {}
            }
        }
        TrayResponse::ChatToken { url, token, .. } => {
            let full_url = if url.contains('?') {
                format!("{url}&token={token}")
            } else {
                format!("{url}?token={token}")
            };
            info!(url = %full_url, "opening chat in browser");
            if let Err(e) = open::that(&full_url) {
                warn!(error = %e, "failed to open chat URL");
                notifications::notify("Error", &format!("Cannot open chat: {e}"));
            }
        }
        TrayResponse::Logs { .. } => {
            // Log lines could be displayed in a window; for now we use file-based viewing.
        }
        TrayResponse::Error { message } => {
            warn!(message = %message, "agent returned error");
            notifications::notify("OpenGate Agent", &message);
        }
        _ => {}
    }
}

/// Handle a push event from the agent.
fn handle_event(
    evt: TrayEvent,
    agent_connected: &mut bool,
    icon_state_tx: &std::sync::mpsc::Sender<(tray::IconState, String)>,
    version: &str,
) {
    match evt {
        TrayEvent::ConnectionChanged { connected } => {
            *agent_connected = connected;
            let state = if connected {
                tray::IconState::Connected
            } else {
                tray::IconState::Reconnecting
            };
            let _ = icon_state_tx.send((state, version.to_string()));

            if connected {
                notifications::notify("OpenGate Agent", "Connected to server");
            } else {
                notifications::notify("OpenGate Agent", "Disconnected from server");
            }
        }
        TrayEvent::UpdateProgress { percent: _, version: ver } => {
            let _ = icon_state_tx.send((tray::IconState::Updating, ver));
        }
        _ => {}
    }
}
