//! The host's end of a session's direct link: one peer connection with one ordered channel,
//! answering the browser's offer with the helpers the server named.

use std::sync::{Arc, Mutex, OnceLock};
use std::time::{Duration, Instant};

use bytes::BytesMut;
use mesh_protocol::direct::{CandidateFilter, LeaveReason, MAX_PIECE};
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

/// How often the host checks its peer on the link with a request the peer's stack answers.
pub const CHECK_EVERY: Duration = Duration::from_secs(1);

/// How long the host hears nothing at all before it calls the link silent: three unanswered checks.
pub const SILENCE_LIMIT: Duration = Duration::from_secs(3);

/// Label of the one ordered channel the browser opens.
pub const CHANNEL_LABEL: &str = "session";

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
    /// The channel is open.
    Open,
    /// One piece from the browser.
    Piece(Vec<u8>),
    /// The link went silent, failed, closed or errored.
    Lost(LeaveReason),
    /// The offer could not be answered.
    Failed(String),
}

/// The session core's handle on its one link.
#[derive(Debug)]
pub(crate) struct LinkHandle {
    commands: mpsc::UnboundedSender<LinkCommand>,
    /// Where the core pushes pieces; the link's writer drains it at the run-time limit.
    pub(crate) outlet: Outlet,
    task: JoinHandle<()>,
}

impl LinkHandle {
    /// Starts a link asking `helpers` for the host's public address; events go to `events`.
    pub(crate) fn start(helpers: Vec<String>, events: mpsc::Sender<LinkEvent>) -> Self {
        let (commands, command_rx) = mpsc::unbounded_channel();
        let (outlet, reader) = Outlet::new();
        let task = tokio::spawn(run(helpers, command_rx, reader, events));
        Self {
            commands,
            outlet,
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
        drop(self.outlet);
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

struct Handler {
    events: mpsc::Sender<LinkEvent>,
    channels: mpsc::Sender<Arc<dyn DataChannel>>,
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
        match channel.label().await {
            Ok(label) if label == CHANNEL_LABEL => {
                self.channels.send(channel).await.ok();
            }
            _ => {
                warn!("direct link: closing a channel the session does not use");
                channel.close().await.ok();
            }
        }
    }
}

async fn build(
    helpers: Vec<String>,
    events: mpsc::Sender<LinkEvent>,
    channels: mpsc::Sender<Arc<dyn DataChannel>>,
    mid: Arc<OnceLock<String>>,
) -> Result<impl PeerConnection, String> {
    let settings = SettingEngineBuilder::new()
        .with_ice_timeouts(Some(SILENCE_LIMIT), Some(SILENCE_LIMIT), Some(CHECK_EVERY))
        .with_sctp_max_message_size(SctpMaxMessageSize::Bounded(MAX_PIECE as u32))
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

async fn run(
    helpers: Vec<String>,
    mut commands: mpsc::UnboundedReceiver<LinkCommand>,
    reader: OutletReader,
    events: mpsc::Sender<LinkEvent>,
) {
    let (channel_tx, mut channel_rx) = mpsc::channel(1);
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
    let mut reader = Some(reader);
    let mut tasks: Vec<JoinHandle<()>> = Vec::new();
    let mut stats = tokio::time::interval(pacing::INTERVAL);

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
            Some(channel) = channel_rx.recv() => {
                if let Some(reader) = reader.take() {
                    if channel.ready_state().await.is_ok_and(|s| s == RTCDataChannelState::Open) {
                        events.send(LinkEvent::Open).await.ok();
                    }
                    tasks.push(tokio::spawn(pump(channel.clone(), events.clone())));
                    tasks.push(tokio::spawn(write(channel, reader, limit.clone(), events.clone())));
                }
            }
            _ = stats.tick() => {
                let rtt = round_trip(&pc).await;
                if let Ok(mut limit) = limit.lock() {
                    let now = Instant::now();
                    if let Some(rtt) = rtt {
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

/// The nominated pair's current round trip, when the link has one.
async fn round_trip(pc: &impl PeerConnection) -> Option<Duration> {
    let report = pc.get_stats(Instant::now(), StatsSelector::None).await;
    report
        .candidate_pairs()
        .filter(|pair| pair.nominated && pair.current_round_trip_time > 0.0)
        .map(|pair| Duration::from_secs_f64(pair.current_round_trip_time))
        .min()
}

/// Turns the channel's events into link events until it closes.
async fn pump(channel: Arc<dyn DataChannel>, events: mpsc::Sender<LinkEvent>) {
    while let Some(event) = channel.poll().await {
        let event = match event {
            DataChannelEvent::OnOpen => LinkEvent::Open,
            DataChannelEvent::OnMessage(message) => LinkEvent::Piece(message.data.to_vec()),
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

/// Sends the pieces the core pushed, holding the channel's outstanding bytes under the limit.
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
}
