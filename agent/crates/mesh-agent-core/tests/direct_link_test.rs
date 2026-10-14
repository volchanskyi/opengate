//! The direct link end to end in one process: a real session against a relay stub, and a
//! webrtc-rs peer standing in for the browser, driven by the same hand-off the browser runs.

use std::collections::HashSet;
use std::sync::Arc;
use std::time::{Duration, Instant};

use bytes::BytesMut;
use futures_util::{SinkExt, StreamExt};
use mesh_agent_core::link::{CHANNEL_LABEL, CHECK_EVERY, SILENCE_LIMIT};
use mesh_agent_core::{NullCapture, NullInput, SessionError, SessionHandler};
use mesh_protocol::direct::{Config, Handoff, Instruction, Outcome, Path};
use mesh_protocol::{ControlMessage, Frame, Permissions, SessionToken};
use tokio::net::{TcpListener, TcpStream};
use tokio::sync::mpsc;
use tokio::task::JoinHandle;
use tokio_tungstenite::tungstenite::Message;
use tokio_tungstenite::WebSocketStream;
use webrtc::data_channel::{DataChannel, DataChannelEvent};
use webrtc::peer_connection::{
    PeerConnection, PeerConnectionBuilder, PeerConnectionEventHandler, RTCIceCandidateInit,
    RTCIceConnectionState, RTCPeerConnectionIceEvent, RTCSessionDescription,
};

type Relay = WebSocketStream<TcpStream>;

/// The resource readings cover the whole process, so that test runs alone; the others share.
static PROCESS: tokio::sync::RwLock<()> = tokio::sync::RwLock::const_new(());

/// Completed sessions the resource slope is fitted through.
const CONSERVATION_POINTS: [usize; 4] = [2, 4, 6, 8];

/// Descriptors retained per completed session the fit may carry; a link left open read 4 per
/// session and a session that closes its link reads 0.
const DESCRIPTOR_SLOPE_TOLERANCE: f64 = 0.25;

/// Tasks retained per completed session the fit may carry; a link left open read 3.3 per
/// session and a session that closes its link reads 0.
const TASK_SLOPE_TOLERANCE: f64 = 0.25;

/// What the test asks of the browser's peer, which runs on a runtime of its own.
enum PeerCommand {
    Answer(String),
    Candidate {
        candidate: String,
        mid: String,
    },
    Send(Vec<u8>),
    /// Blocks the peer's only thread until the sender is dropped: no packet leaves or is read.
    Freeze(std::sync::mpsc::Receiver<()>),
    Close,
}

/// What the browser's peer reports.
#[derive(Debug)]
enum PeerEvent {
    Offer(String),
    Candidate { candidate: String, mid: String },
    Open,
    Message(Vec<u8>),
    Closed,
    Ice(RTCIceConnectionState),
}

struct PeerHandler {
    events: mpsc::UnboundedSender<PeerEvent>,
}

#[async_trait::async_trait]
impl PeerConnectionEventHandler for PeerHandler {
    async fn on_ice_candidate(&self, event: RTCPeerConnectionIceEvent) {
        if let Ok(json) = event.candidate.to_json() {
            self.events
                .send(PeerEvent::Candidate {
                    candidate: json.candidate,
                    mid: json.sdp_mid.unwrap_or_default(),
                })
                .ok();
        }
    }

    async fn on_ice_connection_state_change(&self, state: RTCIceConnectionState) {
        self.events.send(PeerEvent::Ice(state)).ok();
    }
}

/// The browser's peer connection, on a single-threaded runtime of its own so the test can
/// freeze every socket it holds at once.
struct BrowserPeer {
    commands: mpsc::UnboundedSender<PeerCommand>,
    events: mpsc::UnboundedReceiver<PeerEvent>,
    thread: Option<std::thread::JoinHandle<()>>,
    thaw: Option<std::sync::mpsc::Sender<()>>,
}

impl BrowserPeer {
    fn start() -> Self {
        let (commands, command_rx) = mpsc::unbounded_channel();
        let (event_tx, events) = mpsc::unbounded_channel();
        let thread = std::thread::spawn(move || {
            tokio::runtime::Builder::new_current_thread()
                .enable_all()
                .build()
                .expect("peer runtime")
                .block_on(run_peer(command_rx, event_tx));
        });
        Self {
            commands,
            events,
            thread: Some(thread),
            thaw: None,
        }
    }

    fn command(&self, command: PeerCommand) {
        self.commands.send(command).ok();
    }

    fn freeze(&mut self) {
        let (thaw, frozen) = std::sync::mpsc::channel();
        self.command(PeerCommand::Freeze(frozen));
        self.thaw = Some(thaw);
    }

