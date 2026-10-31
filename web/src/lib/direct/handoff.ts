import { decodeFrame, encodeFrame } from '../protocol/codec';
import { FRAME_CONTROL, FRAME_DESKTOP, FRAME_FILE, type ControlMessage } from '../protocol/types';
import { MAX_LINK_FRAME, Reassembler, ReassemblyError, split } from './pieces';

/** Bytes a sender may have numbered on one channel of the link that the peer has not confirmed. */
export const WINDOW_BYTES = 4 * 1024 * 1024;

/** The typing channel's window when each job has a channel of its own; its lane caps it there. */
export const TYPING_WINDOW_BYTES = 256 * 1024;

/** A receiver confirms this long after the first frame it has not yet confirmed. */
export const ACK_DELAY_MS = 250;

/**
 * How long a side holding new frames waits for the peer's fall-back notice before the session
 * ends; re-sending without knowing what arrived could repeat a keystroke.
 */
export const PEER_FALLBACK_WAIT_MS = 10_000;

/** The time an attempt has to put both directions on the link before both stay on the relay. */
export const ATTEMPT_BUDGET_MS = 8_000;

/** The numbers the hand-off works to. */
export interface Config {
  window: number;
  typingWindow: number;
  attemptBudgetMs: number;
}

/** The shipped values. */
export const DEFAULT_CONFIG: Config = {
  window: WINDOW_BYTES,
  typingWindow: TYPING_WINDOW_BYTES,
  attemptBudgetMs: ATTEMPT_BUDGET_MS,
};

/** One of the link's ordered channels; typing is the one channel of a shared link. */
export type Channel = 'typing' | 'screen' | 'files';

/** Every channel, in the order a fall-back re-sends them. */
export const CHANNELS: readonly Channel[] = ['typing', 'screen', 'files'];

/** How the link's channels carry a session: one channel for every frame, or one per job. */
export type Layout = 'shared' | 'separate';

/** A listing page's message after its map header: the encoder writes its type first. */
const LISTING_PAGE_TYPE = Uint8Array.of(
  0xa4,
  ...new TextEncoder().encode('type'),
  0xb0,
  ...new TextEncoder().encode('FileListResponse'),
);

/**
 * The channel an encoded session frame rides when each job has a channel of its own: a listing
 * page is file data, which typing never waits behind.
 */
export function channelOf(frame: Uint8Array): Channel {
  if (frame[0] === FRAME_DESKTOP) return 'screen';
  if (frame[0] === FRAME_FILE || (frame[0] === FRAME_CONTROL && isListingPage(frame))) return 'files';
  return 'typing';
}

/**
 * Whether a control frame carries a listing page: past the frame's type byte and length, the
 * message is a map of at most fifteen keys, its header one byte.
 */
function isListingPage(frame: Uint8Array): boolean {
  const header = frame[5];
  if (header === undefined || header < 0x80 || header > 0x8f) return false;
  return LISTING_PAGE_TYPE.every((byte, i) => frame[6 + i] === byte);
}

/** Where this side sends session frames: relay, link, holding for the peer's notice, or ended. */
export type Path = 'relay' | 'link' | 'holding' | 'ended';

/** Why this side left the link. */
export type LeaveReason =
  | 'silence'
  | 'closed'
  | 'channel_error'
  | 'budget'
  | 'reassembly'
  | 'overflow'
  | 'peer_fell_back';

/** How an attempt ended. */
export type Outcome = 'direct' | 'no_route' | 'dropped';

/** What the caller does next, in order. */
export type Instruction =
  | { kind: 'relay'; frame: Uint8Array }
  | { kind: 'link'; channel: Channel; piece: Uint8Array }
  | { kind: 'deliver'; frame: Uint8Array }
  | { kind: 'signal'; message: ControlMessage }
  | { kind: 'close-link' }
  | { kind: 'end' }
  | { kind: 'outcome'; outcome: Outcome }
  | { kind: 'left'; reason: LeaveReason }
  | { kind: 'confirmed'; bytes: number };

/** A count for each channel. */
type Counts = ReadonlyMap<Channel, number>;

/** What this side sent on one channel and the peer has not yet confirmed. */
interface Sending {
  nextNumber: number;
  kept: [number, Uint8Array][];
  unconfirmed: number;
}

