import { describe, it, expect } from 'vitest';
import { encode } from '@msgpack/msgpack';
import { encodeFrame, decodeFrame } from './codec';
import {
  FRAME_CONTROL,
  FRAME_DESKTOP,
  FRAME_TERMINAL,
  FRAME_FILE,
  FRAME_PING,
  FRAME_PONG,
  MAX_FRAME_SIZE,
} from './types';
import type { Frame, ControlMessage, DesktopFrame, TerminalFrame, FileFrame } from './types';

describe('encodeFrame', () => {
  it('encodes Ping as single byte [0x05]', () => {
    const result = encodeFrame({ type: FRAME_PING });
    expect(result).toEqual(new Uint8Array([0x05]));
  });

  it('encodes Pong as single byte [0x06]', () => {
    const result = encodeFrame({ type: FRAME_PONG });
    expect(result).toEqual(new Uint8Array([0x06]));
  });

  it('encodes Control(RelayReady) with correct wire format', () => {
    const msg: ControlMessage = { type: 'RelayReady' };
    const result = encodeFrame({ type: FRAME_CONTROL, message: msg });

    // [0x01][4-byte BE len][msgpack payload]
    expect(result[0]).toBe(0x01);

    const view = new DataView(result.buffer, result.byteOffset);
    const length = view.getUint32(1, false);
    expect(result.length).toBe(5 + length);

    // Payload should be msgpack encoding of {type: "RelayReady"}
    const expectedPayload = encode({ type: 'RelayReady' });
    expect(result.subarray(5)).toEqual(new Uint8Array(expectedPayload));
  });

  it('encodes DesktopFrame with correct type byte', () => {
    const frame: DesktopFrame = {
      sequence: 1,
      x: 0,
      y: 0,
      width: 100,
      height: 100,
      encoding: 'Raw',
      data: new Uint8Array([255, 0, 0, 255]),
    };
    const result = encodeFrame({ type: FRAME_DESKTOP, frame });
    expect(result[0]).toBe(0x02);
  });

  it('encodes TerminalFrame with correct type byte', () => {
    const frame: TerminalFrame = { data: new Uint8Array([0x48, 0x69]) };
    const result = encodeFrame({ type: FRAME_TERMINAL, frame });
    expect(result[0]).toBe(0x03);
  });

  it('encodes FileFrame with correct type byte', () => {
    const frame: FileFrame = { offset: 0, total_size: 100, data: new Uint8Array([1, 2, 3]) };
    const result = encodeFrame({ type: FRAME_FILE, frame });
    expect(result[0]).toBe(0x04);
  });
});

describe('decodeFrame', () => {
  it('decodes Ping from single byte', () => {
    const { frame, bytesConsumed } = decodeFrame(new Uint8Array([0x05]));
    expect(frame.type).toBe(FRAME_PING);
    expect(bytesConsumed).toBe(1);
  });

  it('decodes Pong from single byte', () => {
    const { frame, bytesConsumed } = decodeFrame(new Uint8Array([0x06]));
    expect(frame.type).toBe(FRAME_PONG);
    expect(bytesConsumed).toBe(1);
  });

  it('rejects unknown frame type byte', () => {
    expect(() => decodeFrame(new Uint8Array([0xff]))).toThrow('unknown frame type: 0xff');
  });

  it('rejects empty data', () => {
    expect(() => decodeFrame(new Uint8Array([]))).toThrow('incomplete frame: empty data');
  });

  it('rejects incomplete header', () => {
    expect(() => decodeFrame(new Uint8Array([0x01, 0x00]))).toThrow(/incomplete frame/);
  });

  it('rejects frames exceeding MAX_FRAME_SIZE', () => {
    const data = new Uint8Array(5);
    data[0] = 0x01;
    const view = new DataView(data.buffer);
    view.setUint32(1, MAX_FRAME_SIZE + 1, false);
    expect(() => decodeFrame(data)).toThrow(/frame too large/);
  });

  it('rejects incomplete payload', () => {
    const data = new Uint8Array(5);
    data[0] = 0x01;
    const view = new DataView(data.buffer);
    view.setUint32(1, 100, false); // says 100 bytes but only 0 follow
    expect(() => decodeFrame(data)).toThrow(/incomplete frame/);
  });
});

