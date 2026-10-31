import fc from 'fast-check';
import { describe, expect, it } from 'vitest';
import { decodeFrame, encodeFrame } from '../protocol/codec';
import { MAX_LINK_FRAME, split } from './pieces';
import { FRAME_CONTROL, FRAME_DESKTOP, FRAME_FILE, FRAME_PING, FRAME_TERMINAL, type ControlMessage } from '../protocol/types';
import {
  ACK_DELAY_MS,
  CHANNELS,
  DEFAULT_CONFIG,
  Handoff,
  PEER_FALLBACK_WAIT_MS,
  channelOf,
  type Channel,
  type Config,
  type Instruction,
  type Layout,
  type LeaveReason,
} from './handoff';

const BROWSER = 0;
const HOST = 1;
type SideIndex = typeof BROWSER | typeof HOST;

/** The member of a pair belonging to one side. */
function pick<T>(pair: readonly [T, T], side: SideIndex): T {
  return side === BROWSER ? pair[0] : pair[1];
}

interface Side {
  machine: Handoff;
  interactive: Uint8Array[];
  screen: Uint8Array[];
  files: Uint8Array[];
  /** Bytes taken from the screen and files lanes, which share the bulk turn by bytes. */
  bulkTaken: [number, number];
  taken: string[];
  delivered: string[];
  outcomes: string[];
  ended: boolean;
  linkClosed: boolean;
}

type Channels = Map<Channel, Uint8Array[]>;

const noChannels = (): Channels => new Map(CHANNELS.map((c) => [c, []]));

/** Two sides, a relay that never loses anything, and a link of three channels that can be cut. */
class World {
  now = 0;
  readonly sides: [Side, Side];
  relay: [Uint8Array[], Uint8Array[]] = [[], []];
  link: [Channels, Channels] = [noChannels(), noChannels()];
  cut = false;

  constructor(config: Config) {
    const side = (): Side => ({
      machine: new Handoff(config),
      interactive: [],
      screen: [],
      files: [],
      bulkTaken: [0, 0],
      taken: [],
      delivered: [],
      outcomes: [],
      ended: false,
      linkClosed: false,
    });
    this.sides = [side(), side()];
  }

  apply(from: SideIndex, instructions: Instruction[]): void {
    const to: SideIndex = from === BROWSER ? HOST : BROWSER;
    for (const instruction of instructions) {
      switch (instruction.kind) {
        case 'relay':
          pick(this.relay, to).push(instruction.frame);
          break;
        case 'link':
          if (!this.cut && !pick(this.sides, from).linkClosed) {
            pick(this.link, to).get(instruction.channel)!.push(instruction.piece);
          }
          break;
        case 'deliver':
          pick(this.sides, from).delivered.push(label(instruction.frame));
          break;
        case 'close-link':
          pick(this.sides, from).linkClosed = true;
          break;
        case 'end':
          pick(this.sides, from).ended = true;
          break;
        case 'outcome':
          pick(this.sides, from).outcomes.push(instruction.outcome);
          break;
        default:
          break;
      }
    }
  }

  /** The lane the sending point takes next: typing first, then the bulk lane with fewer bytes. */
  private nextLane(s: Side): Channel | null {
    if (s.interactive.length > 0 && s.machine.canTake('typing')) return 'typing';
    const screen = s.screen.length > 0 && s.machine.canTake('screen');
    const files = s.files.length > 0 && s.machine.canTake('files');
    if (screen && files) return s.bulkTaken[1] < s.bulkTaken[0] ? 'files' : 'screen';
    if (screen) return 'screen';
    return files ? 'files' : null;
  }

  take(side: SideIndex, count: number): number {
    let taken = 0;
    const s = pick(this.sides, side);
    while (taken < count && !s.ended) {
      const lane = this.nextLane(s);
      if (lane === null) break;
      const frame = laneOf(s, lane === 'typing' ? 'interactive' : lane).shift()!;
      if (lane === 'screen') s.bulkTaken[0] += frame.length;
      if (lane === 'files') s.bulkTaken[1] += frame.length;
      s.taken.push(label(frame));
      this.apply(side, s.machine.send(frame));
      taken++;
    }
    return taken;
  }

