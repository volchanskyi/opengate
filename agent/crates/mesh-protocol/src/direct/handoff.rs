//! The hand-off between relay and link for one side of a session: events in, instructions out,
//! no I/O. Each channel stays in order and exactly once across any change of path.

use std::collections::VecDeque;

use super::pieces::{split, Reassembler, MAX_LINK_FRAME};
use crate::codec::{FRAME_CONTROL, FRAME_DESKTOP, FRAME_FILE};
use crate::{ControlMessage, Frame};

/// Bytes a sender may have numbered on one channel of the link that the peer has not yet
/// confirmed.
pub const WINDOW_BYTES: usize = 4 * 1024 * 1024;

/// The typing channel's window when each job has a channel of its own; its lane caps it there.
pub const TYPING_WINDOW_BYTES: usize = 256 * 1024;

/// A receiver confirms this long after the first frame it has not yet confirmed.
pub const ACK_DELAY_MS: u64 = 250;

/// How long a side holding new frames waits for the peer's fall-back notice before the session
/// ends; re-sending without knowing what arrived could repeat a keystroke.
pub const PEER_FALLBACK_WAIT_MS: u64 = 10_000;

/// How long an attempt may take to put both directions on the link before both stay on the relay.
pub const ATTEMPT_BUDGET_MS: u64 = 8_000;

/// The numbers the hand-off works to; the defaults are the shipped values.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct Config {
    /// See [`WINDOW_BYTES`].
    pub window: usize,
    /// See [`TYPING_WINDOW_BYTES`].
    pub typing_window: usize,
    /// See [`ATTEMPT_BUDGET_MS`].
    pub attempt_budget_ms: u64,
}

impl Default for Config {
    fn default() -> Self {
        Self {
            window: WINDOW_BYTES,
            typing_window: TYPING_WINDOW_BYTES,
            attempt_budget_ms: ATTEMPT_BUDGET_MS,
        }
    }
}

/// One of the link's ordered channels.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
#[non_exhaustive]
pub enum Channel {
    /// Keystrokes, the terminal's replies, control messages and file listings; the one channel
    /// of a shared link.
    Typing,
    /// Screen pictures.
    Screen,
    /// File data, in both directions.
    Files,
}

impl Channel {
    /// Every channel, in the order a fall-back re-sends them.
    pub const ALL: [Channel; 3] = [Channel::Typing, Channel::Screen, Channel::Files];

    /// The channel's position in [`Channel::ALL`].
    pub fn index(self) -> usize {
        match self {
            Channel::Typing => 0,
            Channel::Screen => 1,
            Channel::Files => 2,
        }
    }

    /// The channel an encoded session frame rides when each job has a channel of its own: a
    /// listing page is file data, which typing never waits behind.
    pub fn of(frame: &[u8]) -> Channel {
        match frame.first() {
            Some(&FRAME_DESKTOP) => Channel::Screen,
            Some(&FRAME_FILE) => Channel::Files,
            Some(&FRAME_CONTROL) if is_listing_page(frame) => Channel::Files,
            _ => Channel::Typing,
        }
    }
}

/// A listing page's message after its map header, whose type the encoder writes as the first key.
const LISTING_PAGE_TYPE: &[u8] = b"\xa4type\xb0FileListResponse";

/// Whether `frame` is a control frame carrying a listing page: past the frame's type byte and
/// length, the message is a map of at most fifteen keys, its header one byte.
fn is_listing_page(frame: &[u8]) -> bool {
    frame
        .get(5..)
        .and_then(|message| message.split_first())
        .is_some_and(|(header, rest)| {
            (0x80..=0x8f).contains(header) && rest.starts_with(LISTING_PAGE_TYPE)
        })
}

/// How the link's channels carry a session.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
#[non_exhaustive]
pub enum Layout {
    /// One channel carries every frame, for a peer that opens only one.
    Shared,
    /// Typing, screen and files each ride a channel of their own.
    Separate,
}

/// Where this side sends session frames.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
#[non_exhaustive]
pub enum Path {
    /// On the relay: every frame goes there, unnumbered.
    Relay,
    /// On the link: frames are numbered and kept until the peer confirms them.
    Link,
    /// Left the link; new frames wait for the peer's fall-back notice, which says what to re-send.
    Holding,
    /// The peer stayed silent while this side held frames; the session cannot continue safely.
    Ended,
}