describe('round-trip encode/decode', () => {
  it('round-trips Ping', () => {
    const original: Frame = { type: FRAME_PING };
    const encoded = encodeFrame(original);
    const { frame } = decodeFrame(encoded);
    expect(frame.type).toBe(FRAME_PING);
  });

  it('round-trips Pong', () => {
    const original: Frame = { type: FRAME_PONG };
    const encoded = encodeFrame(original);
    const { frame } = decodeFrame(encoded);
    expect(frame.type).toBe(FRAME_PONG);
  });

  it('round-trips Control(RelayReady)', () => {
    const original: Frame = { type: FRAME_CONTROL, message: { type: 'RelayReady' } };
    const encoded = encodeFrame(original);
    const { frame } = decodeFrame(encoded);
    expect(frame.type).toBe(FRAME_CONTROL);
    if (frame.type === FRAME_CONTROL) {
      expect(frame.message.type).toBe('RelayReady');
    }
  });

  it('round-trips Control(AgentRegister) with fields', () => {
    const original: Frame = {
      type: FRAME_CONTROL,
      message: {
        type: 'AgentRegister',
        capabilities: ['RemoteDesktop', 'Terminal'],
        hostname: 'test-host',
        os: 'Linux',
        arch: 'amd64',
        version: '0.1.0',
      },
    };
    const encoded = encodeFrame(original);
    const { frame } = decodeFrame(encoded);
    expect(frame.type).toBe(FRAME_CONTROL);
    if (frame.type === FRAME_CONTROL) {
      const msg = frame.message as { type: string; capabilities: string[]; hostname: string; os: string; arch: string; version: string };
      expect(msg.type).toBe('AgentRegister');
      expect(msg.capabilities).toEqual(['RemoteDesktop', 'Terminal']);
      expect(msg.hostname).toBe('test-host');
      expect(msg.os).toBe('Linux');
      expect(msg.arch).toBe('amd64');
      expect(msg.version).toBe('0.1.0');
    }
  });

  it('round-trips Control(IceCandidate) with fields', () => {
    const original: Frame = {
      type: FRAME_CONTROL,
      message: { type: 'IceCandidate', candidate: 'candidate:1 udp 123', mid: '0' },
    };
    const encoded = encodeFrame(original);
    const { frame } = decodeFrame(encoded);
    if (frame.type === FRAME_CONTROL) {
      const msg = frame.message as { type: string; candidate: string; mid: string };
      expect(msg.candidate).toBe('candidate:1 udp 123');
      expect(msg.mid).toBe('0');
    }
  });

  it('round-trips DesktopFrame preserving binary data', () => {
    const pixelData = new Uint8Array([255, 0, 0, 255, 0, 255, 0, 255]);
    const original: Frame = {
      type: FRAME_DESKTOP,
      frame: { sequence: 42, x: 10, y: 20, width: 2, height: 1, encoding: 'Raw', data: pixelData },
    };
    const encoded = encodeFrame(original);
    const { frame } = decodeFrame(encoded);
    expect(frame.type).toBe(FRAME_DESKTOP);
    if (frame.type === FRAME_DESKTOP) {
      expect(frame.frame.sequence).toBe(42);
      expect(frame.frame.x).toBe(10);
      expect(frame.frame.y).toBe(20);
      expect(frame.frame.width).toBe(2);
      expect(frame.frame.height).toBe(1);
      expect(frame.frame.encoding).toBe('Raw');
      expect(new Uint8Array(frame.frame.data)).toEqual(pixelData);
    }
  });

  it('round-trips TerminalFrame preserving binary data', () => {
    const textData = new Uint8Array([0x48, 0x65, 0x6c, 0x6c, 0x6f]);
    const original: Frame = { type: FRAME_TERMINAL, frame: { data: textData } };
    const encoded = encodeFrame(original);
    const { frame } = decodeFrame(encoded);
    expect(frame.type).toBe(FRAME_TERMINAL);
    if (frame.type === FRAME_TERMINAL) {
      expect(new Uint8Array(frame.frame.data)).toEqual(textData);
    }
  });

  it('round-trips FileFrame preserving offset and total_size', () => {
    const fileData = new Uint8Array([10, 20, 30]);
    const original: Frame = {
      type: FRAME_FILE,
      frame: { offset: 1048576, total_size: 10485760, data: fileData },
    };
    const encoded = encodeFrame(original);
    const { frame } = decodeFrame(encoded);
    expect(frame.type).toBe(FRAME_FILE);
    if (frame.type === FRAME_FILE) {
      expect(frame.frame.offset).toBe(1048576);
      expect(frame.frame.total_size).toBe(10485760);
      expect(new Uint8Array(frame.frame.data)).toEqual(fileData);
    }
  });

  it('bytesConsumed equals encoded length for all frame types', () => {
    const frames: Frame[] = [
      { type: FRAME_PING },
      { type: FRAME_PONG },
      { type: FRAME_CONTROL, message: { type: 'RelayReady' } },
      { type: FRAME_DESKTOP, frame: { sequence: 0, x: 0, y: 0, width: 1, height: 1, encoding: 'Raw', data: new Uint8Array([1]) } },
      { type: FRAME_TERMINAL, frame: { data: new Uint8Array([1]) } },
      { type: FRAME_FILE, frame: { offset: 0, total_size: 1, data: new Uint8Array([1]) } },
    ];

    for (const original of frames) {
      const encoded = encodeFrame(original);
      const { bytesConsumed } = decodeFrame(encoded);
      expect(bytesConsumed).toBe(encoded.length);
    }
  });

  it('round-trips input control messages', () => {
    const inputMsgs: Frame[] = [
      { type: FRAME_CONTROL, message: { type: 'MouseMove', x: 100, y: 200 } },
      { type: FRAME_CONTROL, message: { type: 'MouseClick', button: 'Left', pressed: true, x: 50, y: 60 } },
      { type: FRAME_CONTROL, message: { type: 'KeyPress', key: 'KeyA', pressed: true } },
      { type: FRAME_CONTROL, message: { type: 'TerminalResize', cols: 80, rows: 24 } },
      { type: FRAME_CONTROL, message: { type: 'ChatMessage', text: 'hello', sender: 'browser' } },
    ];

    for (const original of inputMsgs) {
      const encoded = encodeFrame(original);
      const { frame } = decodeFrame(encoded);
      expect(frame.type).toBe(FRAME_CONTROL);
      if (frame.type === FRAME_CONTROL && original.type === FRAME_CONTROL) {
        expect(frame.message.type).toBe(original.message.type);
      }
    }
  });
});