/** What this side received on one channel. */
interface Receiving {
  held: Uint8Array[];
  heldBytes: number;
  reassembler: Reassembler;
  delivered: number;
  unackedBytes: number;
}

/**
 * One side's hand-off between relay and link: events in, instructions out, no I/O. The caller
 * carries out the instructions it returns before feeding the next event.
 */
export class Handoff {
  private readonly config: Config;
  private currentPath: Path = 'relay';
  private currentLayout: Layout = 'shared';

  private hasAttempted = false;
  private switched = false;
  private leftLink = false;
  private direct = false;
  private budgetDeadline: number | null = null;
  private holdingDeadline: number | null = null;

  private readonly sending = new Map<Channel, Sending>(
    CHANNELS.map((c) => [c, { nextNumber: 1, kept: [], unconfirmed: 0 }]),
  );
  private readonly receiving = new Map<Channel, Receiving>(
    CHANNELS.map((c) => [c, { held: [], heldBytes: 0, reassembler: new Reassembler(), delivered: 0, unackedBytes: 0 }]),
  );
  private peerHasSwitched = false;
  private peerFallback: Counts | null = null;
  private firstUnacked: number | null = null;

  constructor(config: Config = DEFAULT_CONFIG) {
    this.config = config;
  }

  /** Where this side sends session frames now. */
  path(): Path {
    return this.currentPath;
  }

  /** How the link's channels carry the session once this side's link is open. */
  layout(): Layout {
    return this.currentLayout;
  }

  /** This side's link frames the peer has not yet confirmed, on every channel. */
  kept(): number {
    return [...this.sending.values()].reduce((sum, s) => sum + s.kept.length, 0);
  }

  /** Whether a frame for a channel may be taken: on the relay always, on the link in its window. */
  canTake(channel: Channel): boolean {
    switch (this.currentPath) {
      case 'relay':
        return true;
      case 'link': {
        const lane = this.laneOf(channel);
        return this.sendingOn(lane).unconfirmed < this.window(lane);
      }
      default:
        return false;
    }
  }

  /** The earliest moment {@link Handoff.tick} has work to do, or null. */
  nextDeadline(): number | null {
    const due = [
      this.budgetDeadline,
      this.holdingDeadline,
      this.firstUnacked === null ? null : this.firstUnacked + ACK_DELAY_MS,
    ].filter((t): t is number => t !== null);
    return due.length === 0 ? null : Math.min(...due);
  }

  /** Starts the session's one attempt and its budget. */
  beginAttempt(now: number): void {
    if (!this.hasAttempted) {
      this.hasAttempted = true;
      this.budgetDeadline = now + this.config.attemptBudgetMs;
    }
  }

  /** This side's link is open: its switch notice is its last session frame on the relay. */
  linkOpen(layout: Layout): Instruction[] {
    if (this.switched || this.leftLink) return [];
    this.switched = true;
    this.currentLayout = layout;
    this.currentPath = 'link';
    const out: Instruction[] = [{ kind: 'relay', frame: encode({ type: 'DirectSwitch' }) }];
    this.noteDirect(out);
    return out;
  }

  /** Sends one session frame the sending point took, while its channel can take one. */
  send(frame: Uint8Array): Instruction[] {
    switch (this.currentPath) {
      case 'relay':
        return [{ kind: 'relay', frame }];
      case 'link': {
        const lane = this.laneOf(channelOf(frame));
        const sending = this.sendingOn(lane);
        const out = pieces(lane, frame);
        sending.unconfirmed += frame.length;
        sending.kept.push([sending.nextNumber, frame]);
        sending.nextNumber++;
        return out;
      }
      default:
        return [];
    }
  }

  /** A whole relay frame: a notice moves this side, link setup is signalled, all else delivered. */
  fromRelay(frame: Uint8Array, now: number): Instruction[] {
    const message = controlMessage(frame);
    if (message?.type === 'DirectSwitch') return this.peerSwitched(now);
    if (message?.type === 'DirectFallback') return this.peerFellBack(counts(message), now);
    if (message && isLinkSetup(message)) return [{ kind: 'signal', message }];
    return [{ kind: 'deliver', frame }];
  }