/// Why this side left the link.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
#[non_exhaustive]
pub enum LeaveReason {
    /// Nothing at all heard from the peer past the silence limit.
    Silence,
    /// The link failed or closed.
    Closed,
    /// The channel reported an error, a send among them.
    ChannelError,
    /// Both directions were not on the link within the attempt budget.
    Budget,
    /// A piece could not be put back into a frame.
    Reassembly,
    /// More link frames arrived ahead of the peer's switch notice than its windows allow.
    Overflow,
    /// The peer left the link first.
    PeerFellBack,
}

/// How an attempt ended, for logs and fleet counts.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
#[non_exhaustive]
pub enum Outcome {
    /// Both directions moved onto the link.
    Direct,
    /// The link was left before both directions were on it.
    NoRoute,
    /// The link carried the session and was then left.
    Dropped,
}

/// What the caller does next, in order.
#[derive(Debug, Clone, PartialEq)]
#[non_exhaustive]
pub enum Instruction {
    /// Write this whole frame to the relay.
    Relay(Vec<u8>),
    /// Write this piece to the link, on this channel.
    Link {
        /// The channel the piece belongs to.
        channel: Channel,
        /// The piece.
        piece: Vec<u8>,
    },
    /// Hand this session frame to the features; it is counted as delivered once handed.
    Deliver(Vec<u8>),
    /// A link-setup message for the signalling side: offer, answer, candidate or refusal.
    Signal(ControlMessage),
    /// Close the link.
    CloseLink,
    /// End the session.
    End,
    /// How the attempt ended.
    Outcome(Outcome),
    /// Why this side left the link.
    Left(LeaveReason),
    /// The peer confirmed this many bytes of link frames, for the delivery rate.
    Confirmed(usize),
}

/// What this side sent on one channel and the peer has not yet confirmed.
#[derive(Debug)]
struct Sending {
    next_number: u64,
    kept: VecDeque<(u64, Vec<u8>)>,
    unconfirmed: usize,
}

/// What this side received on one channel.
#[derive(Debug, Default)]
struct Receiving {
    held: VecDeque<Vec<u8>>,
    held_bytes: usize,
    reassembler: Reassembler,
    delivered: u64,
    unacked_bytes: usize,
}

/// One side's hand-off. The caller feeds it events and carries out the instructions it returns
/// before feeding the next event.
#[derive(Debug)]
pub struct Handoff {
    config: Config,
    path: Path,
    layout: Layout,

    attempted: bool,
    switched: bool,
    left_link: bool,
    direct: bool,
    budget_deadline: Option<u64>,
    holding_deadline: Option<u64>,

    sending: [Sending; 3],
    receiving: [Receiving; 3],
    peer_switched: bool,
    peer_fallback: Option<[u64; 3]>,
    first_unacked: Option<u64>,
}

impl Handoff {
    /// Returns a hand-off on the relay with no attempt made.
    pub fn new(config: Config) -> Self {
        let sending = || Sending {
            next_number: 1,
            kept: VecDeque::new(),
            unconfirmed: 0,
        };
        Self {
            config,
            path: Path::Relay,
            layout: Layout::Shared,
            attempted: false,
            switched: false,
            left_link: false,
            direct: false,
            budget_deadline: None,
            holding_deadline: None,
            sending: [sending(), sending(), sending()],
            receiving: Default::default(),
            peer_switched: false,
            peer_fallback: None,
            first_unacked: None,
        }
    }

    /// Where this side sends session frames now.
    pub fn path(&self) -> Path {
        self.path
    }

    /// How the link's channels carry the session once this side's link is open.
    pub fn layout(&self) -> Layout {
        self.layout
    }

    /// Whether this session has made its one attempt, so a second offer is refused.
    pub fn attempted(&self) -> bool {
        self.attempted
    }

    /// The peer's link frames handed to the features so far, on every channel.
    pub fn delivered(&self) -> u64 {
        self.receiving.iter().map(|r| r.delivered).sum()
    }

    /// This side's link frames the peer has not yet confirmed, on every channel.
    pub fn kept(&self) -> usize {
        self.sending.iter().map(|s| s.kept.len()).sum()
    }

    /// Whether the sending point may take another frame for `channel`: on the relay always, on
    /// the link while no more than that channel's window is unconfirmed.
    pub fn can_take(&self, channel: Channel) -> bool {
        match self.path {
            Path::Relay => true,
            Path::Link => {
                let slot = self.slot(channel);
                self.sending[slot].unconfirmed < self.window(slot)
            }
            Path::Holding | Path::Ended => false,
        }
    }

    /// The earliest moment [`Handoff::tick`] has work to do.
    pub fn next_deadline(&self) -> Option<u64> {
        [
            self.budget_deadline,
            self.holding_deadline,
            self.first_unacked.map(|t| t + ACK_DELAY_MS),
        ]
        .into_iter()
        .flatten()
        .min()
    }

