//! Replays the shared direct-link scenarios in `testdata/direct-link/` against two hand-offs joined
//! by a relay and a three-channel link, holding random link failures to exactly-once delivery.

use std::collections::VecDeque;
use std::path::PathBuf;

use mesh_protocol::direct::{
    Channel, Config, Handoff, Instruction, Layout, LeaveReason, Outcome, Path,
};
use mesh_protocol::{DesktopFrame, FileFrame, Frame, FrameEncoding, TerminalFrame};
use proptest::prelude::*;
use serde_json::Value;

const BROWSER: usize = 0;
const HOST: usize = 1;

struct Side {
    machine: Handoff,
    interactive: VecDeque<Vec<u8>>,
    screen: VecDeque<Vec<u8>>,
    files: VecDeque<Vec<u8>>,
    /// Bytes taken from the screen and files lanes, which share the bulk turn by bytes.
    bulk_taken: [usize; 2],
    taken: Vec<String>,
    delivered: Vec<String>,
    outcomes: Vec<String>,
    ended: bool,
    link_closed: bool,
}

type Channels = [VecDeque<Vec<u8>>; 3];

/// Two sides, a relay that never loses anything, and a link of three channels that can be cut.
struct World {
    now: u64,
    sides: [Side; 2],
    relay: [VecDeque<Vec<u8>>; 2],
    link: [Channels; 2],
    cut: bool,
}

impl World {
    fn new(config: Config) -> Self {
        let side = || Side {
            machine: Handoff::new(config),
            interactive: VecDeque::new(),
            screen: VecDeque::new(),
            files: VecDeque::new(),
            bulk_taken: [0, 0],
            taken: Vec::new(),
            delivered: Vec::new(),
            outcomes: Vec::new(),
            ended: false,
            link_closed: false,
        };
        Self {
            now: 0,
            sides: [side(), side()],
            relay: [VecDeque::new(), VecDeque::new()],
            link: [Channels::default(), Channels::default()],
            cut: false,
        }
    }

    fn cut_link(&mut self) {
        self.cut = true;
        self.link = [Channels::default(), Channels::default()];
    }

    fn apply(&mut self, from: usize, instructions: Vec<Instruction>) {
        let to = 1 - from;
        for instruction in instructions {
            match instruction {
                Instruction::Relay(frame) => self.relay[to].push_back(frame),
                Instruction::Link { channel, piece } => {
                    if !self.cut && !self.sides[from].link_closed {
                        self.link[to][channel.index()].push_back(piece);
                    }
                }
                Instruction::Deliver(frame) => self.sides[from].delivered.push(label(&frame)),
                Instruction::CloseLink => self.sides[from].link_closed = true,
                Instruction::End => self.sides[from].ended = true,
                Instruction::Outcome(outcome) => self.sides[from]
                    .outcomes
                    .push(outcome_name(outcome).to_string()),
                _ => {}
            }
        }
    }

    /// Takes frames as the sending point does: typing first, then whichever of screen and files
    /// has had fewer bytes taken, each only while its channel's window has room.
    fn take(&mut self, side: usize, count: usize) -> usize {
        let mut taken = 0;
        while taken < count && !self.sides[side].ended {
            let s = &mut self.sides[side];
            let open = |channel| s.machine.can_take(channel);
            let lane = if !s.interactive.is_empty() && open(Channel::Typing) {
                Channel::Typing
            } else {
                let screen = !s.screen.is_empty() && open(Channel::Screen);
                let files = !s.files.is_empty() && open(Channel::Files);
                match (screen, files) {
                    (true, true) if s.bulk_taken[1] < s.bulk_taken[0] => Channel::Files,
                    (true, _) => Channel::Screen,
                    (false, true) => Channel::Files,
                    (false, false) => break,
                }
            };
            let frame = match lane {
                Channel::Typing => s.interactive.pop_front(),
                Channel::Screen => s.screen.pop_front(),
                _ => s.files.pop_front(),
            }
            .expect("the chosen lane holds a frame");
            if lane != Channel::Typing {
                s.bulk_taken[lane.index() - 1] += frame.len();
            }
            s.taken.push(label(&frame));
            let out = s.machine.send(frame, self.now);
            self.apply(side, out);
            taken += 1;
        }
        taken
    }

    fn carry_relay(&mut self, to: usize, count: usize) -> usize {
        let mut carried = 0;
        while carried < count {
            let Some(frame) = self.relay[to].pop_front() else {
                break;
            };
            let out = self.sides[to].machine.from_relay(frame, self.now);
            self.apply(to, out);
            carried += 1;
        }
        carried
    }

