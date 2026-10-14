import { encodeFrame } from '../protocol/codec';
import {
  FRAME_CONTROL,
  FRAME_DESKTOP,
  FRAME_FILE,
  FRAME_PONG,
  FRAME_TERMINAL,
  type ControlMessage,
  type DesktopFrame,
  type FileFrame,
  type TerminalFrame,
} from '../protocol/types';
import { MAX_PIECE } from '../direct/pieces';
import { Handoff, type Instruction, type LeaveReason, type Outcome } from '../direct/handoff';
import { frameDispatch } from './frame-dispatch';

/** Relay socket lifecycle state. */
export type ConnectionState = 'disconnected' | 'connecting' | 'connected' | 'error';

/** Where the session runs, and in plain words why it is not direct. */
export interface SessionPath {
  direct: boolean;
  reason?: string;
}

/** How a session's one attempt ended, as the server counts it. */
export type ReportedOutcome = Outcome | 'unsupported';

/** What the session tells the page. */
export interface SessionChannelEvents {
  onStateChange: (state: ConnectionState) => void;
  onControlMessage: (msg: ControlMessage) => void;
  onDesktopFrame: (frame: DesktopFrame) => void;
  onTerminalFrame: (frame: TerminalFrame) => void;
  onFileFrame: (frame: FileFrame) => void;
  onError: (error: Error) => void;
  onPath: (path: SessionPath) => void;
  onOutcome: (outcome: ReportedOutcome) => void;
  onClosed: (reason: string) => void;
}

/** Largest interactive frame: one link piece. */
export const INTERACTIVE_FRAME_CAP = MAX_PIECE;

/** Largest bulk frame: one 256 KiB file piece and its header and fields. */
export const BULK_FRAME_CAP = 256 * 1024 + 64;

/** A path is fed only while it holds less than this unsent. */
export const UNSENT_LIMIT = 64 * 1024;

/** What the tab shows when the session ended without a reason from the server. */
const CONNECTION_LOST = 'Connection lost — reconnect';

/** How long the link may stay disconnected before the session falls back. */
export const DISCONNECTED_LIMIT_MS = 3_000;

/** Label of the one ordered channel the host answers. */
export const CHANNEL_LABEL = 'session';

// Terminal bytes per frame, leaving room for the frame's header and fields inside one piece.
const TERMINAL_CHUNK = INTERACTIVE_FRAME_CAP - 64;

// How often a frame waiting on a full relay socket asks again; a socket says nothing as it drains.
const RELAY_RETRY_MS = 10;

const REASONS: Record<Outcome, SessionPath> = {
  direct: { direct: true },
  no_route: { direct: false, reason: 'no direct route on this network' },
  dropped: { direct: false, reason: 'the direct link dropped' },
};

/**
 * One session's sending point and path chooser: the relay socket for the whole session, a direct
 * link when one forms, an interactive lane taken before a bulk lane, and the hand-off between them.
 */
export class SessionChannel {
  private readonly events: SessionChannelEvents;
  private readonly helpers: string[];
  private readonly handoff = new Handoff();
  private readonly start = Date.now();
  private ws: WebSocket | null = null;
  private peer: RTCPeerConnection | null = null;
  private link: RTCDataChannel | null = null;
  private readonly interactive: Uint8Array[] = [];
  private readonly bulk: Uint8Array[] = [];
  private tickTimer: ReturnType<typeof setTimeout> | null = null;
  private retryTimer: ReturnType<typeof setTimeout> | null = null;
  private disconnectedTimer: ReturnType<typeof setTimeout> | null = null;
  private ended = false;
  private currentState: ConnectionState = 'disconnected';

  constructor(events: SessionChannelEvents, helpers: string[]) {
    this.events = events;
    this.helpers = helpers;
  }

  get state(): ConnectionState {
    return this.currentState;
  }

  /** Opens the relay as the browser side, authenticated by the signed-in technician's token. */
  connect(relayUrl: string, authToken: string): void {
    this.setState('connecting');
    const separator = relayUrl.includes('?') ? '&' : '?';
    const ws = new WebSocket(`${relayUrl}${separator}side=browser&auth=${encodeURIComponent(authToken)}`);
    ws.binaryType = 'arraybuffer';
    this.ws = ws;
    ws.onopen = () => {
      this.setState('connected');
      this.attempt();
      this.pump();
    };
    ws.onmessage = (event: MessageEvent) => {
      this.run(this.handoff.fromRelay(new Uint8Array(event.data as ArrayBuffer), this.now()));
    };
    ws.onerror = () => {
      this.events.onError(new Error('WebSocket connection error'));
      this.setState('error');
    };
    ws.onclose = (event: CloseEvent) => {
      const ours = this.ended;
      this.teardown();
      if (this.currentState !== 'error') this.setState('disconnected');
      if (!ours) this.events.onClosed(event.reason || CONNECTION_LOST);
    };
  }

