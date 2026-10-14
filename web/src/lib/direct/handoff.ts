import { decodeFrame, encodeFrame } from '../protocol/codec';
import { FRAME_CONTROL, type ControlMessage } from '../protocol/types';
import { MAX_LINK_FRAME, Reassembler, ReassemblyError, split } from './pieces';

/** Bytes a sender may have numbered on the link that the peer has not yet confirmed. */
export const WINDOW_BYTES = 4 * 1024 * 1024;

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
  attemptBudgetMs: number;
}

/** The shipped values. */
export const DEFAULT_CONFIG: Config = { window: WINDOW_BYTES, attemptBudgetMs: ATTEMPT_BUDGET_MS };

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
  | { kind: 'link'; piece: Uint8Array }
  | { kind: 'deliver'; frame: Uint8Array }
  | { kind: 'signal'; message: ControlMessage }
  | { kind: 'close-link' }
  | { kind: 'end' }
  | { kind: 'outcome'; outcome: Outcome }
  | { kind: 'left'; reason: LeaveReason }
  | { kind: 'confirmed'; bytes: number };

/**
 * One side's hand-off between relay and link: events in, instructions out, no I/O. The caller
 * carries out the instructions it returns before feeding the next event.
 */
export class Handoff {
  private readonly config: Config;
  private currentPath: Path = 'relay';

  private hasAttempted = false;
  private switched = false;
  private leftLink = false;
  private direct = false;
  private budgetDeadline: number | null = null;
  private holdingDeadline: number | null = null;

  private nextNumber = 1;
  private keptFrames: [number, Uint8Array][] = [];
  private unconfirmed = 0;

  private peerHasSwitched = false;
  private peerFallback: number | null = null;
  private held: Uint8Array[] = [];
  private heldBytes = 0;
  private readonly reassembler = new Reassembler();
  private deliveredCount = 0;
  private unackedBytes = 0;
  private firstUnacked: number | null = null;

  constructor(config: Config = DEFAULT_CONFIG) {
    this.config = config;
  }

  /** Where this side sends session frames now. */
  path(): Path {
    return this.currentPath;
  }

  /** Whether this session has made its one attempt. */
  attempted(): boolean {
    return this.hasAttempted;
  }

  /** The peer's link frames handed to the features so far. */
  delivered(): number {
    return this.deliveredCount;
  }

  /** This side's link frames the peer has not yet confirmed. */
  kept(): number {
    return this.keptFrames.length;
  }

  /** Whether a frame may be taken: on the relay always, on the link within the window. */
  canTake(): boolean {
    switch (this.currentPath) {
      case 'relay':
        return true;
      case 'link':
        return this.unconfirmed < this.config.window;
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
  linkOpen(): Instruction[] {
    if (this.switched || this.leftLink || this.currentPath !== 'relay') return [];
    this.switched = true;
    this.currentPath = 'link';
    const out: Instruction[] = [{ kind: 'relay', frame: encode({ type: 'DirectSwitch' }) }];
    this.noteDirect(out);
    return out;
  }

  /** Sends one session frame the sending point took; call only while {@link Handoff.canTake}. */
  send(frame: Uint8Array): Instruction[] {
    switch (this.currentPath) {
      case 'relay':
        return [{ kind: 'relay', frame }];
      case 'link': {
        const out = pieces(frame);
        this.unconfirmed += frame.length;
        this.keptFrames.push([this.nextNumber, frame]);
        this.nextNumber++;
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
    if (message?.type === 'DirectFallback') return this.peerFellBack(message.received ?? 0, now);
    if (message && isLinkSetup(message)) return [{ kind: 'signal', message }];
    return [{ kind: 'deliver', frame }];
  }

  /** One piece from the link. */
  fromLink(piece: Uint8Array, now: number): Instruction[] {
    if (this.leftLink) return [];
    let frames: Uint8Array[];
    try {
      frames = this.reassembler.push(piece);
    } catch (err) {
      if (err instanceof ReassemblyError) return this.leave('reassembly', now);
      throw err;
    }
    const out: Instruction[] = [];
    for (const frame of frames) {
      out.push(...this.linkFrame(frame, now));
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

  private linkFrame(frame: Uint8Array, now: number): Instruction[] {
    const message = controlMessage(frame);
    if (message?.type === 'DirectAck') return this.confirmed(message.received ?? 0);
    if (!this.peerHasSwitched) {
      this.heldBytes += frame.length;
      this.held.push(frame);
      if (this.heldBytes > this.config.window + MAX_LINK_FRAME) return this.leave('overflow', now);
      return [];
    }
    return this.deliverLink(frame, now);
  }

  private deliverLink(frame: Uint8Array, now: number): Instruction[] {
    this.deliveredCount++;
    this.unackedBytes += frame.length;
    this.firstUnacked ??= now;
    const out: Instruction[] = [{ kind: 'deliver', frame }];
    if (this.unackedBytes >= this.config.window / 4) out.push(...this.confirm());
    return out;
  }

  private confirm(): Instruction[] {
    this.unackedBytes = 0;
    this.firstUnacked = null;
    if (this.leftLink) return [];
    return pieces(encode({ type: 'DirectAck', received: this.deliveredCount }));
  }

  private confirmed(received: number): Instruction[] {
    let bytes = 0;
    while (this.keptFrames.length > 0 && this.keptFrames[0]![0] <= received) {
      bytes += this.keptFrames.shift()![1].length;
    }
    this.unconfirmed -= bytes;
    return bytes === 0 ? [] : [{ kind: 'confirmed', bytes }];
  }

  private peerSwitched(now: number): Instruction[] {
    if (this.peerHasSwitched || this.leftLink) return [];
    this.peerHasSwitched = true;
    const out: Instruction[] = [];
    this.noteDirect(out);
    this.heldBytes = 0;
    const held = this.held;
    this.held = [];
    for (const frame of held) out.push(...this.deliverLink(frame, now));
    return out;
  }

  private noteDirect(out: Instruction[]): void {
    if (this.switched && this.peerHasSwitched && !this.direct) {
      this.direct = true;
      this.budgetDeadline = null;
      out.push({ kind: 'outcome', outcome: 'direct' });
    }
  }

  private peerFellBack(received: number, now: number): Instruction[] {
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
    this.held = [];
    this.heldBytes = 0;
    this.reassembler.reset();
    this.unackedBytes = 0;
    this.firstUnacked = null;

    const out: Instruction[] = [
      { kind: 'close-link' },
      { kind: 'relay', frame: encode({ type: 'DirectFallback', received: this.deliveredCount }) },
      { kind: 'outcome', outcome: this.direct ? 'dropped' : 'no_route' },
      { kind: 'left', reason },
    ];
    if (this.currentPath === 'link') {
      this.currentPath = 'holding';
      this.holdingDeadline = now + PEER_FALLBACK_WAIT_MS;
    }
    if (this.peerFallback !== null && this.currentPath === 'holding') {
      out.push(...this.resend(this.peerFallback));
    }
    return out;
  }

  private resend(received: number): Instruction[] {
    const out: Instruction[] = this.keptFrames
      .filter(([n]) => n > received)
      .map(([, frame]) => ({ kind: 'relay', frame }));
    this.keptFrames = [];
    this.unconfirmed = 0;
    this.currentPath = 'relay';
    this.holdingDeadline = null;
    return out;
  }
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

function pieces(frame: Uint8Array): Instruction[] {
  return split(frame).map((piece) => ({ kind: 'link', piece }));
}
