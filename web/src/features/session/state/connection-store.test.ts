import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { encodeFrame } from '../../../lib/protocol/codec';
import { FRAME_CONTROL, FRAME_TERMINAL } from '../../../lib/protocol/types';
import { useConnectionStore } from './connection-store';

const mockPost = vi.fn();

vi.mock('../../../lib/api', () => ({
  api: { POST: (...args: unknown[]) => mockPost(...args) },
}));

class FakeSocket {
  static readonly OPEN = 1;
  static all: FakeSocket[] = [];
  readonly url: string;
  binaryType = 'blob';
  readyState = 0;
  bufferedAmount = 0;
  sent: Uint8Array[] = [];
  closed = false;
  onopen: (() => void) | null = null;
  onmessage: ((event: { data: ArrayBuffer }) => void) | null = null;
  onerror: (() => void) | null = null;
  onclose: ((event: { code: number; reason: string }) => void) | null = null;

  constructor(url: string) {
    this.url = url;
    FakeSocket.all.push(this);
  }

  send(data: Uint8Array) {
    this.sent.push(data);
  }

  close() {
    this.closed = true;
    this.readyState = 3;
  }

  open() {
    this.readyState = FakeSocket.OPEN;
    this.onopen?.();
  }

  receive(bytes: Uint8Array) {
    this.onmessage?.({ data: bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength) as ArrayBuffer });
  }
}

const last = () => FakeSocket.all[FakeSocket.all.length - 1]!;

describe('connection store', () => {
  beforeEach(() => {
    FakeSocket.all = [];
    mockPost.mockReset();
    mockPost.mockResolvedValue({ data: undefined, error: undefined });
    vi.stubGlobal('WebSocket', FakeSocket);
    vi.stubGlobal('RTCPeerConnection', undefined);
  });

  afterEach(() => {
    useConnectionStore.getState().disconnect();
    vi.unstubAllGlobals();
  });

  it('follows the relay and shows the session running through the server', () => {
    useConnectionStore.getState().connect('tok', 'wss://edge.test/ws/relay/tok', []);
    expect(useConnectionStore.getState().state).toBe('connecting');
    last().open();

    const state = useConnectionStore.getState();
    expect(state.state).toBe('connected');
    expect(state.token).toBe('tok');
    expect(state.path).toEqual({ direct: false, reason: "this browser can't" });
  });

  it('reports each outcome to the server for the session it belongs to', () => {
    useConnectionStore.getState().connect('tok', 'wss://edge.test/ws/relay/tok', []);
    last().open();
    expect(mockPost).toHaveBeenCalledWith('/api/v1/sessions/{token}/outcome', {
      params: { path: { token: 'tok' } },
      body: { outcome: 'unsupported' },
    });
  });

  it('hands each frame to the feature that registered for it', () => {
    const onTerminal = vi.fn();
    const onControl = vi.fn();
    useConnectionStore.getState().setOnTerminalFrame(onTerminal);
    useConnectionStore.getState().listenControl(onControl);
    useConnectionStore.getState().connect('tok', 'wss://edge.test/ws/relay/tok', []);
    last().open();

    last().receive(encodeFrame({ type: FRAME_TERMINAL, frame: { data: new TextEncoder().encode('ok') } }));
    last().receive(encodeFrame({ type: FRAME_CONTROL, message: { type: 'FileListError', path: '/x', error: 'denied' } }));

    expect(new TextDecoder().decode(onTerminal.mock.calls[0]![0].data)).toBe('ok');
    expect(onControl).toHaveBeenCalledWith({ type: 'FileListError', path: '/x', error: 'denied' });
  });

  it('hands each control message to every feature listening, until one stops', () => {
    const files = vi.fn();
    const chat = vi.fn();
    const stopFiles = useConnectionStore.getState().listenControl(files);
    const stopChat = useConnectionStore.getState().listenControl(chat);
    useConnectionStore.getState().connect('tok', 'wss://edge.test/ws/relay/tok', []);
    last().open();

    last().receive(encodeFrame({ type: FRAME_CONTROL, message: { type: 'FileListError', path: '/x', error: 'denied' } }));
    stopFiles();
    last().receive(encodeFrame({ type: FRAME_CONTROL, message: { type: 'ChatMessage', text: 'hi', sender: 'agent' } }));
    stopChat();

    expect(files.mock.calls.map(([m]) => m.type)).toEqual(['FileListError']);
    expect(chat.mock.calls.map(([m]) => m.type)).toEqual(['FileListError', 'ChatMessage']);
  });

  it('keeps why the relay closed, for the page to show', () => {
    useConnectionStore.getState().connect('tok', 'wss://edge.test/ws/relay/tok', []);
    last().open();
    last().onclose?.({ code: 4000, reason: 'Opened elsewhere' });

    expect(useConnectionStore.getState().closedReason).toBe('Opened elsewhere');
    expect(useConnectionStore.getState().state).toBe('disconnected');
  });

  it('closes the earlier session when it connects again', () => {
    useConnectionStore.getState().connect('one', 'wss://edge.test/ws/relay/one', []);
    const first = last();
    useConnectionStore.getState().connect('two', 'wss://edge.test/ws/relay/two', []);
    expect(first.closed).toBe(true);
    expect(useConnectionStore.getState().token).toBe('two');
  });

  it('forgets the session and every feature callback on disconnect', () => {
    useConnectionStore.getState().setOnTerminalFrame(vi.fn());
    useConnectionStore.getState().connect('tok', 'wss://edge.test/ws/relay/tok', []);
    const socket = last();
    useConnectionStore.getState().disconnect();

    const state = useConnectionStore.getState();
    expect(socket.closed).toBe(true);
    expect(state.transport).toBeNull();
    expect(state.token).toBeNull();
    expect(state.onTerminalFrame).toBeNull();
    expect(state.closedReason).toBeNull();
  });
});
