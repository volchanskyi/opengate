import { encodeFrame } from '../protocol/codec';
import {
  FRAME_CONTROL,
  FRAME_PONG,
  FRAME_TERMINAL,
  type ControlMessage,
  type DesktopFrame,
  type FileFrame,
  type TerminalFrame,
} from '../protocol/types';
import { MAX_PIECE } from '../direct/pieces';
import {
  CHANNELS,
  Handoff,
  channelOf,
  type Channel,
  type Instruction,
  type Layout,
  type LeaveReason,
  type Outcome,
} from '../direct/handoff';
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

/** Label of the one ordered channel a host without a channel per job answers. */
export const SHARED_LABEL = 'session';

/** Labels of the typing, screen and files channels a host with a channel per job answers. */
export const CHANNEL_LABELS: ReadonlyMap<Channel, string> = new Map(CHANNELS.map((c) => [c, c]));

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
 * One session's sending point and path chooser: the relay for the whole session, a direct link
 * when one forms, a lane per link channel with typing first, and the hand-off between them.
 */
export class SessionChannel {
  private readonly events: SessionChannelEvents;
  private readonly helpers: string[];
  private readonly layout: Layout;
  private readonly handoff = new Handoff();
  private ws: WebSocket | null = null;
  private peer: RTCPeerConnection | null = null;
  private readonly links = new Map<Channel, RTCDataChannel>();
  private readonly opened = new Set<Channel>();
  private readonly lanes = new Map<Channel, Uint8Array[]>(CHANNELS.map((c) => [c, []]));
  /** Bytes taken from the screen and files lanes, which share the bulk turn by bytes. */
  private readonly bulkTaken = new Map<Channel, number>();
  private tickTimer: ReturnType<typeof setTimeout> | null = null;
  private retryTimer: ReturnType<typeof setTimeout> | null = null;
  private disconnectedTimer: ReturnType<typeof setTimeout> | null = null;
  private ended = false;
  private currentState: ConnectionState = 'disconnected';

  constructor(events: SessionChannelEvents, helpers: string[], layout: Layout = 'shared') {
    this.events = events;
    this.helpers = helpers;
    this.layout = layout;
  }

  get state(): ConnectionState {
    return this.currentState;
  }

