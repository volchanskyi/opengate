//! The session's sending point: an interactive lane taken before a bulk lane, each capped in
//! bytes, so typing never waits behind file data on either path.

use std::sync::Arc;

use mesh_protocol::direct::MAX_PIECE;
use mesh_protocol::{Frame, FRAME_DESKTOP, FRAME_FILE};
use tokio::sync::{mpsc, OwnedSemaphorePermit, Semaphore};

/// Largest interactive frame: one link piece.
pub const INTERACTIVE_FRAME_CAP: usize = MAX_PIECE;

/// Most bytes the interactive lane holds; a producer waits for room past it.
pub const INTERACTIVE_LANE_CAP: usize = 256 * 1024;

/// Bytes of file data in one bulk frame.
pub const FILE_PIECE: usize = 256 * 1024;

/// Largest bulk frame: one file piece and the frame's header and fields.
pub const BULK_FRAME_CAP: usize = FILE_PIECE + 64;

/// Most bytes the bulk lane holds: two pieces.
pub const BULK_LANE_CAP: usize = 2 * BULK_FRAME_CAP;

/// Why a frame was not queued.
#[derive(Debug, thiserror::Error)]
#[non_exhaustive]
pub enum OutboxError {
    /// The frame is past its lane's cap; it is refused where it is produced and never sent.
    #[error("a {lane} frame of {len} bytes is past the lane's {cap}-byte cap")]
    TooLarge {
        /// Which lane the frame belongs to.
        lane: &'static str,
        /// The frame's encoded size.
        len: usize,
        /// The lane's frame cap.
        cap: usize,
    },
    /// The frame did not encode.
    #[error("encode: {0}")]
    Encode(#[from] mesh_protocol::ProtocolError),
    /// The session has ended.
    #[error("the session has ended")]
    Closed,
}

/// One queued frame and the room it holds until the sending point takes it.
#[derive(Debug)]
pub(crate) struct Queued {
    pub(crate) bytes: Vec<u8>,
    _room: Vec<OwnedSemaphorePermit>,
}

#[derive(Debug, Clone)]
struct Lane {
    name: &'static str,
    tx: mpsc::UnboundedSender<Queued>,
    room: Arc<Semaphore>,
    frame_cap: usize,
}

/// Where every feature of a session queues its frames. Cloned freely; all clones share lanes.
#[derive(Debug, Clone)]
pub struct Outbox {
    interactive: Lane,
    bulk: Lane,
}

/// The sending point's side of the lanes.
#[derive(Debug)]
pub(crate) struct Lanes {
    interactive: mpsc::UnboundedReceiver<Queued>,
    bulk: mpsc::UnboundedReceiver<Queued>,
}

/// One frame in flight for a paced producer: it waits for the frame before to be taken.
pub type Pace = Arc<Semaphore>;

/// Returns a pace that lets a producer hold one frame in its lane at a time.
pub fn one_at_a_time() -> Pace {
    Arc::new(Semaphore::new(1))
}

impl Outbox {
    /// Returns a session's lanes and the outbox that fills them.
    pub(crate) fn new() -> (Self, Lanes) {
        let (itx, irx) = mpsc::unbounded_channel();
        let (btx, brx) = mpsc::unbounded_channel();
        let outbox = Self {
            interactive: Lane {
                name: "interactive",
                tx: itx,
                room: Arc::new(Semaphore::new(INTERACTIVE_LANE_CAP)),
                frame_cap: INTERACTIVE_FRAME_CAP,
            },
            bulk: Lane {
                name: "bulk",
                tx: btx,
                room: Arc::new(Semaphore::new(BULK_LANE_CAP)),
                frame_cap: BULK_FRAME_CAP,
            },
        };
        (
            outbox,
            Lanes {
                interactive: irx,
                bulk: brx,
            },
        )
    }

    /// Encodes and queues `frame`, waiting for room in its lane.
    pub async fn send(&self, frame: &Frame) -> Result<(), OutboxError> {
        self.send_encoded(frame.encode()?).await
    }

    /// Queues an encoded frame, waiting for room in its lane.
    pub async fn send_encoded(&self, bytes: Vec<u8>) -> Result<(), OutboxError> {
        self.queue(bytes, None).await
    }

    /// Queues `frame` once the producer's previous frame under `pace` has been taken.
    pub async fn send_paced(&self, frame: &Frame, pace: &Pace) -> Result<(), OutboxError> {
        self.queue(frame.encode()?, Some(pace)).await
    }