    fn close(&mut self) {
        self.thaw = None;
        self.command(PeerCommand::Close);
        if let Some(thread) = self.thread.take() {
            thread.join().expect("peer thread");
        }
    }
}

impl Drop for BrowserPeer {
    fn drop(&mut self) {
        self.close();
    }
}

async fn run_peer(
    mut commands: mpsc::UnboundedReceiver<PeerCommand>,
    events: mpsc::UnboundedSender<PeerEvent>,
) {
    let pc = PeerConnectionBuilder::new()
        .with_handler(Arc::new(PeerHandler {
            events: events.clone(),
        }))
        .with_udp_addrs(vec!["0.0.0.0:0".to_string()])
        .build()
        .await
        .expect("browser peer connection");
    let channel = pc
        .create_data_channel(CHANNEL_LABEL, None)
        .await
        .expect("browser channel");
    let offer = pc.create_offer(None).await.expect("offer");
    pc.set_local_description(offer)
        .await
        .expect("local description");
    let sdp = pc.local_description().await.expect("offer sdp").sdp;
    events.send(PeerEvent::Offer(sdp)).ok();
    let pump = tokio::spawn(pump_channel(channel.clone(), events.clone()));

    while let Some(command) = commands.recv().await {
        match command {
            PeerCommand::Answer(sdp) => {
                let answer = RTCSessionDescription::answer(sdp).expect("answer sdp");
                pc.set_remote_description(answer)
                    .await
                    .expect("remote description");
            }
            PeerCommand::Candidate { candidate, mid } => {
                let init = RTCIceCandidateInit {
                    candidate,
                    sdp_mid: Some(mid),
                    ..Default::default()
                };
                pc.add_ice_candidate(init).await.ok();
            }
            PeerCommand::Send(piece) => {
                channel.send(BytesMut::from(&piece[..])).await.ok();
            }
            PeerCommand::Freeze(frozen) => {
                frozen.recv().ok();
            }
            PeerCommand::Close => break,
        }
    }
    pc.close().await.ok();
    pump.abort();
    pump.await.ok();
}

async fn pump_channel(channel: Arc<dyn DataChannel>, events: mpsc::UnboundedSender<PeerEvent>) {
    while let Some(event) = channel.poll().await {
        let event = match event {
            DataChannelEvent::OnOpen => PeerEvent::Open,
            DataChannelEvent::OnMessage(message) => PeerEvent::Message(message.data.to_vec()),
            DataChannelEvent::OnClose => PeerEvent::Closed,
            _ => continue,
        };
        events.send(event).ok();
    }
}

/// The browser's side of one session: the relay, the peer, and the hand-off joining them.
struct Browser {
    ws: Relay,
    peer: BrowserPeer,
    handoff: Handoff,
    start: Instant,
    frozen: bool,
    delivered: Vec<Frame>,
    outcomes: Vec<Outcome>,
    host_switched: bool,
    host_fell_back_at: Option<Instant>,
    relay_data_after_switch: usize,
    peer_closed: bool,
    relay_open: bool,
    offer_mid: Option<String>,
    host_mids: Vec<String>,
}

impl Browser {
    fn new(ws: Relay) -> Self {
        Self {
            ws,
            peer: BrowserPeer::start(),
            handoff: Handoff::new(Config::default()),
            start: Instant::now(),
            frozen: false,
            delivered: Vec::new(),
            outcomes: Vec::new(),
            host_switched: false,
            host_fell_back_at: None,
            relay_data_after_switch: 0,
            peer_closed: false,
            relay_open: true,
            offer_mid: None,
            host_mids: Vec::new(),
        }
    }

    fn now(&self) -> u64 {
        u64::try_from(self.start.elapsed().as_millis()).expect("test clock")
    }

    /// Queues one session frame the way the browser's sending point does.
    async fn send(&mut self, frame: Frame) {
        let out = self
            .handoff
            .send(frame.encode().expect("encode"), self.now());
        self.execute(out).await;
    }

    /// Runs the browser until `done` holds, failing after `limit`.
    async fn until(&mut self, limit: Duration, what: &str, done: impl Fn(&Browser) -> bool) {
        let deadline = tokio::time::Instant::now() + limit;
        while !done(self) {
            let tick = self
                .handoff
                .next_deadline()
                .map(|ms| tokio::time::Instant::from_std(self.start + Duration::from_millis(ms)));
            tokio::select! {
                () = tokio::time::sleep_until(deadline) => panic!("timed out waiting for {what}"),
                msg = self.ws.next(), if self.relay_open => match msg {
                    Some(Ok(Message::Binary(data))) => self.on_relay(data.to_vec()).await,
                    Some(Ok(Message::Close(_))) | Some(Err(_)) | None => self.relay_open = false,
                    Some(Ok(_)) => {}
                },
                Some(event) = self.peer.events.recv() => self.on_peer(event).await,
                () = sleep_until(tick), if tick.is_some() => {
                    let out = self.handoff.tick(self.now());
                    self.execute(out).await;
                }
            }
        }
    }

