//! The session core: one task owning the hand-off, choosing each frame's path and never waiting
//! on a writer or a feature, so typing is never held behind a slow one.

use std::time::{Duration, Instant};

use mesh_protocol::direct::{Config, Handoff, Instruction, LeaveReason, Outcome, Path};
use mesh_protocol::{ControlMessage, Frame};
use tokio::sync::mpsc;
use tokio::task::JoinHandle;
use tracing::{debug, info, warn};

use super::outbox::Lanes;
use super::outlet::Outlet;
use crate::link::peer::{LinkCommand, LinkEvent, LinkHandle};

/// Frames handed to the features that may wait before the core stops reading its inputs.
pub(crate) const DISPATCH_BACKLOG: usize = 256;

/// What the browser is told when it offers a second link.
const REFUSAL: &str = "this session has made its one direct-link attempt";

/// The core's ends of everything around it.
pub(crate) struct Core {
    handoff: Handoff,
    start: Instant,
    helpers: Vec<String>,
    relay: Outlet,
    dispatch: Outlet,
    lanes: Lanes,
    link: Option<LinkHandle>,
    link_events_tx: mpsc::Sender<LinkEvent>,
    link_events: mpsc::Receiver<LinkEvent>,
    closing: Vec<JoinHandle<()>>,
    ended: bool,
}

impl Core {
    /// Returns a core sending on `relay`, handing frames to `dispatch` and taking from `lanes`.
    pub(crate) fn new(helpers: Vec<String>, relay: Outlet, dispatch: Outlet, lanes: Lanes) -> Self {
        let (link_events_tx, link_events) = mpsc::channel(64);
        Self {
            handoff: Handoff::new(Config::default()),
            start: Instant::now(),
            helpers,
            relay,
            dispatch,
            lanes,
            link: None,
            link_events_tx,
            link_events,
            closing: Vec::new(),
            ended: false,
        }
    }

    /// Runs until the relay closes or the hand-off ends the session, then releases the link.
    pub(crate) async fn run(mut self, mut relay_in: mpsc::Receiver<Vec<u8>>) {
        loop {
            let deadline = self
                .handoff
                .next_deadline()
                .map(|ms| tokio::time::Instant::from_std(self.start + Duration::from_millis(ms)));
            let reading = self.dispatch.queued() < DISPATCH_BACKLOG;
            let taking = self.handoff.can_take();
            let writer = self.writer();
            let ready = writer.as_ref().is_some_and(Outlet::is_empty);

            tokio::select! {
                biased;
                frame = relay_in.recv(), if reading => match frame {
                    Some(frame) => {
                        let out = self.handoff.from_relay(frame, self.now());
                        self.execute(out);
                    }
                    None => break,
                },
                Some(event) = self.link_events.recv(), if reading => self.on_link(event),
                () = sleep_until(deadline), if deadline.is_some() => {
                    let out = self.handoff.tick(self.now());
                    self.execute(out);
                }
                Some(queued) = self.lanes.next(), if taking && ready => self.take(queued.bytes),
                () = wait_empty(writer.as_ref()), if taking && !ready && writer.is_some() => {}
                () = self.dispatch.at_most(DISPATCH_BACKLOG - 1), if !reading => {}
            }
            if self.ended {
                break;
            }
        }
        if let Some(link) = self.link.take() {
            link.close().await;
        }
        for task in self.closing {
            task.await.ok();
        }
    }

    fn now(&self) -> u64 {
        u64::try_from(self.start.elapsed().as_millis()).unwrap_or(u64::MAX)
    }

    /// The writer the next session frame goes to, if the hand-off sends at all now.
    fn writer(&self) -> Option<Outlet> {
        match self.handoff.path() {
            Path::Relay => Some(self.relay.clone()),
            Path::Link => self.link.as_ref().map(|l| l.outlet.clone()),
            _ => None,
        }
    }

