import { describe, it, expect, vi, beforeEach } from 'vitest';
import { WebRTCTransport, type RTCConfig } from './webrtc-transport';
import type { TransportEvents } from './ws-transport';
import { encodeFrame } from '../protocol/codec';
import {
  FRAME_CONTROL,
  FRAME_DESKTOP,
  FRAME_FILE,
  FRAME_PING,
  FRAME_PONG,
  FRAME_TERMINAL,
} from '../protocol/types';

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
    createOffer: vi.fn(async (): Promise<{ type: RTCSdpType; sdp: string | undefined }> => ({
      type: 'offer',
      sdp: 'v=0\r\nmock-offer',
    })),
    setLocalDescription: vi.fn(async () => {}),
    setRemoteDescription: vi.fn(async () => {}),
    addIceCandidate: vi.fn(async () => {}),
    close: vi.fn(),
    _channels: channels,
  };
  return pc;
}

/** Deliver bytes to the transport through one channel's onmessage handler. */
function deliver(pc: ReturnType<typeof createMockPC>, channelIndex: number, bytes: Uint8Array): void {
  const channel = pc._channels.at(channelIndex);
  if (!channel?.onmessage) throw new Error(`channel ${channelIndex} has no onmessage handler`);
  const buffer = bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength);
  channel.onmessage({ data: buffer } as MessageEvent);
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

  it('sendControl sends encoded frame on open control channel', async () => {
    await transport.createOffer(testConfig);
    transport.sendControl({ type: 'RelayReady' });
    expect(mockPC._channels[0]!.send).toHaveBeenCalledTimes(1);
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

  it('passes the configured ICE servers to the peer connection', async () => {
    await transport.createOffer(testConfig);
    expect(RTCPeerConnection).toHaveBeenCalledWith({ iceServers: testConfig.iceServers });
  });

  it('returns an empty offer when the peer connection produces no SDP', async () => {
    mockPC.createOffer = vi.fn(async () => ({ type: 'offer' as RTCSdpType, sdp: undefined }));
    expect(await transport.createOffer(testConfig)).toBe('');
  });

  it('forwards a local candidate with an empty mid when the peer omits sdpMid', async () => {
    const candidateCb = vi.fn();
    transport.onLocalIceCandidate = candidateCb;
    await transport.createOffer(testConfig);

    mockPC.onicecandidate?.({
      candidate: { candidate: 'local-candidate', sdpMid: null },
    } as RTCPeerConnectionIceEvent);

    expect(candidateCb).toHaveBeenCalledWith('local-candidate', '');
  });

  it('ignores the end-of-candidates event', async () => {
    const candidateCb = vi.fn();
    transport.onLocalIceCandidate = candidateCb;
    await transport.createOffer(testConfig);

    mockPC.onicecandidate?.({ candidate: null } as RTCPeerConnectionIceEvent);

    expect(candidateCb).not.toHaveBeenCalled();
  });

  it('names the ICE state in the failure it reports', async () => {
    await transport.createOffer(testConfig);
    mockPC.iceConnectionState = 'disconnected';
    mockPC.oniceconnectionstatechange?.();

    expect(transport.state).toBe('failed');
    expect(events.onError).toHaveBeenCalledWith(new Error('ICE disconnected'));
  });

  it('returns to idle when ICE closes', async () => {
    await transport.createOffer(testConfig);
    mockPC.iceConnectionState = 'connected';
    mockPC.oniceconnectionstatechange?.();
    mockPC.iceConnectionState = 'closed';
    mockPC.oniceconnectionstatechange?.();

    expect(transport.state).toBe('idle');
    expect(events.onError).not.toHaveBeenCalled();
  });

  it('holds its state while ICE is still gathering', async () => {
    await transport.createOffer(testConfig);
    mockPC.iceConnectionState = 'checking';
    mockPC.oniceconnectionstatechange?.();

    expect(transport.state).toBe('offering');
  });

  it('sends terminal data and file frames on the bulk channel', async () => {
    await transport.createOffer(testConfig);
    const bulk = mockPC._channels[2]!;

    transport.sendTerminalData(new Uint8Array([0x68, 0x69]));
    transport.sendFileFrame({ offset: 0, total_size: 2, data: new Uint8Array([1, 2]) });

    expect(bulk.send).toHaveBeenCalledTimes(2);
    expect(mockPC._channels[0]!.send).not.toHaveBeenCalled();
  });

  it('names the channel in the not-open error', async () => {
    await transport.createOffer(testConfig);
    const control = mockPC._channels[0] as unknown as { readyState: RTCDataChannelState };
    control.readyState = 'closing';

    expect(() => {
      transport.sendControl({ type: 'RelayReady' });
    }).toThrow('DataChannel control not open');
  });

  it('reports an unknown channel when there is none at all', () => {
    expect(() => {
      transport.sendControl({ type: 'RelayReady' });
    }).toThrow('DataChannel unknown not open');
  });

  it('prepares every data channel for binary frames', async () => {
    await transport.createOffer(testConfig);
    for (const channel of mockPC._channels) {
      expect(channel.binaryType).toBe('arraybuffer');
      expect(channel.onmessage).toBeTypeOf('function');
      expect(channel.onerror).toBeTypeOf('function');
    }
  });

  it('names the channel in a data-channel error', async () => {
    await transport.createOffer(testConfig);
    mockPC._channels[1]!.onerror?.(new Event('error') as never);

    expect(events.onError).toHaveBeenCalledWith(new Error('DataChannel error: desktop'));
  });

  it('answers a ping on the control channel', async () => {
    await transport.createOffer(testConfig);
    deliver(mockPC, 1, encodeFrame({ type: FRAME_PING }));

    expect(mockPC._channels[0]!.send).toHaveBeenCalledWith(encodeFrame({ type: FRAME_PONG }));
  });

  it('ignores a pong', async () => {
    await transport.createOffer(testConfig);
    deliver(mockPC, 0, encodeFrame({ type: FRAME_PONG }));

    expect(mockPC._channels[0]!.send).not.toHaveBeenCalled();
    expect(events.onError).not.toHaveBeenCalled();
  });

  it('routes each frame type to its handler', async () => {
    await transport.createOffer(testConfig);

    deliver(mockPC, 0, encodeFrame({ type: FRAME_CONTROL, message: { type: 'RelayReady' } }));
    expect(events.onControlMessage).toHaveBeenCalledWith({ type: 'RelayReady' });

    const desktop = {
      sequence: 7,
      x: 0,
      y: 0,
      width: 2,
      height: 2,
      encoding: 'Raw' as const,
      data: new Uint8Array([1, 2, 3, 4]),
    };
    deliver(mockPC, 1, encodeFrame({ type: FRAME_DESKTOP, frame: desktop }));
    expect(events.onDesktopFrame).toHaveBeenCalledWith(expect.objectContaining({ sequence: 7 }));

    deliver(mockPC, 2, encodeFrame({ type: FRAME_TERMINAL, frame: { data: new Uint8Array([0x48]) } }));
    expect(events.onTerminalFrame).toHaveBeenCalledWith(expect.objectContaining({ data: expect.anything() }));

    deliver(mockPC, 2, encodeFrame({
      type: FRAME_FILE,
      frame: { offset: 0, total_size: 1, data: new Uint8Array([9]) },
    }));
    expect(events.onFileFrame).toHaveBeenCalledWith(expect.objectContaining({ total_size: 1 }));

    expect(events.onError).not.toHaveBeenCalled();
  });

  it('buffers candidates again after a close, as a fresh connection must', async () => {
    await transport.createOffer(testConfig);
    await transport.handleAnswer('v=0\r\nmock-answer');
    transport.close();

    // A reused transport starts over: until the new peer has a remote
    // description, a candidate has nowhere to go and must be held.
    mockPC = createMockPC();
    vi.stubGlobal('RTCPeerConnection', vi.fn(function () { return mockPC; }));
    await transport.createOffer(testConfig);
    await transport.addIceCandidate('candidate-3', 'mid-2');
    expect(mockPC.addIceCandidate).not.toHaveBeenCalled();

    await transport.handleAnswer('v=0\r\nmock-answer-2');
    expect(mockPC.addIceCandidate).toHaveBeenCalledWith({ candidate: 'candidate-3', sdpMid: 'mid-2' });
  });

  it('reports an undecodable frame instead of throwing', async () => {
    await transport.createOffer(testConfig);
    deliver(mockPC, 0, new Uint8Array([0xff, 0xff, 0xff]));

    expect(events.onError).toHaveBeenCalledTimes(1);
    expect(events.onError).toHaveBeenCalledWith(expect.any(Error));
  });
});
