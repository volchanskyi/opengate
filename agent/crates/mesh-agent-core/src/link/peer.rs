//! The host's end of a session's direct link: one peer connection with a channel per job, or one
//! shared channel, answering the browser's offer with the helpers the server named.

use std::sync::{Arc, Mutex, OnceLock};
use std::time::{Duration, Instant};

use bytes::BytesMut;
use mesh_protocol::direct::{CandidateFilter, Channel, Layout, LeaveReason, MAX_PIECE};
use rtc::peer_connection::configuration::setting_engine::SctpMaxMessageSize;
use tokio::sync::mpsc;
use tokio::task::JoinHandle;
use tracing::{debug, info, warn};
use webrtc::data_channel::{DataChannel, DataChannelEvent, RTCDataChannelState};
use webrtc::peer_connection::{
    PeerConnection, PeerConnectionBuilder, PeerConnectionEventHandler, RTCConfigurationBuilder,
    RTCIceCandidateInit, RTCIceConnectionState, RTCIceServer, RTCPeerConnectionIceEvent,
    RTCSessionDescription, SettingEngineBuilder, StatsSelector,
};

use super::pacing::{self, SendLimit};
use crate::session::outlet::{Outlet, OutletReader};
use crate::session::share::{Next, Share};

/// How often the host checks its peer on the link with a request the peer's stack answers.
pub const CHECK_EVERY: Duration = Duration::from_secs(1);

/// How long the host hears nothing at all before it calls the link silent: three unanswered checks.
pub const SILENCE_LIMIT: Duration = Duration::from_secs(3);

/// Label of the one ordered channel a browser without a channel per job opens.
pub const SHARED_LABEL: &str = "session";

/// Labels of the typing, screen and files channels, in [`Channel::ALL`] order.
pub const CHANNEL_LABELS: [&str; 3] = ["typing", "screen", "files"];

/// DTLS records a late one may trail and still be read, as Chrome's receiver allows: a router that
/// reorders under load lets a packet fall behind more than RFC 6347's default 64.
const DTLS_REPLAY_WINDOW: usize = 256;

/// How often the writer re-reads the channel's outstanding bytes while it waits at the limit.
const LIMIT_POLL: Duration = Duration::from_millis(2);

/// What the session core asks of the link.
#[derive(Debug)]
pub(crate) enum LinkCommand {
    /// The browser's offer.
    Offer(String),
    /// One of the browser's candidates.
    Candidate { candidate: String, mid: String },
    /// The browser confirmed this many bytes of link frames.
    Confirmed(usize),
}

/// What the link tells the session core.
#[derive(Debug)]
pub(crate) enum LinkEvent {
    /// The host's answer to the offer.
    Answer(String),
    /// One of the host's own candidates, for the browser.
    LocalCandidate { candidate: String, mid: String },
    /// Every channel of the layout is open.
    Open(Layout),
    /// One piece from the browser, on a channel.
    Piece { channel: Channel, piece: Vec<u8> },
    /// The link went silent, failed, closed or errored.
    Lost(LeaveReason),
    /// The offer could not be answered.
    Failed(String),
}

/// The session core's handle on its one link.
#[derive(Debug)]
pub(crate) struct LinkHandle {
    commands: mpsc::UnboundedSender<LinkCommand>,
    /// Where the core pushes each channel's pieces, in [`Channel::ALL`] order; the link's writers
    /// drain them, screen and files together at the run-time limit.
    pub(crate) outlets: [Outlet; 3],
    task: JoinHandle<()>,
}

impl LinkHandle {
    /// Starts a link asking `helpers` for the host's public address; events go to `events`.
    pub(crate) fn start(helpers: Vec<String>, events: mpsc::Sender<LinkEvent>) -> Self {
        let (commands, command_rx) = mpsc::unbounded_channel();
        let [(typing, t), (screen, s), (files, f)] = [(); 3].map(|()| Outlet::new());
        let task = tokio::spawn(run(helpers, command_rx, [t, s, f], events));
        Self {
            commands,
            outlets: [typing, screen, files],
            task,
        }
    }

    /// Passes a command to the link; one sent after it closed is dropped.
    pub(crate) fn command(&self, command: LinkCommand) {
        self.commands.send(command).ok();
    }

    /// Closes the link and waits until its socket and tasks are gone.
    pub(crate) async fn close(self) {
        drop(self.commands);
        drop(self.outlets);
        if let Err(e) = self.task.await {
            debug!("link task ended abnormally: {e}");
        }
    }
}

