//! The relay side of a session: the socket, its reader and writer, and the screen capture loop.

use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::Arc;
use std::time::{Duration, Instant};

use futures_util::{SinkExt, StreamExt};
use mesh_protocol::{DesktopFrame, Frame, FrameEncoding};
use tokio::net::TcpStream;
use tokio::sync::mpsc;
use tokio_tungstenite::tungstenite::Message;
use tokio_tungstenite::{MaybeTlsStream, WebSocketStream};
use tracing::{debug, warn};

use super::outbox::Outbox;
use super::outlet::OutletReader;
use crate::link::pacing::{self, SendLimit};
use crate::platform::ScreenCapture;
use crate::session_error::SessionError;

/// Bytes the relay socket keeps unsent at most, so frames wait in the lanes, where an
/// interactive one can still go first.
pub(crate) const RELAY_UNSENT_LIMIT: u32 = 64 * 1024;

/// A connected relay socket.
pub(crate) type RelaySocket = WebSocketStream<MaybeTlsStream<TcpStream>>;

/// Builds the relay URL with the `side=agent` query parameter.
pub(crate) fn build_relay_url(relay_url: &str) -> Result<String, SessionError> {
    let mut parsed = url::Url::parse(relay_url)
        .map_err(|e| SessionError::WebSocket(format!("invalid relay URL: {e}")))?;
    parsed.query_pairs_mut().append_pair("side", "agent");
    Ok(parsed.to_string())
}

/// Connects to the relay with the socket's unsent bytes held to [`RELAY_UNSENT_LIMIT`], returning
/// the socket and a gauge reading what it holds.
pub(crate) async fn connect(url: &str) -> Result<(RelaySocket, SocketGauge), SessionError> {
    let parsed = url::Url::parse(url)
        .map_err(|e| SessionError::WebSocket(format!("invalid relay URL: {e}")))?;
    let host = parsed
        .host_str()
        .ok_or_else(|| SessionError::WebSocket("relay URL names no host".to_string()))?;
    let port = parsed
        .port_or_known_default()
        .ok_or_else(|| SessionError::WebSocket("relay URL names no port".to_string()))?;
    let tcp = TcpStream::connect((host, port))
        .await
        .map_err(|e| SessionError::WebSocket(format!("relay connect: {e}")))?;
    hold_unsent(&tcp)?;
    let gauge = SocketGauge::new(&tcp)
        .map_err(|e| SessionError::WebSocket(format!("relay socket gauge: {e}")))?;
    let (ws, _response) = tokio_tungstenite::client_async_tls(url, tcp).await?;
    Ok((ws, gauge))
}

#[cfg(target_os = "linux")]
fn hold_unsent(tcp: &TcpStream) -> Result<(), SessionError> {
    socket2::SockRef::from(tcp)
        .set_tcp_notsent_lowat(RELAY_UNSENT_LIMIT)
        .map_err(|e| SessionError::WebSocket(format!("relay socket option: {e}")))
}

#[cfg(not(target_os = "linux"))]
fn hold_unsent(_tcp: &TcpStream) -> Result<(), SessionError> {
    Ok(())
}

/// Reads what a relay socket holds unacknowledged and what it has delivered, through a second
/// handle on the same socket.
#[cfg(target_os = "linux")]
pub(crate) struct SocketGauge(socket2::Socket);

#[cfg(target_os = "linux")]
impl SocketGauge {
    /// Returns a gauge over `tcp`.
    pub(crate) fn new(tcp: &TcpStream) -> std::io::Result<Self> {
        socket2::SockRef::from(tcp).try_clone().map(Self)
    }

    /// Bytes written to the socket that the peer has not acknowledged, sent or not.
    pub(crate) fn outstanding(&self) -> std::io::Result<usize> {
        use std::os::fd::AsRawFd;
        let mut queued: libc::c_int = 0;
        // SAFETY: SIOCOUTQ writes one int through a pointer to a live int on an open socket.
        let rc = unsafe { libc::ioctl(self.0.as_raw_fd(), libc::TIOCOUTQ, &mut queued) };
        if rc < 0 {
            return Err(std::io::Error::last_os_error());
        }
        Ok(usize::try_from(queued).unwrap_or(0))
    }

