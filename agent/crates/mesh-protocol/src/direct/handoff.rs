//! The hand-off between relay and link for one side of a session: events in, instructions out,
//! no I/O. Each direction stays in order and exactly once across any change of path.

use std::collections::VecDeque;

use super::pieces::{split, Reassembler, MAX_LINK_FRAME};
use crate::{ControlMessage, Frame};

/// Bytes a sender may have numbered on the link that the peer has not yet confirmed.
pub const WINDOW_BYTES: usize = 4 * 1024 * 1024;

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
    /// See [`ATTEMPT_BUDGET_MS`].
    pub attempt_budget_ms: u64,
}

impl Default for Config {
    fn default() -> Self {
        Self {
            window: WINDOW_BYTES,
            attempt_budget_ms: ATTEMPT_BUDGET_MS,
        }
    }
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
    /// More link frames arrived ahead of the peer's switch notice than its window allows.
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
    /// Write this piece to the link.
    Link(Vec<u8>),
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

/// One side's hand-off. The caller feeds it events and carries out the instructions it returns
/// before feeding the next event.
#[derive(Debug)]
pub struct Handoff {
    config: Config,
    path: Path,

    attempted: bool,
    switched: bool,
    left_link: bool,
    direct: bool,
    budget_deadline: Option<u64>,
    holding_deadline: Option<u64>,

    next_number: u64,
    kept: VecDeque<(u64, Vec<u8>)>,
    unconfirmed: usize,

    peer_switched: bool,
    peer_fallback: Option<u64>,
    held: VecDeque<Vec<u8>>,
    held_bytes: usize,
    reassembler: Reassembler,
    delivered: u64,
    unacked_bytes: usize,
    first_unacked: Option<u64>,
}

impl Handoff {
    /// Returns a hand-off on the relay with no attempt made.
    pub fn new(config: Config) -> Self {
        Self {
            config,
            path: Path::Relay,
            attempted: false,
            switched: false,
            left_link: false,
            direct: false,
            budget_deadline: None,
            holding_deadline: None,
            next_number: 1,
            kept: VecDeque::new(),
            unconfirmed: 0,
            peer_switched: false,
            peer_fallback: None,
            held: VecDeque::new(),
            held_bytes: 0,
            reassembler: Reassembler::new(),
            delivered: 0,
            unacked_bytes: 0,
            first_unacked: None,
        }
    }

    /// Where this side sends session frames now.
    pub fn path(&self) -> Path {
        self.path
    }

    /// Whether this session has made its one attempt, so a second offer is refused.
    pub fn attempted(&self) -> bool {
        self.attempted
    }

    /// The peer's link frames handed to the features so far.
    pub fn delivered(&self) -> u64 {
        self.delivered
    }

    /// This side's link frames the peer has not yet confirmed.
    pub fn kept(&self) -> usize {
        self.kept.len()
    }

