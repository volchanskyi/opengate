import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { useConnectionStore } from './connection-store';
import { useToastStore } from '../../../lib/feedback/toast-store';

// Captured transport events for testing dispatch
let capturedEvents: Record<string, (...args: unknown[]) => void> = {};

// The WebRTC half of the store: the events it hands the transport, and the
// transport instance itself, so a test can drive the signaling round trip.
let capturedWebrtcEvents: Record<string, (...args: unknown[]) => void> = {};
let mockWebrtc: MockWebRTCTransport | null = null;

/** When set, the next createOffer rejects with it — the SDP negotiation failing
 * before an upgrade ever gets off the ground. */
let offerRejection: Error | null = null;

class MockWebRTCTransport {
  handleAnswer = vi.fn<(sdp: string) => Promise<void>>().mockResolvedValue(undefined);
  addIceCandidate = vi.fn<(c: string, m: string) => Promise<void>>().mockResolvedValue(undefined);
  createOffer = vi.fn<() => Promise<string>>(async () => {
    if (offerRejection) throw offerRejection;
    return 'v=0\r\nmock-offer';
  });
  close = vi.fn();
  onLocalIceCandidate: ((candidate: string, mid: string) => void) | null = null;

  constructor(events: Record<string, (...args: unknown[]) => void>) {
    capturedWebrtcEvents = events;
  }
}

// The factory runs while the mocked module is imported — before this file's
// class declaration has been evaluated — so it hands back a shim that defers the
// reference to construction time and returns the real double from there.
vi.mock('../../../lib/transport/webrtc-transport', () => ({
  WebRTCTransport: class {
    constructor(events: Record<string, (...args: unknown[]) => void>) {
      mockWebrtc = new MockWebRTCTransport(events);
      return mockWebrtc as never;
    }
  },
}));

const iceServers: RTCIceServer[] = [{ urls: 'stun:stun.example.com:3478' }];

/** Connect, then take the relay-to-WebRTC upgrade as far as `upgrading`. */
function startUpgrade(): MockWebRTCTransport {
  const { connect } = useConnectionStore.getState();
  connect('token', 'ws://host/relay', 'jwt', iceServers);
  useConnectionStore.getState().initiateWebRTCUpgrade();
  if (!mockWebrtc) throw new Error('the upgrade did not create a WebRTC transport');
  return mockWebrtc;
}

/** Messages of every toast currently raised. */
function toastMessages(): string[] {
  return useToastStore.getState().toasts.map((t) => t.message);
}

