use std::time::Duration;

use mesh_protocol::{
    codec::{self, FRAME_CONTROL},
    ControlMessage, Frame,
};
use rand::{Rng, RngExt};
use tokio::io::{AsyncRead, AsyncReadExt, AsyncWrite, AsyncWriteExt};
use tracing::{debug, info, warn};

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
}

impl<S: ControlStream> AgentConnection<S> {
    /// Create a new AgentConnection with the given stream.
    pub fn new(stream: S) -> Self {
        Self { stream }
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
        let mut type_buf = [0u8; 1];
        self.stream.read_exact(&mut type_buf).await?;

        let frame_type = type_buf[0];

        if frame_type == codec::FRAME_PING {
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

        let mut payload = vec![0u8; payload_len];
        self.stream.read_exact(&mut payload).await?;

        let msg: ControlMessage = rmp_serde::from_slice(&payload).map_err(|e| {
            ConnectionError::Protocol(mesh_protocol::ProtocolError::MsgpackDecode(e))
        })?;

        Ok(msg)
    }

    /// Accepts a SessionRequest and spawns a `SessionHandler` task that connects to the relay.
    pub async fn handle_session_request(
        &mut self,
        token: mesh_protocol::SessionToken,
        relay_url: String,
        permissions: mesh_protocol::Permissions,
        capture: Box<dyn ScreenCapture>,
        injector: Box<dyn InputInjector>,
    ) -> Result<tokio::task::JoinHandle<()>, ConnectionError> {
        self.send_control(ControlMessage::SessionAccept {
            token: token.clone(),
            relay_url: relay_url.clone(),
        })
        .await?;

        info!(token = %token.redacted(), "accepted session, connecting to relay");

        let handler = SessionHandler::new(token, permissions);
        let handle = tokio::spawn(async move {
            if let Err(e) = handler.run(&relay_url, capture, injector).await {
                warn!("session ended with error: {e}");
            }
        });

        Ok(handle)
    }
}

const BACKOFF_BASE: Duration = Duration::from_secs(1);
const BACKOFF_CAP: Duration = Duration::from_secs(30);
/// Cap on the doubling exponent so `1u32 << exp` cannot overflow.
const MAX_BACKOFF_SHIFT: u32 = 16;

/// Full-jitter backoff: a uniform-random delay in `[0, min(cap, base · 2^exp)]` from the given RNG.
pub fn full_jitter<R: Rng + ?Sized>(
    base: Duration,
    cap: Duration,
    exp: u32,
    rng: &mut R,
) -> Duration {
    let ceiling = base
        .saturating_mul(1u32 << exp.min(MAX_BACKOFF_SHIFT))
        .min(cap);
    let ceiling_ms = ceiling.as_millis() as u64;
    Duration::from_millis(rng.random_range(0..=ceiling_ms))
}

/// Escalates a jittered backoff across consecutive short sessions and resets on a stable one.
pub struct ReconnectGovernor {
    flap_count: u32,
    base: Duration,
    cap: Duration,
    stability_window: Duration,
}

impl ReconnectGovernor {
    /// Base delay applied to the first flap.
    pub const DEFAULT_BASE: Duration = BACKOFF_BASE;
    /// Upper bound on any single flap backoff.
    pub const DEFAULT_CAP: Duration = BACKOFF_CAP;
    /// A session registered for at least this long counts as stable; a shorter one is a flap.
    pub const DEFAULT_STABILITY_WINDOW: Duration = Duration::from_secs(5);

    /// Create a governor with the default schedule.
    pub fn new() -> Self {
        Self {
            flap_count: 0,
            base: Self::DEFAULT_BASE,
            cap: Self::DEFAULT_CAP,
            stability_window: Self::DEFAULT_STABILITY_WINDOW,
        }
    }

    /// Records a session end; returns an escalating backoff after a flap, or `None` after a stable one.
    pub fn record_disconnect<R: Rng + ?Sized>(
        &mut self,
        session_duration: Duration,
        rng: &mut R,
    ) -> Option<Duration> {
        if session_duration >= self.stability_window {
            self.flap_count = 0;
            return None;
        }
        self.flap_count = self.flap_count.saturating_add(1);
        Some(full_jitter(self.base, self.cap, self.flap_count - 1, rng))
    }