    /// Whether the sending point may take another frame: on the relay always, on the link while
    /// no more than the window is unconfirmed.
    pub fn can_take(&self) -> bool {
        match self.path {
            Path::Relay => true,
            Path::Link => self.unconfirmed < self.config.window,
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

    /// This side's link is open: its switch notice goes on the relay as its last session frame
    /// there, and later frames go on the link.
    pub fn link_open(&mut self, _now: u64) -> Vec<Instruction> {
        if self.switched || self.left_link || self.path != Path::Relay {
            return Vec::new();
        }
        self.switched = true;
        self.path = Path::Link;
        let mut out = vec![Instruction::Relay(encode(ControlMessage::DirectSwitch))];
        self.note_direct(&mut out);
        out
    }

    /// Sends one session frame the sending point took; call only while [`Handoff::can_take`].
    pub fn send(&mut self, frame: Vec<u8>, _now: u64) -> Vec<Instruction> {
        match self.path {
            Path::Relay => vec![Instruction::Relay(frame)],
            Path::Link => {
                let out = pieces(&frame);
                self.unconfirmed += frame.len();
                self.kept.push_back((self.next_number, frame));
                self.next_number += 1;
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
            Ok((Frame::Control(ControlMessage::DirectFallback { received }), _)) => {
                self.peer_fell_back(received, now)
            }
            Ok((Frame::Control(msg), _)) if is_link_setup(&msg) => vec![Instruction::Signal(msg)],
            _ => vec![Instruction::Deliver(frame)],
        }
    }

    /// One piece from the link.
    pub fn from_link(&mut self, piece: &[u8], now: u64) -> Vec<Instruction> {
        if self.left_link {
            return Vec::new();
        }
        let frames = match self.reassembler.push(piece) {
            Ok(frames) => frames,
            Err(_) => return self.leave(LeaveReason::Reassembly, now),
        };
        let mut out = Vec::new();
        for frame in frames {
            out.extend(self.link_frame(frame, now));
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

    fn link_frame(&mut self, frame: Vec<u8>, now: u64) -> Vec<Instruction> {
        if let Ok((Frame::Control(ControlMessage::DirectAck { received }), _)) =
            Frame::decode(&frame)
        {
            return self.confirmed(received);
        }
        if !self.peer_switched {
            self.held_bytes += frame.len();
            self.held.push_back(frame);
            if self.held_bytes > self.config.window + MAX_LINK_FRAME {
                return self.leave(LeaveReason::Overflow, now);
            }
            return Vec::new();
        }
        self.deliver_link(frame, now)
    }

    fn deliver_link(&mut self, frame: Vec<u8>, now: u64) -> Vec<Instruction> {
        self.delivered += 1;
        self.unacked_bytes += frame.len();
        self.first_unacked.get_or_insert(now);
        let mut out = vec![Instruction::Deliver(frame)];
        if self.unacked_bytes >= self.config.window / 4 {
            out.extend(self.confirm());
        }
        out
    }

    fn confirm(&mut self) -> Vec<Instruction> {
        self.unacked_bytes = 0;
        self.first_unacked = None;
        if self.left_link {
            return Vec::new();
        }
        pieces(&encode(ControlMessage::DirectAck {
            received: self.delivered,
        }))
    }

    fn confirmed(&mut self, received: u64) -> Vec<Instruction> {
        let mut bytes = 0;
        while self.kept.front().is_some_and(|(n, _)| *n <= received) {
            if let Some((_, frame)) = self.kept.pop_front() {
                bytes += frame.len();
            }
        }
        self.unconfirmed -= bytes;
        if bytes == 0 {
            return Vec::new();
        }
        vec![Instruction::Confirmed(bytes)]
    }

    fn peer_switched(&mut self, now: u64) -> Vec<Instruction> {
        if self.peer_switched || self.left_link {
            return Vec::new();
        }
        self.peer_switched = true;
        let mut out = Vec::new();
        self.note_direct(&mut out);
        self.held_bytes = 0;
        while let Some(frame) = self.held.pop_front() {
            out.extend(self.deliver_link(frame, now));
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

    fn peer_fell_back(&mut self, received: u64, now: u64) -> Vec<Instruction> {
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
        self.held.clear();
        self.held_bytes = 0;
        self.reassembler.reset();
        self.unacked_bytes = 0;
        self.first_unacked = None;

        let outcome = if self.direct {
            Outcome::Dropped
        } else {
            Outcome::NoRoute
        };
        let mut out = vec![
            Instruction::CloseLink,
            Instruction::Relay(encode(ControlMessage::DirectFallback {
                received: self.delivered,
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

    fn resend(&mut self, received: u64) -> Vec<Instruction> {
        let out = self
            .kept
            .drain(..)
            .filter(|(n, _)| *n > received)
            .map(|(_, frame)| Instruction::Relay(frame))
            .collect();
        self.unconfirmed = 0;
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

fn pieces(frame: &[u8]) -> Vec<Instruction> {
    split(frame)
        .map(|p| Instruction::Link(p.to_vec()))
        .collect()
}
