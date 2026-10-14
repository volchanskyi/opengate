//! Host memory during a download, read from the process's resident size. The only test in its
//! binary, so nothing else allocates while it reads.

use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::Arc;
use std::time::Duration;

use futures_util::{SinkExt, StreamExt};
use mesh_agent_core::session::handlers::DOWNLOAD_RUNNING;
use mesh_agent_core::{NullCapture, NullInput, SessionHandler};
use mesh_protocol::{ControlMessage, Frame, Permissions, SessionToken};
use tokio::net::TcpListener;
use tokio_tungstenite::tungstenite::Message;

/// Sizes the download runs at; each is a sparse file, so reading it costs no disk.
const SIZES: [u64; 3] = [32 << 20, 128 << 20, 512 << 20];

/// Most a session may add to the host's resident size: the per-session figure of the
/// non-functional requirements, under 13 MiB.
const SESSION_CEILING: u64 = 13 << 20;

/// Most the largest download may add beyond the smallest.
const GROWTH_WITH_SIZE: u64 = 4 << 20;

fn resident() -> u64 {
    let statm = std::fs::read_to_string("/proc/self/statm").expect("the process's memory");
    let pages: u64 = statm
        .split_whitespace()
        .nth(1)
        .and_then(|field| field.parse().ok())
        .expect("resident pages");
    pages * 4096
}

/// Downloads `size` bytes through a session, asking for a second file halfway, and returns the
/// most the host's resident size rose above where it stood before the download.
async fn peak_growth(dir: &std::path::Path, size: u64) -> u64 {
    let file = dir.join(format!("image-{size}.raw"));
    std::fs::File::create(&file)
        .and_then(|f| f.set_len(size))
        .expect("a sparse file");
    let path = file.to_string_lossy().to_string();

    let listener = TcpListener::bind("127.0.0.1:0")
        .await
        .expect("bind relay stub");
    let url = format!(
        "ws://{}/ws/relay/token",
        listener.local_addr().expect("addr")
    );
    let permissions = Permissions {
        desktop: false,
        terminal: false,
        file_read: true,
        file_write: false,
        input: false,
    };
    let session = tokio::spawn(async move {
        SessionHandler::new(SessionToken::generate(), permissions)
            .run(&url, Box::new(NullCapture), Box::new(NullInput))
            .await
    });
    let (stream, _) = listener.accept().await.expect("accept");
    let mut ws = tokio_tungstenite::accept_async(stream)
        .await
        .expect("handshake");

    tokio::time::sleep(Duration::from_millis(200)).await;
    let before = resident();
    let peak = Arc::new(AtomicU64::new(before));
    let sampling = Arc::new(AtomicBool::new(true));
    let sampler = tokio::spawn({
        let (peak, sampling) = (peak.clone(), sampling.clone());
        async move {
            while sampling.load(Ordering::Relaxed) {
                peak.fetch_max(resident(), Ordering::Relaxed);
                tokio::time::sleep(Duration::from_millis(5)).await;
            }
        }
    });

    let request = |path: String| {
        let frame = Frame::Control(ControlMessage::FileDownloadRequest { path });
        Message::Binary(frame.encode().expect("encode").into())
    };
    ws.send(request(path.clone())).await.expect("request");
    let (mut received, mut asked_again, mut refused) = (0u64, false, false);
    while received < size {
        let msg = tokio::time::timeout(Duration::from_secs(30), ws.next())
            .await
            .expect("the download continues")
            .expect("relay open")
            .expect("relay message");
        let Message::Binary(data) = msg else { continue };
        match Frame::decode(&data).expect("a whole frame").0 {
            Frame::FileTransfer(piece) => received += piece.data.len() as u64,
            Frame::Control(ControlMessage::FileDownloadError { error, .. }) => {
                assert_eq!(error, DOWNLOAD_RUNNING);
                refused = true;
            }
            other => panic!("unexpected frame: {other:?}"),
        }
        if !asked_again && received >= size / 2 {
            ws.send(request(path.clone()))
                .await
                .expect("second request");
            asked_again = true;
        }
    }
    sampling.store(false, Ordering::Relaxed);
    sampler.await.expect("sampler");
    assert!(
        refused,
        "the second request is refused while the first runs"
    );

    ws.close(None).await.expect("close");
    while let Some(Ok(_)) = ws.next().await {}
    tokio::time::timeout(Duration::from_secs(10), session)
        .await
        .expect("the session ends")
        .expect("session task")
        .expect("a clean end");
    std::fs::remove_file(&file).expect("remove the file");
    peak.load(Ordering::Relaxed).saturating_sub(before)
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn a_download_holds_host_memory_flat_whatever_the_file_size() {
    let dir = tempfile::tempdir().expect("tempdir");
    // The first download grows the allocator's pools, so it runs before measuring.
    peak_growth(dir.path(), SIZES[0]).await;

    let mut growth = Vec::new();
    for size in SIZES {
        growth.push(peak_growth(dir.path(), size).await);
    }
    for (size, grew) in SIZES.iter().zip(&growth) {
        assert!(
            *grew <= SESSION_CEILING,
            "a {size}-byte download raised the host's memory by {grew} bytes"
        );
    }
    let spread = growth[2].saturating_sub(growth[0]);
    assert!(
        spread <= GROWTH_WITH_SIZE,
        "memory grew with the file: {growth:?} bytes for {SIZES:?}"
    );
}