  /** Queues a control message in the interactive lane. */
  sendControl(msg: ControlMessage): void {
    this.enqueue(encodeFrame({ type: FRAME_CONTROL, message: msg }));
  }

  /** Queues terminal input, cut into frames that each fit one piece. */
  sendTerminalData(data: Uint8Array): void {
    for (let at = 0; at < Math.max(data.length, 1); at += TERMINAL_CHUNK) {
      const chunk = data.subarray(at, at + TERMINAL_CHUNK);
      this.enqueue(encodeFrame({ type: FRAME_TERMINAL, frame: { data: chunk } }));
    }
  }

  /** Ends the session: the link and the relay close and nothing more is sent. */
  disconnect(): void {
    this.ended = true;
    this.teardown();
    this.ws?.close();
    this.ws = null;
    this.setState('disconnected');
  }

  private now(): number {
    return Date.now() - this.start;
  }

  private setState(state: ConnectionState): void {
    this.currentState = state;
    this.events.onStateChange(state);
  }

  /** Makes the session's one attempt, or reports that this browser cannot. */
  private attempt(): void {
    if (typeof RTCPeerConnection === 'undefined') {
      console.info('direct link: unsupported in this browser');
      this.events.onOutcome('unsupported');
      this.events.onPath({ direct: false, reason: "this browser can't" });
      return;
    }
    this.events.onPath({ direct: false, reason: 'looking for a direct route' });
    this.handoff.beginAttempt(this.now());
    this.rearm();

    const peer = new RTCPeerConnection({ iceServers: this.helpers.map((urls) => ({ urls })) });
    const link = peer.createDataChannel(CHANNEL_LABEL, { ordered: true });
    link.binaryType = 'arraybuffer';
    link.bufferedAmountLowThreshold = UNSENT_LIMIT;
    this.peer = peer;
    this.link = link;

    peer.onicecandidate = (event) => {
      if (!event.candidate) return;
      console.info(`direct link: local candidate, ${candidateKind(event.candidate.candidate)}`);
      this.relayControl({ type: 'IceCandidate', candidate: event.candidate.candidate, mid: event.candidate.sdpMid ?? '' });
    };
    peer.oniceconnectionstatechange = () => this.onIceState(peer.iceConnectionState);
    link.onopen = () => this.run(this.handoff.linkOpen());
    link.onmessage = (event: MessageEvent) => {
      this.run(this.handoff.fromLink(new Uint8Array(event.data as ArrayBuffer), this.now()));
    };
    link.onclose = () => this.lose('closed');
    link.onerror = () => this.lose('channel_error');
    link.onbufferedamountlow = () => this.pump();

    peer
      .createOffer()
      .then(async (offer) => {
        await peer.setLocalDescription(offer);
        this.relayControl({ type: 'DirectOffer', sdp: offer.sdp ?? '' });
      })
      .catch(() => this.lose('closed'));
  }

  /** A disconnected link is passing; it falls back once it has not reconnected within the limit. */
  private onIceState(state: RTCIceConnectionState): void {
    switch (state) {
      case 'connected':
      case 'completed':
        this.clearDisconnected();
        break;
      case 'disconnected':
        this.disconnectedTimer ??= setTimeout(() => this.lose('silence'), DISCONNECTED_LIMIT_MS);
        break;
      case 'failed':
      case 'closed':
        this.lose('closed');
        break;
      default:
        break;
    }
  }

  private lose(reason: LeaveReason): void {
    this.run(this.handoff.linkLost(reason, this.now()));
  }

  private enqueue(frame: Uint8Array): void {
    const bulk = frame[0] === FRAME_FILE || frame[0] === FRAME_DESKTOP;
    const cap = bulk ? BULK_FRAME_CAP : INTERACTIVE_FRAME_CAP;
    if (frame.length > cap) {
      throw new Error(`a ${bulk ? 'bulk' : 'interactive'} frame of ${frame.length} bytes is past the lane's ${cap}-byte cap`);
    }
    (bulk ? this.bulk : this.interactive).push(frame);
    this.pump();
  }

  /** Hands the hand-off the next frame, interactive before bulk, while the path takes more. */
  private pump(): void {
    while (!this.ended && this.handoff.canTake() && (this.interactive.length > 0 || this.bulk.length > 0)) {
      if (!this.writerReady()) return;
      const frame = (this.interactive.shift() ?? this.bulk.shift())!;
      this.run(this.handoff.send(frame));
    }
  }