    /// Bytes the peer has acknowledged since the socket opened, and the least round trip seen.
    pub(crate) fn delivered(&self) -> std::io::Result<(u64, Duration)> {
        use std::os::fd::AsRawFd;
        let mut info = std::mem::MaybeUninit::<libc::tcp_info>::zeroed();
        let mut len = libc::socklen_t::try_from(std::mem::size_of::<libc::tcp_info>())
            .unwrap_or(libc::socklen_t::MAX);
        // SAFETY: TCP_INFO writes at most `len` bytes into a zeroed tcp_info of that size.
        let rc = unsafe {
            libc::getsockopt(
                self.0.as_raw_fd(),
                libc::IPPROTO_TCP,
                libc::TCP_INFO,
                info.as_mut_ptr().cast(),
                &mut len,
            )
        };
        if rc < 0 {
            return Err(std::io::Error::last_os_error());
        }
        // SAFETY: every bit pattern is a valid tcp_info, and the struct started zeroed.
        let info = unsafe { info.assume_init() };
        Ok((
            info.tcpi_bytes_acked,
            Duration::from_micros(u64::from(info.tcpi_min_rtt)),
        ))
    }
}

/// Reads nothing where the socket statistics are Linux's own: the writer never waits.
#[cfg(not(target_os = "linux"))]
pub(crate) struct SocketGauge;

#[cfg(not(target_os = "linux"))]
impl SocketGauge {
    /// Returns a gauge that reads an empty socket.
    pub(crate) fn new(_tcp: &TcpStream) -> std::io::Result<Self> {
        Ok(Self)
    }

    /// Always zero.
    pub(crate) fn outstanding(&self) -> std::io::Result<usize> {
        Ok(0)
    }

    /// Always zero.
    pub(crate) fn delivered(&self) -> std::io::Result<(u64, Duration)> {
        Ok((0, Duration::ZERO))
    }
}

/// Reads whole frames from the relay into `frames` until the relay closes.
pub(crate) async fn read_relay<S>(mut ws_rx: S, frames: mpsc::Sender<Vec<u8>>)
where
    S: futures_util::Stream<Item = Result<Message, tokio_tungstenite::tungstenite::Error>> + Unpin,
{
    while let Some(msg) = ws_rx.next().await {
        let data = match msg {
            Ok(Message::Binary(data)) => data,
            Ok(Message::Close(_)) => break,
            // The library answers a ping with its own pong on the next read.
            Ok(_) => continue,
            Err(e) => {
                warn!("WebSocket receive error: {e}");
                break;
            }
        };
        if data.is_empty() {
            continue;
        }
        if frames.send(data.to_vec()).await.is_err() {
            break;
        }
    }
}

/// Writes what the core pushed to the relay, each frame counted written once the socket holds
/// under its send limit again, so the next one waits in the lanes, where typing goes first.
pub(crate) async fn write_relay<S>(mut ws_tx: S, mut frames: OutletReader, gauge: SocketGauge)
where
    S: futures_util::Sink<Message> + Unpin,
    S::Error: std::fmt::Display,
{
    let mut pacer = RelayPacer::new(gauge);
    loop {
        let frame = tokio::select! {
            biased;
            frame = frames.next() => match frame {
                Some(frame) => frame,
                None => break,
            },
            () = pacer.tick() => continue,
        };
        // The limit keeps up while a full socket holds the send, which runs to completion.
        let send = ws_tx.send(Message::Binary(frame.into()));
        tokio::pin!(send);
        let sent = loop {
            tokio::select! {
                biased;
                sent = &mut send => break sent,
                () = pacer.tick() => {}
            }
        };
        if let Err(e) = sent {
            warn!("WebSocket send error: {e}");
            break;
        }
        pacer.wait_for_room().await;
        frames.written();
    }
    if let Err(e) = ws_tx.close().await {
        debug!("WebSocket close error on writer-loop exit: {e}");
    }
}