    /// [`Outbox::send_encoded`] from a blocking thread, such as the terminal's reader.
    pub fn blocking_send_encoded(&self, bytes: Vec<u8>) -> Result<(), OutboxError> {
        tokio::runtime::Handle::current().block_on(self.send_encoded(bytes))
    }

    /// Which lane `bytes` belongs to: file data and screen pictures are bulk, all else
    /// interactive.
    fn lane(&self, bytes: &[u8]) -> &Lane {
        match bytes.first() {
            Some(&FRAME_FILE) | Some(&FRAME_DESKTOP) => &self.bulk,
            _ => &self.interactive,
        }
    }

    async fn queue(&self, bytes: Vec<u8>, pace: Option<&Pace>) -> Result<(), OutboxError> {
        let lane = self.lane(&bytes);
        if bytes.len() > lane.frame_cap {
            return Err(OutboxError::TooLarge {
                lane: lane.name,
                len: bytes.len(),
                cap: lane.frame_cap,
            });
        }
        let mut room = Vec::with_capacity(2);
        if let Some(pace) = pace {
            room.push(
                pace.clone()
                    .acquire_owned()
                    .await
                    .map_err(|_| OutboxError::Closed)?,
            );
        }
        let len = u32::try_from(bytes.len()).map_err(|_| OutboxError::TooLarge {
            lane: lane.name,
            len: bytes.len(),
            cap: lane.frame_cap,
        })?;
        room.push(
            lane.room
                .clone()
                .acquire_many_owned(len)
                .await
                .map_err(|_| OutboxError::Closed)?,
        );
        lane.tx
            .send(Queued { bytes, _room: room })
            .map_err(|_| OutboxError::Closed)
    }
}

impl Lanes {
    /// The next frame: the interactive lane first, then the bulk lane; `None` once both are
    /// closed and empty. Safe to cancel.
    pub(crate) async fn next(&mut self) -> Option<Queued> {
        tokio::select! {
            biased;
            Some(queued) = self.interactive.recv() => Some(queued),
            Some(queued) = self.bulk.recv() => Some(queued),
            else => None,
        }
    }

