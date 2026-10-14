//! Replays the shared direct-link scenarios in `testdata/direct-link/` against two hand-offs joined
//! by a scripted relay and link, and holds random link failures to exactly-once, in-order delivery.

use std::collections::VecDeque;
use std::path::PathBuf;

use mesh_protocol::direct::{Config, Handoff, Instruction, LeaveReason, Outcome, Path};
use mesh_protocol::{FileFrame, Frame, TerminalFrame};
use proptest::prelude::*;
use serde_json::Value;

const BROWSER: usize = 0;
const HOST: usize = 1;

struct Side {
    machine: Handoff,
    interactive: VecDeque<Vec<u8>>,
    bulk: VecDeque<Vec<u8>>,
    taken: Vec<String>,
    delivered: Vec<String>,
    outcomes: Vec<String>,
    ended: bool,
    link_closed: bool,
}

/// Two sides, a relay that never loses anything, and a link that can be cut.
struct World {
    now: u64,
    sides: [Side; 2],
    relay: [VecDeque<Vec<u8>>; 2],
    link: [VecDeque<Vec<u8>>; 2],
    cut: bool,
}

impl World {
    fn new(config: Config) -> Self {
        let side = || Side {
            machine: Handoff::new(config),
            interactive: VecDeque::new(),
            bulk: VecDeque::new(),
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
            link: [VecDeque::new(), VecDeque::new()],
            cut: false,
        }
    }

    fn apply(&mut self, from: usize, instructions: Vec<Instruction>) {
        let to = 1 - from;
        for instruction in instructions {
            match instruction {
                Instruction::Relay(frame) => self.relay[to].push_back(frame),
                Instruction::Link(piece) => {
                    if !self.cut && !self.sides[from].link_closed {
                        self.link[to].push_back(piece);
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

    fn take(&mut self, side: usize, count: usize) -> usize {
        let mut taken = 0;
        while taken < count && !self.sides[side].ended && self.sides[side].machine.can_take() {
            let s = &mut self.sides[side];
            let Some(frame) = s.interactive.pop_front().or_else(|| s.bulk.pop_front()) else {
                break;
            };
            s.taken.push(label(&frame));
            let out = s.machine.send(frame, self.now);
            self.apply(side, out);
            taken += 1;
        }
        taken
    }

    fn carry(&mut self, on_link: bool, to: usize, count: usize) -> usize {
        let mut carried = 0;
        while carried < count {
            let queue = if on_link {
                &mut self.link[to]
            } else {
                &mut self.relay[to]
            };
            let Some(item) = queue.pop_front() else { break };
            let now = self.now;
            let out = if on_link {
                self.sides[to].machine.from_link(&item, now)
            } else {
                self.sides[to].machine.from_relay(item, now)
            };
            self.apply(to, out);
            carried += 1;
        }
        carried
    }

    fn settle(&mut self) {
        loop {
            let mut moved = 0;
            for to in [BROWSER, HOST] {
                moved += self.carry(false, to, usize::MAX);
                moved += self.carry(true, to, usize::MAX);
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
                    let out = world.sides[s].machine.link_open(world.now);
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
                        if lane == "interactive" {
                            target.interactive.push_back(bytes);
                        } else {
                            target.bulk.push_back(bytes);
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
                world.carry(step["path"] == "link", to, count(step));
            }
            "cut" => {
                world.cut = true;
                world.link = [VecDeque::new(), VecDeque::new()];
            }
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
                world.link[to].push_back(bytes);
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
        names.len() >= 19,
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

/// One random run: what each side sends, when the link opens, and where it dies.
#[derive(Debug, Clone)]
struct Run {
    frames: [Vec<(bool, usize)>; 2],
    open_after: [usize; 2],
    carries: Vec<(bool, usize, usize)>,
    cut_at: Option<usize>,
    losers: [bool; 2],
}

fn arb_run() -> impl Strategy<Value = Run> {
    let frames = || prop::collection::vec((any::<bool>(), 1usize..40_000), 0..10);
    (
        frames(),
        frames(),
        (0usize..12, 0usize..12),
        prop::collection::vec((any::<bool>(), 0usize..2, 1usize..6), 0..40),
        prop::option::of(0usize..40),
        (any::<bool>(), any::<bool>()),
    )
        .prop_map(|(a, b, open, carries, cut_at, losers)| Run {
            frames: [a, b],
            open_after: [open.0, open.1],
            carries,
            cut_at,
            losers: [losers.0, losers.1],
        })
}

fn play(run: &Run) -> World {
    let mut world = World::new(Config {
        window: 64 * 1024,
        ..Config::default()
    });
    for side in [BROWSER, HOST] {
        world.sides[side].machine.begin_attempt(0);
        for (n, (interactive, size)) in run.frames[side].iter().enumerate() {
            let lane = if *interactive { "interactive" } else { "bulk" };
            let bytes = frame(lane, &format!("{side}-{n}"), *size);
            let s = &mut world.sides[side];
            if *interactive {
                s.interactive.push_back(bytes);
            } else {
                s.bulk.push_back(bytes);
            }
        }
    }
    for (step, (on_link, to, count)) in run.carries.iter().enumerate() {
        for side in [BROWSER, HOST] {
            if step == run.open_after[side] {
                let out = world.sides[side].machine.link_open(world.now);
                world.apply(side, out);
            }
            world.take(side, 1);
        }
        if run.cut_at == Some(step) {
            world.cut = true;
            world.link = [VecDeque::new(), VecDeque::new()];
            for side in [BROWSER, HOST] {
                if run.losers[side] {
                    let out = world.sides[side]
                        .machine
                        .link_lost(LeaveReason::Silence, world.now);
                    world.apply(side, out);
                }
            }
        }
        world.carry(*on_link, *to, *count);
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

proptest! {
    #![proptest_config(ProptestConfig::with_cases(10_000))]

    #[test]
    fn any_drop_moment_keeps_both_directions_exactly_once_and_in_order(run in arb_run()) {
        let world = play(&run);
        for side in [BROWSER, HOST] {
            let peer = 1 - side;
            prop_assert!(!world.sides[side].ended, "a reachable peer never leaves a side waiting");
            prop_assert_eq!(&world.sides[side].delivered, &world.sides[peer].taken);
            prop_assert_eq!(world.sides[peer].taken.len(), run.frames[peer].len(),
                "every frame a side queued was sent");
        }
    }
}