/// How often the relay writer re-reads the socket while it waits at the limit.
const LIMIT_POLL: Duration = Duration::from_millis(2);

/// The relay socket's send limit, set from the deliveries and round trip the socket reports.
struct RelayPacer {
    gauge: SocketGauge,
    limit: SendLimit,
    acked: Option<u64>,
    ticks: tokio::time::Interval,
}

impl RelayPacer {
    fn new(gauge: SocketGauge) -> Self {
        let mut ticks = tokio::time::interval(pacing::INTERVAL);
        ticks.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Delay);
        Self {
            gauge,
            limit: SendLimit::new(Instant::now()),
            acked: None,
            ticks,
        }
    }

    /// Resolves at the next interval, having fed the limit what the socket delivered since.
    async fn tick(&mut self) {
        self.ticks.tick().await;
        let now = Instant::now();
        if let Ok((acked, rtt)) = self.gauge.delivered() {
            if let Some(last) = self.acked {
                let bytes = usize::try_from(acked.saturating_sub(last)).unwrap_or(usize::MAX);
                self.limit.confirmed(bytes, now);
            }
            self.acked = Some(acked);
            self.limit.round_trip(rtt, now);
        }
        self.limit.recompute(now);
    }

    /// Waits until the socket holds less than the limit unacknowledged, so a frame larger than the
    /// limit still keeps the path busy; a socket that cannot be read never waits.
    async fn wait_for_room(&mut self) {
        loop {
            match self.gauge.outstanding() {
                Ok(out) if out < self.limit.current() => return,
                Ok(_) => {}
                Err(_) => return,
            }
            let waiting = Instant::now();
            tokio::select! {
                biased;
                () = self.tick() => {}
                () = tokio::time::sleep(LIMIT_POLL) => {}
            }
            self.limit.blocked(waiting.elapsed());
        }
    }
}

/// Maximum consecutive capture failures before the loop gives up.
const MAX_CONSECUTIVE_CAPTURE_ERRORS: u32 = 3;

/// JPEG quality on a 0-100 scale.
const JPEG_QUALITY: u8 = 70;

/// Captures desktop frames and sends them to the relay until `running` clears.
pub(crate) async fn capture_loop(
    capture: &mut dyn ScreenCapture,
    outbox: Outbox,
    running: Arc<AtomicBool>,
) {
    let sequence = AtomicU64::new(0);
    let frame_interval = Duration::from_millis(100); // ~10 FPS
    let mut consecutive_errors: u32 = 0;

    while running.load(Ordering::Relaxed) {
        match capture.next_frame().await {
            Ok(raw_frame) => {
                consecutive_errors = 0;
                let seq = sequence.fetch_add(1, Ordering::Relaxed);

                let (encoding, data) = match encode_jpeg(&raw_frame, JPEG_QUALITY) {
                    Ok(jpeg_data) => (FrameEncoding::Jpeg, jpeg_data),
                    Err(e) => {
                        warn!("JPEG encode failed, sending raw: {e}");
                        (FrameEncoding::Raw, raw_frame.data)
                    }
                };

                let desktop_frame = DesktopFrame {
                    sequence: seq,
                    x: 0,
                    y: 0,
                    width: raw_frame.width as u16,
                    height: raw_frame.height as u16,
                    encoding,
                    data,
                };
                let frame = Frame::Desktop(desktop_frame);
                match outbox.send(&frame).await {
                    Ok(()) => {}
                    Err(super::outbox::OutboxError::TooLarge { len, cap, .. }) => {
                        warn!(
                            len,
                            cap, "screen picture past the bulk lane's cap, not sent"
                        );
                    }
                    Err(_) => break,
                }
            }
            Err(e) => {
                consecutive_errors += 1;
                if consecutive_errors >= MAX_CONSECUTIVE_CAPTURE_ERRORS {
                    warn!("capture failed {consecutive_errors} times, stopping: {e}");
                    break;
                }
                warn!("capture error ({consecutive_errors}/{MAX_CONSECUTIVE_CAPTURE_ERRORS}): {e}");
                tokio::time::sleep(frame_interval).await;
            }
        }

        tokio::time::sleep(frame_interval).await;
    }
}