    /// The next frame if one is queued now.
    #[cfg(test)]
    pub(crate) fn try_next(&mut self) -> Option<Vec<u8>> {
        use futures_util::FutureExt;
        self.next().now_or_never().flatten().map(|q| q.bytes)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use mesh_protocol::{ControlMessage, FileFrame, TerminalFrame};
    use std::time::Duration;

    fn terminal(len: usize) -> Frame {
        Frame::Terminal(TerminalFrame { data: vec![1; len] })
    }

    fn file(len: usize) -> Frame {
        Frame::FileTransfer(FileFrame {
            offset: 0,
            total_size: len as u64,
            data: vec![2; len],
        })
    }

    #[tokio::test]
    async fn an_interactive_frame_is_taken_before_queued_bulk_frames() {
        let (outbox, mut lanes) = Outbox::new();
        outbox.send(&file(1000)).await.unwrap();
        outbox.send(&file(1000)).await.unwrap();
        outbox.send(&terminal(5)).await.unwrap();

        let first = lanes.try_next().unwrap();
        assert!(matches!(
            Frame::decode(&first).unwrap().0,
            Frame::Terminal(_)
        ));
        assert!(matches!(
            Frame::decode(&lanes.try_next().unwrap()).unwrap().0,
            Frame::FileTransfer(_)
        ));
    }

    #[tokio::test]
    async fn frames_keep_their_production_order_within_a_lane() {
        let (outbox, mut lanes) = Outbox::new();
        for n in 1..=3u8 {
            outbox
                .send(&Frame::Terminal(TerminalFrame { data: vec![n] }))
                .await
                .unwrap();
        }
        let order: Vec<u8> = std::iter::from_fn(|| lanes.try_next())
            .map(|b| match Frame::decode(&b).unwrap().0 {
                Frame::Terminal(t) => t.data[0],
                _ => 0,
            })
            .collect();
        assert_eq!(order, vec![1, 2, 3]);
    }

    #[tokio::test]
    async fn a_frame_over_its_lanes_cap_is_refused_where_it_is_produced() {
        let (outbox, mut lanes) = Outbox::new();
        let err = outbox
            .send(&terminal(INTERACTIVE_FRAME_CAP))
            .await
            .unwrap_err();
        assert!(matches!(
            err,
            OutboxError::TooLarge {
                lane: "interactive",
                ..
            }
        ));
        let err = outbox.send(&file(FILE_PIECE + 100)).await.unwrap_err();
        assert!(matches!(err, OutboxError::TooLarge { lane: "bulk", .. }));
        assert!(
            lanes.try_next().is_none(),
            "a refused frame never reaches the path"
        );

        outbox.send(&file(FILE_PIECE)).await.unwrap();
        assert!(
            lanes.try_next().is_some(),
            "a full file piece fits the bulk lane"
        );
    }

    #[tokio::test]
    async fn the_bulk_lane_holds_two_pieces_and_a_producer_waits_for_room() {
        let (outbox, mut lanes) = Outbox::new();
        outbox.send(&file(FILE_PIECE)).await.unwrap();
        outbox.send(&file(FILE_PIECE)).await.unwrap();

        let third = tokio::spawn({
            let outbox = outbox.clone();
            async move { outbox.send(&file(FILE_PIECE)).await }
        });
        tokio::time::sleep(Duration::from_millis(50)).await;
        assert!(!third.is_finished(), "a third piece waits for room");

        lanes.try_next().unwrap();
        tokio::time::timeout(Duration::from_secs(1), third)
            .await
            .expect("taking a piece makes room")
            .unwrap()
            .unwrap();
    }

    #[tokio::test]
    async fn the_interactive_lane_holds_256_kib() {
        let (outbox, mut lanes) = Outbox::new();
        let frame = terminal(16 * 1024 - 64);
        let len = frame.encode().unwrap().len();
        for _ in 0..INTERACTIVE_LANE_CAP / len {
            outbox.send(&frame).await.unwrap();
        }
        let next = tokio::spawn({
            let outbox = outbox.clone();
            let frame = frame.clone();
            async move { outbox.send(&frame).await }
        });
        tokio::time::sleep(Duration::from_millis(50)).await;
        assert!(!next.is_finished());
        lanes.try_next().unwrap();
        tokio::time::timeout(Duration::from_secs(1), next)
            .await
            .unwrap()
            .unwrap()
            .unwrap();
    }

    #[tokio::test]
    async fn a_paced_producer_holds_one_frame_in_its_lane() {
        let (outbox, mut lanes) = Outbox::new();
        let pace = one_at_a_time();
        let page = Frame::Control(ControlMessage::FileListResponse {
            path: "/".into(),
            entries: Vec::new(),
            more: true,
        });
        outbox.send_paced(&page, &pace).await.unwrap();
        let second = tokio::spawn({
            let (outbox, pace, page) = (outbox.clone(), pace.clone(), page.clone());
            async move { outbox.send_paced(&page, &pace).await }
        });
        tokio::time::sleep(Duration::from_millis(50)).await;
        assert!(
            !second.is_finished(),
            "the next page waits for the last to be taken"
        );
        outbox.send(&terminal(3)).await.unwrap();

        lanes.try_next().unwrap();
        tokio::time::timeout(Duration::from_secs(1), second)
            .await
            .unwrap()
            .unwrap()
            .unwrap();
        assert!(
            matches!(
                Frame::decode(&lanes.try_next().unwrap()).unwrap().0,
                Frame::Terminal(_)
            ),
            "an echo produced while a page waited goes ahead of the next page"
        );
    }

    #[test]
    fn the_largest_encoding_of_every_message_the_host_sends_fits_its_lane() {
        // Paths stop at Linux's PATH_MAX, and error text is cut to 1 KiB where it is produced.
        let path = "p".repeat(4096);
        let error = "e".repeat(1024);
        let interactive = [
            Frame::Pong,
            Frame::Control(ControlMessage::FileListError {
                path: path.clone(),
                error: error.clone(),
            }),
            Frame::Control(ControlMessage::FileDownloadError { path, error }),
        ];
        for frame in interactive {
            let len = frame.encode().unwrap().len();
            assert!(
                len <= INTERACTIVE_FRAME_CAP,
                "{frame:?} encodes to {len} bytes"
            );
        }
        let piece = Frame::FileTransfer(FileFrame {
            offset: u64::MAX,
            total_size: u64::MAX,
            data: vec![0; FILE_PIECE],
        });
        assert!(piece.encode().unwrap().len() <= BULK_FRAME_CAP);
    }

    #[tokio::test]
    async fn a_send_after_the_session_ends_fails() {
        let (outbox, lanes) = Outbox::new();
        drop(lanes);
        assert!(matches!(
            outbox.send(&terminal(1)).await,
            Err(OutboxError::Closed)
        ));
    }
}