  carryRelay(to: SideIndex, count: number): number {
    let carried = 0;
    const queue = pick(this.relay, to);
    while (carried < count) {
      const frame = queue.shift();
      if (!frame) break;
      this.apply(to, pick(this.sides, to).machine.fromRelay(frame, this.now));
      carried++;
    }
    return carried;
  }

  /** Carries pieces on the named channels, each channel drained in turn. */
  carryLink(to: SideIndex, channels: readonly Channel[], count: number): number {
    let carried = 0;
    for (const channel of channels) {
      const queue = pick(this.link, to).get(channel)!;
      while (carried < count) {
        const piece = queue.shift();
        if (!piece) break;
        this.apply(to, pick(this.sides, to).machine.fromLink(channel, piece, this.now));
        carried++;
      }
    }
    return carried;
  }

  settle(): void {
    for (;;) {
      let moved = 0;
      for (const to of [BROWSER, HOST] as const) {
        moved += this.carryRelay(to, Infinity);
        moved += this.carryLink(to, CHANNELS, Infinity);
      }
      moved += this.take(BROWSER, Infinity);
      moved += this.take(HOST, Infinity);
      if (moved === 0) return;
    }
  }

  tick(ms: number): void {
    this.now += ms;
    for (const side of [BROWSER, HOST] as const) {
      this.apply(side, pick(this.sides, side).machine.tick(this.now));
    }
  }

  cutLink(): void {
    this.cut = true;
    this.link = [noChannels(), noChannels()];
  }
}

const decoder = new TextDecoder();

function label(frame: Uint8Array): string {
  const { frame: decoded } = decodeFrame(frame);
  if (decoded.type !== FRAME_TERMINAL && decoded.type !== FRAME_FILE && decoded.type !== FRAME_DESKTOP) {
    throw new Error(`a scenario delivers only its own frames, got type ${decoded.type}`);
  }
  const data = decoded.frame.data;
  const end = data.indexOf(0);
  return decoder.decode(end === -1 ? data : data.subarray(0, end));
}

function frame(lane: string, name: string, size: number): Uint8Array {
  const named = new TextEncoder().encode(name);
  const data = new Uint8Array(Math.max(size, named.length));
  data.set(named);
  if (lane === 'interactive') return encodeFrame({ type: FRAME_TERMINAL, frame: { data } });
  if (lane === 'bulk') return encodeFrame({ type: FRAME_FILE, frame: { offset: 0, total_size: 0, data } });
  if (lane === 'screen') {
    const picture = { sequence: 0, x: 0, y: 0, width: 0, height: 0, encoding: 'Raw' as const, data };
    return encodeFrame({ type: FRAME_DESKTOP, frame: picture });
  }
  throw new Error(`unknown lane ${lane}`);
}

/** A typing frame of exactly length encoded bytes. */
function typedOfLength(length: number): Uint8Array {
  for (let size = Math.max(0, length - 16); size <= length; size++) {
    const typed = encodeFrame({ type: FRAME_TERMINAL, frame: { data: new Uint8Array(size) } });
    if (typed.length === length) return typed;
  }
  throw new Error(`no typing frame encodes to ${length} bytes`);
}

/** Frames back to back, as one piece carrying them all. */
function joined(...frames: Uint8Array[]): Uint8Array {
  const out = new Uint8Array(frames.reduce((sum, f) => sum + f.length, 0));
  let at = 0;
  for (const f of frames) {
    out.set(f, at);
    at += f.length;
  }
  return out;
}

function laneOf(side: Side, lane: string): Uint8Array[] {
  if (lane === 'interactive') return side.interactive;
  return lane === 'screen' ? side.screen : side.files;
}

function sidesNamed(name: string): SideIndex[] {
  if (name === 'browser') return [BROWSER];
  if (name === 'host') return [HOST];
  if (name === 'both') return [BROWSER, HOST];
  throw new Error(`unknown side ${name}`);
}

const REASONS = new Map<string, LeaveReason>([
  ['silence', 'silence'],
  ['closed', 'closed'],
  ['channel_error', 'channel_error'],
]);

interface Expectation {
  delivered?: string[];
  path?: string;
  outcomes?: string[];
  ended?: boolean;
  kept?: number;
}