/// The kind of address a candidate line names — host, srflx, prflx or relay — never the address.
pub(crate) fn candidate_kind(candidate: &str) -> &str {
    let mut fields = candidate.split_ascii_whitespace();
    while let Some(field) = fields.next() {
        if field == "typ" {
            return fields.next().unwrap_or("unknown");
        }
    }
    "unknown"
}

/// A channel the browser opened: one of [`CHANNEL_LABELS`] by position, or `None` if shared.
type Arrived = (Option<usize>, Arc<dyn DataChannel>);

struct Handler {
    events: mpsc::Sender<LinkEvent>,
    channels: mpsc::Sender<Arrived>,
    /// The media section the browser's offer names; Firefox refuses a candidate naming any other.
    mid: Arc<OnceLock<String>>,
}

/// The media section an offer names, from its first `a=mid:` line.
fn media_id(sdp: &str) -> Option<&str> {
    sdp.lines()
        .find_map(|line| line.strip_prefix("a=mid:"))
        .map(str::trim)
}

#[async_trait::async_trait]
impl PeerConnectionEventHandler for Handler {
    async fn on_ice_candidate(&self, event: RTCPeerConnectionIceEvent) {
        let Ok(json) = event.candidate.to_json() else {
            return;
        };
        info!(
            kind = candidate_kind(&json.candidate),
            "direct link: local candidate"
        );
        self.events
            .send(LinkEvent::LocalCandidate {
                candidate: json.candidate,
                mid: self.mid.get().cloned().unwrap_or_default(),
            })
            .await
            .ok();
    }

    async fn on_ice_connection_state_change(&self, state: RTCIceConnectionState) {
        debug!(%state, "direct link: ICE state");
        let reason = match state {
            RTCIceConnectionState::Disconnected => LeaveReason::Silence,
            RTCIceConnectionState::Failed | RTCIceConnectionState::Closed => LeaveReason::Closed,
            _ => return,
        };
        self.events.send(LinkEvent::Lost(reason)).await.ok();
    }

    async fn on_data_channel(&self, channel: Arc<dyn DataChannel>) {
        let which = match channel.label().await {
            Ok(label) if label == SHARED_LABEL => Some(None),
            Ok(label) => CHANNEL_LABELS.iter().position(|l| *l == label).map(Some),
            Err(_) => None,
        };
        match which {
            Some(which) => {
                self.channels.send((which, channel)).await.ok();
            }
            None => {
                warn!("direct link: closing a channel the session does not use");
                channel.close().await.ok();
            }
        }
    }
}

async fn build(
    helpers: Vec<String>,
    events: mpsc::Sender<LinkEvent>,
    channels: mpsc::Sender<Arrived>,
    mid: Arc<OnceLock<String>>,
) -> Result<impl PeerConnection, String> {
    // A late packet is told from a lost one by time, and the channels share the path by bytes.
    let settings = SettingEngineBuilder::new()
        .with_ice_timeouts(Some(SILENCE_LIMIT), Some(SILENCE_LIMIT), Some(CHECK_EVERY))
        .with_sctp_max_message_size(SctpMaxMessageSize::Bounded(MAX_PIECE as u32))
        .with_sctp_rack_tlp(true)
        .with_sctp_weighted_fair_queueing(true)
        .with_dtls_replay_protection_window(DTLS_REPLAY_WINDOW)
        .build();
    let config = RTCConfigurationBuilder::new()
        .with_ice_servers(
            helpers
                .into_iter()
                .map(|url| RTCIceServer {
                    urls: vec![url],
                    ..Default::default()
                })
                .collect(),
        )
        .build();
    PeerConnectionBuilder::new()
        .with_configuration(config)
        .with_setting_engine(settings)
        .with_handler(Arc::new(Handler {
            events,
            channels,
            mid,
        }))
        .with_udp_addrs(vec!["0.0.0.0:0".to_string()])
        .with_data_channel_send_buffer_limit(pacing::CEILING + MAX_PIECE)
        .build()
        .await
        .map_err(|e| format!("peer connection: {e}"))
}

/// The channels the browser has opened so far, and which of them report open.
#[derive(Default)]
struct Channels {
    layout: Option<Layout>,
    arrived: [Option<Arc<dyn DataChannel>>; 3],
    open: [bool; 3],
}

