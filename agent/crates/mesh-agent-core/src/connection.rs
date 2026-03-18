use std::time::Duration;

use mesh_protocol::{
    codec::{self, FRAME_CONTROL},
    ControlMessage, Frame,
};
use tokio::io::{AsyncRead, AsyncReadExt, AsyncWrite, AsyncWriteExt};
use tracing::{debug, info, warn};

use crate::config::AgentConfig;
use crate::error::ConnectionError;
use crate::platform::{InputInjector, ScreenCapture};
use crate::session::SessionHandler;

/// Trait abstracting the control stream for testability.
pub trait ControlStream: Send {
    /// Send raw bytes to the stream.
    fn write_all(
        &mut self,
        buf: &[u8],
    ) -> impl std::future::Future<Output = Result<(), std::io::Error>> + Send;

    /// Read exactly `n` bytes from the stream.
    fn read_exact(
        &mut self,
        buf: &mut [u8],
    ) -> impl std::future::Future<Output = Result<(), std::io::Error>> + Send;

    /// Read available bytes into the buffer, returning number of bytes read.
    fn read(
        &mut self,
        buf: &mut [u8],
    ) -> impl std::future::Future<Output = Result<usize, std::io::Error>> + Send;
}

/// Implementation of ControlStream for any AsyncRead + AsyncWrite.
pub struct AsyncControlStream<S> {
    stream: S,
}

impl<S> AsyncControlStream<S> {
    /// Create a new AsyncControlStream wrapping the given stream.
    pub fn new(stream: S) -> Self {
        Self { stream }
    }
}

impl<S: AsyncRead + AsyncWrite + Unpin + Send> ControlStream for AsyncControlStream<S> {
    async fn write_all(&mut self, buf: &[u8]) -> Result<(), std::io::Error> {
        self.stream.write_all(buf).await?;
        self.stream.flush().await
    }

    async fn read_exact(&mut self, buf: &mut [u8]) -> Result<(), std::io::Error> {
        self.stream.read_exact(buf).await?;
        Ok(())
    }

    async fn read(&mut self, buf: &mut [u8]) -> Result<usize, std::io::Error> {
        self.stream.read(buf).await
    }
}

/// An established connection to the server with control stream framing.
pub struct AgentConnection<S: ControlStream> {
    stream: S,
    #[allow(dead_code)] // Used in QUIC connect flow (Phase 4D)
    config: AgentConfig,
}

impl<S: ControlStream> AgentConnection<S> {
    /// Create a new AgentConnection with the given stream and config.
    pub fn new(stream: S, config: AgentConfig) -> Self {
        Self { stream, config }
    }

    /// Send a control message to the server.
    pub async fn send_control(&mut self, msg: ControlMessage) -> Result<(), ConnectionError> {
        let frame = Frame::Control(msg);
        let encoded = frame.encode()?;
        self.stream.write_all(&encoded).await?;
        Ok(())
    }

    /// Receive the next control message from the server.
    pub async fn receive_control(&mut self) -> Result<ControlMessage, ConnectionError> {
        // Read type byte
        let mut type_buf = [0u8; 1];
        self.stream.read_exact(&mut type_buf).await?;

        let frame_type = type_buf[0];

        if frame_type == codec::FRAME_PING {
            // Respond with pong
            self.stream.write_all(&[codec::FRAME_PONG]).await?;
            return Err(ConnectionError::Io(std::io::Error::other(
                "ping received, pong sent",
            )));
        }

        if frame_type != FRAME_CONTROL {
            return Err(ConnectionError::Protocol(
                mesh_protocol::ProtocolError::UnknownFrameType(frame_type),
            ));
        }

        // Read 4-byte big-endian length
        let mut len_buf = [0u8; 4];
        self.stream.read_exact(&mut len_buf).await?;
        let payload_len = u32::from_be_bytes(len_buf) as usize;

        if payload_len > codec::MAX_FRAME_SIZE {
            return Err(ConnectionError::Protocol(
                mesh_protocol::ProtocolError::FrameTooLarge {
                    size: payload_len,
                    max: codec::MAX_FRAME_SIZE,
                },
            ));
        }

        // Read payload
        let mut payload = vec![0u8; payload_len];
        self.stream.read_exact(&mut payload).await?;

        // Decode control message
        let msg: ControlMessage = rmp_serde::from_slice(&payload).map_err(|e| {
            ConnectionError::Protocol(mesh_protocol::ProtocolError::MsgpackDecode(e))
        })?;

        Ok(msg)
    }