  /** Whether the path holds less than the unsent limit; a full relay is asked again shortly. */
  private writerReady(): boolean {
    if (this.handoff.path() === 'link') {
      return this.link !== null && this.link.bufferedAmount < UNSENT_LIMIT;
    }
    const ws = this.ws;
    if (ws?.readyState === WebSocket.OPEN && ws.bufferedAmount < UNSENT_LIMIT) return true;
    this.retryTimer ??= setTimeout(() => {
      this.retryTimer = null;
      this.pump();
    }, RELAY_RETRY_MS);
    return false;
  }

  /** Carries out instructions in order; a failed link send queues the hand-off's answer. */
  private run(instructions: Instruction[]): void {
    const queue = [...instructions];
    for (let next = queue.shift(); next; next = queue.shift()) {
      switch (next.kind) {
        case 'relay':
          this.sendRelay(next.frame);
          break;
        case 'link':
          if (!this.link) break;
          try {
            this.link.send(next.piece as Uint8Array<ArrayBuffer>);
          } catch {
            queue.push(...this.handoff.linkLost('channel_error', this.now()));
          }
          break;
        case 'deliver':
          this.deliver(next.frame);
          break;
        case 'signal':
          this.signal(next.message);
          break;
        case 'close-link':
          this.closeLink();
          break;
        case 'end':
          this.endLost();
          return;
        case 'outcome':
          console.info(`direct link: ${next.outcome}`);
          this.events.onOutcome(next.outcome);
          this.events.onPath(REASONS[next.outcome]);
          break;
        case 'left':
          console.info(`direct link: left the link, ${next.reason}`);
          break;
        default:
          break;
      }
    }
    this.rearm();
    this.pump();
  }

  private deliver(frame: Uint8Array): void {
    const data = frame.buffer.slice(frame.byteOffset, frame.byteOffset + frame.byteLength) as ArrayBuffer;
    frameDispatch(data, this.events, () => this.enqueue(encodeFrame({ type: FRAME_PONG })));
  }

  private signal(message: ControlMessage): void {
    const peer = this.peer;
    switch (message.type) {
      case 'DirectAnswer':
        peer?.setRemoteDescription({ type: 'answer', sdp: message.sdp }).catch(() => this.lose('closed'));
        break;
      case 'IceCandidate':
        peer?.addIceCandidate({ candidate: message.candidate, sdpMid: message.mid }).catch(() => {
          console.info(`direct link: a ${candidateKind(message.candidate)} candidate was refused`);
        });
        break;
      case 'DirectRefused':
        this.lose('closed');
        break;
      default:
        break;
    }
  }

  private sendRelay(frame: Uint8Array): void {
    if (this.ws?.readyState === WebSocket.OPEN) this.ws.send(frame as Uint8Array<ArrayBuffer>);
  }

  private relayControl(message: ControlMessage): void {
    this.sendRelay(encodeFrame({ type: FRAME_CONTROL, message }));
  }

  /** The host's notice never came: re-sending without it could repeat a keystroke. */
  private endLost(): void {
    this.ended = true;
    this.teardown();
    this.ws?.close();
    this.setState('disconnected');
    this.events.onClosed(CONNECTION_LOST);
  }

  /** Schedules the hand-off's next timer. */
  private rearm(): void {
    if (this.tickTimer) clearTimeout(this.tickTimer);
    this.tickTimer = null;
    const due = this.handoff.nextDeadline();
    if (due === null || this.ended) return;
    this.tickTimer = setTimeout(() => {
      this.tickTimer = null;
      this.run(this.handoff.tick(this.now()));
    }, Math.max(0, due - this.now()));
  }

  private clearDisconnected(): void {
    if (this.disconnectedTimer) clearTimeout(this.disconnectedTimer);
    this.disconnectedTimer = null;
  }

  private closeLink(): void {
    this.clearDisconnected();
    this.link?.close();
    this.peer?.close();
    this.link = null;
    this.peer = null;
  }

  private teardown(): void {
    this.ended = true;
    this.closeLink();
    if (this.tickTimer) clearTimeout(this.tickTimer);
    if (this.retryTimer) clearTimeout(this.retryTimer);
    this.tickTimer = null;
    this.retryTimer = null;
  }
}

/** The kind of address a candidate names — host, srflx, prflx or relay — never the address. */
function candidateKind(candidate: string): string {
  const fields = candidate.split(/\s+/);
  const at = fields.indexOf('typ');
  return at >= 0 && fields[at + 1] ? fields[at + 1]! : 'unknown';
}