interface Step extends Expectation {
  step: string;
  side?: string;
  channels?: Layout;
  channel?: Channel;
  lane?: string;
  frames?: { label: string; size?: number }[];
  count?: number;
  path?: string;
  to?: string;
  reason?: string;
  ms?: number;
  bytes?: number[];
}

interface Scenario {
  window?: number;
  budget_ms?: number;
  steps: Step[];
  expect: { browser: Expectation; host: Expectation };
}

function check(world: World, side: SideIndex, expected: Expectation, at: string): void {
  const s = pick(world.sides, side);
  const who = side === BROWSER ? 'browser' : 'host';
  if (expected.delivered) expect(s.delivered, `${at}: ${who} delivered`).toEqual(expected.delivered);
  if (expected.path) expect(s.machine.path(), `${at}: ${who} path`).toBe(expected.path);
  if (expected.outcomes) expect(s.outcomes, `${at}: ${who} outcomes`).toEqual(expected.outcomes);
  if (expected.ended !== undefined) expect(s.ended, `${at}: ${who} ended`).toBe(expected.ended);
  if (expected.kept !== undefined) expect(s.machine.kept(), `${at}: ${who} kept`).toBe(expected.kept);
}

function run(name: string, scenario: Scenario): void {
  const world = new World({
    ...DEFAULT_CONFIG,
    window: scenario.window ?? DEFAULT_CONFIG.window,
    attemptBudgetMs: scenario.budget_ms ?? DEFAULT_CONFIG.attemptBudgetMs,
  });
  scenario.steps.forEach((step, i) => {
    const at = `${name} step ${i}`;
    const sides = () => sidesNamed(step.side!);
    switch (step.step) {
      case 'begin':
        sides().forEach((s) => pick(world.sides, s).machine.beginAttempt(world.now));
        break;
      case 'open':
        sides().forEach((s) => world.apply(s, pick(world.sides, s).machine.linkOpen(step.channels ?? 'shared')));
        break;
      case 'queue':
        for (const s of sides()) {
          for (const f of step.frames!) {
            const bytes = frame(step.lane!, f.label, f.size ?? 0);
            laneOf(pick(world.sides, s), step.lane!).push(bytes);
          }
        }
        break;
      case 'take':
        sides().forEach((s) => world.take(s, step.count ?? Infinity));
        break;
      case 'carry': {
        const to = sidesNamed(step.to!)[0]!;
        if (step.path === 'link') world.carryLink(to, step.channel ? [step.channel] : CHANNELS, step.count ?? Infinity);
        else world.carryRelay(to, step.count ?? Infinity);
        break;
      }
      case 'cut':
        world.cutLink();
        break;
      case 'lose':
        for (const s of sides()) {
          world.apply(s, pick(world.sides, s).machine.linkLost(REASONS.get(step.reason!)!, world.now));
        }
        break;
      case 'tick':
        world.tick(step.ms!);
        break;
      case 'settle':
        world.settle();
        break;
      case 'inject':
        pick(world.link, sidesNamed(step.to!)[0]!)
          .get(step.channel ?? 'typing')!
          .push(new Uint8Array(step.bytes!));
        break;
      case 'expect':
        sides().forEach((s) => check(world, s, step, at));
        break;
      default:
        throw new Error(`${at}: unknown step ${step.step}`);
    }
  });
  check(world, BROWSER, scenario.expect.browser, `${name} end`);
  check(world, HOST, scenario.expect.host, `${name} end`);
}

// The shared scenarios, read through the alias the test config points at testdata/direct-link.
const scenarios = import.meta.glob<Scenario>('@direct-link-scenarios/*.json', { eager: true, import: 'default' });

describe('channelOf', () => {
  it('sends a listing page on the files channel and its request and refusal on typing', () => {
    const entries = [{ name: 'syslog', is_dir: false, size: 4096, modified: 1_700_000_200 }];
    const page = (more?: boolean): Uint8Array =>
      encodeFrame({ type: FRAME_CONTROL, message: { type: 'FileListResponse', path: '/var/log', entries, more } });
    expect(channelOf(page(true))).toBe('files');
    expect(channelOf(page())).toBe('files');
    expect(channelOf(encodeFrame({ type: FRAME_CONTROL, message: { type: 'FileListRequest', path: '/var/log' } }))).toBe('typing');
    expect(
      channelOf(encodeFrame({ type: FRAME_CONTROL, message: { type: 'FileListError', path: '/var/log', error: 'denied' } })),
    ).toBe('typing');
  });
});