    /// Handle a SessionRequest by accepting and spawning a session task.
    ///
    /// Sends `SessionAccept` back on the control stream and spawns a
    /// `SessionHandler` on a new tokio task that connects to the relay.
    pub async fn handle_session_request(
        &mut self,
        token: mesh_protocol::SessionToken,
        relay_url: String,
        permissions: mesh_protocol::Permissions,
        capture: Box<dyn ScreenCapture>,
        injector: Box<dyn InputInjector>,
    ) -> Result<tokio::task::JoinHandle<()>, ConnectionError> {
        // Send acceptance back to server
        self.send_control(ControlMessage::SessionAccept {
            token: token.clone(),
            relay_url: relay_url.clone(),
        })
        .await?;

        info!(token = %token.as_str(), "accepted session, connecting to relay");

        // Spawn the session handler on a separate task
        let handler = SessionHandler::new(token, permissions);
        let handle = tokio::spawn(async move {
            if let Err(e) = handler.run(&relay_url, capture, injector).await {
                warn!("session ended with error: {e}");
            }
        });

        Ok(handle)
    }
}

/// Reconnect with exponential backoff.
/// Delays: 1s, 2s, 4s, 8s, 16s, 30s (capped).
pub async fn reconnect_with_backoff<F, Fut, T, E>(
    mut connect_fn: F,
    max_attempts: u32,
) -> Result<T, ConnectionError>
where
    F: FnMut() -> Fut,
    Fut: std::future::Future<Output = Result<T, E>>,
    E: std::fmt::Display,
{
    let mut delay = Duration::from_secs(1);
    let max_delay = Duration::from_secs(30);

    for attempt in 1..=max_attempts {
        match connect_fn().await {
            Ok(val) => {
                info!(attempt, "reconnected successfully");
                return Ok(val);
            }
            Err(e) => {
                warn!(attempt, max_attempts, error = %e, "connection attempt failed");
                if attempt < max_attempts {
                    debug!(delay_ms = delay.as_millis(), "waiting before retry");
                    tokio::time::sleep(delay).await;
                    delay = (delay * 2).min(max_delay);
                }
            }
        }
    }

    Err(ConnectionError::Quic(format!(
        "failed after {max_attempts} attempts"
    )))
}

#[cfg(test)]
mod tests {
    use super::*;
    use mesh_protocol::{codec::FRAME_CONTROL, ControlMessage, Frame, SessionToken};
    use std::sync::atomic::{AtomicU32, Ordering};
    use std::sync::Arc;

    /// Test helper: wraps a tokio DuplexStream as a ControlStream.
    impl ControlStream for tokio::io::DuplexStream {
        async fn write_all(&mut self, buf: &[u8]) -> Result<(), std::io::Error> {
            AsyncWriteExt::write_all(self, buf).await
        }

        async fn read_exact(&mut self, buf: &mut [u8]) -> Result<(), std::io::Error> {
            AsyncReadExt::read_exact(self, buf).await?;
            Ok(())
        }

        async fn read(&mut self, buf: &mut [u8]) -> Result<usize, std::io::Error> {
            AsyncReadExt::read(self, buf).await
        }
    }

    fn test_config() -> AgentConfig {
        AgentConfig {
            server_addr: "127.0.0.1:9090".to_string(),
            server_ca_pem: String::new(),
            data_dir: std::path::PathBuf::from("/tmp/test"),
        }
    }

