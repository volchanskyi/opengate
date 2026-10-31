//! The session's sending point: an interactive lane taken before the screen and files lanes,
//! which share their turn by bytes, each capped in bytes, so typing never waits behind file data.

use std::sync::Arc;

use mesh_protocol::direct::{Channel, MAX_PIECE};
use mesh_protocol::Frame;
use tokio::sync::{mpsc, OwnedSemaphorePermit, Semaphore};

use super::share::Share;

/// Largest interactive frame: one link piece.
pub const INTERACTIVE_FRAME_CAP: usize = MAX_PIECE;

/// Most bytes the interactive lane holds; a producer waits for room past it.
pub const INTERACTIVE_LANE_CAP: usize = 256 * 1024;

/// Bytes of file data in one bulk frame.
pub const FILE_PIECE: usize = 256 * 1024;

/// Largest bulk frame: one file piece and the frame's header and fields.
pub const BULK_FRAME_CAP: usize = FILE_PIECE + 64;

/// Most bytes each of the screen and files lanes holds: two pieces.
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
    screen: Lane,
    files: Lane,
}

/// The sending point's side of the lanes, holding the next screen and files frames it has read.
#[derive(Debug)]
pub(crate) struct Lanes {
    interactive: mpsc::UnboundedReceiver<Queued>,
    bulk: [mpsc::UnboundedReceiver<Queued>; 2],
    heads: [Option<Queued>; 2],
    share: Share,
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
        let (stx, srx) = mpsc::unbounded_channel();
        let (ftx, frx) = mpsc::unbounded_channel();
        let bulk = |name, tx| Lane {
            name,
            tx,
            room: Arc::new(Semaphore::new(BULK_LANE_CAP)),
            frame_cap: BULK_FRAME_CAP,
        };
        let outbox = Self {
            interactive: Lane {
                name: "interactive",
                tx: itx,
                room: Arc::new(Semaphore::new(INTERACTIVE_LANE_CAP)),
                frame_cap: INTERACTIVE_FRAME_CAP,
            },
            screen: bulk("screen", stx),
            files: bulk("files", ftx),
        };
        (
            outbox,
            Lanes {
                interactive: irx,
                bulk: [srx, frx],
                heads: [None, None],
                share: Share::default(),
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

    /// Which lane `bytes` belongs to: the one of the link channel it rides.
    fn lane(&self, bytes: &[u8]) -> &Lane {
        match Channel::of(bytes) {
            Channel::Screen => &self.screen,
            Channel::Files => &self.files,
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
    /// The next frame from the lanes `open` names in [`Channel::ALL`] order: interactive first, then
    /// screen or files by [`Share`]; `None` once every open lane is closed and empty.
    pub(crate) async fn next(&mut self, open: [bool; 3]) -> Option<Queued> {
        loop {
            if open[0] {
                if let Ok(queued) = self.interactive.try_recv() {
                    return Some(queued);
                }
            }
            for slot in 0..2 {
                if self.heads[slot].is_none() {
                    self.heads[slot] = self.bulk[slot].try_recv().ok();
                }
            }
            let waiting = [0, 1].map(|slot| open[slot + 1] && self.heads[slot].is_some());
            if let Some(slot) = self.share.pick(waiting) {
                let queued = self.heads[slot].take()?;
                self.share.sent(slot, queued.bytes.len());
                return Some(queued);
            }
            let [screen, files] = &mut self.bulk;
            let (screen_empty, files_empty) = (self.heads[0].is_none(), self.heads[1].is_none());
            tokio::select! {
                biased;
                Some(queued) = self.interactive.recv(), if open[0] => return Some(queued),
                Some(queued) = screen.recv(), if open[1] && screen_empty => {
                    self.heads[0] = Some(queued);
                }
                Some(queued) = files.recv(), if open[2] && files_empty => {
                    self.heads[1] = Some(queued);
                }
                else => return None,
            }
        }
    }

    /// The next frame from every lane, if one is queued now.
    #[cfg(test)]
    pub(crate) fn try_next(&mut self) -> Option<Vec<u8>> {
        self.try_next_from([true; 3])
    }

    /// The next frame from the lanes whose channel is `open`, if one is queued now.
    #[cfg(test)]
    pub(crate) fn try_next_from(&mut self, open: [bool; 3]) -> Option<Vec<u8>> {
        use futures_util::FutureExt;
        self.next(open).now_or_never().flatten().map(|q| q.bytes)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use mesh_protocol::{ControlMessage, DesktopFrame, FileFrame, FrameEncoding, TerminalFrame};
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

    fn picture(len: usize) -> Frame {
        Frame::Desktop(DesktopFrame {
            sequence: 0,
            x: 0,
            y: 0,
            width: 0,
            height: 0,
            encoding: FrameEncoding::Raw,
            data: vec![3; len],
        })
    }

    fn kind(bytes: &[u8]) -> &'static str {
        match Frame::decode(bytes).unwrap().0 {
            Frame::Desktop(_) => "screen",
            Frame::FileTransfer(_) => "files",
            _ => "interactive",
        }
    }

    #[tokio::test]
    async fn screen_pictures_and_file_pieces_each_take_a_lane_of_their_own() {
        let (outbox, mut lanes) = Outbox::new();
        outbox.send(&file(1000)).await.unwrap();
        outbox.send(&picture(1000)).await.unwrap();
        outbox.send(&terminal(5)).await.unwrap();

        assert_eq!(
            lanes.try_next_from([false, false, true]).map(|b| kind(&b)),
            Some("files")
        );
        assert!(
            lanes.try_next_from([false, false, true]).is_none(),
            "a closed lane's frames wait"
        );
        assert_eq!(
            lanes.try_next_from([false, true, false]).map(|b| kind(&b)),
            Some("screen")
        );
        assert_eq!(lanes.try_next().map(|b| kind(&b)), Some("interactive"));
    }

    #[tokio::test]
    async fn the_screen_and_files_lanes_take_turns_by_bytes() {
        let (outbox, mut lanes) = Outbox::new();
        for _ in 0..8 {
            outbox.send(&picture(50_000)).await.unwrap();
        }
        for _ in 0..4 {
            outbox.send(&file(100_000)).await.unwrap();
        }
        let (mut screen_bytes, mut file_bytes) = (0usize, 0usize);
        let mut order = Vec::new();
        while let Some(bytes) = lanes.try_next() {
            match kind(&bytes) {
                "screen" => screen_bytes += bytes.len(),
                _ => file_bytes += bytes.len(),
            }
            order.push(kind(&bytes));
            assert!(
                screen_bytes.abs_diff(file_bytes) <= 100_100,
                "after {order:?}: screen {screen_bytes} against files {file_bytes}"
            );
        }
        assert_eq!(order.len(), 12);
        assert_eq!(&order[..3], ["screen", "files", "screen"]);
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
        assert!(matches!(err, OutboxError::TooLarge { lane: "files", .. }));
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

        assert!(
            matches!(
                Frame::decode(&lanes.try_next().unwrap()).unwrap().0,
                Frame::Terminal(_)
            ),
            "an echo goes ahead of a page queued before it"
        );
        assert!(matches!(
            Frame::decode(&lanes.try_next().unwrap()).unwrap().0,
            Frame::Control(ControlMessage::FileListResponse { .. })
        ));
        tokio::time::timeout(Duration::from_secs(1), second)
            .await
            .unwrap()
            .unwrap()
            .unwrap();
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