    /// Number of consecutive short sessions seen since the last stable one.
    pub fn flap_count(&self) -> u32 {
        self.flap_count
    }
}

impl Default for ReconnectGovernor {
    fn default() -> Self {
        Self::new()
    }
}

/// Retries `connect_fn` up to `max_attempts` times with full-jitter backoff between attempts.
pub async fn reconnect_with_backoff<F, Fut, T, E>(
    mut connect_fn: F,
    max_attempts: u32,
) -> Result<T, ConnectionError>
where
    F: FnMut() -> Fut,
    Fut: std::future::Future<Output = Result<T, E>>,
    E: std::fmt::Display,
{
    let mut rng = rand::rng();

    for attempt in 1..=max_attempts {
        match connect_fn().await {
            Ok(val) => {
                info!(attempt, "reconnected successfully");
                return Ok(val);
            }
            Err(e) => {
                warn!(attempt, max_attempts, error = %e, "connection attempt failed");
                if attempt < max_attempts {
                    let delay = full_jitter(BACKOFF_BASE, BACKOFF_CAP, attempt - 1, &mut rng);
                    debug!(delay_ms = delay.as_millis(), "waiting before retry");
                    tokio::time::sleep(delay).await;
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
    use rand::{rngs::StdRng, SeedableRng};
    use std::sync::atomic::{AtomicU32, Ordering};
    use std::sync::Arc;

    #[test]
    fn full_jitter_stays_within_bounds() {
        let base = Duration::from_secs(1);
        let cap = Duration::from_secs(30);
        let mut rng = StdRng::seed_from_u64(7);
        for exp in 0..8u32 {
            let ceiling = base.saturating_mul(1u32 << exp).min(cap);
            let mut max_seen = Duration::ZERO;
            for _ in 0..1000 {
                let d = full_jitter(base, cap, exp, &mut rng);
                assert!(
                    d <= ceiling,
                    "delay {d:?} exceeds ceiling {ceiling:?} at exp {exp}"
                );
                max_seen = max_seen.max(d);
            }
            assert!(
                max_seen * 2 > ceiling,
                "at exp {exp} the widest of 1000 draws was {max_seen:?}, \
                 which does not reach past the midpoint of the {ceiling:?} window"
            );
        }
    }

    #[test]
    fn full_jitter_respects_cap() {
        let base = Duration::from_secs(1);
        let cap = Duration::from_secs(30);
        let mut rng = StdRng::seed_from_u64(1);
        for _ in 0..1000 {
            let d = full_jitter(base, cap, 1_000_000, &mut rng);
            assert!(d <= cap, "delay {d:?} exceeds cap {cap:?}");
        }
    }

    #[test]
    fn governor_backs_off_short_sessions_and_resets_on_stable() {
        let mut g = ReconnectGovernor::new();
        let mut rng = StdRng::seed_from_u64(99);

        let short = Duration::from_millis(100);
        let d1 = g
            .record_disconnect(short, &mut rng)
            .expect("flap 1 backs off");
        assert_eq!(g.flap_count(), 1);
        assert!(d1 <= ReconnectGovernor::DEFAULT_CAP);

        let _d2 = g
            .record_disconnect(short, &mut rng)
            .expect("flap 2 backs off");
        assert_eq!(g.flap_count(), 2);

        assert!(
            g.record_disconnect(ReconnectGovernor::DEFAULT_STABILITY_WINDOW, &mut rng)
                .is_none(),
            "a session at the window boundary is stable"
        );
        assert_eq!(g.flap_count(), 0);
    }

    #[test]
    fn governor_rate_limits_accept_then_drop_storm() {
        let mut g = ReconnectGovernor::new();
        let mut rng = StdRng::seed_from_u64(2024);
        let drop_fast = Duration::from_millis(5);
        for cycle in 1..=50u32 {
            let d = g
                .record_disconnect(drop_fast, &mut rng)
                .expect("a sub-window session must back off");
            assert!(d <= ReconnectGovernor::DEFAULT_CAP);
            assert_eq!(g.flap_count(), cycle);
        }
        assert!(g
            .record_disconnect(Duration::from_secs(10), &mut rng)
            .is_none());
        assert_eq!(g.flap_count(), 0);
    }

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

    #[tokio::test]
    async fn test_send_control_encodes_agent_register() {
        let (client, mut server) = tokio::io::duplex(4096);
        let mut conn = AgentConnection::new(client);

        let msg = ControlMessage::AgentRegister {
            capabilities: vec![mesh_protocol::AgentCapability::Terminal],
            hostname: "test-host".to_string(),
            os: "linux".to_string(),
            arch: "amd64".to_string(),
            version: "0.1.0".to_string(),
        };

        let send_handle = tokio::spawn(async move {
            conn.send_control(msg).await.unwrap();
        });

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
        let mut conn = AgentConnection::new(client);

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

        let frame = Frame::Control(msg);
        let encoded = frame.encode().unwrap();

        tokio::spawn(async move {
            AsyncWriteExt::write_all(&mut server, &encoded)
                .await
                .unwrap();
        });

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

    #[tokio::test(start_paused = true)]
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
        assert_eq!(attempt_count.load(Ordering::SeqCst), 3);
    }

    #[tokio::test]
    async fn test_send_control_encodes_heartbeat() {
        let (client, mut server) = tokio::io::duplex(4096);
        let mut conn = AgentConnection::new(client);

        let msg = ControlMessage::AgentHeartbeat {
            timestamp: 1700000000,
        };

        let send_handle = tokio::spawn(async move {
            conn.send_control(msg).await.unwrap();
        });

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

        let decoded: ControlMessage = rmp_serde::from_slice(&payload).unwrap();
        match decoded {
            ControlMessage::AgentHeartbeat { timestamp } => {
                assert_eq!(timestamp, 1700000000);
            }
            _ => panic!("expected AgentHeartbeat"),
        }

        send_handle.await.unwrap();
    }

    #[tokio::test(start_paused = true)]
    async fn reconnect_backoff_does_not_sleep_after_last_attempt() {
        let start = tokio::time::Instant::now();
        let result: Result<u32, _> =
            reconnect_with_backoff(|| async { Err::<u32, String>("fail".to_string()) }, 1).await;
        let elapsed = start.elapsed();
        assert!(result.is_err());
        assert_eq!(
            elapsed,
            Duration::ZERO,
            "a single-attempt failure waited {elapsed:?} before returning"
        );
    }

    #[tokio::test]
    async fn async_control_stream_write_all_actually_writes() {
        let (client, mut server) = tokio::io::duplex(64);
        let mut acs = AsyncControlStream::new(client);
        let payload = b"hello-wire";
        ControlStream::write_all(&mut acs, payload).await.unwrap();
        drop(acs);

        let mut buf = Vec::new();
        AsyncReadExt::read_to_end(&mut server, &mut buf)
            .await
            .unwrap();
        assert_eq!(buf, payload, "bytes must reach the peer");
    }

    #[tokio::test]
    async fn async_control_stream_read_exact_actually_reads() {
        let (client, mut server) = tokio::io::duplex(64);
        let mut acs = AsyncControlStream::new(client);
        let writer = tokio::spawn(async move {
            AsyncWriteExt::write_all(&mut server, b"abcdef")
                .await
                .unwrap();
        });
        let mut buf = [0u8; 6];
        ControlStream::read_exact(&mut acs, &mut buf).await.unwrap();
        assert_eq!(&buf, b"abcdef");
        writer.await.unwrap();
    }

    #[tokio::test]
    async fn async_control_stream_read_returns_actual_byte_count() {
        let (client, mut server) = tokio::io::duplex(64);
        let mut acs = AsyncControlStream::new(client);
        let writer = tokio::spawn(async move {
            AsyncWriteExt::write_all(&mut server, b"three")
                .await
                .unwrap();
        });
        let mut buf = [0u8; 16];
        let n = ControlStream::read(&mut acs, &mut buf).await.unwrap();
        assert_eq!(n, 5, "read must report exactly 5 bytes for 'three'");
        assert_eq!(&buf[..n], b"three");
        writer.await.unwrap();
    }

    #[tokio::test]
    async fn receive_control_rejects_payload_above_max_frame_size() {
        let (client, mut server) = tokio::io::duplex(8192);
        let mut conn = AgentConnection::new(client);

        let too_big = (codec::MAX_FRAME_SIZE as u32) + 1;
        tokio::spawn(async move {
            AsyncWriteExt::write_all(&mut server, &[FRAME_CONTROL])
                .await
                .unwrap();
            AsyncWriteExt::write_all(&mut server, &too_big.to_be_bytes())
                .await
                .unwrap();
        });

        match conn.receive_control().await {
            Err(ConnectionError::Protocol(mesh_protocol::ProtocolError::FrameTooLarge {
                ..
            })) => {}
            other => panic!("expected FrameTooLarge, got {:?}", other),
        }
    }

    #[tokio::test]
    async fn receive_control_decodes_unknown_control_as_catch_all() {
        let (client, mut server) = tokio::io::duplex(4096);
        let mut conn = AgentConnection::new(client);
        let payload = rmp_serde::to_vec_named(&std::collections::BTreeMap::from([(
            "type",
            "FutureServerControl",
        )]))
        .unwrap();
        let payload_len = (payload.len() as u32).to_be_bytes();

        tokio::spawn(async move {
            AsyncWriteExt::write_all(&mut server, &[FRAME_CONTROL])
                .await
                .unwrap();
            AsyncWriteExt::write_all(&mut server, &payload_len)
                .await
                .unwrap();
            AsyncWriteExt::write_all(&mut server, &payload)
                .await
                .unwrap();
        });

        let msg = conn.receive_control().await.unwrap();
        assert_eq!(msg, ControlMessage::Unknown);
    }

    #[tokio::test(start_paused = true)]
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

    #[test]
    fn a_first_flap_is_drawn_against_the_base_delay() {
        let mut rng = StdRng::seed_from_u64(4242);
        let short = Duration::from_millis(50);
        let mut widest = Duration::ZERO;
        for _ in 0..500 {
            let mut g = ReconnectGovernor::new();
            let d = g
                .record_disconnect(short, &mut rng)
                .expect("a sub-window session must back off");
            assert!(
                d <= ReconnectGovernor::DEFAULT_BASE,
                "a first flap waited {d:?}, past the {:?} base",
                ReconnectGovernor::DEFAULT_BASE
            );
            widest = widest.max(d);
        }
        assert!(
            widest * 2 > ReconnectGovernor::DEFAULT_BASE,
            "500 first flaps never reached past the midpoint of the base window"
        );
    }

    #[tokio::test]
    async fn receive_control_accepts_a_frame_at_exactly_the_size_cap() {
        let (client, mut server) = tokio::io::duplex(64);
        let mut conn = AgentConnection::new(client);

        let at_cap = codec::MAX_FRAME_SIZE as u32;
        tokio::spawn(async move {
            AsyncWriteExt::write_all(&mut server, &[FRAME_CONTROL])
                .await
                .unwrap();
            AsyncWriteExt::write_all(&mut server, &at_cap.to_be_bytes())
                .await
                .unwrap();
        });

        match conn.receive_control().await {
            Err(ConnectionError::Protocol(mesh_protocol::ProtocolError::FrameTooLarge {
                size,
                max,
            })) => panic!("a frame of {size} bytes was refused against a {max}-byte cap"),
            Err(ConnectionError::Io(_)) => {}
            other => panic!("expected the truncated payload to fail the read, got {other:?}"),
        }
    }

    #[tokio::test(start_paused = true)]
    async fn reconnect_backoff_waits_between_attempts() {
        let start = tokio::time::Instant::now();
        let result: Result<u32, _> = reconnect_with_backoff(
            || async { Err::<u32, String>("unreachable".to_string()) },
            8,
        )
        .await;
        assert!(result.is_err());
        assert!(
            start.elapsed() > Duration::ZERO,
            "the reconnect loop retried without waiting"
        );
    }
}
