//! One remote session on the host: the relay, the direct link when it forms, and the features.

mod core;
mod handler;
pub mod handlers;
pub mod outbox;
pub(crate) mod outlet;
pub(crate) mod relay;
pub(crate) mod terminal_handle;

use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;

use futures_util::StreamExt;
use mesh_protocol::{Permissions, SessionToken};
use tokio::sync::mpsc;
use tracing::{info, warn};

pub use terminal_handle::TerminalHandle;

use crate::file_ops::FileOpsHandler;
use crate::platform::{InputInjector, ScreenCapture};
use crate::session_error::SessionError;
use crate::terminal::TerminalSession;
use handler::{dispatch, Features};
use handlers::FileTasks;
use outbox::Outbox;
use outlet::Outlet;
use relay::{build_relay_url, capture_loop, read_relay, write_relay};

/// Frames read from the relay that may wait for the core.
const RELAY_IN_DEPTH: usize = 64;

/// Manages one session between the host and a browser.
pub struct SessionHandler {
    token: SessionToken,
    pub(crate) permissions: Permissions,
    pub(crate) helpers: Vec<String>,
}

impl SessionHandler {
    /// Creates a handler for the session `token` with the granted `permissions`.
    pub fn new(token: SessionToken, permissions: Permissions) -> Self {
        Self {
            token,
            permissions,
            helpers: Vec::new(),
        }
    }

    /// Sets the helpers the host asks for its public address, as the server named them.
    pub fn with_helpers(mut self, helpers: Vec<String>) -> Self {
        self.helpers = helpers;
        self
    }

    /// Runs the session until the relay closes, then waits until its link, its file tasks and
    /// its writers are gone.
    pub async fn run(
        self,
        relay_url: &str,
        mut capture: Box<dyn ScreenCapture>,
        injector: Box<dyn InputInjector>,
    ) -> Result<(), SessionError> {
        let url = build_relay_url(relay_url)?;
        info!(token = %self.token.redacted(), "connecting to relay");
        let (ws, gauge) = relay::connect(&url).await?;
        let (ws_tx, ws_rx) = ws.split();
        info!(token = %self.token.redacted(), "connected to relay");

        let (outbox, lanes) = Outbox::new();
        let (relay_out, relay_writer) = Outlet::new();
        let writer = tokio::spawn(write_relay(ws_tx, relay_writer, gauge));
        let (relay_in_tx, relay_in) = mpsc::channel(RELAY_IN_DEPTH);
        let reader = tokio::spawn(read_relay(ws_rx, relay_in_tx));
        let running = Arc::new(AtomicBool::new(true));

        let capture_task = self.permissions.desktop.then(|| {
            let (outbox, running) = (outbox.clone(), running.clone());
            tokio::spawn(async move { capture_loop(&mut *capture, outbox, running).await })
        });

        let terminal = if self.permissions.terminal {
            match TerminalSession::spawn(80, 24) {
                Ok(term) => Some(term.run(outbox.clone(), running.clone()).await?),
                Err(e) => {
                    warn!("failed to spawn terminal: {e}");
                    None
                }
            }
        } else {
            None
        };

        let (to_features, from_core) = Outlet::new();
        let features = tokio::spawn(dispatch(
            Features {
                permissions: self.permissions.clone(),
                injector,
                file_ops: FileOpsHandler::new(
                    self.permissions.file_read,
                    self.permissions.file_write,
                ),
                outbox,
                terminal,
                files: FileTasks::default(),
            },
            from_core,
        ));

        core::Core::new(self.helpers, relay_out, to_features, lanes)
            .run(relay_in)
            .await;

        info!(token = %self.token.redacted(), "session ended");
        running.store(false, Ordering::Relaxed);
        if let Some(task) = capture_task {
            task.abort();
            task.await.ok();
        }
        features.await.ok();
        writer.await.ok();
        reader.abort();
        reader.await.ok();
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn permissions() -> Permissions {
        Permissions {
            desktop: true,
            terminal: false,
            file_read: true,
            file_write: false,
            input: true,
        }
    }

    #[test]
    fn new_keeps_the_token_and_permissions_and_starts_without_helpers() {
        let token = SessionToken::generate();
        let handler = SessionHandler::new(token.clone(), permissions());

        assert_eq!(handler.token, token);
        assert!(handler.permissions.desktop);
        assert!(!handler.permissions.terminal);
        assert!(handler.helpers.is_empty());
    }

    #[test]
    fn with_helpers_replaces_the_list() {
        let handler = SessionHandler::new(SessionToken::generate(), permissions())
            .with_helpers(vec!["stun:quic.example.com:9090".to_string()]);
        assert_eq!(handler.helpers, ["stun:quic.example.com:9090"]);
        assert!(handler.with_helpers(Vec::new()).helpers.is_empty());
    }
}
