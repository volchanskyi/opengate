import fc from 'fast-check';
import { describe, expect, it } from 'vitest';
import { decodeFrame, encodeFrame } from '../protocol/codec';
import { FRAME_FILE, FRAME_TERMINAL } from '../protocol/types';
import {
  DEFAULT_CONFIG,
  Handoff,
  type Config,
  type Instruction,
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
  bulk: Uint8Array[];
  taken: string[];
  delivered: string[];
  outcomes: string[];
  ended: boolean;
  linkClosed: boolean;
}

/** Two sides, a relay that never loses anything, and a link that can be cut. */
class World {
  now = 0;
  readonly sides: [Side, Side];
  relay: [Uint8Array[], Uint8Array[]] = [[], []];
  link: [Uint8Array[], Uint8Array[]] = [[], []];
  cut = false;

  constructor(config: Config) {
    const side = (): Side => ({
      machine: new Handoff(config),
      interactive: [],
      bulk: [],
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
          if (!this.cut && !pick(this.sides, from).linkClosed) pick(this.link, to).push(instruction.piece);
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

  take(side: SideIndex, count: number): number {
    let taken = 0;
    const s = pick(this.sides, side);
    while (taken < count && !s.ended && s.machine.canTake()) {
      const frame = s.interactive.shift() ?? s.bulk.shift();
      if (!frame) break;
      s.taken.push(label(frame));
      this.apply(side, s.machine.send(frame));
      taken++;
    }
    return taken;
  }

  carry(onLink: boolean, to: SideIndex, count: number): number {
    let carried = 0;
    const queue = onLink ? pick(this.link, to) : pick(this.relay, to);
    while (carried < count) {
      const item = queue.shift();
      if (!item) break;
      const machine = pick(this.sides, to).machine;
      this.apply(to, onLink ? machine.fromLink(item, this.now) : machine.fromRelay(item, this.now));
      carried++;
    }
    return carried;
  }

  settle(): void {
    for (;;) {
      let moved = 0;
      for (const to of [BROWSER, HOST] as const) {
        moved += this.carry(false, to, Infinity);
        moved += this.carry(true, to, Infinity);
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
    this.link = [[], []];
  }
}

const decoder = new TextDecoder();

function label(frame: Uint8Array): string {
  const { frame: decoded } = decodeFrame(frame);
  if (decoded.type !== FRAME_TERMINAL && decoded.type !== FRAME_FILE) {
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
  throw new Error(`unknown lane ${lane}`);
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
        sides().forEach((s) => world.apply(s, pick(world.sides, s).machine.linkOpen()));
        break;
      case 'queue':
        for (const s of sides()) {
          for (const f of step.frames!) {
            const bytes = frame(step.lane!, f.label, f.size ?? 0);
            (step.lane === 'interactive' ? pick(world.sides, s).interactive : pick(world.sides, s).bulk).push(bytes);
          }
        }
        break;
      case 'take':
        sides().forEach((s) => world.take(s, step.count ?? Infinity));
        break;
      case 'carry':
        world.carry(step.path === 'link', sidesNamed(step.to!)[0]!, step.count ?? Infinity);
        break;
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
        pick(world.link, sidesNamed(step.to!)[0]!).push(new Uint8Array(step.bytes!));
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

describe('Handoff', () => {
  it('gives every shared scenario its exact deliveries', () => {
    const named = Object.entries(scenarios).sort(([a], [b]) => a.localeCompare(b));
    expect(named.length).toBeGreaterThanOrEqual(19);
    for (const [name, scenario] of named) {
      run(name.replace(/^.*\/|\.json$/g, ''), scenario);
    }
  });

  interface Run {
    frames: [[boolean, number][], [boolean, number][]];
    openAfter: [number, number];
    carries: [boolean, number, number][];
    cutAt: number | null;
    losers: [boolean, boolean];
  }

  const frames = fc.array(fc.tuple(fc.boolean(), fc.integer({ min: 1, max: 39_999 })), { maxLength: 9 });
  const runs: fc.Arbitrary<Run> = fc.record({
    frames: fc.tuple(frames, frames),
    openAfter: fc.tuple(fc.nat(11), fc.nat(11)),
    carries: fc.array(fc.tuple(fc.boolean(), fc.nat(1), fc.integer({ min: 1, max: 5 })), { maxLength: 39 }),
    cutAt: fc.option(fc.nat(39), { nil: null }),
    losers: fc.tuple(fc.boolean(), fc.boolean()),
  });

  function play(r: Run): World {
    const world = new World({ ...DEFAULT_CONFIG, window: 64 * 1024 });
    for (const side of [BROWSER, HOST] as const) {
      pick(world.sides, side).machine.beginAttempt(0);
      pick(r.frames, side).forEach(([interactive, size], n) => {
        const bytes = frame(interactive ? 'interactive' : 'bulk', `${side}-${n}`, size);
        (interactive ? pick(world.sides, side).interactive : pick(world.sides, side).bulk).push(bytes);
      });
    }
    r.carries.forEach(([onLink, to, count], step) => {
      for (const side of [BROWSER, HOST] as const) {
        if (step === pick(r.openAfter, side)) world.apply(side, pick(world.sides, side).machine.linkOpen());
        world.take(side, 1);
      }
      if (r.cutAt === step) {
        world.cutLink();
        for (const side of [BROWSER, HOST] as const) {
          if (pick(r.losers, side)) world.apply(side, pick(world.sides, side).machine.linkLost('silence', world.now));
        }
      }
      world.carry(onLink, to as SideIndex, count);
    });
    if (r.cutAt !== null && !r.losers.some(Boolean)) {
      world.apply(HOST, pick(world.sides, HOST).machine.linkLost('silence', world.now));
    }
    world.settle();
    world.tick(10);
    world.settle();
    return world;
  }

  it('keeps both directions exactly once and in order whenever the link drops', () => {
    fc.assert(
      fc.property(runs, (r) => {
        const world = play(r);
        for (const side of [BROWSER, HOST] as const) {
          const peer: SideIndex = side === BROWSER ? HOST : BROWSER;
          expect(pick(world.sides, side).ended, 'a reachable peer never leaves a side waiting').toBe(false);
          expect(pick(world.sides, side).delivered).toEqual(pick(world.sides, peer).taken);
          expect(pick(world.sides, peer).taken).toHaveLength(pick(r.frames, peer).length);
        }
      }),
      { numRuns: 10_000 },
    );
    // Ten thousand runs take seconds alone and longer beside the rest of the suite.
  }, 120_000);
});