  /** Opens the relay as the browser side; the console's session cookie proves who it is. */
  connect(relayUrl: string): void {
    this.setState('connecting');
    const separator = relayUrl.includes('?') ? '&' : '?';
    const ws = new WebSocket(`${relayUrl}${separator}side=browser`);
    ws.binaryType = 'arraybuffer';
    this.ws = ws;
    ws.onopen = () => {
      this.setState('connected');
      this.attempt();
      this.pump();
    };
    ws.onmessage = (event: MessageEvent) => {
      this.run(this.handoff.fromRelay(new Uint8Array(event.data as ArrayBuffer), Date.now()));
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
    this.teardown();
    this.ws?.close();
    this.ws = null;
    this.setState('disconnected');
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
    this.handoff.beginAttempt(Date.now());
    this.rearm();

    const peer = new RTCPeerConnection({ iceServers: this.helpers.map((urls) => ({ urls })) });
    this.peer = peer;
    const opening = this.layout === 'separate' ? CHANNEL_LABELS : new Map([['typing' as const, SHARED_LABEL]]);
    for (const [which, label] of opening) this.links.set(which, this.openChannel(peer, which, label));

    peer.onicecandidate = (event) => {
      if (!event.candidate) return;
      console.info(`direct link: local candidate, ${candidateKind(event.candidate.candidate)}`);
      this.relayControl({ type: 'IceCandidate', candidate: event.candidate.candidate, mid: event.candidate.sdpMid ?? '' });
    };
    peer.oniceconnectionstatechange = () => this.onIceState(peer.iceConnectionState);

    peer
      .createOffer()
      .then(async (offer) => {
        await peer.setLocalDescription(offer);
        this.relayControl({ type: 'DirectOffer', sdp: offer.sdp ?? '' });
      })
      .catch(() => this.lose('closed'));
  }

  /** Opens one ordered channel; the link counts as open once every channel of the layout is. */
  private openChannel(peer: RTCPeerConnection, which: Channel, label: string): RTCDataChannel {
    const link = peer.createDataChannel(label, { ordered: true });
    link.binaryType = 'arraybuffer';
    link.bufferedAmountLowThreshold = UNSENT_LIMIT;
    link.onopen = () => {
      this.opened.add(which);
      if (this.opened.size === this.links.size) this.run(this.handoff.linkOpen(this.layout));
    };
    link.onmessage = (event: MessageEvent) => {
      this.run(this.handoff.fromLink(which, new Uint8Array(event.data as ArrayBuffer), Date.now()));
    };
    link.onclose = () => this.lose('closed');
    link.onerror = () => this.lose('channel_error');
    link.onbufferedamountlow = () => this.pump();
    return link;
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
    this.run(this.handoff.linkLost(reason, Date.now()));
  }

  private enqueue(frame: Uint8Array): void {
    const lane = channelOf(frame);
    const cap = lane === 'typing' ? INTERACTIVE_FRAME_CAP : BULK_FRAME_CAP;
    if (frame.length > cap) {
      throw new Error(`a ${lane} frame of ${frame.length} bytes is past the lane's ${cap}-byte cap`);
    }
    this.laneQueue(lane).push(frame);
    this.pump();
  }

  /** Hands the hand-off the next frame, typing first, while its channel and path take more. */
  private pump(): void {
    while (!this.ended) {
      const lane = this.nextLane();
      if (lane === null) return;
      const frame = this.laneQueue(lane).shift()!;
      if (lane !== 'typing') this.bulkTaken.set(lane, this.taken(lane) + frame.length);
      this.run(this.handoff.send(frame));
    }
  }

  private laneQueue(lane: Channel): Uint8Array[] {
    return this.lanes.get(lane)!;
  }

  private taken(lane: Channel): number {
    return this.bulkTaken.get(lane) ?? 0;
  }

  /** The lane to take from now: typing, then whichever of screen and files has taken fewer bytes. */
  private nextLane(): Channel | null {
    const ready = (lane: Channel) =>
      this.laneQueue(lane).length > 0 && this.handoff.canTake(lane) && this.writerReady(lane);
    if (ready('typing')) return 'typing';
    const [screen, files] = [ready('screen'), ready('files')];
    if (screen && files) return this.taken('files') < this.taken('screen') ? 'files' : 'screen';
    if (screen) return 'screen';
    return files ? 'files' : null;
  }

  /** The link channel a lane's frames ride: its own, or the one shared channel. */
  private linkFor(lane: Channel): RTCDataChannel | undefined {
    return this.links.get(this.handoff.layout() === 'separate' ? lane : 'typing');
  }

  /** Whether a lane's path holds less than the unsent limit; a full relay is asked again shortly. */
  private writerReady(lane: Channel): boolean {
    if (this.handoff.path() === 'link') {
      const link = this.linkFor(lane);
      return link !== undefined && link.bufferedAmount < UNSENT_LIMIT;
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
        case 'link': {
          const link = this.links.get(next.channel);
          if (!link) break;
          try {
            link.send(next.piece as Uint8Array<ArrayBuffer>);
          } catch {
            queue.push(...this.handoff.linkLost('channel_error', Date.now()));
          }
          break;
        }
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
      this.run(this.handoff.tick(Date.now()));
    }, Math.max(0, due - Date.now()));
  }

  private clearDisconnected(): void {
    if (this.disconnectedTimer) clearTimeout(this.disconnectedTimer);
    this.disconnectedTimer = null;
  }

  private closeLink(): void {
    this.clearDisconnected();
    for (const link of this.links.values()) link.close();
    this.peer?.close();
    this.links.clear();
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
  return / typ (\S+)/.exec(candidate)?.[1] ?? 'unknown';
}
