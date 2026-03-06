import { describe, it, expect, vi, beforeEach, type Mock } from 'vitest';
import { WSTransport, type TransportEvents } from './ws-transport';
import { encodeFrame } from '../protocol/codec';
import {
  FRAME_CONTROL,
  FRAME_DESKTOP,
  FRAME_TERMINAL,
  FRAME_FILE,
  FRAME_PING,
  FRAME_PONG,
} from '../protocol/types';

/** Get a proper ArrayBuffer from a Uint8Array (handles shared buffer offsets). */
function toArrayBuffer(arr: Uint8Array): ArrayBuffer {
  return arr.buffer.slice(arr.byteOffset, arr.byteOffset + arr.byteLength) as ArrayBuffer;
}

// Mock WebSocket
class MockWebSocket {
  static readonly OPEN = 1;
  static readonly CLOSED = 3;

  binaryType = 'blob';
  readyState = MockWebSocket.OPEN;
  onopen: (() => void) | null = null;
  onmessage: ((event: { data: ArrayBuffer }) => void) | null = null;
  onerror: (() => void) | null = null;
  onclose: (() => void) | null = null;
  send = vi.fn();
  close = vi.fn(() => {
    this.readyState = MockWebSocket.CLOSED;
    this.onclose?.();
  });

  url: string;
  constructor(url: string) {
    this.url = url;
  }

  simulateOpen() {
    this.readyState = MockWebSocket.OPEN;
    this.onopen?.();
  }

  simulateMessage(data: ArrayBuffer) {
    this.onmessage?.({ data });
  }

  simulateError() {
    this.onerror?.();
  }

  simulateClose() {
    this.onclose?.();
  }
}

let mockWsInstance: MockWebSocket;

vi.stubGlobal('WebSocket', class extends MockWebSocket {
  constructor(url: string) {
    super(url);
    // eslint-disable-next-line @typescript-eslint/no-this-alias
    mockWsInstance = this;
  }

  // WebSocket static constants
  static override readonly OPEN = 1;
  static override readonly CLOSED = 3;
});

function createEvents(): TransportEvents & { [K in keyof TransportEvents]: Mock } {
  return {
    onStateChange: vi.fn(),
    onControlMessage: vi.fn(),
    onDesktopFrame: vi.fn(),
    onTerminalFrame: vi.fn(),
    onFileFrame: vi.fn(),
    onError: vi.fn(),
  };
}