    /// Starts the session's one attempt and its budget.
    pub fn begin_attempt(&mut self, now: u64) {
        if !self.attempted {
            self.attempted = true;
            self.budget_deadline = Some(now + self.config.attempt_budget_ms);
        }
    }

    /// This side's link is open with `layout`: its switch notice goes on the relay as its last
    /// session frame there, and later frames go on the link.
    pub fn link_open(&mut self, _now: u64, layout: Layout) -> Vec<Instruction> {
        if self.switched || self.left_link || self.path != Path::Relay {
            return Vec::new();
        }
        self.switched = true;
        self.layout = layout;
        self.path = Path::Link;
        let mut out = vec![Instruction::Relay(encode(ControlMessage::DirectSwitch))];
        self.note_direct(&mut out);
        out
    }

    /// Sends one session frame the sending point took; call only while [`Handoff::can_take`]
    /// holds for the frame's channel.
    pub fn send(&mut self, frame: Vec<u8>, _now: u64) -> Vec<Instruction> {
        match self.path {
            Path::Relay => vec![Instruction::Relay(frame)],
            Path::Link => {
                let slot = self.slot(Channel::of(&frame));
                let out = pieces(Channel::ALL[slot], &frame);
                let sending = &mut self.sending[slot];
                sending.unconfirmed += frame.len();
                sending.kept.push_back((sending.next_number, frame));
                sending.next_number += 1;
                out
            }
            Path::Holding | Path::Ended => Vec::new(),
        }
    }

    /// A whole frame from the relay: a notice moves this side, a link-setup message goes to the
    /// signalling side, anything else is session data, delivered in order.
    pub fn from_relay(&mut self, frame: Vec<u8>, now: u64) -> Vec<Instruction> {
        match Frame::decode(&frame) {
            Ok((Frame::Control(ControlMessage::DirectSwitch), _)) => self.peer_switched(now),
            Ok((
                Frame::Control(ControlMessage::DirectFallback {
                    received,
                    received_screen,
                    received_files,
                }),
                _,
            )) => self.peer_fell_back([received, received_screen, received_files], now),
            Ok((Frame::Control(msg), _)) if is_link_setup(&msg) => vec![Instruction::Signal(msg)],
            _ => vec![Instruction::Deliver(frame)],
        }
    }

    /// One piece from the link, on `channel`.
    pub fn from_link(&mut self, channel: Channel, piece: &[u8], now: u64) -> Vec<Instruction> {
        if self.left_link {
            return Vec::new();
        }
        let slot = channel.index();
        let frames = match self.receiving[slot].reassembler.push(piece) {
            Ok(frames) => frames,
            Err(_) => return self.leave(LeaveReason::Reassembly, now),
        };
        let mut out = Vec::new();
        for frame in frames {
            out.extend(self.link_frame(slot, frame, now));
            if self.left_link {
                break;
            }
        }
        out
    }

    /// This side's link failed, went silent, closed or errored.
    pub fn link_lost(&mut self, reason: LeaveReason, now: u64) -> Vec<Instruction> {
        self.leave(reason, now)
    }

    /// Runs the timers due at `now`: confirmations, the attempt budget and the peer's silence.
    pub fn tick(&mut self, now: u64) -> Vec<Instruction> {
        let mut out = Vec::new();
        if self.first_unacked.is_some_and(|t| now >= t + ACK_DELAY_MS) {
            out.extend(self.confirm());
        }
        if self.budget_deadline.is_some_and(|t| now >= t) {
            self.budget_deadline = None;
            if !(self.switched && self.peer_switched) {
                out.extend(self.leave(LeaveReason::Budget, now));
            }
        }
        if self.holding_deadline.is_some_and(|t| now >= t) {
            self.holding_deadline = None;
            self.path = Path::Ended;
            out.push(Instruction::End);
        }
        out
    }

    /// The channel slot a frame for `channel` uses under this side's layout.
    fn slot(&self, channel: Channel) -> usize {
        match self.layout {
            Layout::Separate => channel.index(),
            _ => 0,
        }
    }

    /// The window of a channel slot: typing's own when each job has a channel, else the shared one.
    fn window(&self, slot: usize) -> usize {
        if slot == 0 && self.layout == Layout::Separate {
            self.config.typing_window
        } else {
            self.config.window
        }
    }