describe('Handoff', () => {
  it('holds frames ahead of the switch notice up to the peer\'s windows on all channels', () => {
    const side = new Handoff();
    const frame = encodeFrame({ type: FRAME_FILE, frame: { offset: 0, total_size: 0, data: new Uint8Array(256 * 1024) } });
    const limit = 2 * DEFAULT_CONFIG.window + DEFAULT_CONFIG.typingWindow + MAX_LINK_FRAME;
    const feed = (channel: Channel) => split(frame).flatMap((piece) => side.fromLink(channel, piece, 0));
    let held = 0;
    for (const channel of ['screen', 'files'] as const) {
      for (let i = 0; i < DEFAULT_CONFIG.window / frame.length - 1; i++) {
        expect(feed(channel)).toEqual([]);
        held += frame.length;
      }
    }
    let left = false;
    while (!left && held <= limit) {
      left = feed('typing').some((step) => step.kind === 'left' && step.reason === 'overflow');
      if (!left) held += frame.length;
    }
    expect(left).toBe(true);
    expect(held).toBeLessThanOrEqual(limit);
    expect(held + frame.length).toBeGreaterThan(limit);
  });

  const small: Config = { ...DEFAULT_CONFIG, window: 64 * 1024, typingWindow: 32 * 1024 };
  const heldLimit = 2 * small.window + small.typingWindow + MAX_LINK_FRAME;
  const notice = (message: ControlMessage) => encodeFrame({ type: FRAME_CONTROL, message });
  const SWITCH = notice({ type: 'DirectSwitch' });

  it('takes frames until a channel holds its window unconfirmed, typing its own when each job has a channel', () => {
    const shared = new Handoff(small);
    shared.linkOpen('shared');
    expect(shared.canTake('typing')).toBe(true);
    shared.send(typedOfLength(small.window));
    expect(shared.canTake('typing')).toBe(false);

    const separate = new Handoff(small);
    separate.linkOpen('separate');
    separate.send(typedOfLength(small.typingWindow));
    expect(separate.canTake('typing')).toBe(false);
    expect(separate.canTake('screen')).toBe(true);
  });

  it('holds exactly the peer\'s windows and one frame ahead of the switch notice, and leaves at a byte more', () => {
    const side = new Handoff(small);
    expect(side.fromLink('files', typedOfLength(MAX_LINK_FRAME), 0)).toEqual([]);
    expect(side.fromLink('screen', typedOfLength(heldLimit - MAX_LINK_FRAME), 0)).toEqual([]);
    expect(side.fromLink('typing', Uint8Array.of(FRAME_PING), 0)).toContainEqual({ kind: 'left', reason: 'overflow' });
  });

  it('delivers every frame a piece completes, and nothing after the frame that made it leave', () => {
    const switched = new Handoff(small);
    switched.fromRelay(SWITCH, 0);
    const [a, b] = [typedOfLength(100), typedOfLength(200)];
    const delivered = switched.fromLink('typing', joined(a, b), 0).filter((step) => step.kind === 'deliver');
    expect(delivered).toEqual([
      { kind: 'deliver', frame: a },
      { kind: 'deliver', frame: b },
    ]);

    const waiting = new Handoff(small);
    waiting.linkOpen('shared');
    waiting.send(typedOfLength(100));
    const past = joined(
      typedOfLength(MAX_LINK_FRAME),
      typedOfLength(heldLimit - MAX_LINK_FRAME + 1),
      notice({ type: 'DirectAck', received: 1 }),
    );
    expect(waiting.fromLink('typing', past, 0).at(-1)).toEqual({ kind: 'left', reason: 'overflow' });
  });

  it('reports the bytes each confirmation frees, and nothing for one that frees none', () => {
    const side = new Handoff(small);
    side.linkOpen('shared');
    side.fromRelay(SWITCH, 0);
    side.send(typedOfLength(300));
    side.send(typedOfLength(500));
    expect(side.fromLink('typing', notice({ type: 'DirectAck', received: 2 }), 0)).toEqual([{ kind: 'confirmed', bytes: 800 }]);
    expect(side.fromLink('typing', notice({ type: 'DirectAck', received: 2 }), 0)).toEqual([]);
    expect(side.kept()).toBe(0);
  });

  it('confirms at once when a channel has taken a quarter of its window unconfirmed', () => {
    const side = new Handoff(small);
    side.fromRelay(SWITCH, 0);
    expect(side.fromLink('typing', typedOfLength(small.window / 4 - 1), 0).map((step) => step.kind)).toEqual(['deliver']);
    expect(side.tick(0)).toEqual([]);

    const quarter = new Handoff(small);
    quarter.fromRelay(SWITCH, 0);
    expect(quarter.fromLink('typing', typedOfLength(small.window / 4), 0).map((step) => step.kind)).toEqual([
      'deliver',
      'link',
    ]);
  });

  it('runs one budget from the first begin, and confirms on time without falling back inside it', () => {
    const side = new Handoff();
    side.beginAttempt(0);
    side.beginAttempt(5_000);
    expect(side.nextDeadline()).toBe(DEFAULT_CONFIG.attemptBudgetMs);

    side.fromRelay(SWITCH, 0);
    side.fromLink('typing', typedOfLength(100), 100);
    expect(side.nextDeadline()).toBe(100 + ACK_DELAY_MS);
    const confirming = side.tick(100 + ACK_DELAY_MS);
    expect(confirming.map((step) => step.kind)).toEqual(['link']);
    expect(side.nextDeadline()).toBe(DEFAULT_CONFIG.attemptBudgetMs);
  });

  it.each([
    ['only this side switched', (side: Handoff) => side.linkOpen('shared')],
    ['only the peer switched', (side: Handoff) => side.fromRelay(SWITCH, 0)],
  ])('falls back at the budget when %s', (_name, switchOne) => {
    const side = new Handoff();
    side.beginAttempt(0);
    switchOne(side);
    expect(side.tick(DEFAULT_CONFIG.attemptBudgetMs)).toContainEqual({ kind: 'left', reason: 'budget' });
  });

  it('stays on the relay with nothing pending when the budget runs out before any link', () => {
    const side = new Handoff();
    side.beginAttempt(0);
    expect(side.tick(DEFAULT_CONFIG.attemptBudgetMs)).toContainEqual({ kind: 'left', reason: 'budget' });
    expect(side.path()).toBe('relay');
    expect(side.nextDeadline()).toBeNull();
  });

  it('leaves the link once, however many ways it is lost', () => {
    const side = new Handoff();
    side.beginAttempt(0);
    side.linkOpen('shared');
    expect(side.linkLost('silence', 0)).toContainEqual({ kind: 'left', reason: 'silence' });
    expect(side.linkLost('closed', 0)).toEqual([]);
  });

  it('takes no switch notice that arrives after it left the link', () => {
    const side = new Handoff();
    side.beginAttempt(0);
    side.linkOpen('shared');
    side.linkLost('silence', 0);
    expect(side.fromRelay(SWITCH, 0)).toEqual([]);
  });

  it('re-sends nothing for a fall-back notice that arrives after the session ended', () => {
    const side = new Handoff();
    side.beginAttempt(0);
    side.linkOpen('shared');
    side.send(typedOfLength(100));
    side.linkLost('closed', 0);
    expect(side.tick(PEER_FALLBACK_WAIT_MS)).toContainEqual({ kind: 'end' });
    expect(side.fromRelay(notice({ type: 'DirectFallback' }), PEER_FALLBACK_WAIT_MS + 1)).toEqual([]);
    expect(side.path()).toBe('ended');
  });

  it.each<ControlMessage>([
    { type: 'DirectOffer', sdp: 'v=0 browser offer' },
    { type: 'DirectAnswer', sdp: 'v=0 host answer' },
    { type: 'IceCandidate', candidate: 'candidate:2 1 udp 1 10.0.0.2 6000 typ host', mid: '0' },
    { type: 'DirectRefused', reason: 'direct links are off on this host' },
  ])('signals $type from the relay outside the session stream', (message) => {
    expect(new Handoff().fromRelay(notice(message), 0)).toEqual([{ kind: 'signal', message }]);
  });

  it('gives every shared scenario its exact deliveries', () => {
    const named = Object.entries(scenarios).sort(([a], [b]) => a.localeCompare(b));
    expect(named.length).toBeGreaterThanOrEqual(22);
    for (const [name, scenario] of named) {
      run(name.replace(/^.*\/|\.json$/g, ''), scenario);
    }
  });

  const LANES = ['interactive', 'screen', 'bulk'] as const;

  interface Run {
    frames: [[number, number][], [number, number][]];
    separate: boolean;
    openAfter: [number, number];
    /** Which channel a carry drains (3 is the relay), to which side, how many. */
    carries: [number, number, number][];
    cutAt: number | null;
    losers: [boolean, boolean];
  }

  const frames = fc.array(fc.tuple(fc.nat(2), fc.integer({ min: 1, max: 39_999 })), { maxLength: 9 });
  const runs: fc.Arbitrary<Run> = fc.record({
    frames: fc.tuple(frames, frames),
    separate: fc.boolean(),
    openAfter: fc.tuple(fc.nat(11), fc.nat(11)),
    carries: fc.array(fc.tuple(fc.nat(3), fc.nat(1), fc.integer({ min: 1, max: 5 })), { maxLength: 39 }),
    cutAt: fc.option(fc.nat(39), { nil: null }),
    losers: fc.tuple(fc.boolean(), fc.boolean()),
  });

  function play(r: Run): World {
    const world = new World({ ...DEFAULT_CONFIG, window: 64 * 1024, typingWindow: 32 * 1024 });
    const layout: Layout = r.separate ? 'separate' : 'shared';
    for (const side of [BROWSER, HOST] as const) {
      pick(world.sides, side).machine.beginAttempt(0);
      pick(r.frames, side).forEach(([lane, size], n) => {
        const name = LANES.at(lane)!;
        laneOf(pick(world.sides, side), name).push(frame(name, `${side}-${n}-${lane}`, size));
      });
    }
    r.carries.forEach(([on, to, count], step) => {
      for (const side of [BROWSER, HOST] as const) {
        if (step === pick(r.openAfter, side)) world.apply(side, pick(world.sides, side).machine.linkOpen(layout));
        world.take(side, 1);
      }
      if (r.cutAt === step) {
        world.cutLink();
        for (const side of [BROWSER, HOST] as const) {
          if (pick(r.losers, side)) world.apply(side, pick(world.sides, side).machine.linkLost('silence', world.now));
        }
      }
      if (on === 3) world.carryRelay(to as SideIndex, count);
      else world.carryLink(to as SideIndex, [CHANNELS.at(on)!], count);
    });
    if (r.cutAt !== null && !r.losers.some(Boolean)) {
      world.apply(HOST, pick(world.sides, HOST).machine.linkLost('silence', world.now));
    }
    world.settle();
    world.tick(10);
    world.settle();
    return world;
  }

  const ofLane = (labels: string[], lane: number) => labels.filter((l) => l.endsWith(`-${lane}`));

  it('keeps every channel exactly once and in order whenever the link drops', () => {
    fc.assert(
      fc.property(runs, (r) => {
        const world = play(r);
        for (const side of [BROWSER, HOST] as const) {
          const peer: SideIndex = side === BROWSER ? HOST : BROWSER;
          const delivered = pick(world.sides, side).delivered;
          const taken = pick(world.sides, peer).taken;
          expect(pick(world.sides, side).ended, 'a reachable peer never leaves a side waiting').toBe(false);
          if (r.separate) {
            for (const lane of [0, 1, 2]) expect(ofLane(delivered, lane)).toEqual(ofLane(taken, lane));
            expect(delivered).toHaveLength(taken.length);
          } else {
            expect(delivered).toEqual(taken);
          }
          expect(taken).toHaveLength(pick(r.frames, peer).length);
        }
      }),
      { numRuns: 10_000 },
    );
    // Ten thousand runs take seconds alone and longer beside the rest of the suite.
  }, 120_000);
});