    /// Carries pieces on the named channels, each channel drained in turn.
    fn carry_link(&mut self, to: usize, channels: &[Channel], count: usize) -> usize {
        let mut carried = 0;
        for &channel in channels {
            while carried < count {
                let Some(piece) = self.link[to][channel.index()].pop_front() else {
                    break;
                };
                let out = self.sides[to].machine.from_link(channel, &piece, self.now);
                self.apply(to, out);
                carried += 1;
            }
        }
        carried
    }

    fn settle(&mut self) {
        loop {
            let mut moved = 0;
            for to in [BROWSER, HOST] {
                moved += self.carry_relay(to, usize::MAX);
                moved += self.carry_link(to, &Channel::ALL, usize::MAX);
            }
            moved += self.take(BROWSER, usize::MAX);
            moved += self.take(HOST, usize::MAX);
            if moved == 0 {
                return;
            }
        }
    }

    fn tick(&mut self, ms: u64) {
        self.now += ms;
        for side in [BROWSER, HOST] {
            let out = self.sides[side].machine.tick(self.now);
            self.apply(side, out);
        }
    }
}

fn label(frame: &[u8]) -> String {
    let data = match Frame::decode(frame) {
        Ok((Frame::Terminal(t), _)) => t.data,
        Ok((Frame::FileTransfer(f), _)) => f.data,
        Ok((Frame::Desktop(d), _)) => d.data,
        other => panic!("a scenario delivers only its own frames, got {other:?}"),
    };
    let end = data.iter().position(|b| *b == 0).unwrap_or(data.len());
    String::from_utf8(data[..end].to_vec()).unwrap()
}

fn frame(lane: &str, name: &str, size: usize) -> Vec<u8> {
    let mut data = name.as_bytes().to_vec();
    data.resize(size.max(data.len()), 0);
    match lane {
        "interactive" => Frame::Terminal(TerminalFrame { data }),
        "bulk" => Frame::FileTransfer(FileFrame {
            offset: 0,
            total_size: 0,
            data,
        }),
        "screen" => Frame::Desktop(DesktopFrame {
            sequence: 0,
            x: 0,
            y: 0,
            width: 0,
            height: 0,
            encoding: FrameEncoding::Raw,
            data,
        }),
        other => panic!("unknown lane {other}"),
    }
    .encode()
    .unwrap()
}

fn outcome_name(outcome: Outcome) -> &'static str {
    match outcome {
        Outcome::Direct => "direct",
        Outcome::NoRoute => "no_route",
        Outcome::Dropped => "dropped",
        _ => "unknown",
    }
}

fn path_name(path: Path) -> &'static str {
    match path {
        Path::Relay => "relay",
        Path::Link => "link",
        Path::Holding => "holding",
        Path::Ended => "ended",
        _ => "unknown",
    }
}

fn reason(name: &str) -> LeaveReason {
    match name {
        "silence" => LeaveReason::Silence,
        "closed" => LeaveReason::Closed,
        "channel_error" => LeaveReason::ChannelError,
        other => panic!("unknown leave reason {other}"),
    }
}

fn channel(name: &str) -> Channel {
    match name {
        "typing" => Channel::Typing,
        "screen" => Channel::Screen,
        "files" => Channel::Files,
        other => panic!("unknown channel {other}"),
    }
}

fn layout(step: &Value) -> Layout {
    match step.get("channels").and_then(Value::as_str) {
        None | Some("shared") => Layout::Shared,
        Some("separate") => Layout::Separate,
        Some(other) => panic!("unknown channel layout {other}"),
    }
}

fn sides(name: &str) -> Vec<usize> {
    match name {
        "browser" => vec![BROWSER],
        "host" => vec![HOST],
        "both" => vec![BROWSER, HOST],
        other => panic!("unknown side {other}"),
    }
}

fn strings(v: &Value) -> Vec<String> {
    v.as_array()
        .unwrap()
        .iter()
        .map(|s| s.as_str().unwrap().to_string())
        .collect()
}