describe('WSTransport', () => {
  let transport: WSTransport;
  let events: ReturnType<typeof createEvents>;

  beforeEach(() => {
    events = createEvents();
    transport = new WSTransport(events);
  });

  describe('connect', () => {
    it('opens WebSocket with correct URL including side and auth params', () => {
      transport.connect('ws://host/ws/relay/token123', 'jwt-token');
      expect(mockWsInstance.url).toBe('ws://host/ws/relay/token123?side=browser&auth=jwt-token');
      expect(mockWsInstance.binaryType).toBe('arraybuffer');
    });

    it('appends params with & when URL already has query string', () => {
      transport.connect('ws://host/ws/relay/token?existing=1', 'jwt');
      expect(mockWsInstance.url).toBe('ws://host/ws/relay/token?existing=1&side=browser&auth=jwt');
    });

    it('transitions to connecting then connected on open', () => {
      transport.connect('ws://host/relay', 'jwt');
      expect(events.onStateChange).toHaveBeenCalledWith('connecting');

      mockWsInstance.simulateOpen();
      expect(events.onStateChange).toHaveBeenCalledWith('connected');
      expect(transport.state).toBe('connected');
    });

    it('transitions to error on WebSocket error', () => {
      transport.connect('ws://host/relay', 'jwt');
      mockWsInstance.simulateError();
      expect(events.onStateChange).toHaveBeenCalledWith('error');
      expect(events.onError).toHaveBeenCalled();
      expect(transport.state).toBe('error');
    });

    it('transitions to disconnected on clean close', () => {
      transport.connect('ws://host/relay', 'jwt');
      mockWsInstance.simulateOpen();
      mockWsInstance.simulateClose();
      expect(events.onStateChange).toHaveBeenCalledWith('disconnected');
    });
  });

  describe('incoming frames', () => {
    beforeEach(() => {
      transport.connect('ws://host/relay', 'jwt');
      mockWsInstance.simulateOpen();
    });

    it('dispatches Control message to onControlMessage', () => {
      const encoded = encodeFrame({ type: FRAME_CONTROL, message: { type: 'RelayReady' } });
      mockWsInstance.simulateMessage(toArrayBuffer(encoded));
      expect(events.onControlMessage).toHaveBeenCalledWith(
        expect.objectContaining({ type: 'RelayReady' }),
      );
    });

    it('dispatches DesktopFrame to onDesktopFrame', () => {
      const encoded = encodeFrame({
        type: FRAME_DESKTOP,
        frame: { sequence: 1, x: 0, y: 0, width: 10, height: 10, encoding: 'Raw', data: new Uint8Array([1, 2]) },
      });
      mockWsInstance.simulateMessage(toArrayBuffer(encoded));
      expect(events.onDesktopFrame).toHaveBeenCalledWith(
        expect.objectContaining({ sequence: 1, width: 10 }),
      );
    });

    it('dispatches TerminalFrame to onTerminalFrame', () => {
      const encoded = encodeFrame({
        type: FRAME_TERMINAL,
        frame: { data: new Uint8Array([0x48, 0x69]) },
      });
      mockWsInstance.simulateMessage(toArrayBuffer(encoded));
      expect(events.onTerminalFrame).toHaveBeenCalled();
    });

    it('dispatches FileFrame to onFileFrame', () => {
      const encoded = encodeFrame({
        type: FRAME_FILE,
        frame: { offset: 0, total_size: 100, data: new Uint8Array([1]) },
      });
      mockWsInstance.simulateMessage(toArrayBuffer(encoded));
      expect(events.onFileFrame).toHaveBeenCalledWith(
        expect.objectContaining({ offset: 0, total_size: 100 }),
      );
    });

    it('auto-responds to Ping with Pong', () => {
      const pingData = encodeFrame({ type: FRAME_PING });
      mockWsInstance.simulateMessage(toArrayBuffer(pingData));

      expect(mockWsInstance.send).toHaveBeenCalledTimes(1);
      const sentData = mockWsInstance.send.mock.calls[0]![0] as Uint8Array;
      expect(sentData).toEqual(new Uint8Array([FRAME_PONG]));
    });

    it('ignores Pong frames', () => {
      const pongData = encodeFrame({ type: FRAME_PONG });
      mockWsInstance.simulateMessage(toArrayBuffer(pongData));

      expect(events.onControlMessage).not.toHaveBeenCalled();
      expect(events.onDesktopFrame).not.toHaveBeenCalled();
    });

    it('calls onError for invalid frame data', () => {
      const invalid = toArrayBuffer(new Uint8Array([0xff, 0x00]));
      mockWsInstance.simulateMessage(invalid);
      expect(events.onError).toHaveBeenCalled();
    });
  });

  describe('sending', () => {
    beforeEach(() => {
      transport.connect('ws://host/relay', 'jwt');
      mockWsInstance.simulateOpen();
    });

    it('sendControl sends encoded control frame', () => {
      transport.sendControl({ type: 'RelayReady' });
      expect(mockWsInstance.send).toHaveBeenCalledTimes(1);
      const sent = mockWsInstance.send.mock.calls[0]![0] as Uint8Array;
      expect(sent[0]).toBe(FRAME_CONTROL);
    });

    it('sendTerminalData sends encoded terminal frame', () => {
      transport.sendTerminalData(new Uint8Array([0x48, 0x69]));
      expect(mockWsInstance.send).toHaveBeenCalledTimes(1);
      const sent = mockWsInstance.send.mock.calls[0]![0] as Uint8Array;
      expect(sent[0]).toBe(FRAME_TERMINAL);
    });

    it('sendFileFrame sends encoded file frame', () => {
      transport.sendFileFrame({ offset: 0, total_size: 10, data: new Uint8Array([1]) });
      expect(mockWsInstance.send).toHaveBeenCalledTimes(1);
      const sent = mockWsInstance.send.mock.calls[0]![0] as Uint8Array;
      expect(sent[0]).toBe(FRAME_FILE);
    });

    it('throws when sending on disconnected transport', () => {
      transport.disconnect();
      expect(() => transport.sendControl({ type: 'RelayReady' })).toThrow('WebSocket not connected');
    });
  });

  describe('disconnect', () => {
    it('closes WebSocket and transitions to disconnected', () => {
      transport.connect('ws://host/relay', 'jwt');
      mockWsInstance.simulateOpen();
      transport.disconnect();
      expect(transport.state).toBe('disconnected');
    });

    it('is safe to call when already disconnected', () => {
      transport.disconnect();
      expect(transport.state).toBe('disconnected');
    });
  });
});