impl Channels {
    /// Takes a channel that arrived, returning the slot it fills; a channel of the other layout
    /// than the first one's is refused.
    fn arrive(&mut self, (which, channel): Arrived) -> Option<usize> {
        let (layout, slot) = match which {
            None => (Layout::Shared, 0),
            Some(slot) => (Layout::Separate, slot),
        };
        if self.layout.is_some_and(|l| l != layout) || self.arrived[slot].is_some() {
            return None;
        }
        self.layout = Some(layout);
        self.arrived[slot] = Some(channel);
        Some(slot)
    }

    /// The layout, once every channel it needs is open.
    fn ready(&self) -> Option<Layout> {
        let layout = self.layout?;
        let needed = if layout == Layout::Separate { 3 } else { 1 };
        self.open[..needed].iter().all(|o| *o).then_some(layout)
    }
}

async fn run(
    helpers: Vec<String>,
    mut commands: mpsc::UnboundedReceiver<LinkCommand>,
    readers: [OutletReader; 3],
    events: mpsc::Sender<LinkEvent>,
) {
    let (channel_tx, mut channel_rx) = mpsc::channel(3);
    let (opened_tx, mut opened_rx) = mpsc::channel(3);
    let mid = Arc::new(OnceLock::new());
    let pc = match build(helpers, events.clone(), channel_tx, mid.clone()).await {
        Ok(pc) => pc,
        Err(e) => {
            events.send(LinkEvent::Failed(e)).await.ok();
            return;
        }
    };

    let limit = Arc::new(Mutex::new(SendLimit::new(Instant::now())));
    let mut filter = CandidateFilter::new();
    let mut remote_set = false;
    let mut early: Vec<RTCIceCandidateInit> = Vec::new();
    let mut readers = Some(readers);
    let mut channels = Channels::default();
    let mut tasks: Vec<JoinHandle<()>> = Vec::new();
    let mut stats = tokio::time::interval(pacing::INTERVAL);
    let mut answered = None;

    loop {
        tokio::select! {
            command = commands.recv() => match command {
                Some(LinkCommand::Offer(sdp)) => {
                    if let Some(id) = media_id(&sdp) {
                        mid.set(id.to_owned()).ok();
                    }
                    match answer(&pc, filter.filter_sdp(&sdp), &mut early).await {
                        Ok(answer) => {
                            remote_set = true;
                            events.send(LinkEvent::Answer(answer)).await.ok();
                        }
                        Err(e) => {
                            events.send(LinkEvent::Failed(e)).await.ok();
                        }
                    }
                }
                Some(LinkCommand::Candidate { candidate, mid }) => {
                    if !filter.admit(&candidate) {
                        debug!(kind = candidate_kind(&candidate), "direct link: candidate ignored");
                        continue;
                    }
                    let init = RTCIceCandidateInit {
                        candidate,
                        sdp_mid: Some(mid),
                        ..Default::default()
                    };
                    if !remote_set {
                        early.push(init);
                    } else if let Err(e) = pc.add_ice_candidate(init).await {
                        debug!("direct link: candidate refused: {e}");
                    }
                }
                Some(LinkCommand::Confirmed(bytes)) => {
                    if let Ok(mut limit) = limit.lock() {
                        limit.confirmed(bytes, Instant::now());
                    }
                }
                None => break,
            },
            Some(arrived) = channel_rx.recv() => {
                let channel = arrived.1.clone();
                let Some(slot) = channels.arrive(arrived) else {
                    warn!("direct link: closing a channel outside the session's layout");
                    channel.close().await.ok();
                    continue;
                };
                if channel.ready_state().await.is_ok_and(|s| s == RTCDataChannelState::Open) {
                    channels.open[slot] = true;
                }
                tasks.push(tokio::spawn(pump(
                    channel,
                    Channel::ALL[slot],
                    events.clone(),
                    opened_tx.clone(),
                )));
                start_writers(&channels, &mut readers, &limit, &events, &mut tasks).await;
            }
            Some(slot) = opened_rx.recv() => {
                channels.open[slot] = true;
                start_writers(&channels, &mut readers, &limit, &events, &mut tasks).await;
            }
            _ = stats.tick() => {
                let check = latest_check(&pc).await;
                if let Ok(mut limit) = limit.lock() {
                    let now = Instant::now();
                    if let Some(rtt) = check.and_then(|check| new_check(&mut answered, check)) {
                        limit.round_trip(rtt, now);
                    }
                    limit.recompute(now);
                }
            }
        }
    }

    if let Err(e) = pc.close().await {
        debug!("direct link: close: {e}");
    }
    for task in tasks {
        task.abort();
        task.await.ok();
    }
}