fn check(world: &World, side: usize, expect: &Value, at: &str) {
    let s = &world.sides[side];
    let who = if side == BROWSER { "browser" } else { "host" };
    if let Some(delivered) = expect.get("delivered") {
        assert_eq!(s.delivered, strings(delivered), "{at}: {who} delivered");
    }
    if let Some(path) = expect.get("path") {
        assert_eq!(
            path_name(s.machine.path()),
            path.as_str().unwrap(),
            "{at}: {who} path"
        );
    }
    if let Some(outcomes) = expect.get("outcomes") {
        assert_eq!(s.outcomes, strings(outcomes), "{at}: {who} outcomes");
    }
    if let Some(ended) = expect.get("ended") {
        assert_eq!(s.ended, ended.as_bool().unwrap(), "{at}: {who} ended");
    }
    if let Some(kept) = expect.get("kept") {
        assert_eq!(
            s.machine.kept() as u64,
            kept.as_u64().unwrap(),
            "{at}: {who} kept"
        );
    }
}

fn run(name: &str, scenario: &Value) {
    let mut config = Config::default();
    if let Some(window) = scenario.get("window") {
        config.window = window.as_u64().unwrap() as usize;
    }
    if let Some(budget) = scenario.get("budget_ms") {
        config.attempt_budget_ms = budget.as_u64().unwrap();
    }
    let mut world = World::new(config);
    let count = |step: &Value| {
        step.get("count")
            .map_or(usize::MAX, |c| c.as_u64().unwrap() as usize)
    };

    for (i, step) in scenario["steps"].as_array().unwrap().iter().enumerate() {
        let at = format!("{name} step {i}");
        let side = || sides(step["side"].as_str().unwrap());
        match step["step"].as_str().unwrap() {
            "begin" => side()
                .into_iter()
                .for_each(|s| world.sides[s].machine.begin_attempt(world.now)),
            "open" => {
                for s in side() {
                    let out = world.sides[s].machine.link_open(world.now, layout(step));
                    world.apply(s, out);
                }
            }
            "queue" => {
                let lane = step["lane"].as_str().unwrap();
                for s in side() {
                    for f in step["frames"].as_array().unwrap() {
                        let size = f.get("size").map_or(0, |v| v.as_u64().unwrap() as usize);
                        let bytes = frame(lane, f["label"].as_str().unwrap(), size);
                        let target = &mut world.sides[s];
                        match lane {
                            "interactive" => target.interactive.push_back(bytes),
                            "screen" => target.screen.push_back(bytes),
                            _ => target.files.push_back(bytes),
                        }
                    }
                }
            }
            "take" => {
                for s in side() {
                    world.take(s, count(step));
                }
            }
            "carry" => {
                let to = sides(step["to"].as_str().unwrap())[0];
                if step["path"] == "link" {
                    let channels = match step.get("channel").and_then(Value::as_str) {
                        Some(name) => vec![channel(name)],
                        None => Channel::ALL.to_vec(),
                    };
                    world.carry_link(to, &channels, count(step));
                } else {
                    world.carry_relay(to, count(step));
                }
            }
            "cut" => world.cut_link(),
            "lose" => {
                for s in side() {
                    let out = world.sides[s]
                        .machine
                        .link_lost(reason(step["reason"].as_str().unwrap()), world.now);
                    world.apply(s, out);
                }
            }
            "tick" => world.tick(step["ms"].as_u64().unwrap()),
            "settle" => world.settle(),
            "inject" => {
                let to = sides(step["to"].as_str().unwrap())[0];
                let bytes = step["bytes"]
                    .as_array()
                    .unwrap()
                    .iter()
                    .map(|b| b.as_u64().unwrap() as u8)
                    .collect();
                let on = step
                    .get("channel")
                    .and_then(Value::as_str)
                    .map_or(Channel::Typing, channel);
                world.link[to][on.index()].push_back(bytes);
            }
            "expect" => {
                for s in side() {
                    check(&world, s, step, &at);
                }
            }
            other => panic!("{at}: unknown step {other}"),
        }
    }
    check(
        &world,
        BROWSER,
        &scenario["expect"]["browser"],
        &format!("{name} end"),
    );
    check(
        &world,
        HOST,
        &scenario["expect"]["host"],
        &format!("{name} end"),
    );
}

fn scenario_dir() -> PathBuf {
    let mut path = PathBuf::from(env!("CARGO_MANIFEST_DIR"));
    path.push("../../../testdata/direct-link");
    path
}

#[test]
fn every_shared_scenario_gives_its_exact_deliveries() {
    let mut names: Vec<PathBuf> = std::fs::read_dir(scenario_dir())
        .unwrap()
        .map(|e| e.unwrap().path())
        .filter(|p| p.extension().is_some_and(|x| x == "json"))
        .collect();
    names.sort();
    assert!(
        names.len() >= 22,
        "the scenario set is read, found {}",
        names.len()
    );
    for path in names {
        let name = path.file_stem().unwrap().to_string_lossy().to_string();
        let scenario: Value =
            serde_json::from_str(&std::fs::read_to_string(&path).unwrap()).unwrap();
        run(&name, &scenario);
    }
}