    #[tokio::test]
    async fn test_send_control_encodes_agent_register() {
        let (client, mut server) = tokio::io::duplex(4096);
        let mut conn = AgentConnection::new(client, test_config());

        let msg = ControlMessage::AgentRegister {
            capabilities: vec![mesh_protocol::AgentCapability::Terminal],
            hostname: "test-host".to_string(),
            os: "linux".to_string(),
            arch: "amd64".to_string(),
            version: "0.1.0".to_string(),
        };

        // Send in background
        let send_handle = tokio::spawn(async move {
            conn.send_control(msg).await.unwrap();
        });

        // Read on the server side
        let mut type_buf = [0u8; 1];
        AsyncReadExt::read_exact(&mut server, &mut type_buf)
            .await
            .unwrap();
        assert_eq!(type_buf[0], FRAME_CONTROL);

        let mut len_buf = [0u8; 4];
        AsyncReadExt::read_exact(&mut server, &mut len_buf)
            .await
            .unwrap();
        let payload_len = u32::from_be_bytes(len_buf) as usize;
        assert!(payload_len > 0);

        let mut payload = vec![0u8; payload_len];
        AsyncReadExt::read_exact(&mut server, &mut payload)
            .await
            .unwrap();

        // Decode and verify
        let decoded: ControlMessage = rmp_serde::from_slice(&payload).unwrap();
        match decoded {
            ControlMessage::AgentRegister {
                hostname,
                os,
                capabilities,
                ..
            } => {
                assert_eq!(hostname, "test-host");
                assert_eq!(os, "linux");
                assert_eq!(capabilities.len(), 1);
            }
            _ => panic!("expected AgentRegister"),
        }

        send_handle.await.unwrap();
    }

    #[tokio::test]
    async fn test_receive_control_decodes_session_request() {
        let (client, mut server) = tokio::io::duplex(4096);
        let mut conn = AgentConnection::new(client, test_config());

        let token = SessionToken::generate();
        let msg = ControlMessage::SessionRequest {
            token: token.clone(),
            relay_url: "wss://relay/test".to_string(),
            permissions: mesh_protocol::Permissions {
                desktop: true,
                terminal: false,
                file_read: true,
                file_write: false,
                input: false,
            },
        };

        // Encode and write the frame on the server side
        let frame = Frame::Control(msg);
        let encoded = frame.encode().unwrap();

        tokio::spawn(async move {
            AsyncWriteExt::write_all(&mut server, &encoded)
                .await
                .unwrap();
        });

        // Receive on the client side
        let received = conn.receive_control().await.unwrap();
        match received {
            ControlMessage::SessionRequest {
                token: t,
                relay_url,
                permissions,
            } => {
                assert_eq!(t.as_str(), token.as_str());
                assert_eq!(relay_url, "wss://relay/test");
                assert!(permissions.desktop);
                assert!(!permissions.terminal);
            }
            _ => panic!("expected SessionRequest"),
        }
    }

    #[tokio::test]
    async fn test_reconnect_backoff_succeeds_after_failures() {
        let attempt_count = Arc::new(AtomicU32::new(0));
        let count = attempt_count.clone();

        let result = reconnect_with_backoff(
            move || {
                let count = count.clone();
                async move {
                    let n = count.fetch_add(1, Ordering::SeqCst);
                    if n < 2 {
                        Err::<u32, String>(format!("fail {n}"))
                    } else {
                        Ok(42u32)
                    }
                }
            },
            5,
        )
        .await;

        assert!(result.is_ok());
        assert_eq!(result.unwrap(), 42);
        assert_eq!(attempt_count.load(Ordering::SeqCst), 3); // failed twice, succeeded on 3rd
    }

    #[tokio::test]
    async fn test_reconnect_backoff_all_failures() {
        let result: Result<u32, _> = reconnect_with_backoff(
            || async { Err::<u32, String>("always fail".to_string()) },
            3,
        )
        .await;

        assert!(result.is_err());
        let err = result.unwrap_err();
        assert!(err.to_string().contains("failed after 3 attempts"));
    }
}