/// Applies the offer, with the candidates that arrived before it, and returns the answer.
async fn answer(
    pc: &impl PeerConnection,
    offer: String,
    early: &mut Vec<RTCIceCandidateInit>,
) -> Result<String, String> {
    let offer = RTCSessionDescription::offer(offer).map_err(|e| format!("offer: {e}"))?;
    pc.set_remote_description(offer)
        .await
        .map_err(|e| format!("set remote description: {e}"))?;
    for candidate in early.drain(..) {
        if let Err(e) = pc.add_ice_candidate(candidate).await {
            debug!("direct link: early candidate refused: {e}");
        }
    }
    let answer = pc
        .create_answer(None)
        .await
        .map_err(|e| format!("create answer: {e}"))?;
    pc.set_local_description(answer)
        .await
        .map_err(|e| format!("set local description: {e}"))?;
    pc.local_description()
        .await
        .map(|d| d.sdp)
        .ok_or_else(|| "no local description".to_string())
}

/// The nominated pair's count of answered checks and its latest check's round trip, when the link
/// has one.
async fn latest_check(pc: &impl PeerConnection) -> Option<(u64, Duration)> {
    let report = pc.get_stats(Instant::now(), StatsSelector::None).await;
    report
        .candidate_pairs()
        .filter(|pair| pair.nominated && pair.current_round_trip_time > 0.0)
        .map(|pair| {
            let rtt = Duration::from_secs_f64(pair.current_round_trip_time);
            (pair.responses_received, rtt)
        })
        .min_by_key(|(_, rtt)| *rtt)
}

/// The round trip of `check` when it is one `answered` has not yet counted: the statistics repeat a
/// check's reading until the pair answers the next.
fn new_check(answered: &mut Option<u64>, (count, rtt): (u64, Duration)) -> Option<Duration> {
    (answered.replace(count) != Some(count)).then_some(rtt)
}

/// Once every channel of the layout is open, says so and starts its writer, which holds every
/// channel it feeds under the limit together.
async fn start_writers(
    channels: &Channels,
    readers: &mut Option<[OutletReader; 3]>,
    limit: &Arc<Mutex<SendLimit>>,
    events: &mpsc::Sender<LinkEvent>,
    tasks: &mut Vec<JoinHandle<()>>,
) {
    let (Some(layout), [Some(t), s, f]) = (channels.ready(), channels.arrived.clone()) else {
        return;
    };
    let Some([typing, screen, files]) = readers.take() else {
        return;
    };
    events.send(LinkEvent::Open(layout)).await.ok();
    match (layout, s, f) {
        (Layout::Separate, Some(s), Some(f)) => {
            tasks.push(tokio::spawn(write_separate(
                [t, s, f],
                [typing, screen, files],
                limit.clone(),
                events.clone(),
            )));
        }
        _ => tasks.push(tokio::spawn(write(
            t,
            typing,
            limit.clone(),
            events.clone(),
        ))),
    }
}

/// Turns one channel's events into link events until it closes; its opening goes to `opened`.
async fn pump(
    channel: Arc<dyn DataChannel>,
    which: Channel,
    events: mpsc::Sender<LinkEvent>,
    opened: mpsc::Sender<usize>,
) {
    while let Some(event) = channel.poll().await {
        let event = match event {
            DataChannelEvent::OnOpen => {
                opened.send(which.index()).await.ok();
                continue;
            }
            DataChannelEvent::OnMessage(message) => LinkEvent::Piece {
                channel: which,
                piece: message.data.to_vec(),
            },
            DataChannelEvent::OnError => LinkEvent::Lost(LeaveReason::ChannelError),
            DataChannelEvent::OnClose => LinkEvent::Lost(LeaveReason::Closed),
            _ => continue,
        };
        if events.send(event).await.is_err() {
            return;
        }
    }
    events.send(LinkEvent::Lost(LeaveReason::Closed)).await.ok();
}