const LANES: [&str; 3] = ["interactive", "screen", "bulk"];

/// One random run: what each side sends on which lane, when the link opens with which channels,
/// which channel each carry drains, and where the link dies.
#[derive(Debug, Clone)]
struct Run {
    frames: [Vec<(usize, usize)>; 2],
    separate: bool,
    open_after: [usize; 2],
    carries: Vec<(Option<usize>, usize, usize)>,
    cut_at: Option<usize>,
    losers: [bool; 2],
}

fn arb_run() -> impl Strategy<Value = Run> {
    let frames = || prop::collection::vec((0usize..3, 1usize..40_000), 0..10);
    (
        frames(),
        frames(),
        any::<bool>(),
        (0usize..12, 0usize..12),
        prop::collection::vec((prop::option::of(0usize..4), 0usize..2, 1usize..6), 0..40),
        prop::option::of(0usize..40),
        (any::<bool>(), any::<bool>()),
    )
        .prop_map(|(a, b, separate, open, carries, cut_at, losers)| Run {
            frames: [a, b],
            separate,
            open_after: [open.0, open.1],
            carries,
            cut_at,
            losers: [losers.0, losers.1],
        })
}

fn play(run: &Run) -> World {
    let mut world = World::new(Config {
        window: 64 * 1024,
        typing_window: 32 * 1024,
        ..Config::default()
    });
    let layout = if run.separate {
        Layout::Separate
    } else {
        Layout::Shared
    };
    for side in [BROWSER, HOST] {
        world.sides[side].machine.begin_attempt(0);
        for (n, (lane, size)) in run.frames[side].iter().enumerate() {
            let bytes = frame(LANES[*lane], &format!("{side}-{n}-{lane}"), *size);
            let s = &mut world.sides[side];
            match lane {
                0 => s.interactive.push_back(bytes),
                1 => s.screen.push_back(bytes),
                _ => s.files.push_back(bytes),
            }
        }
    }
    for (step, (on, to, count)) in run.carries.iter().enumerate() {
        for side in [BROWSER, HOST] {
            if step == run.open_after[side] {
                let out = world.sides[side].machine.link_open(world.now, layout);
                world.apply(side, out);
            }
            world.take(side, 1);
        }
        if run.cut_at == Some(step) {
            world.cut_link();
            for side in [BROWSER, HOST] {
                if run.losers[side] {
                    let out = world.sides[side]
                        .machine
                        .link_lost(LeaveReason::Silence, world.now);
                    world.apply(side, out);
                }
            }
        }
        match on {
            Some(3) | None => world.carry_relay(*to, *count),
            Some(c) => world.carry_link(*to, &[Channel::ALL[*c]], *count),
        };
    }
    if run.cut_at.is_some() && !run.losers.iter().any(|l| *l) {
        let out = world.sides[HOST]
            .machine
            .link_lost(LeaveReason::Silence, world.now);
        world.apply(HOST, out);
    }
    world.settle();
    world.tick(10);
    world.settle();
    world
}

/// The frames of one lane, in the order they appear.
fn of_lane(labels: &[String], lane: usize) -> Vec<String> {
    labels
        .iter()
        .filter(|l| l.ends_with(&format!("-{lane}")))
        .cloned()
        .collect()
}

proptest! {
    #![proptest_config(ProptestConfig::with_cases(10_000))]

    #[test]
    fn any_drop_moment_keeps_every_channel_exactly_once_and_in_order(run in arb_run()) {
        let world = play(&run);
        for side in [BROWSER, HOST] {
            let peer = 1 - side;
            let (delivered, taken) = (&world.sides[side].delivered, &world.sides[peer].taken);
            prop_assert!(!world.sides[side].ended, "a reachable peer never leaves a side waiting");
            if run.separate {
                for lane in 0..3 {
                    prop_assert_eq!(of_lane(delivered, lane), of_lane(taken, lane));
                }
                prop_assert_eq!(delivered.len(), taken.len());
            } else {
                prop_assert_eq!(delivered, taken);
            }
            prop_assert_eq!(taken.len(), run.frames[peer].len(),
                "every frame a side queued was sent");
        }
    }
}