  /** One piece from the link, on a channel. */
  fromLink(channel: Channel, piece: Uint8Array, now: number): Instruction[] {
    if (this.leftLink) return [];
    let frames: Uint8Array[];
    try {
      frames = this.receivingOn(channel).reassembler.push(piece);
    } catch (err) {
      if (err instanceof ReassemblyError) return this.leave('reassembly', now);
      throw err;
    }
    const out: Instruction[] = [];
    for (const frame of frames) {
      out.push(...this.linkFrame(channel, frame, now));
      if (this.leftLink) break;
    }
    return out;
  }

  /** This side's link failed, went silent, closed or errored. */
  linkLost(reason: LeaveReason, now: number): Instruction[] {
    return this.leave(reason, now);
  }

  /** Runs the timers due at now: confirmations, the attempt budget and the peer's silence. */
  tick(now: number): Instruction[] {
    const out: Instruction[] = [];
    if (this.firstUnacked !== null && now >= this.firstUnacked + ACK_DELAY_MS) {
      out.push(...this.confirm());
    }
    if (this.budgetDeadline !== null && now >= this.budgetDeadline) {
      this.budgetDeadline = null;
      if (!(this.switched && this.peerHasSwitched)) out.push(...this.leave('budget', now));
    }
    if (this.holdingDeadline !== null && now >= this.holdingDeadline) {
      this.holdingDeadline = null;
      this.currentPath = 'ended';
      out.push({ kind: 'end' });
    }
    return out;
  }

  /** The channel a frame for a channel rides under this side's layout. */
  private laneOf(channel: Channel): Channel {
    return this.currentLayout === 'separate' ? channel : 'typing';
  }

  /** A channel's window: typing's own when each job has a channel, else the shared one. */
  private window(channel: Channel): number {
    return channel === 'typing' && this.currentLayout === 'separate' ? this.config.typingWindow : this.config.window;
  }

  /** The most a side holds ahead of the peer's switch notice: the peer's windows and one frame. */
  private heldLimit(): number {
    return 2 * this.config.window + this.config.typingWindow + MAX_LINK_FRAME;
  }

  private sendingOn(channel: Channel): Sending {
    return this.sending.get(channel)!;
  }

  private receivingOn(channel: Channel): Receiving {
    return this.receiving.get(channel)!;
  }

  private linkFrame(channel: Channel, frame: Uint8Array, now: number): Instruction[] {
    const message = controlMessage(frame);
    if (message?.type === 'DirectAck') return this.confirmed(counts(message));
    if (!this.peerHasSwitched) {
      const receiving = this.receivingOn(channel);
      receiving.heldBytes += frame.length;
      receiving.held.push(frame);
      const held = [...this.receiving.values()].reduce((sum, r) => sum + r.heldBytes, 0);
      if (held > this.heldLimit()) return this.leave('overflow', now);
      return [];
    }
    return this.deliverLink(channel, frame, now);
  }

  private deliverLink(channel: Channel, frame: Uint8Array, now: number): Instruction[] {
    const receiving = this.receivingOn(channel);
    receiving.delivered++;
    receiving.unackedBytes += frame.length;
    this.firstUnacked ??= now;
    const out: Instruction[] = [{ kind: 'deliver', frame }];
    if (receiving.unackedBytes >= this.window(channel) / 4) out.push(...this.confirm());
    return out;
  }

  private confirm(): Instruction[] {
    for (const receiving of this.receiving.values()) receiving.unackedBytes = 0;
    this.firstUnacked = null;
    return pieces('typing', encode({ type: 'DirectAck', ...wireCounts(this.deliveredEach()) }));
  }

  private deliveredEach(): Counts {
    return new Map([...this.receiving].map(([channel, r]) => [channel, r.delivered]));
  }

  private confirmed(received: Counts): Instruction[] {
    let total = 0;
    for (const [channel, sending] of this.sending) {
      const upTo = received.get(channel) ?? 0;
      let bytes = 0;
      while (sending.kept.length > 0 && sending.kept[0]![0] <= upTo) {
        bytes += sending.kept.shift()![1].length;
      }
      sending.unconfirmed -= bytes;
      total += bytes;
    }
    return total === 0 ? [] : [{ kind: 'confirmed', bytes: total }];
  }