    fn take(&mut self, frame: Vec<u8>) {
        let out = self.handoff.send(frame, self.now());
        self.execute(out);
    }

    fn on_link(&mut self, event: LinkEvent) {
        let now = self.now();
        match event {
            LinkEvent::Answer(sdp) => self.relay_control(ControlMessage::DirectAnswer { sdp }),
            LinkEvent::LocalCandidate { candidate, mid } => {
                self.relay_control(ControlMessage::IceCandidate { candidate, mid })
            }
            LinkEvent::Open => {
                let out = self.handoff.link_open(now);
                self.execute(out);
            }
            LinkEvent::Piece(piece) => {
                let out = self.handoff.from_link(&piece, now);
                self.execute(out);
            }
            LinkEvent::Lost(reason) => {
                let out = self.handoff.link_lost(reason, now);
                self.execute(out);
            }
            LinkEvent::Failed(e) => {
                warn!(error = %e, "direct link: the offer could not be answered");
                let out = self.handoff.link_lost(LeaveReason::Closed, now);
                self.execute(out);
            }
        }
    }

    fn execute(&mut self, instructions: Vec<Instruction>) {
        for instruction in instructions {
            match instruction {
                Instruction::Relay(frame) => {
                    self.relay.push(frame);
                }
                Instruction::Link(piece) => {
                    if let Some(link) = &self.link {
                        link.outlet.push(piece);
                    }
                }
                Instruction::Deliver(frame) => {
                    self.dispatch.push(frame);
                }
                Instruction::Signal(msg) => self.on_signal(msg),
                Instruction::CloseLink => {
                    if let Some(link) = self.link.take() {
                        self.closing.push(tokio::spawn(link.close()));
                    }
                }
                Instruction::End => {
                    warn!(
                        "direct link: the browser never said what it received; ending the session"
                    );
                    self.ended = true;
                }
                Instruction::Outcome(outcome) => {
                    info!(outcome = outcome_name(outcome), "direct link")
                }
                Instruction::Left(reason) => info!(?reason, "direct link: left the link"),
                Instruction::Confirmed(bytes) => {
                    if let Some(link) = &self.link {
                        link.command(LinkCommand::Confirmed(bytes));
                    }
                }
                _ => {}
            }
        }
    }

    fn on_signal(&mut self, msg: ControlMessage) {
        match msg {
            ControlMessage::DirectOffer { sdp } => {
                if self.handoff.attempted() || self.link.is_some() {
                    self.relay_control(ControlMessage::DirectRefused {
                        reason: REFUSAL.to_string(),
                    });
                    return;
                }
                self.handoff.begin_attempt(self.now());
                let link = LinkHandle::start(self.helpers.clone(), self.link_events_tx.clone());
                link.command(LinkCommand::Offer(sdp));
                self.link = Some(link);
            }
            ControlMessage::IceCandidate { candidate, mid } => {
                if let Some(link) = &self.link {
                    link.command(LinkCommand::Candidate { candidate, mid });
                }
            }
            other => debug!(
                ?other,
                "direct link: a browser-bound message reached the host"
            ),
        }
    }

    fn relay_control(&mut self, msg: ControlMessage) {
        match Frame::Control(msg).encode() {
            Ok(frame) => {
                self.relay.push(frame);
            }
            Err(e) => warn!("direct link: a relay message did not encode: {e}"),
        }
    }
}

fn outcome_name(outcome: Outcome) -> &'static str {
    match outcome {
        Outcome::Direct => "direct",
        Outcome::NoRoute => "no direct route on this network",
        Outcome::Dropped => "the direct link dropped",
        _ => "unknown",
    }
}

async fn sleep_until(deadline: Option<tokio::time::Instant>) {
    if let Some(deadline) = deadline {
        tokio::time::sleep_until(deadline).await;
    }
}

async fn wait_empty(writer: Option<&Outlet>) {
    if let Some(writer) = writer {
        writer.empty().await;
    }
}