    async fn on_relay(&mut self, frame: Vec<u8>) {
        match Frame::decode(&frame).expect("a whole relay frame").0 {
            Frame::Control(ControlMessage::DirectSwitch) => self.host_switched = true,
            Frame::Control(ControlMessage::DirectFallback { .. }) => {
                self.host_fell_back_at = Some(Instant::now());
            }
            Frame::Control(ControlMessage::IceCandidate { ref mid, .. }) => {
                self.host_mids.push(mid.clone());
            }
            Frame::Control(
                ControlMessage::DirectAnswer { .. } | ControlMessage::DirectRefused { .. },
            ) => {}
            _ if self.host_switched && self.host_fell_back_at.is_none() => {
                self.relay_data_after_switch += 1;
            }
            _ => {}
        }
        let out = self.handoff.from_relay(frame, self.now());
        self.execute(out).await;
    }

    async fn on_peer(&mut self, event: PeerEvent) {
        let now = self.now();
        let out = match event {
            PeerEvent::Offer(sdp) => {
                self.offer_mid = sdp
                    .lines()
                    .find_map(|line| line.strip_prefix("a=mid:"))
                    .map(|mid| mid.trim().to_owned());
                self.handoff.begin_attempt(now);
                self.relay(ControlMessage::DirectOffer { sdp }).await;
                return;
            }
            PeerEvent::Candidate { candidate, mid } => {
                self.relay(ControlMessage::IceCandidate { candidate, mid })
                    .await;
                return;
            }
            PeerEvent::Open => self.handoff.link_open(now),
            PeerEvent::Message(piece) => self.handoff.from_link(&piece, now),
            PeerEvent::Closed => {
                self.peer_closed = true;
                return;
            }
            PeerEvent::Ice(state) => {
                if matches!(
                    state,
                    RTCIceConnectionState::Disconnected
                        | RTCIceConnectionState::Failed
                        | RTCIceConnectionState::Closed
                ) {
                    self.peer_closed = true;
                }
                return;
            }
        };
        self.execute(out).await;
    }

    async fn relay(&mut self, msg: ControlMessage) {
        let frame = Frame::Control(msg).encode().expect("encode");
        self.ws
            .send(Message::Binary(frame.into()))
            .await
            .expect("relay send");
    }

    async fn execute(&mut self, instructions: Vec<Instruction>) {
        for instruction in instructions {
            match instruction {
                Instruction::Relay(frame) => self
                    .ws
                    .send(Message::Binary(frame.into()))
                    .await
                    .expect("relay send"),
                // A frozen peer sends nothing: what goes to it is lost on the way.
                Instruction::Link(piece) if !self.frozen => {
                    self.peer.command(PeerCommand::Send(piece));
                }
                Instruction::Deliver(frame) => self
                    .delivered
                    .push(Frame::decode(&frame).expect("a whole frame").0),
                Instruction::Signal(ControlMessage::DirectAnswer { sdp }) => {
                    self.peer.command(PeerCommand::Answer(sdp));
                }
                Instruction::Signal(ControlMessage::IceCandidate { candidate, mid }) => {
                    self.peer.command(PeerCommand::Candidate { candidate, mid });
                }
                Instruction::CloseLink => {
                    self.frozen = true;
                    self.peer.close();
                }
                Instruction::End => panic!("the browser ended the session"),
                Instruction::Outcome(outcome) => self.outcomes.push(outcome),
                _ => {}
            }
        }
    }

    fn direct(&self) -> bool {
        self.host_switched
            && self.handoff.path() == Path::Link
            && self.outcomes == [Outcome::Direct]
    }

    fn chats(&self) -> Vec<&str> {
        self.delivered
            .iter()
            .filter_map(|frame| match frame {
                Frame::Control(ControlMessage::ChatMessage { text, sender }) => {
                    assert_eq!(sender, "agent");
                    Some(text.as_str())
                }
                _ => None,
            })
            .collect()
    }

    /// Every listed name, failing on one listed twice, and whether the listing is complete.
    fn listing(&self) -> (HashSet<String>, bool) {
        let mut names = HashSet::new();
        let mut complete = false;
        for frame in &self.delivered {
            if let Frame::Control(ControlMessage::FileListResponse { entries, more, .. }) = frame {
                for entry in entries {
                    assert!(
                        names.insert(entry.name.clone()),
                        "{} listed twice",
                        entry.name
                    );
                }
                complete = !more;
            }
        }
        (names, complete)
    }