  private peerSwitched(now: number): Instruction[] {
    if (this.peerHasSwitched || this.leftLink) return [];
    this.peerHasSwitched = true;
    const out: Instruction[] = [];
    this.noteDirect(out);
    for (const [channel, receiving] of this.receiving) {
      const held = receiving.held;
      receiving.held = [];
      receiving.heldBytes = 0;
      for (const frame of held) out.push(...this.deliverLink(channel, frame, now));
    }
    return out;
  }

  private noteDirect(out: Instruction[]): void {
    if (this.switched && this.peerHasSwitched && !this.direct) {
      this.direct = true;
      this.budgetDeadline = null;
      out.push({ kind: 'outcome', outcome: 'direct' });
    }
  }

  private peerFellBack(received: Counts, now: number): Instruction[] {
    this.peerFallback = received;
    if (!this.leftLink) return this.leave('peer_fell_back', now);
    if (this.currentPath === 'holding') return this.resend(received);
    return [];
  }

  private leave(reason: LeaveReason, now: number): Instruction[] {
    if (this.leftLink) return [];
    this.leftLink = true;
    this.hasAttempted = true;
    this.budgetDeadline = null;
    for (const receiving of this.receiving.values()) {
      receiving.held = [];
      receiving.heldBytes = 0;
      receiving.reassembler.reset();
      receiving.unackedBytes = 0;
    }
    this.firstUnacked = null;

    const out: Instruction[] = [
      { kind: 'close-link' },
      { kind: 'relay', frame: encode({ type: 'DirectFallback', ...wireCounts(this.deliveredEach()) }) },
      { kind: 'outcome', outcome: this.direct ? 'dropped' : 'no_route' },
      { kind: 'left', reason },
    ];
    if (this.currentPath === 'link') {
      this.currentPath = 'holding';
      this.holdingDeadline = now + PEER_FALLBACK_WAIT_MS;
      if (this.peerFallback !== null) out.push(...this.resend(this.peerFallback));
    }
    return out;
  }

  private resend(received: Counts): Instruction[] {
    const out: Instruction[] = [];
    for (const [channel, sending] of this.sending) {
      const upTo = received.get(channel) ?? 0;
      for (const [n, frame] of sending.kept) {
        if (n > upTo) out.push({ kind: 'relay', frame });
      }
      sending.kept = [];
      sending.unconfirmed = 0;
    }
    this.currentPath = 'relay';
    this.holdingDeadline = null;
    return out;
  }
}

/** The per-channel counts a notice carries; an absent count is zero. */
function counts(message: { received?: number; received_screen?: number; received_files?: number }): Counts {
  return new Map<Channel, number>([
    ['typing', message.received ?? 0],
    ['screen', message.received_screen ?? 0],
    ['files', message.received_files ?? 0],
  ]);
}

/** A notice's counts with each zero left out, as the host's encoder writes them. */
function wireCounts(each: Counts) {
  const [received, screen, files] = [each.get('typing') ?? 0, each.get('screen') ?? 0, each.get('files') ?? 0];
  return {
    ...(received === 0 ? {} : { received }),
    ...(screen === 0 ? {} : { received_screen: screen }),
    ...(files === 0 ? {} : { received_files: files }),
  };
}

/** Whether a control message sets up the link: these travel on the relay outside the stream. */
function isLinkSetup(message: ControlMessage): boolean {
  return (
    message.type === 'DirectOffer' ||
    message.type === 'DirectAnswer' ||
    message.type === 'IceCandidate' ||
    message.type === 'DirectRefused'
  );
}

/** The control message a frame carries, or null for any other frame or one that does not decode. */
function controlMessage(frame: Uint8Array): ControlMessage | null {
  if (frame[0] !== FRAME_CONTROL) return null;
  try {
    const { frame: decoded } = decodeFrame(frame);
    return decoded.type === FRAME_CONTROL ? decoded.message : null;
  } catch {
    return null;
  }
}

function encode(message: ControlMessage): Uint8Array {
  return encodeFrame({ type: FRAME_CONTROL, message });
}

function pieces(channel: Channel, frame: Uint8Array): Instruction[] {
  return split(frame).map((piece) => ({ kind: 'link', channel, piece }));
}