fn encode_jpeg(frame: &crate::platform::RawFrame, quality: u8) -> Result<Vec<u8>, SessionError> {
    use image::codecs::jpeg::JpegEncoder;

    // JPEG has no alpha channel, so every fourth byte is dropped.
    let pixel_count = (frame.width * frame.height) as usize;
    let mut rgb = Vec::with_capacity(pixel_count * 3);
    for px in frame.data.as_chunks::<4>().0 {
        rgb.push(px[0]);
        rgb.push(px[1]);
        rgb.push(px[2]);
    }

    // Pre-allocates a quarter byte per pixel; the buffer grows when the encoding needs more.
    let mut buf = Vec::with_capacity(pixel_count / 4);
    let mut encoder = JpegEncoder::new_with_quality(&mut buf, quality);
    encoder
        .encode(
            &rgb,
            frame.width,
            frame.height,
            image::ExtendedColorType::Rgb8,
        )
        .map_err(|e| SessionError::Encode(e.to_string()))?;
    Ok(buf)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_build_relay_url_adds_side_param() {
        let url = build_relay_url("wss://localhost/ws/relay/abc123").unwrap();
        assert!(url.contains("side=agent"));
        assert!(url.contains("abc123"));
    }

    #[test]
    fn test_build_relay_url_preserves_existing_params() {
        let url = build_relay_url("wss://localhost/ws/relay/abc?foo=bar").unwrap();
        assert!(url.contains("foo=bar"));
        assert!(url.contains("side=agent"));
    }

    #[test]
    fn test_build_relay_url_invalid() {
        let result = build_relay_url("not a url");
        assert!(result.is_err());
    }

    #[test]
    fn test_encode_jpeg_valid_rgba() {
        let frame = crate::platform::RawFrame {
            width: 8,
            height: 8,
            // Fully transparent, so a retained alpha byte cannot pass as colour.
            data: [10u8, 200, 30, 0].repeat(8 * 8),
        };
        let jpeg = encode_jpeg(&frame, 90).expect("encode should succeed");

        assert!(jpeg.len() >= 2);
        assert_eq!(jpeg[0], 0xFF);
        assert_eq!(jpeg[1], 0xD8);

        let decoded = image::load_from_memory(&jpeg)
            .expect("re-decode the encoded JPEG")
            .to_rgb8();
        assert_eq!(decoded.dimensions(), (8, 8));
        for px in decoded.pixels() {
            let [r, g, b] = px.0;
            assert!((i16::from(r) - 10).abs() <= 12, "red decoded as {r}");
            assert!((i16::from(g) - 200).abs() <= 12, "green decoded as {g}");
            assert!((i16::from(b) - 30).abs() <= 12, "blue decoded as {b}");
        }
    }

    #[tokio::test(flavor = "multi_thread", worker_threads = 2)]
    async fn capture_loop_exits_after_max_consecutive_errors() {
        use crate::platform::{CaptureError, RawFrame, ScreenCapture};
        use std::sync::atomic::{AtomicU32, Ordering};

        struct CountingErrorCapture {
            calls: Arc<AtomicU32>,
        }
        #[async_trait::async_trait]
        impl ScreenCapture for CountingErrorCapture {
            async fn next_frame(&mut self) -> Result<RawFrame, CaptureError> {
                self.calls.fetch_add(1, Ordering::SeqCst);
                Err(CaptureError::NoDisplay)
            }
            fn resolution(&self) -> (u32, u32) {
                (0, 0)
            }
        }

        let calls = Arc::new(AtomicU32::new(0));
        let mut capture = CountingErrorCapture {
            calls: calls.clone(),
        };
        let (outbox, _lanes) = Outbox::new();
        let running = Arc::new(AtomicBool::new(true));

        let r = running.clone();
        let result = tokio::time::timeout(
            Duration::from_secs(3),
            capture_loop(&mut capture, outbox, r),
        )
        .await;

        running.store(false, Ordering::Relaxed);

        assert!(
            result.is_ok(),
            "capture_loop should exit after {} consecutive errors, but it hung",
            MAX_CONSECUTIVE_CAPTURE_ERRORS
        );
        assert_eq!(
            calls.load(Ordering::SeqCst),
            MAX_CONSECUTIVE_CAPTURE_ERRORS,
            "capture_loop must call next_frame exactly {} times before exiting",
            MAX_CONSECUTIVE_CAPTURE_ERRORS
        );
    }

    #[tokio::test(flavor = "multi_thread", worker_threads = 2)]
    async fn capture_loop_honors_running_flag_for_clean_shutdown() {
        use crate::platform::NullCapture;

        let mut capture = NullCapture;
        let (outbox, _lanes) = Outbox::new();
        let running = Arc::new(AtomicBool::new(false));

        let elapsed = std::time::Instant::now();
        capture_loop(&mut capture, outbox, running).await;
        assert!(
            elapsed.elapsed() < Duration::from_millis(500),
            "capture_loop must exit promptly when running=false from start"
        );
    }

    #[tokio::test(flavor = "multi_thread", worker_threads = 2)]
    async fn the_relay_writer_sends_in_order_and_reports_each_frame_written() {
        use std::sync::Mutex as StdMutex;

        struct CaptureSink {
            messages: Arc<StdMutex<Vec<Message>>>,
        }
        impl futures_util::Sink<Message> for CaptureSink {
            type Error = tokio_tungstenite::tungstenite::Error;
            fn poll_ready(
                self: std::pin::Pin<&mut Self>,
                _cx: &mut std::task::Context<'_>,
            ) -> std::task::Poll<Result<(), Self::Error>> {
                std::task::Poll::Ready(Ok(()))
            }
            fn start_send(
                self: std::pin::Pin<&mut Self>,
                item: Message,
            ) -> Result<(), Self::Error> {
                self.messages.lock().unwrap().push(item);
                Ok(())
            }
            fn poll_flush(
                self: std::pin::Pin<&mut Self>,
                _cx: &mut std::task::Context<'_>,
            ) -> std::task::Poll<Result<(), Self::Error>> {
                std::task::Poll::Ready(Ok(()))
            }
            fn poll_close(
                self: std::pin::Pin<&mut Self>,
                _cx: &mut std::task::Context<'_>,
            ) -> std::task::Poll<Result<(), Self::Error>> {
                std::task::Poll::Ready(Ok(()))
            }
        }

        let messages = Arc::new(StdMutex::new(Vec::<Message>::new()));
        let sink = CaptureSink {
            messages: messages.clone(),
        };
        let (outlet, reader) = super::super::outlet::Outlet::new();
        let (idle, _peer) = connected_pair().await;
        let gauge = SocketGauge::new(&idle).unwrap();
        let writer = tokio::spawn(write_relay(sink, reader, gauge));

        outlet.push(b"hello".to_vec());
        outlet.push(b"world".to_vec());
        tokio::time::timeout(Duration::from_secs(1), outlet.empty())
            .await
            .expect("both frames are reported written");
        drop(outlet);
        writer.await.unwrap();

        let captured = messages.lock().unwrap();
        assert_eq!(captured.len(), 2);
        let extract = |m: &Message| match m {
            Message::Binary(b) => b.to_vec(),
            other => panic!("expected Binary, got {other:?}"),
        };
        assert_eq!(extract(&captured[0]), b"hello");
        assert_eq!(extract(&captured[1]), b"world");
    }

    #[test]
    fn test_encode_jpeg_smaller_than_raw() {
        let frame = crate::platform::RawFrame {
            width: 100,
            height: 100,
            data: vec![128; 100 * 100 * 4],
        };
        let jpeg = encode_jpeg(&frame, 70).expect("encode should succeed");
        assert!(
            jpeg.len() < frame.data.len(),
            "JPEG ({}) should be smaller than raw ({})",
            jpeg.len(),
            frame.data.len()
        );
    }

    /// A loopback listener whose connections take 4 KiB before acknowledging no more, so what a
    /// peer has yet to read stays unacknowledged at the sender.
    #[cfg(target_os = "linux")]
    async fn small_window_listener() -> (tokio::net::TcpListener, std::net::SocketAddr) {
        let listener = socket2::Socket::new(
            socket2::Domain::IPV4,
            socket2::Type::STREAM,
            Some(socket2::Protocol::TCP),
        )
        .unwrap();
        listener.set_recv_buffer_size(4096).unwrap();
        listener
            .bind(
                &"127.0.0.1:0"
                    .parse::<std::net::SocketAddr>()
                    .unwrap()
                    .into(),
            )
            .unwrap();
        listener.listen(1).unwrap();
        listener.set_nonblocking(true).unwrap();
        let listener = tokio::net::TcpListener::from_std(listener.into()).unwrap();
        let addr = listener.local_addr().unwrap();
        (listener, addr)
    }

    /// A connected loopback pair: the client end, and the server end it reached.
    async fn connected_pair() -> (TcpStream, TcpStream) {
        let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let client = TcpStream::connect(listener.local_addr().unwrap())
            .await
            .unwrap();
        let (server, _) = listener.accept().await.unwrap();
        (client, server)
    }

    #[cfg(target_os = "linux")]
    #[tokio::test(flavor = "multi_thread", worker_threads = 2)]
    async fn the_relay_writer_holds_the_socket_to_what_a_slow_peer_takes() {
        const FRAMES: usize = 400;
        const FRAME: usize = 16 * 1024;

        let (listener, addr) = small_window_listener().await;
        // The peer takes a frame every 10 ms: 1.6 MB/s, which needs no more than the floor queued.
        let peer = tokio::spawn(async move {
            let (tcp, _) = listener.accept().await.unwrap();
            let mut ws = tokio_tungstenite::accept_async(tcp).await.unwrap();
            let mut lengths = Vec::new();
            while let Some(Ok(Message::Binary(data))) = ws.next().await {
                lengths.push(data.len());
                if lengths.len() == FRAMES {
                    break;
                }
                tokio::time::sleep(Duration::from_millis(10)).await;
            }
            lengths
        });

        // A send buffer this large lets the socket take more frames than the limit allows.
        let tcp = socket2::Socket::new(
            socket2::Domain::IPV4,
            socket2::Type::STREAM,
            Some(socket2::Protocol::TCP),
        )
        .unwrap();
        tcp.set_send_buffer_size(8 << 20).unwrap();
        tcp.connect(&addr.into()).unwrap();
        tcp.set_nonblocking(true).unwrap();
        let tcp = TcpStream::from_std(tcp.into()).unwrap();
        let gauge = SocketGauge::new(&tcp).unwrap();
        let probe = SocketGauge::new(&tcp).unwrap();
        let (ws, _) = tokio_tungstenite::client_async(format!("ws://{addr}/"), tcp)
            .await
            .unwrap();
        let (ws_tx, _ws_rx) = ws.split();
        let (outlet, reader) = super::super::outlet::Outlet::new();
        let writer = tokio::spawn(write_relay(ws_tx, reader, gauge));
        for _ in 0..FRAMES {
            outlet.push(vec![7u8; FRAME]);
        }

        // The limit settles once a second of deliveries is measured.
        tokio::time::sleep(Duration::from_secs(2)).await;
        let mut held = 0;
        for _ in 0..50 {
            held = held.max(probe.outstanding().unwrap());
            tokio::time::sleep(Duration::from_millis(20)).await;
        }
        assert!(
            held <= pacing::FLOOR + FRAME + 64,
            "the socket held {held} bytes for a peer taking 1.6 MB/s"
        );

        tokio::time::timeout(Duration::from_secs(10), outlet.empty())
            .await
            .expect("every frame goes as the peer reads");
        assert_eq!(peer.await.unwrap(), vec![FRAME; FRAMES]);
        drop(outlet);
        writer.await.unwrap();
    }

    #[cfg(target_os = "linux")]
    #[tokio::test(flavor = "multi_thread", worker_threads = 2)]
    async fn the_relay_writer_feeds_the_socket_while_it_holds_under_the_limit() {
        const FRAME: usize = 100 * 1024;

        // A peer that never reads leaves every byte the writer hands over unacknowledged.
        let (listener, addr) = small_window_listener().await;
        let peer = tokio::spawn(async move {
            let (tcp, _) = listener.accept().await.unwrap();
            let ws = tokio_tungstenite::accept_async(tcp).await.unwrap();
            tokio::time::sleep(Duration::from_secs(5)).await;
            drop(ws);
        });
        let tcp = TcpStream::connect(addr).await.unwrap();
        let gauge = SocketGauge::new(&tcp).unwrap();
        let probe = SocketGauge::new(&tcp).unwrap();
        let (ws, _) = tokio_tungstenite::client_async(format!("ws://{addr}/"), tcp)
            .await
            .unwrap();
        let (ws_tx, _ws_rx) = ws.split();
        let (outlet, reader) = super::super::outlet::Outlet::new();
        let writer = tokio::spawn(write_relay(ws_tx, reader, gauge));

        // Nothing delivered for a second settles the limit at its floor.
        tokio::time::sleep(Duration::from_secs(1)).await;
        for _ in 0..3 {
            outlet.push(vec![7u8; FRAME]);
        }
        tokio::time::sleep(Duration::from_millis(300)).await;
        let held = probe.outstanding().unwrap();
        assert!(
            held > FRAME && held <= 2 * FRAME + 64,
            "the second frame goes while the socket holds under the floor, the third waits: {held}"
        );
        assert_eq!(
            outlet.queued(),
            2,
            "the second frame counts as written only once the socket holds under the floor again"
        );

        peer.abort();
        peer.await.ok();
        drop(outlet);
        writer.abort();
        writer.await.ok();
    }

    #[cfg(target_os = "linux")]
    #[tokio::test(flavor = "multi_thread", worker_threads = 2)]
    async fn the_socket_gauge_reads_what_waits_and_what_was_delivered() {
        use tokio::io::{AsyncReadExt, AsyncWriteExt};

        let (mut client, mut server) = connected_pair().await;
        let gauge = SocketGauge::new(&client).unwrap();
        let (_, rtt_before) = gauge.delivered().unwrap();
        assert!(
            rtt_before > Duration::ZERO,
            "the handshake gave a round trip"
        );

        let payload = vec![1u8; 8 << 20];
        let writing = tokio::spawn(async move {
            client.write_all(&payload).await.unwrap();
            client
        });
        let waited = tokio::time::timeout(Duration::from_secs(2), async {
            while gauge.outstanding().unwrap() == 0 {
                tokio::time::sleep(Duration::from_millis(5)).await;
            }
        })
        .await;
        assert!(
            waited.is_ok(),
            "bytes the peer has not read wait in the socket"
        );

        let mut received = 0;
        let mut buf = vec![0u8; 1 << 16];
        while received < 8 << 20 {
            received += server.read(&mut buf).await.unwrap();
        }
        let _client = writing.await.unwrap();
        tokio::time::timeout(Duration::from_secs(2), async {
            while gauge.outstanding().unwrap() > 0 {
                tokio::time::sleep(Duration::from_millis(5)).await;
            }
        })
        .await
        .expect("acknowledged bytes leave the socket");
        let (delivered, _) = gauge.delivered().unwrap();
        assert!(delivered >= 8 << 20, "delivered {delivered}");
    }
}
