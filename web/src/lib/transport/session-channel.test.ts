import { afterEach, beforeEach, describe, expect, it, vi, type Mock } from 'vitest';
import { decodeFrame, encodeFrame } from '../protocol/codec';
import { FRAME_CONTROL, FRAME_PING, FRAME_PONG, FRAME_TERMINAL, type ControlMessage, type Frame } from '../protocol/types';
import { split } from '../direct/pieces';
import { INTERACTIVE_FRAME_CAP, SessionChannel, type SessionChannelEvents } from './session-channel';

function buffer(bytes: Uint8Array): ArrayBuffer {
  return bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength) as ArrayBuffer;
}

function control(message: ControlMessage): ArrayBuffer {
  return buffer(encodeFrame({ type: FRAME_CONTROL, message }));
}

function decoded(sent: unknown): Frame {
  return decodeFrame(new Uint8Array(sent as ArrayBuffer | Uint8Array)).frame;
}

/** Each frame as text: its terminal bytes, or its type for any other frame. */
function texts(sent: Uint8Array[]): string[] {
  return sent.map((s) => {
    const f = decoded(s);
    return f.type === FRAME_TERMINAL ? new TextDecoder().decode(f.frame.data) : `type ${f.type}`;
  });
}

class FakeSocket {
  static readonly OPEN = 1;
  static last: FakeSocket;
  readonly url: string;
  binaryType = 'blob';
  readyState = 0;
  bufferedAmount = 0;
  sent: Uint8Array[] = [];
  onopen: (() => void) | null = null;
  onmessage: ((event: { data: ArrayBuffer }) => void) | null = null;
  onerror: (() => void) | null = null;
  onclose: ((event: { code: number; reason: string }) => void) | null = null;

  constructor(url: string) {
    this.url = url;
    FakeSocket.last = this;
  }

  send(data: Uint8Array) {
    this.sent.push(new Uint8Array(data));
  }

  close() {
    this.readyState = 3;
  }

  open() {
    this.readyState = FakeSocket.OPEN;
    this.onopen?.();
  }

  receive(data: ArrayBuffer) {
    this.onmessage?.({ data });
  }

  frames(): Frame[] {
    return this.sent.map((s) => decoded(s));
  }

  messages(): ControlMessage[] {
    return this.frames().flatMap((f) => (f.type === FRAME_CONTROL ? [f.message] : []));
  }
}

class FakeChannel {
  readonly label: string;
  binaryType = 'blob';
  bufferedAmount = 0;
  bufferedAmountLowThreshold = 0;
  sent: Uint8Array[] = [];
  failSends = false;
  onopen: (() => void) | null = null;
  onmessage: ((event: { data: ArrayBuffer }) => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;
  onbufferedamountlow: (() => void) | null = null;
  closed = false;

  constructor(label: string) {
    this.label = label;
  }

  send(data: Uint8Array) {
    if (this.failSends) throw new Error('send queue full');
    this.sent.push(new Uint8Array(data));
  }

  close() {
    this.closed = true;
  }
}

class FakePeer {
  static last: FakePeer | null = null;
  readonly config: RTCConfiguration;
  channel: FakeChannel | null = null;
  iceConnectionState = 'new';
  remote: RTCSessionDescriptionInit | null = null;
  candidates: RTCIceCandidateInit[] = [];
  closed = false;
  onicecandidate: ((event: { candidate: { candidate: string; sdpMid: string | null } | null }) => void) | null = null;
  oniceconnectionstatechange: (() => void) | null = null;

  constructor(config: RTCConfiguration) {
    this.config = config;
    FakePeer.last = this;
  }

  createDataChannel(label: string) {
    this.channel = new FakeChannel(label);
    return this.channel;
  }

  createOffer() {
    return Promise.resolve({ type: 'offer', sdp: 'v=0 browser offer' });
  }

  setLocalDescription() {
    return Promise.resolve();
  }

  setRemoteDescription(description: RTCSessionDescriptionInit) {
    this.remote = description;
    return Promise.resolve();
  }

  addIceCandidate(candidate: RTCIceCandidateInit) {
    this.candidates.push(candidate);
    return Promise.resolve();
  }

  close() {
    this.closed = true;
  }

