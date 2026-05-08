//! WebSocket relay utilities for session transport.

use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::Arc;
use std::time::Duration;

use futures_util::SinkExt;
use mesh_protocol::{DesktopFrame, Frame, FrameEncoding};
use tokio::sync::mpsc;
use tokio_tungstenite::tungstenite::Message;
use tracing::{debug, warn};

use crate::platform::ScreenCapture;
use crate::session_error::SessionError;

/// Build relay URL with ?side=agent query parameter.
pub(crate) fn build_relay_url(relay_url: &str) -> Result<String, SessionError> {
    let mut parsed = url::Url::parse(relay_url)
        .map_err(|e| SessionError::WebSocket(format!("invalid relay URL: {e}")))?;
    parsed.query_pairs_mut().append_pair("side", "agent");
    Ok(parsed.to_string())
}

/// WebSocket writer loop: sends encoded frames from the channel.
///
/// Generic over the sink so tests can substitute an in-memory Sink without
/// constructing a real WebSocket stream. The production callsite passes the
/// `SplitSink` half of `tokio_tungstenite`'s `WebSocketStream`.
pub(crate) async fn ws_writer_loop<S>(
    mut ws_tx: S,
    mut frame_rx: mpsc::Receiver<Vec<u8>>,
    running: Arc<AtomicBool>,
) where
    S: futures_util::Sink<Message> + Unpin,
    S::Error: std::fmt::Display,
{
    while let Some(data) = frame_rx.recv().await {
        if !running.load(Ordering::Relaxed) {
            break;
        }
        if let Err(e) = ws_tx.send(Message::Binary(data.into())).await {
            warn!("WebSocket send error: {e}");
            break;
        }
    }
    if let Err(e) = ws_tx.close().await {
        debug!("WebSocket close error on writer-loop exit: {e}");
    }
}

/// Maximum consecutive capture failures before the loop gives up.
const MAX_CONSECUTIVE_CAPTURE_ERRORS: u32 = 3;

/// Default JPEG quality (0-100). Matches MeshCentral's default.
const JPEG_QUALITY: u8 = 70;

/// Desktop capture loop: captures frames and sends them to the relay.
pub(crate) async fn capture_loop(
    capture: &mut dyn ScreenCapture,
    frame_tx: mpsc::Sender<Vec<u8>>,
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
                if send_frame(&frame_tx, &frame).await.is_err() {
                    break;
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

/// Encode RGBA pixel data as JPEG (strips alpha → RGB for JPEG compatibility).
fn encode_jpeg(frame: &crate::platform::RawFrame, quality: u8) -> Result<Vec<u8>, SessionError> {
    use image::codecs::jpeg::JpegEncoder;

    // JPEG doesn't support alpha — convert RGBA → RGB by dropping every 4th byte.
    let pixel_count = (frame.width * frame.height) as usize;
    let mut rgb = Vec::with_capacity(pixel_count * 3);
    for chunk in frame.data.chunks_exact(4) {
        rgb.push(chunk[0]); // R
        rgb.push(chunk[1]); // G
        rgb.push(chunk[2]); // B
    }

    // Conservative pre-allocation: JPEG output is typically ~2-5% of raw RGB size.
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

/// Encode a frame and send it via the channel.
pub(crate) async fn send_frame(
    tx: &mpsc::Sender<Vec<u8>>,
    frame: &Frame,
) -> Result<(), SessionError> {
    let encoded = frame.encode()?;
    tx.send(encoded)
        .await
        .map_err(|_| SessionError::WebSocket("send channel closed".to_string()))
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
            width: 2,
            height: 2,
            data: vec![
                255, 0, 0, 255, // red
                0, 255, 0, 255, // green
                0, 0, 255, 255, // blue
                255, 255, 255, 255, // white
            ],
        };
        let jpeg = encode_jpeg(&frame, 70).expect("encode should succeed");
        // JPEG files start with SOI marker: 0xFF 0xD8
        assert!(jpeg.len() >= 2);
        assert_eq!(jpeg[0], 0xFF);
        assert_eq!(jpeg[1], 0xD8);
    }

    /// Pin capture_loop's consecutive-error counting AND the
    /// `>= MAX_CONSECUTIVE_CAPTURE_ERRORS` exit condition. Mutating
    /// `+=` to `-=` or `*=` makes the counter never reach the threshold;
    /// mutating `>=` to `<` exits on the first error.
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
        let (tx, _rx) = mpsc::channel::<Vec<u8>>(8);
        let running = Arc::new(AtomicBool::new(true));

        // Outer timeout guards against `+=`-mutated counters that never
        // reach the threshold.
        let r = running.clone();
        let result =
            tokio::time::timeout(Duration::from_secs(3), capture_loop(&mut capture, tx, r)).await;

        // Force the loop to stop in case of hang.
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

    /// Pin capture_loop's `running` flag check: setting running=false must
    /// halt the loop on the next iteration. Replacing the function body
    /// with `()` would skip the work entirely (loop never runs).
    #[tokio::test(flavor = "multi_thread", worker_threads = 2)]
    async fn capture_loop_honors_running_flag_for_clean_shutdown() {
        use crate::platform::NullCapture;

        let mut capture = NullCapture;
        let (tx, _rx) = mpsc::channel::<Vec<u8>>(8);
        let running = Arc::new(AtomicBool::new(false)); // pre-set false

        // With running=false from the start, NullCapture's next_frame should
        // never be called and the loop should exit immediately.
        let elapsed = std::time::Instant::now();
        capture_loop(&mut capture, tx, running).await;
        assert!(
            elapsed.elapsed() < Duration::from_millis(500),
            "capture_loop must exit promptly when running=false from start"
        );
    }

    /// Pin ws_writer_loop: bytes from the channel must reach the WebSocket sink.
    /// Use a forwarding sink so we can observe what was written. Mutating the
    /// function body to `()` or `delete !` (in `if !running { break }`) breaks
    /// either the forwarding or the shutdown semantics.
    #[tokio::test(flavor = "multi_thread", worker_threads = 2)]
    async fn ws_writer_loop_forwards_bytes_and_honors_running_flag() {
        use std::sync::Mutex as StdMutex;

        // A trivial Sink that captures all messages it receives.
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
        let (frame_tx, frame_rx) = mpsc::channel::<Vec<u8>>(4);
        let running = Arc::new(AtomicBool::new(true));

        let r = running.clone();
        let writer = tokio::spawn(async move {
            ws_writer_loop(sink, frame_rx, r).await;
        });

        frame_tx.send(b"hello".to_vec()).await.unwrap();
        frame_tx.send(b"world".to_vec()).await.unwrap();
        // Trigger shutdown: drop sender.
        drop(frame_tx);
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
        // 100x100 RGBA = 40,000 bytes raw
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
}