// Mock WSTransport
vi.mock('../../../lib/transport/ws-transport', () => {
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
    mockWebrtc = null;
    capturedWebrtcEvents = {};
    offerRejection = null;
    useToastStore.setState({ toasts: [] });
    // The store's fallback toast fires once per session; disconnect resets it.
    useConnectionStore.getState().disconnect();
    // Reset store state between tests
    useConnectionStore.setState({
      state: 'disconnected',
      token: null,
      error: null,
      transport: null,
      webrtcTransport: null,
      signalingState: 'relay-only',
      iceServers: [],
      onControlMessage: null,
      onDesktopFrame: null,
      onTerminalFrame: null,
      onFileFrame: null,
    });
    vi.spyOn(console, 'warn').mockImplementation(() => {});
  });

  afterEach(() => {
    vi.restoreAllMocks();
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

    const transport = useConnectionStore.getState().transport;
    const { disconnect } = useConnectionStore.getState();
    disconnect();

    // The socket has to be told, not just forgotten: dropping the reference
    // without disconnecting leaves it open.
    expect(transport?.disconnect).toHaveBeenCalledTimes(1);
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

  it('keeps signaling messages out of the application callback', () => {
    const cb = vi.fn();
    const webrtc = startUpgrade();
    useConnectionStore.getState().setOnControlMessage(cb);

    capturedEvents['onControlMessage']?.({ type: 'IceCandidate', candidate: 'cand', mid: '0' });

    expect(webrtc.addIceCandidate).toHaveBeenCalledWith('cand', '0');
    expect(cb).not.toHaveBeenCalled();

    // A non-signaling message still reaches the application.
    capturedEvents['onControlMessage']?.({ type: 'RelayReady' });
    expect(cb).toHaveBeenCalledWith({ type: 'RelayReady' });
  });

  it('ignores an ICE candidate that arrives before an upgrade starts', () => {
    const cb = vi.fn();
    const { connect } = useConnectionStore.getState();
    connect('token', 'ws://host/relay', 'jwt', iceServers);
    useConnectionStore.getState().setOnControlMessage(cb);

    capturedEvents['onControlMessage']?.({ type: 'IceCandidate', candidate: 'cand', mid: '0' });

    // Consumed as signaling even with no transport to hand it to.
    expect(cb).not.toHaveBeenCalled();
    expect(mockWebrtc).toBeNull();
  });

  describe('WebRTC upgrade', () => {
    it('does not start without a transport', () => {
      useConnectionStore.getState().initiateWebRTCUpgrade();
      expect(mockWebrtc).toBeNull();
      expect(useConnectionStore.getState().signalingState).toBe('relay-only');
    });

    it('does not start without ICE servers', () => {
      const { connect } = useConnectionStore.getState();
      connect('token', 'ws://host/relay', 'jwt');
      useConnectionStore.getState().initiateWebRTCUpgrade();
      expect(mockWebrtc).toBeNull();
      expect(useConnectionStore.getState().signalingState).toBe('relay-only');
    });

    it('does not start a second time while one is in flight', () => {
      const first = startUpgrade();
      useConnectionStore.getState().initiateWebRTCUpgrade();
      expect(mockWebrtc).toBe(first);
    });

    it('offers over the relay and moves to upgrading', async () => {
      const webrtc = startUpgrade();
      const transport = useConnectionStore.getState().transport;

      expect(useConnectionStore.getState().signalingState).toBe('upgrading');
      expect(webrtc.createOffer).toHaveBeenCalledWith({ iceServers });

      await vi.waitFor(() => {
        expect(transport?.sendControl).toHaveBeenCalledWith({
          type: 'SwitchToWebRTC',
          sdp_offer: 'v=0\r\nmock-offer',
        });
      });
    });

    it('falls back to the relay when the offer fails', async () => {
      offerRejection = new Error('no ICE transport');
      startUpgrade();

      await vi.waitFor(() => {
        expect(useConnectionStore.getState().signalingState).toBe('fallback');
      });
      expect(toastMessages()).toEqual([
        'WebRTC unavailable, using WebSocket fallback (no ICE transport)',
      ]);
      expect(console.warn).toHaveBeenCalledWith('[webrtc] createOffer failed:', offerRejection);
    });

    it('names the failed step when the offer rejects with a non-error', async () => {
      offerRejection = 'peer connection unavailable' as unknown as Error;
      startUpgrade();

      await vi.waitFor(() => {
        expect(useConnectionStore.getState().signalingState).toBe('fallback');
      });
      expect(toastMessages()).toEqual([
        'WebRTC unavailable, using WebSocket fallback (createOffer failed)',
      ]);
    });

    it('forwards local ICE candidates over the relay', () => {
      const webrtc = startUpgrade();
      const transport = useConnectionStore.getState().transport;

      webrtc.onLocalIceCandidate?.('local-cand', '1');

      expect(transport?.sendControl).toHaveBeenCalledWith({
        type: 'IceCandidate',
        candidate: 'local-cand',
        mid: '1',
      });
    });

    it('applies the agent answer while upgrading', () => {
      const webrtc = startUpgrade();

      capturedEvents['onControlMessage']?.({ type: 'SwitchToWebRTC', sdp_offer: 'v=0\r\nanswer' });

      expect(webrtc.handleAnswer).toHaveBeenCalledWith('v=0\r\nanswer');
    });

    it('falls back when the agent answer cannot be applied', async () => {
      const webrtc = startUpgrade();
      const failure = new Error('bad answer');
      webrtc.handleAnswer.mockRejectedValueOnce(failure);

      capturedEvents['onControlMessage']?.({ type: 'SwitchToWebRTC', sdp_offer: 'v=0\r\nanswer' });

      await vi.waitFor(() => {
        expect(useConnectionStore.getState().signalingState).toBe('fallback');
      });
      expect(toastMessages()).toEqual([
        'WebRTC unavailable, using WebSocket fallback (bad answer)',
      ]);
      expect(console.warn).toHaveBeenCalledWith('[webrtc] handleAnswer failed:', failure);
    });

    it('names the failed step when the answer rejects with a non-error', async () => {
      const webrtc = startUpgrade();
      webrtc.handleAnswer.mockRejectedValueOnce('malformed sdp');

      capturedEvents['onControlMessage']?.({ type: 'SwitchToWebRTC', sdp_offer: 'v=0\r\nanswer' });

      await vi.waitFor(() => {
        expect(useConnectionStore.getState().signalingState).toBe('fallback');
      });
      expect(toastMessages()).toEqual([
        'WebRTC unavailable, using WebSocket fallback (handleAnswer failed)',
      ]);
    });

    it('ignores an answer that arrives outside the upgrade window', () => {
      const webrtc = startUpgrade();
      useConnectionStore.setState({ signalingState: 'webrtc' });

      capturedEvents['onControlMessage']?.({ type: 'SwitchToWebRTC', sdp_offer: 'v=0\r\nlate' });

      expect(webrtc.handleAnswer).not.toHaveBeenCalled();
    });

    it('acknowledges the switch and completes the upgrade', () => {
      startUpgrade();
      const transport = useConnectionStore.getState().transport;

      capturedEvents['onControlMessage']?.({ type: 'SwitchAck' });

      expect(transport?.sendControl).toHaveBeenCalledWith({ type: 'SwitchAck' });
      expect(useConnectionStore.getState().signalingState).toBe('webrtc');
    });

    it('ignores a switch ack outside the upgrade window', () => {
      startUpgrade();
      useConnectionStore.setState({ signalingState: 'relay-only' });
      const transport = useConnectionStore.getState().transport;
      (transport?.sendControl as ReturnType<typeof vi.fn>).mockClear();

      capturedEvents['onControlMessage']?.({ type: 'SwitchAck' });

      expect(transport?.sendControl).not.toHaveBeenCalled();
      expect(useConnectionStore.getState().signalingState).toBe('relay-only');
    });

    it('swallows an ICE candidate the peer rejects', async () => {
      const webrtc = startUpgrade();
      const failure = new Error('stale candidate');
      webrtc.addIceCandidate.mockRejectedValueOnce(failure);

      capturedEvents['onControlMessage']?.({ type: 'IceCandidate', candidate: 'cand', mid: '0' });

      await vi.waitFor(() => {
        expect(console.warn).toHaveBeenCalledWith('[webrtc] addIceCandidate failed:', failure);
      });
      // Recoverable: the session stays on its upgrade path, with no fallback.
      expect(useConnectionStore.getState().signalingState).toBe('upgrading');
      expect(toastMessages()).toEqual([]);
    });

    it('consumes every signaling message instead of forwarding it', () => {
      const cb = vi.fn();
      startUpgrade();
      useConnectionStore.getState().setOnControlMessage(cb);

      capturedEvents['onControlMessage']?.({ type: 'SwitchToWebRTC', sdp_offer: 'v=0\r\nanswer' });
      capturedEvents['onControlMessage']?.({ type: 'SwitchAck' });
      capturedEvents['onControlMessage']?.({ type: 'IceCandidate', candidate: 'cand', mid: '0' });

      expect(cb).not.toHaveBeenCalled();
    });

    it('routes WebRTC frames to the same application callbacks', () => {
      startUpgrade();
      const onControl = vi.fn();
      const onDesktop = vi.fn();
      const onTerminal = vi.fn();
      const onFile = vi.fn();
      const store = useConnectionStore.getState();
      store.setOnControlMessage(onControl);
      store.setOnDesktopFrame(onDesktop);
      store.setOnTerminalFrame(onTerminal);
      store.setOnFileFrame(onFile);

      capturedWebrtcEvents['onStateChange']?.('connected');
      capturedWebrtcEvents['onControlMessage']?.({ type: 'RelayReady' });
      capturedWebrtcEvents['onDesktopFrame']?.({ sequence: 3 });
      capturedWebrtcEvents['onTerminalFrame']?.({ data: new Uint8Array([1]) });
      capturedWebrtcEvents['onFileFrame']?.({ offset: 0 });

      expect(onControl).toHaveBeenCalledWith({ type: 'RelayReady' });
      expect(onDesktop).toHaveBeenCalledWith({ sequence: 3 });
      expect(onTerminal).toHaveBeenCalledWith({ data: new Uint8Array([1]) });
      expect(onFile).toHaveBeenCalledWith({ offset: 0 });
      // onStateChange is deliberately inert: the relay owns connection state.
      expect(useConnectionStore.getState().state).not.toBe('connected');
    });

    it('falls back once and records the error when the peer connection fails', () => {
      startUpgrade();

      capturedWebrtcEvents['onError']?.(new Error('ICE failed'));
      capturedWebrtcEvents['onError']?.(new Error('ICE failed again'));

      const state = useConnectionStore.getState();
      expect(state.signalingState).toBe('fallback');
      expect(state.error).toBe('ICE failed again');
      // One session, one fallback notice.
      expect(toastMessages()).toEqual(['WebRTC unavailable, using WebSocket fallback (ICE failed)']);
    });

    it('closes the WebRTC transport when a new session connects', () => {
      const webrtc = startUpgrade();
      useConnectionStore.getState().connect('token-2', 'ws://host/relay', 'jwt');

      expect(webrtc.close).toHaveBeenCalled();
      expect(useConnectionStore.getState().webrtcTransport).toBeNull();
      expect(useConnectionStore.getState().signalingState).toBe('relay-only');
    });

    it('re-arms the fallback notice for the next session', () => {
      startUpgrade();
      capturedWebrtcEvents['onError']?.(new Error('first session'));
      expect(toastMessages()).toHaveLength(1);

      useConnectionStore.getState().disconnect();
      startUpgrade();
      capturedWebrtcEvents['onError']?.(new Error('second session'));

      expect(toastMessages()).toHaveLength(2);
    });
  });

  it('disconnect closes the WebRTC transport too', () => {
    const webrtc = startUpgrade();

    useConnectionStore.getState().disconnect();

    expect(webrtc.close).toHaveBeenCalledTimes(1);
    expect(useConnectionStore.getState().webrtcTransport).toBeNull();
  });

  it('disconnect resets signaling state and clears webrtc transport', () => {
    const { connect, disconnect } = useConnectionStore.getState();
    connect('token', 'ws://host/relay', 'jwt');

    // Trigger error to set signalingState
    capturedEvents['onError']?.(new Error('connection lost'));
    expect(useConnectionStore.getState().error).toBe('connection lost');

    disconnect();
    expect(useConnectionStore.getState().state).toBe('disconnected');
    expect(useConnectionStore.getState().signalingState).toBe('relay-only');
    expect(useConnectionStore.getState().webrtcTransport).toBeNull();
  });
});