    fn link_frame(&mut self, slot: usize, frame: Vec<u8>, now: u64) -> Vec<Instruction> {
        if let Ok((
            Frame::Control(ControlMessage::DirectAck {
                received,
                received_screen,
                received_files,
            }),
            _,
        )) = Frame::decode(&frame)
        {
            return self.confirmed([received, received_screen, received_files]);
        }
        if !self.peer_switched {
            let receiving = &mut self.receiving[slot];
            receiving.held_bytes += frame.len();
            receiving.held.push_back(frame);
            let held: usize = self.receiving.iter().map(|r| r.held_bytes).sum();
            if held > self.held_limit() {
                return self.leave(LeaveReason::Overflow, now);
            }
            return Vec::new();
        }
        self.deliver_link(slot, frame, now)
    }

    /// The most a side holds ahead of the peer's switch notice: the peer's three windows and one
    /// frame.
    fn held_limit(&self) -> usize {
        2 * self.config.window + self.config.typing_window + MAX_LINK_FRAME
    }

    fn deliver_link(&mut self, slot: usize, frame: Vec<u8>, now: u64) -> Vec<Instruction> {
        let ack_at = self.window(slot) / 4;
        let receiving = &mut self.receiving[slot];
        receiving.delivered += 1;
        receiving.unacked_bytes += frame.len();
        let due = receiving.unacked_bytes >= ack_at;
        self.first_unacked.get_or_insert(now);
        let mut out = vec![Instruction::Deliver(frame)];
        if due {
            out.extend(self.confirm());
        }
        out
    }

    fn confirm(&mut self) -> Vec<Instruction> {
        for receiving in &mut self.receiving {
            receiving.unacked_bytes = 0;
        }
        self.first_unacked = None;
        if self.left_link {
            return Vec::new();
        }
        let [received, received_screen, received_files] = self.delivered_each();
        pieces(
            Channel::Typing,
            &encode(ControlMessage::DirectAck {
                received,
                received_screen,
                received_files,
            }),
        )
    }

    fn delivered_each(&self) -> [u64; 3] {
        [0, 1, 2].map(|slot| self.receiving[slot].delivered)
    }

    fn confirmed(&mut self, received: [u64; 3]) -> Vec<Instruction> {
        let mut total = 0;
        for (sending, received) in self.sending.iter_mut().zip(received) {
            let mut bytes = 0;
            while sending.kept.front().is_some_and(|(n, _)| *n <= received) {
                if let Some((_, frame)) = sending.kept.pop_front() {
                    bytes += frame.len();
                }
            }
            sending.unconfirmed -= bytes;
            total += bytes;
        }
        if total == 0 {
            return Vec::new();
        }
        vec![Instruction::Confirmed(total)]
    }

    fn peer_switched(&mut self, now: u64) -> Vec<Instruction> {
        if self.peer_switched || self.left_link {
            return Vec::new();
        }
        self.peer_switched = true;
        let mut out = Vec::new();
        self.note_direct(&mut out);
        for slot in 0..self.receiving.len() {
            self.receiving[slot].held_bytes = 0;
            while let Some(frame) = self.receiving[slot].held.pop_front() {
                out.extend(self.deliver_link(slot, frame, now));
            }
        }
        out
    }

    fn note_direct(&mut self, out: &mut Vec<Instruction>) {
        if self.switched && self.peer_switched && !self.direct {
            self.direct = true;
            self.budget_deadline = None;
            out.push(Instruction::Outcome(Outcome::Direct));
        }
    }

    fn peer_fell_back(&mut self, received: [u64; 3], now: u64) -> Vec<Instruction> {
        self.peer_fallback = Some(received);
        if !self.left_link {
            return self.leave(LeaveReason::PeerFellBack, now);
        }
        if self.path == Path::Holding {
            return self.resend(received);
        }
        Vec::new()
    }

    fn leave(&mut self, reason: LeaveReason, now: u64) -> Vec<Instruction> {
        if self.left_link {
            return Vec::new();
        }
        self.left_link = true;
        self.attempted = true;
        self.budget_deadline = None;
        for receiving in &mut self.receiving {
            receiving.held.clear();
            receiving.held_bytes = 0;
            receiving.reassembler.reset();
            receiving.unacked_bytes = 0;
        }
        self.first_unacked = None;

        let outcome = if self.direct {
            Outcome::Dropped
        } else {
            Outcome::NoRoute
        };
        let [received, received_screen, received_files] = self.delivered_each();
        let mut out = vec![
            Instruction::CloseLink,
            Instruction::Relay(encode(ControlMessage::DirectFallback {
                received,
                received_screen,
                received_files,
            })),
            Instruction::Outcome(outcome),
            Instruction::Left(reason),
        ];
        if self.path == Path::Link {
            self.path = Path::Holding;
            self.holding_deadline = Some(now + PEER_FALLBACK_WAIT_MS);
        }
        if let Some(received) = self.peer_fallback {
            if self.path == Path::Holding {
                out.extend(self.resend(received));
            }
        }
        out
    }

