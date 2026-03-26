//! WebSocket relay utilities for session transport.

use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::Arc;
use std::time::Duration;

use futures_util::stream::SplitSink;
use futures_util::SinkExt;
use mesh_protocol::{DesktopFrame, Frame, FrameEncoding};
use tokio::sync::mpsc;
use tokio_tungstenite::tungstenite::Message;
use tokio_tungstenite::MaybeTlsStream;
use tracing::warn;

use crate::platform::ScreenCapture;
use crate::session_error::SessionError;

pub(crate) type WsStream =
    tokio_tungstenite::WebSocketStream<MaybeTlsStream<tokio::net::TcpStream>>;

/// Build relay URL with ?side=agent query parameter.
pub(crate) fn build_relay_url(relay_url: &str) -> Result<String, SessionError> {
    let mut parsed = url::Url::parse(relay_url)
        .map_err(|e| SessionError::WebSocket(format!("invalid relay URL: {e}")))?;
    parsed.query_pairs_mut().append_pair("side", "agent");
    Ok(parsed.to_string())
}

/// WebSocket writer loop: sends encoded frames from the channel.
pub(crate) async fn ws_writer_loop(
    mut ws_tx: SplitSink<WsStream, Message>,
    mut frame_rx: mpsc::Receiver<Vec<u8>>,
    running: Arc<AtomicBool>,
) {
    while let Some(data) = frame_rx.recv().await {
        if !running.load(Ordering::Relaxed) {
            break;
        }
        if let Err(e) = ws_tx.send(Message::Binary(data.into())).await {
            warn!("WebSocket send error: {e}");
            break;
        }
    }
    let _ = ws_tx.close().await;
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

    let mut buf = Vec::new();
    let mut encoder = JpegEncoder::new_with_quality(&mut buf, quality);
    encoder
        .encode(
            &rgb,
            frame.width,
            frame.height,
            image::ExtendedColorType::Rgb8,
        )
        .map_err(|e| SessionError::WebSocket(format!("jpeg encode: {e}")))?;
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