  ice(state: string) {
    this.iceConnectionState = state;
    this.oniceconnectionstatechange?.();
  }
}

type MockedEvents = { [K in keyof SessionChannelEvents]: Mock<SessionChannelEvents[K]> };

function events(): MockedEvents {
  return {
    onStateChange: vi.fn<SessionChannelEvents['onStateChange']>(),
    onControlMessage: vi.fn<SessionChannelEvents['onControlMessage']>(),
    onDesktopFrame: vi.fn<SessionChannelEvents['onDesktopFrame']>(),
    onTerminalFrame: vi.fn<SessionChannelEvents['onTerminalFrame']>(),
    onFileFrame: vi.fn<SessionChannelEvents['onFileFrame']>(),
    onError: vi.fn<SessionChannelEvents['onError']>(),
    onPath: vi.fn<SessionChannelEvents['onPath']>(),
    onOutcome: vi.fn<SessionChannelEvents['onOutcome']>(),
    onClosed: vi.fn<SessionChannelEvents['onClosed']>(),
  };
}

const terminal = (text: string) => new TextEncoder().encode(text);

/** Puts the session on the link: the browser's channel opens, then the host's notice arrives. */
async function goDirect(channel: SessionChannel) {
  await vi.advanceTimersByTimeAsync(1);
  const peer = FakePeer.last!;
  FakeSocket.last.receive(control({ type: 'DirectAnswer', sdp: 'v=0 host answer' }));
  await Promise.resolve();
  peer.ice('connected');
  peer.channel!.onopen?.();
  FakeSocket.last.receive(control({ type: 'DirectSwitch' }));
  return { peer, link: peer.channel!, channel };
}

describe('SessionChannel', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.stubGlobal('WebSocket', FakeSocket);
    FakePeer.last = null;
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  describe('in a browser without the direct-link API', () => {
    beforeEach(() => {
      vi.stubGlobal('RTCPeerConnection', undefined);
    });

    it('carries the session on the relay and says this browser cannot go direct', () => {
      const ev = events();
      const channel = new SessionChannel(ev, []);
      channel.connect('wss://edge.test/ws/relay/tok');
      const relay = FakeSocket.last;
      expect(relay.url).toBe('wss://edge.test/ws/relay/tok?side=browser');
      relay.open();

      expect(ev.onOutcome).toHaveBeenCalledWith('unsupported');
      expect(ev.onPath).toHaveBeenLastCalledWith({ direct: false, reason: "this browser can't" });

      channel.sendTerminalData(terminal('ls\n'));
      expect(texts(relay.sent)).toEqual(['ls\n']);

      relay.receive(buffer(encodeFrame({ type: FRAME_TERMINAL, frame: { data: terminal('file\n') } })));
      expect(new TextDecoder().decode(ev.onTerminalFrame.mock.calls[0]![0].data)).toBe('file\n');
    });

    it('answers a ping with a pong', () => {
      const channel = new SessionChannel(events(), []);
      channel.connect('wss://edge.test/ws/relay/tok');
      FakeSocket.last.open();
      FakeSocket.last.receive(buffer(encodeFrame({ type: FRAME_PING })));
      expect(texts(FakeSocket.last.sent)).toEqual([`type ${FRAME_PONG}`]);
    });

    it('refuses an interactive frame past one piece and sends nothing', () => {
      const channel = new SessionChannel(events(), []);
      channel.connect('wss://edge.test/ws/relay/tok');
      FakeSocket.last.open();
      expect(() =>
        channel.sendControl({ type: 'ChatMessage', text: 'x'.repeat(INTERACTIVE_FRAME_CAP), sender: 'tech' }),
      ).toThrow(/past the lane's/);
      expect(FakeSocket.last.sent).toHaveLength(0);
    });

    it('fits the largest encoding of every message the browser sends into one piece', () => {
      const channel = new SessionChannel(events(), []);
      channel.connect('wss://edge.test/ws/relay/tok');
      FakeSocket.last.open();
      // Paths stop at Linux's PATH_MAX; key and mouse fields are bounded by their types.
      const path = 'p'.repeat(4096);
      const largest: ControlMessage[] = [
        { type: 'FileListRequest', path },
        { type: 'FileDownloadRequest', path },
        { type: 'TerminalResize', cols: 65_535, rows: 65_535 },
        { type: 'MouseMove', x: -2_147_483_648, y: -2_147_483_648 },
        { type: 'MouseClick', button: 'Forward', pressed: true, x: -2_147_483_648, y: -2_147_483_648 },
        { type: 'KeyPress', key: 'ShiftRight', pressed: true },
      ];
      for (const message of largest) channel.sendControl(message);
      channel.sendTerminalData(new Uint8Array(100_000).fill(0xff));
      expect(FakeSocket.last.sent.every((frame) => frame.length <= INTERACTIVE_FRAME_CAP)).toBe(true);
      expect(FakeSocket.last.messages()).toHaveLength(largest.length);
    });

    it('splits a long paste into frames that each fit one piece, in order', () => {
      const channel = new SessionChannel(events(), []);
      channel.connect('wss://edge.test/ws/relay/tok');
      FakeSocket.last.open();
      const paste = new Uint8Array(40_000).map((_, i) => 32 + (i % 90));
      channel.sendTerminalData(paste);
      const sent = FakeSocket.last.sent;
      expect(sent.length).toBeGreaterThan(2);
      expect(sent.every((s) => s.length <= INTERACTIVE_FRAME_CAP)).toBe(true);
      expect(texts(sent).join('')).toBe(new TextDecoder().decode(paste));
    });

    it('holds frames while the relay socket has 64 KiB unsent', async () => {
      const channel = new SessionChannel(events(), []);
      channel.connect('wss://edge.test/ws/relay/tok');
      const relay = FakeSocket.last;
      relay.open();
      relay.bufferedAmount = 64 * 1024;
      channel.sendTerminalData(terminal('a'));
      expect(relay.sent).toHaveLength(0);
      relay.bufferedAmount = 0;
      await vi.advanceTimersByTimeAsync(50);
      expect(relay.sent).toHaveLength(1);
    });

    it('reports the relay closing with its reason', () => {
      const ev = events();
      const channel = new SessionChannel(ev, []);
      channel.connect('wss://edge.test/ws/relay/tok');
      FakeSocket.last.open();
      FakeSocket.last.onclose?.({ code: 4000, reason: 'Ended by Carol' });
      expect(ev.onClosed).toHaveBeenCalledWith('Ended by Carol');
      expect(ev.onStateChange).toHaveBeenLastCalledWith('disconnected');
    });

    it.each([
      ['a relay that never opened', false],
      ['a relay dropped without a reason', true],
    ])('reports %s as a lost connection', (_name, opened) => {
      const ev = events();
      const channel = new SessionChannel(ev, []);
      channel.connect('wss://edge.test/ws/relay/tok');
      if (opened) FakeSocket.last.open();
      FakeSocket.last.onclose?.({ code: 1006, reason: '' });
      expect(ev.onClosed).toHaveBeenCalledWith('Connection lost — reconnect');
    });
  });

  describe('in a browser with the direct-link API', () => {
    beforeEach(() => {
      vi.stubGlobal('RTCPeerConnection', FakePeer);
    });

    it('offers a link with the helpers the server named and trades candidates on the relay', async () => {
      const channel = new SessionChannel(events(), ['stun:quic.example.test:9090']);
      channel.connect('wss://edge.test/ws/relay/tok');
      FakeSocket.last.open();
      await vi.advanceTimersByTimeAsync(1);

      const peer = FakePeer.last!;
      expect(peer.config.iceServers).toEqual([{ urls: 'stun:quic.example.test:9090' }]);
      expect(peer.channel!.label).toBe('session');
      expect(FakeSocket.last.messages()).toContainEqual({ type: 'DirectOffer', sdp: 'v=0 browser offer' });

      peer.onicecandidate?.({ candidate: { candidate: 'candidate:1 1 udp 1 192.0.2.7 5000 typ srflx', sdpMid: '0' } });
      expect(FakeSocket.last.messages()).toContainEqual({
        type: 'IceCandidate',
        candidate: 'candidate:1 1 udp 1 192.0.2.7 5000 typ srflx',
        mid: '0',
      });

      FakeSocket.last.receive(control({ type: 'DirectAnswer', sdp: 'v=0 host answer' }));
      FakeSocket.last.receive(control({ type: 'IceCandidate', candidate: 'candidate:2 1 udp 1 10.0.0.2 6000 typ host', mid: '0' }));
      await Promise.resolve();
      expect(peer.remote).toEqual({ type: 'answer', sdp: 'v=0 host answer' });
      expect(peer.candidates).toEqual([{ candidate: 'candidate:2 1 udp 1 10.0.0.2 6000 typ host', sdpMid: '0' }]);
    });

    it('moves the session onto the link once both sides have switched', async () => {
      const ev = events();
      const channel = new SessionChannel(ev, []);
      channel.connect('wss://edge.test/ws/relay/tok');
      FakeSocket.last.open();
      const { link } = await goDirect(channel);

      expect(FakeSocket.last.messages()).toContainEqual({ type: 'DirectSwitch' });
      expect(ev.onOutcome).toHaveBeenCalledWith('direct');
      expect(ev.onPath).toHaveBeenLastCalledWith({ direct: true });

      const relayBefore = FakeSocket.last.sent.length;
      channel.sendTerminalData(terminal('whoami\n'));
      expect(FakeSocket.last.sent).toHaveLength(relayBefore);
      expect(texts(link.sent)).toEqual(['whoami\n']);

      const echo = encodeFrame({ type: FRAME_TERMINAL, frame: { data: terminal('root\n') } });
      for (const piece of split(echo)) link.onmessage?.({ data: buffer(piece) });
      expect(new TextDecoder().decode(ev.onTerminalFrame.mock.calls[0]![0].data)).toBe('root\n');
    });

    it('feeds the link only while it holds under 64 KiB unsent', async () => {
      const channel = new SessionChannel(events(), []);
      channel.connect('wss://edge.test/ws/relay/tok');
      FakeSocket.last.open();
      const { link } = await goDirect(channel);
      expect(link.bufferedAmountLowThreshold).toBe(64 * 1024);

      link.bufferedAmount = 64 * 1024;
      channel.sendTerminalData(terminal('held'));
      expect(link.sent).toHaveLength(0);
      link.bufferedAmount = 0;
      link.onbufferedamountlow?.();
      expect(link.sent).toHaveLength(1);
    });

    it('treats a short disconnection as passing and falls back after three seconds of it', async () => {
      const ev = events();
      const channel = new SessionChannel(ev, []);
      channel.connect('wss://edge.test/ws/relay/tok');
      FakeSocket.last.open();
      const { peer } = await goDirect(channel);

      peer.ice('disconnected');
      await vi.advanceTimersByTimeAsync(2_000);
      peer.ice('connected');
      await vi.advanceTimersByTimeAsync(5_000);
      expect(ev.onOutcome).not.toHaveBeenCalledWith('dropped');

      peer.ice('disconnected');
      await vi.advanceTimersByTimeAsync(3_000);
      expect(ev.onOutcome).toHaveBeenCalledWith('dropped');
      expect(ev.onPath).toHaveBeenLastCalledWith({ direct: false, reason: 'the direct link dropped' });
      expect(FakeSocket.last.messages()).toContainEqual({ type: 'DirectFallback', received: 0 });
      expect(peer.closed).toBe(true);
    });

    it('falls back at once when the link fails, and re-sends on the relay what the host did not take', async () => {
      const ev = events();
      const channel = new SessionChannel(ev, []);
      channel.connect('wss://edge.test/ws/relay/tok');
      FakeSocket.last.open();
      const { peer } = await goDirect(channel);
      channel.sendTerminalData(terminal('rm -rf /tmp/b'));
      channel.sendTerminalData(terminal('uild'));

      peer.ice('failed');
      expect(ev.onOutcome).toHaveBeenCalledWith('dropped');
      FakeSocket.last.sent = [];
      FakeSocket.last.receive(control({ type: 'DirectFallback', received: 1 }));
      channel.sendTerminalData(terminal('\n'));

      expect(texts(FakeSocket.last.sent)).toEqual(['uild', '\n']);
    });

    it('falls back when a send on the link fails', async () => {
      const ev = events();
      const channel = new SessionChannel(ev, []);
      channel.connect('wss://edge.test/ws/relay/tok');
      FakeSocket.last.open();
      const { link } = await goDirect(channel);
      link.failSends = true;
      channel.sendTerminalData(terminal('x'));
      expect(ev.onOutcome).toHaveBeenCalledWith('dropped');
    });

    it('stays on the relay when no route forms within the attempt budget', async () => {
      const ev = events();
      const channel = new SessionChannel(ev, []);
      channel.connect('wss://edge.test/ws/relay/tok');
      FakeSocket.last.open();
      await vi.advanceTimersByTimeAsync(8_000);
      expect(ev.onOutcome).toHaveBeenCalledWith('no_route');
      expect(ev.onPath).toHaveBeenLastCalledWith({ direct: false, reason: 'no direct route on this network' });
      expect(FakePeer.last!.closed).toBe(true);
    });

    it('ends the session when the host never says what it received', async () => {
      const ev = events();
      const channel = new SessionChannel(ev, []);
      channel.connect('wss://edge.test/ws/relay/tok');
      FakeSocket.last.open();
      const { peer } = await goDirect(channel);
      channel.sendTerminalData(terminal('pending'));
      peer.ice('failed');
      await vi.advanceTimersByTimeAsync(10_000);
      expect(ev.onClosed).toHaveBeenCalledWith('Connection lost — reconnect');
    });

    it('closes its link when the session ends', async () => {
      const channel = new SessionChannel(events(), []);
      channel.connect('wss://edge.test/ws/relay/tok');
      FakeSocket.last.open();
      const { peer, link } = await goDirect(channel);
      channel.disconnect();
      expect(peer.closed).toBe(true);
      expect(link.closed).toBe(true);
      expect(FakeSocket.last.readyState).toBe(3);
    });
  });
});