    fn pieces(&self) -> usize {
        self.delivered
            .iter()
            .filter(|frame| matches!(frame, Frame::FileTransfer(_)))
            .count()
    }

    fn pages(&self) -> usize {
        self.delivered
            .iter()
            .filter(|frame| {
                matches!(
                    frame,
                    Frame::Control(ControlMessage::FileListResponse { .. })
                )
            })
            .count()
    }
}

async fn sleep_until(deadline: Option<tokio::time::Instant>) {
    if let Some(deadline) = deadline {
        tokio::time::sleep_until(deadline).await;
    }
}

/// Starts a session that may read files against a relay stub, and returns the browser's end.
async fn open_session() -> (Browser, JoinHandle<Result<(), SessionError>>) {
    let listener = TcpListener::bind("127.0.0.1:0")
        .await
        .expect("bind relay stub");
    let url = format!(
        "ws://{}/ws/relay/session-token",
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
    let (stream, _) = listener.accept().await.expect("relay stub accept");
    let ws = tokio_tungstenite::accept_async(stream)
        .await
        .expect("relay stub handshake");
    (Browser::new(ws), session)
}

fn folder(entries: usize) -> tempfile::TempDir {
    let dir = tempfile::tempdir().expect("tempdir");
    for i in 0..entries {
        let name = format!("event-{i:05}-collected-from-the-host-journal.log");
        std::fs::write(dir.path().join(name), b"").expect("write a file");
    }
    dir
}

fn chat(text: &str) -> Frame {
    Frame::Control(ControlMessage::ChatMessage {
        text: text.to_string(),
        sender: "browser".to_string(),
    })
}

fn list(dir: &tempfile::TempDir) -> Frame {
    Frame::Control(ControlMessage::FileListRequest {
        path: dir.path().to_string_lossy().to_string(),
    })
}

fn download(path: &std::path::Path) -> Frame {
    Frame::Control(ControlMessage::FileDownloadRequest {
        path: path.to_string_lossy().to_string(),
    })
}

/// Open descriptors, sockets and files among them, and live tasks on this runtime.
fn reading() -> (usize, usize) {
    let descriptors = std::fs::read_dir("/proc/self/fd")
        .expect("the process's descriptors")
        .count();
    let tasks = tokio::runtime::Handle::current()
        .metrics()
        .num_alive_tasks();
    (descriptors, tasks)
}

/// The reading once two taken 100 ms apart agree, so teardown still finishing is not counted.
async fn settle() -> (usize, usize) {
    let mut last = reading();
    for _ in 0..50 {
        tokio::time::sleep(Duration::from_millis(100)).await;
        let now = reading();
        if now == last {
            return now;
        }
        last = now;
    }
    last
}

fn slope(xs: &[f64], ys: &[f64]) -> f64 {
    let n = xs.len() as f64;
    let (mx, my) = (xs.iter().sum::<f64>() / n, ys.iter().sum::<f64>() / n);
    let covariance: f64 = xs.iter().zip(ys).map(|(x, y)| (x - mx) * (y - my)).sum();
    let variance: f64 = xs.iter().map(|x| (x - mx).powi(2)).sum();
    covariance / variance
}

/// One session that forms its link, starts a download and ends while the download runs.
async fn session_ended_mid_download(file: &std::path::Path, size: u64) {
    let (mut browser, session) = open_session().await;
    browser
        .until(
            Duration::from_secs(15),
            "both sides on the link",
            Browser::direct,
        )
        .await;
    browser.send(download(file)).await;
    browser
        .until(Duration::from_secs(15), "the first pieces", |b| {
            b.pieces() >= 4
        })
        .await;
    let piece = mesh_agent_core::session::outbox::FILE_PIECE as u64;
    assert!(
        (browser.pieces() as u64) < size / piece,
        "the session ends while its download runs"
    );
    end(browser, session).await;
}

async fn end(mut browser: Browser, session: JoinHandle<Result<(), SessionError>>) -> Browser {
    browser.ws.close(None).await.expect("relay close");
    tokio::time::timeout(Duration::from_secs(10), session)
        .await
        .expect("the session ends when its relay closes")
        .expect("session task")
        .expect("a clean end");
    browser
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn a_link_forms_and_carries_the_session_with_no_session_data_on_the_relay() {
    let _shared = PROCESS.read().await;
    let dir = folder(3_000);
    let (mut browser, session) = open_session().await;

    browser
        .until(
            Duration::from_secs(15),
            "both sides on the link",
            Browser::direct,
        )
        .await;

    for text in ["one", "two", "three"] {
        browser.send(chat(text)).await;
    }
    browser.send(list(&dir)).await;
    browser
        .until(Duration::from_secs(15), "the echoes and the listing", |b| {
            b.chats().len() == 3 && b.listing().1
        })
        .await;

    assert_eq!(browser.chats(), ["one", "two", "three"]);
    assert_eq!(browser.listing().0.len(), 3_000);
    assert_eq!(
        browser.relay_data_after_switch, 0,
        "after the host's switch notice the relay carries no session data"
    );
    assert_eq!(browser.handoff.path(), Path::Link);
    assert!(
        !browser.host_mids.is_empty(),
        "the host sent its candidates"
    );
    let offer_mid = browser
        .offer_mid
        .clone()
        .expect("the offer names its media section");
    assert!(
        browser.host_mids.iter().all(|mid| *mid == offer_mid),
        "every host candidate names the offer's media section {offer_mid:?}: {:?}",
        browser.host_mids
    );

    let mut browser = end(browser, session).await;
    browser
        .until(
            Duration::from_secs(10),
            "the host's end of the link to close",
            |b| b.peer_closed,
        )
        .await;
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn a_silent_browser_moves_the_session_back_to_the_relay_with_nothing_lost() {
    let _shared = PROCESS.read().await;
    let dir = folder(20_000);
    let (mut browser, session) = open_session().await;

    browser
        .until(
            Duration::from_secs(15),
            "both sides on the link",
            Browser::direct,
        )
        .await;
    for text in ["one", "two", "three"] {
        browser.send(chat(text)).await;
    }
    browser.send(list(&dir)).await;
    browser
        .until(
            Duration::from_secs(15),
            "the first pages over the link",
            |b| b.chats().len() == 3 && b.pages() >= 5,
        )
        .await;
    assert!(
        !browser.listing().1,
        "the listing is still paging when the browser goes silent"
    );

    browser.peer.freeze();
    browser.frozen = true;
    let silent_from = Instant::now();
    browser.send(chat("four")).await;

    browser
        .until(
            Duration::from_secs(15),
            "the host's fall-back notice",
            |b| b.host_fell_back_at.is_some(),
        )
        .await;
    let noticed = browser.host_fell_back_at.expect("fall-back") - silent_from;
    assert!(
        noticed >= SILENCE_LIMIT - CHECK_EVERY && noticed <= SILENCE_LIMIT + CHECK_EVERY,
        "the host falls back on the silence limit, after {noticed:?}"
    );

    browser
        .until(Duration::from_secs(20), "the rest over the relay", |b| {
            b.chats().len() == 4 && b.listing().1
        })
        .await;
    assert_eq!(browser.chats(), ["one", "two", "three", "four"]);
    assert_eq!(browser.listing().0.len(), 20_000);
    assert_eq!(browser.handoff.path(), Path::Relay);
    assert_eq!(browser.outcomes, [Outcome::Direct, Outcome::Dropped]);

    end(browser, session).await;
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn sessions_ended_mid_download_on_a_link_give_back_their_sockets_files_and_tasks() {
    let _alone = PROCESS.write().await;
    let dir = tempfile::tempdir().expect("tempdir");
    let file = dir.path().join("disk-image.vhdx");
    let size = 1u64 << 30;
    std::fs::File::create(&file)
        .and_then(|f| f.set_len(size))
        .expect("a sparse gigabyte");

    // The first session builds what every session after it shares, so it runs before measuring.
    session_ended_mid_download(&file, size).await;
    settle().await;

    let (mut xs, mut descriptors, mut tasks) = (Vec::new(), Vec::new(), Vec::new());
    let mut completed = 0;
    for point in CONSERVATION_POINTS {
        while completed < point {
            session_ended_mid_download(&file, size).await;
            completed += 1;
        }
        let (open, live) = settle().await;
        xs.push(point as f64);
        descriptors.push(open as f64);
        tasks.push(live as f64);
    }

    let (descriptor_slope, task_slope) = (slope(&xs, &descriptors), slope(&xs, &tasks));
    assert!(
        descriptor_slope.abs() <= DESCRIPTOR_SLOPE_TOLERANCE,
        "sessions {xs:?} left descriptors {descriptors:?}: {descriptor_slope:.3} per session"
    );
    assert!(
        task_slope.abs() <= TASK_SLOPE_TOLERANCE,
        "sessions {xs:?} left tasks {tasks:?}: {task_slope:.3} per session"
    );
}