/// Sends a shared channel's pieces, holding its outstanding bytes under the limit.
async fn write(
    channel: Arc<dyn DataChannel>,
    mut reader: OutletReader,
    limit: Arc<Mutex<SendLimit>>,
    events: mpsc::Sender<LinkEvent>,
) {
    while let Some(piece) = reader.next().await {
        loop {
            let allowed = limit.lock().map_or(pacing::FLOOR, |l| l.current());
            match channel.outstanding_bytes().await {
                Ok(0) => break,
                Ok(out) if out + piece.len() <= allowed => break,
                Ok(_) => {
                    tokio::time::sleep(LIMIT_POLL).await;
                    if let Ok(mut limit) = limit.lock() {
                        limit.blocked(LIMIT_POLL);
                    }
                }
                Err(_) => {
                    events
                        .send(LinkEvent::Lost(LeaveReason::ChannelError))
                        .await
                        .ok();
                    return;
                }
            }
        }
        if channel.send(BytesMut::from(&piece[..])).await.is_err() {
            events
                .send(LinkEvent::Lost(LeaveReason::ChannelError))
                .await
                .ok();
            return;
        }
        reader.written();
    }
}

/// Sends the typing, screen and files channels' pieces by [`Share::link_next`], holding the three
/// channels' outstanding bytes together under the limit.
async fn write_separate(
    channels: [Arc<dyn DataChannel>; 3],
    mut readers: [OutletReader; 3],
    limit: Arc<Mutex<SendLimit>>,
    events: mpsc::Sender<LinkEvent>,
) {
    let mut heads: [Option<Vec<u8>>; 3] = [None, None, None];
    let mut share = Share::default();
    loop {
        for (head, reader) in heads.iter_mut().zip(readers.iter_mut()) {
            if head.is_none() {
                *head = reader.try_next();
            }
        }
        if heads.iter().all(Option::is_none) {
            let [typing, screen, files] = &mut readers;
            let (slot, piece) = tokio::select! {
                biased;
                piece = typing.next() => (0, piece),
                piece = screen.next() => (1, piece),
                piece = files.next() => (2, piece),
            };
            let Some(piece) = piece else {
                return;
            };
            heads[slot] = Some(piece);
            continue;
        }
        let allowed = limit.lock().map_or(pacing::FLOOR, |l| l.current());
        let mut each = [0; 3];
        for (bytes, channel) in each.iter_mut().zip(&channels) {
            match channel.outstanding_bytes().await {
                Ok(outstanding) => *bytes = outstanding,
                Err(_) => {
                    events
                        .send(LinkEvent::Lost(LeaveReason::ChannelError))
                        .await
                        .ok();
                    return;
                }
            }
        }
        let [typing, screen, files] = heads.each_ref().map(|head| head.as_ref().map(Vec::len));
        let next = share.link_next(typing, each[0], [screen, files], each.iter().sum(), allowed);
        let Some(next) = next else {
            tokio::time::sleep(LIMIT_POLL).await;
            if let Ok(mut limit) = limit.lock() {
                limit.blocked(LIMIT_POLL);
            }
            continue;
        };
        let slot = match next {
            Next::Typing => 0,
            Next::Bulk(slot) => slot + 1,
        };
        let Some(piece) = heads[slot].take() else {
            continue;
        };
        if channels[slot]
            .send(BytesMut::from(&piece[..]))
            .await
            .is_err()
        {
            events
                .send(LinkEvent::Lost(LeaveReason::ChannelError))
                .await
                .ok();
            return;
        }
        readers[slot].written();
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_candidate_is_logged_by_its_kind_and_never_its_address() {
        assert_eq!(
            candidate_kind("candidate:1 1 udp 2122260223 192.0.2.7 50000 typ srflx raddr 0.0.0.0"),
            "srflx"
        );
        assert_eq!(
            candidate_kind("candidate:1 1 udp 1 x.local 5 typ host"),
            "host"
        );
        assert_eq!(candidate_kind("candidate:1 1 udp 1 192.0.2.7 5"), "unknown");
        assert_eq!(candidate_kind("candidate:1 typ"), "unknown");
    }

    #[test]
    fn an_offer_names_its_media_section_on_its_first_mid_line() {
        let offer =
            "v=0\r\nm=application 9 UDP/DTLS/SCTP webrtc-datachannel\r\na=mid:0\r\na=mid:1\r\n";
        assert_eq!(media_id(offer), Some("0"));
        assert_eq!(media_id("v=0\r\nm=application 9 UDP/DTLS/SCTP\r\n"), None);
    }

    #[test]
    fn a_check_read_on_several_ticks_counts_once() {
        let mut answered = None;
        let rtt = Duration::from_millis(40);
        assert_eq!(new_check(&mut answered, (1, rtt)), Some(rtt));
        assert_eq!(new_check(&mut answered, (1, rtt)), None);
        assert_eq!(new_check(&mut answered, (1, rtt)), None);
        let next = Duration::from_millis(41);
        assert_eq!(new_check(&mut answered, (2, next)), Some(next));
    }
}
