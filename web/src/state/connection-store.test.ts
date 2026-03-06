import { describe, it, expect, vi, beforeEach } from 'vitest';
import { useConnectionStore } from './connection-store';

// Captured transport events for testing dispatch
let capturedEvents: Record<string, (...args: unknown[]) => void> = {};

// Mock WSTransport
vi.mock('../lib/transport/ws-transport', () => {
  class MockWSTransport {
    state = 'disconnected';
    sendControl = vi.fn();
    sendTerminalData = vi.fn();
    sendFileFrame = vi.fn();
    disconnect = vi.fn();
    private _events: Record<string, (...args: unknown[]) => void>;

    constructor(events: Record<string, (...args: unknown[]) => void>) {
      this._events = events;
      capturedEvents = events;
      this.disconnect.mockImplementation(() => {
        this._events['onStateChange']?.('disconnected');
      });
    }

    connect = vi.fn(() => {
      this._events['onStateChange']?.('connecting');
      setTimeout(() => this._events['onStateChange']?.('connected'), 0);
    });
  }

  return { WSTransport: MockWSTransport };
});

describe('connection-store', () => {
  beforeEach(() => {
    // Reset store state between tests
    useConnectionStore.setState({
      state: 'disconnected',
      token: null,
      error: null,
      transport: null,
      onControlMessage: null,
      onDesktopFrame: null,
      onTerminalFrame: null,
      onFileFrame: null,
    });
  });

  it('has correct initial state', () => {
    const state = useConnectionStore.getState();
    expect(state.state).toBe('disconnected');
    expect(state.token).toBeNull();
    expect(state.error).toBeNull();
    expect(state.transport).toBeNull();
  });

  it('connect creates transport and sets token', () => {
    const { connect } = useConnectionStore.getState();
    connect('test-token', 'ws://host/relay', 'jwt');

    const state = useConnectionStore.getState();
    expect(state.transport).not.toBeNull();
    expect(state.token).toBe('test-token');
    expect(state.state).toBe('connecting');
  });

  it('connect transitions to connected', async () => {
    const { connect } = useConnectionStore.getState();
    connect('test-token', 'ws://host/relay', 'jwt');

    // Wait for simulated async connection
    await new Promise((r) => setTimeout(r, 10));

    const state = useConnectionStore.getState();
    expect(state.state).toBe('connected');
  });

  it('disconnect cleans up transport and resets state', () => {
    const { connect } = useConnectionStore.getState();
    connect('test-token', 'ws://host/relay', 'jwt');

    const { disconnect } = useConnectionStore.getState();
    disconnect();

    const state = useConnectionStore.getState();
    expect(state.transport).toBeNull();
    expect(state.token).toBeNull();
    expect(state.state).toBe('disconnected');
  });

  it('disconnect is safe when not connected', () => {
    const { disconnect } = useConnectionStore.getState();
    disconnect();
    expect(useConnectionStore.getState().state).toBe('disconnected');
  });

  it('connect disconnects existing transport first', () => {
    const { connect } = useConnectionStore.getState();
    connect('token-1', 'ws://host/relay', 'jwt');

    const firstTransport = useConnectionStore.getState().transport;
    connect('token-2', 'ws://host/relay', 'jwt');

    expect(firstTransport?.disconnect).toHaveBeenCalled();
    expect(useConnectionStore.getState().token).toBe('token-2');
  });

  it('sets and clears frame event callbacks', () => {
    const cb = vi.fn();
    const { setOnControlMessage } = useConnectionStore.getState();
    setOnControlMessage(cb);
    expect(useConnectionStore.getState().onControlMessage).toBe(cb);

    setOnControlMessage(null);
    expect(useConnectionStore.getState().onControlMessage).toBeNull();
  });

  it('sets all four frame event callbacks', () => {
    const { setOnControlMessage, setOnDesktopFrame, setOnTerminalFrame, setOnFileFrame } = useConnectionStore.getState();
    const cb1 = vi.fn();
    const cb2 = vi.fn();
    const cb3 = vi.fn();
    const cb4 = vi.fn();
    setOnControlMessage(cb1);
    setOnDesktopFrame(cb2);
    setOnTerminalFrame(cb3);
    setOnFileFrame(cb4);

    const state = useConnectionStore.getState();
    expect(state.onControlMessage).toBe(cb1);
    expect(state.onDesktopFrame).toBe(cb2);
    expect(state.onTerminalFrame).toBe(cb3);
    expect(state.onFileFrame).toBe(cb4);
  });

  it('disconnect clears frame callbacks', () => {
    const { connect, setOnControlMessage, setOnDesktopFrame } = useConnectionStore.getState();
    connect('token', 'ws://host/relay', 'jwt');
    setOnControlMessage(vi.fn());
    setOnDesktopFrame(vi.fn());

    useConnectionStore.getState().disconnect();

    const state = useConnectionStore.getState();
    expect(state.onControlMessage).toBeNull();
    expect(state.onDesktopFrame).toBeNull();
  });

  it('dispatches control messages to registered callback', () => {
    const cb = vi.fn();
    const { connect, setOnControlMessage } = useConnectionStore.getState();
    connect('token', 'ws://host/relay', 'jwt');
    setOnControlMessage(cb);

    capturedEvents['onControlMessage']?.({ type: 'RelayReady' });
    expect(cb).toHaveBeenCalledWith({ type: 'RelayReady' });
  });

  it('dispatches desktop frames to registered callback', () => {
    const cb = vi.fn();
    const { connect, setOnDesktopFrame } = useConnectionStore.getState();
    connect('token', 'ws://host/relay', 'jwt');
    setOnDesktopFrame(cb);

    const frame = { sequence: 1, x: 0, y: 0, width: 10, height: 10, encoding: 'Raw', data: new Uint8Array([1]) };
    capturedEvents['onDesktopFrame']?.(frame);
    expect(cb).toHaveBeenCalledWith(frame);
  });

  it('dispatches terminal frames to registered callback', () => {
    const cb = vi.fn();
    const { connect, setOnTerminalFrame } = useConnectionStore.getState();
    connect('token', 'ws://host/relay', 'jwt');
    setOnTerminalFrame(cb);

    const frame = { data: new Uint8Array([0x48]) };
    capturedEvents['onTerminalFrame']?.(frame);
    expect(cb).toHaveBeenCalledWith(frame);
  });

  it('dispatches file frames to registered callback', () => {
    const cb = vi.fn();
    const { connect, setOnFileFrame } = useConnectionStore.getState();
    connect('token', 'ws://host/relay', 'jwt');
    setOnFileFrame(cb);

    const frame = { offset: 0, total_size: 100, data: new Uint8Array([1]) };
    capturedEvents['onFileFrame']?.(frame);
    expect(cb).toHaveBeenCalledWith(frame);
  });

  it('sets error on transport error event', () => {
    const { connect } = useConnectionStore.getState();
    connect('token', 'ws://host/relay', 'jwt');

    capturedEvents['onError']?.(new Error('test error'));
    expect(useConnectionStore.getState().error).toBe('test error');
  });
});
