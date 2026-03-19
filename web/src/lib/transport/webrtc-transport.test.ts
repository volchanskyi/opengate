import { describe, it, expect, vi, beforeEach } from 'vitest';
import { WebRTCTransport, type RTCConfig } from './webrtc-transport';
import type { TransportEvents } from './ws-transport';

// Mock RTCPeerConnection and RTCDataChannel
function createMockChannel(label: string): RTCDataChannel {
  const channel = {
    label,
    readyState: 'open' as RTCDataChannelState,
    binaryType: 'arraybuffer' as BinaryType,
    onmessage: null as ((event: MessageEvent) => void) | null,
    onerror: null as ((event: Event) => void) | null,
    send: vi.fn(),
    close: vi.fn(),
  };
  return channel as unknown as RTCDataChannel;
}

function createMockPC() {
  const channels: RTCDataChannel[] = [];
  const pc = {
    iceConnectionState: 'new' as RTCIceConnectionState,
    onicecandidate: null as ((event: RTCPeerConnectionIceEvent) => void) | null,
    oniceconnectionstatechange: null as (() => void) | null,
    createDataChannel: vi.fn((label: string) => {
      const ch = createMockChannel(label);
      channels.push(ch);
      return ch;
    }),
    createOffer: vi.fn(async () => ({ type: 'offer' as RTCSdpType, sdp: 'v=0\r\nmock-offer' })),
    setLocalDescription: vi.fn(async () => {}),
    setRemoteDescription: vi.fn(async () => {}),
    addIceCandidate: vi.fn(async () => {}),
    close: vi.fn(),
    _channels: channels,
  };
  return pc;
}

function createEvents(): TransportEvents {
  return {
    onStateChange: vi.fn(),
    onControlMessage: vi.fn(),
    onDesktopFrame: vi.fn(),
    onTerminalFrame: vi.fn(),
    onFileFrame: vi.fn(),
    onError: vi.fn(),
  };
}

const testConfig: RTCConfig = {
  iceServers: [{ urls: 'stun:stun.l.google.com:19302' }],
};

describe('WebRTCTransport', () => {
  let transport: WebRTCTransport;
  let events: TransportEvents;
  let mockPC: ReturnType<typeof createMockPC>;

  beforeEach(() => {
    events = createEvents();
    transport = new WebRTCTransport(events);

    mockPC = createMockPC();
    // Must use a regular function (not arrow) so it can be called with `new`
    vi.stubGlobal('RTCPeerConnection', vi.fn(function () { return mockPC; }));
  });

  it('starts in idle state', () => {
    expect(transport.state).toBe('idle');
  });

  it('creates offer and transitions to offering', async () => {
    const sdp = await transport.createOffer(testConfig);

    expect(sdp).toBe('v=0\r\nmock-offer');
    expect(transport.state).toBe('offering');
    expect(mockPC.createDataChannel).toHaveBeenCalledTimes(3);
    expect(mockPC.createDataChannel).toHaveBeenCalledWith('control', expect.objectContaining({ ordered: true }));
    expect(mockPC.createDataChannel).toHaveBeenCalledWith('desktop', expect.objectContaining({ ordered: false, maxRetransmits: 0 }));
    expect(mockPC.createDataChannel).toHaveBeenCalledWith('bulk', expect.objectContaining({ ordered: true }));
  });

  it('handles SDP answer', async () => {
    await transport.createOffer(testConfig);
    await transport.handleAnswer('v=0\r\nmock-answer');

    expect(mockPC.setRemoteDescription).toHaveBeenCalledWith({
      type: 'answer',
      sdp: 'v=0\r\nmock-answer',
    });
    expect(transport.state).toBe('answering');
  });

  it('buffers ICE candidates before remote description', async () => {
    await transport.createOffer(testConfig);

    // Add candidate before answer
    await transport.addIceCandidate('candidate-1', 'mid-0');
    expect(mockPC.addIceCandidate).not.toHaveBeenCalled();

    // Set answer — should flush
    await transport.handleAnswer('v=0\r\nmock-answer');
    expect(mockPC.addIceCandidate).toHaveBeenCalledWith({
      candidate: 'candidate-1',
      sdpMid: 'mid-0',
    });
  });

  it('adds ICE candidates directly after remote description', async () => {
    await transport.createOffer(testConfig);
    await transport.handleAnswer('v=0\r\nmock-answer');

    await transport.addIceCandidate('candidate-2', 'mid-1');
    expect(mockPC.addIceCandidate).toHaveBeenCalledTimes(1);
    expect(mockPC.addIceCandidate).toHaveBeenCalledWith({
      candidate: 'candidate-2',
      sdpMid: 'mid-1',
    });
  });

  it('fires onLocalIceCandidate callback', async () => {
    const candidateCb = vi.fn();
    transport.onLocalIceCandidate = candidateCb;

    await transport.createOffer(testConfig);

    // Simulate ICE candidate event
    const event = {
      candidate: { candidate: 'local-candidate', sdpMid: '0' },
    } as RTCPeerConnectionIceEvent;
    mockPC.onicecandidate?.(event);

    expect(candidateCb).toHaveBeenCalledWith('local-candidate', '0');
  });

  it('transitions to connected on ICE connected', async () => {
    await transport.createOffer(testConfig);
    mockPC.iceConnectionState = 'connected';
    mockPC.oniceconnectionstatechange?.();
    expect(transport.state).toBe('connected');
  });

  it('transitions to failed on ICE failure', async () => {
    await transport.createOffer(testConfig);
    mockPC.iceConnectionState = 'failed';
    mockPC.oniceconnectionstatechange?.();
    expect(transport.state).toBe('failed');
    expect(events.onError).toHaveBeenCalled();
  });

  it('close cleans up everything', async () => {
    await transport.createOffer(testConfig);
    transport.close();

    expect(mockPC.close).toHaveBeenCalled();
    expect(transport.state).toBe('idle');
  });

  it('sendControl throws when channel not open', () => {
    expect(() => {
      transport.sendControl({ type: 'RelayReady' });
    }).toThrow('not open');
  });

  it('ignores addIceCandidate when no peer connection', async () => {
    // No createOffer called — should not throw and state remains idle
    await transport.addIceCandidate('candidate', 'mid');
    expect(transport.state).toBe('idle');
  });
});