    fn resend(&mut self, received: [u64; 3]) -> Vec<Instruction> {
        let mut out = Vec::new();
        for (sending, received) in self.sending.iter_mut().zip(received) {
            out.extend(
                sending
                    .kept
                    .drain(..)
                    .filter(|(n, _)| *n > received)
                    .map(|(_, frame)| Instruction::Relay(frame)),
            );
            sending.unconfirmed = 0;
        }
        self.path = Path::Relay;
        self.holding_deadline = None;
        out
    }
}

/// Whether a control message sets up the link: these travel on the relay outside the stream.
fn is_link_setup(msg: &ControlMessage) -> bool {
    matches!(
        msg,
        ControlMessage::DirectOffer { .. }
            | ControlMessage::DirectAnswer { .. }
            | ControlMessage::IceCandidate { .. }
            | ControlMessage::DirectRefused { .. }
    )
}

fn encode(msg: ControlMessage) -> Vec<u8> {
    Frame::Control(msg)
        .encode()
        .expect("a link notice always encodes")
}

fn pieces(channel: Channel, frame: &[u8]) -> Vec<Instruction> {
    split(frame)
        .map(|p| Instruction::Link {
            channel,
            piece: p.to_vec(),
        })
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::FileFrame;

    fn listing(more: bool) -> Vec<u8> {
        Frame::Control(ControlMessage::FileListResponse {
            path: "/var/log".to_string(),
            entries: vec![crate::FileEntry {
                name: "syslog".to_string(),
                is_dir: false,
                size: 4096,
                modified: 1_700_000_200,
            }],
            more,
        })
        .encode()
        .unwrap()
    }

    #[test]
    fn a_listing_page_rides_the_files_channel_and_its_request_rides_typing() {
        assert_eq!(Channel::of(&listing(true)), Channel::Files);
        assert_eq!(Channel::of(&listing(false)), Channel::Files);
        let request = Frame::Control(ControlMessage::FileListRequest {
            path: "/var/log".to_string(),
        })
        .encode()
        .unwrap();
        assert_eq!(Channel::of(&request), Channel::Typing);
        let refusal = Frame::Control(ControlMessage::FileListError {
            path: "/var/log".to_string(),
            error: "permission denied".to_string(),
        })
        .encode()
        .unwrap();
        assert_eq!(Channel::of(&refusal), Channel::Typing);
        assert_eq!(Channel::of(&file_frame(16)), Channel::Files);
    }

    fn file_frame(len: usize) -> Vec<u8> {
        Frame::FileTransfer(FileFrame {
            offset: 0,
            total_size: 0,
            data: vec![7; len],
        })
        .encode()
        .unwrap()
    }

    /// Feeds whole frames on `channel` ahead of the peer's switch notice until the side leaves.
    fn hold_until_left(side: &mut Handoff, channel: Channel, frame: &[u8]) -> Option<usize> {
        let mut held = 0;
        for _ in 0..200 {
            let out: Vec<Instruction> = split(frame)
                .flat_map(|piece| side.from_link(channel, piece, 0))
                .collect();
            if out.contains(&Instruction::Left(LeaveReason::Overflow)) {
                return Some(held);
            }
            held += frame.len();
        }
        None
    }

    #[test]
    fn frames_ahead_of_the_switch_notice_are_held_up_to_the_peers_windows_on_all_channels() {
        let config = Config::default();
        let limit = 2 * config.window + config.typing_window + MAX_LINK_FRAME;
        let frame = file_frame(256 * 1024);
        let mut side = Handoff::new(config);
        let mut held = 0;
        for channel in [Channel::Screen, Channel::Files] {
            for _ in 0..(config.window / frame.len()) {
                let out: Vec<Instruction> = split(&frame)
                    .flat_map(|piece| side.from_link(channel, piece, 0))
                    .collect();
                assert!(out.is_empty(), "within its window the side only holds");
                held += frame.len();
            }
        }
        let more = hold_until_left(&mut side, Channel::Typing, &frame).expect("the side leaves");
        assert!(held + more <= limit, "held {} past {limit}", held + more);
        assert!(
            held + more + frame.len() > limit,
            "left at {} under {limit}",
            held + more
        );
    }
}
