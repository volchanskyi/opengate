import { describe, it, expect, vi } from 'vitest';
import { frameDispatch } from './frame-dispatch';
import { encodeFrame } from '../protocol/codec';
import type { Frame } from '../protocol/types';
import {
  FRAME_CONTROL,
  FRAME_DESKTOP,
  FRAME_TERMINAL,
  FRAME_FILE,
  FRAME_PING,
  FRAME_PONG,
} from '../protocol/types';

function toBuffer(frame: Frame): ArrayBuffer {
  const arr = encodeFrame(frame);
  return arr.buffer.slice(arr.byteOffset, arr.byteOffset + arr.byteLength) as ArrayBuffer;
}

function makeSink() {
  return {
    onControlMessage: vi.fn(),
    onDesktopFrame: vi.fn(),
    onTerminalFrame: vi.fn(),
    onFileFrame: vi.fn(),
    onError: vi.fn(),
  };
}

describe('frameDispatch', () => {
  it('routes a control frame to onControlMessage', () => {
    const sink = makeSink();
    const sendPong = vi.fn();
    frameDispatch(
      toBuffer({ type: FRAME_CONTROL, message: { type: 'RelayReady' } }),
      sink,
      sendPong,
    );
    expect(sink.onControlMessage).toHaveBeenCalledWith({ type: 'RelayReady' });
    expect(sendPong).not.toHaveBeenCalled();
  });

  it('routes a desktop frame to onDesktopFrame only', () => {
    const sink = makeSink();
    frameDispatch(
      toBuffer({
        type: FRAME_DESKTOP,
        frame: {
          sequence: 7,
          x: 0,
          y: 0,
          width: 2,
          height: 2,
          encoding: 'Jpeg',
          data: new Uint8Array([1, 2]),
        },
      }),
      sink,
      vi.fn(),
    );
    expect(sink.onDesktopFrame).toHaveBeenCalledTimes(1);
    expect(sink.onDesktopFrame.mock.calls[0]![0].sequence).toBe(7);
    expect(sink.onTerminalFrame).not.toHaveBeenCalled();
    expect(sink.onFileFrame).not.toHaveBeenCalled();
  });

  it('routes a terminal frame to onTerminalFrame only', () => {
    const sink = makeSink();
    frameDispatch(
      toBuffer({ type: FRAME_TERMINAL, frame: { data: new Uint8Array([104, 105]) } }),
      sink,
      vi.fn(),
    );
    expect(Array.from(sink.onTerminalFrame.mock.calls[0]![0].data)).toEqual([104, 105]);
    expect(sink.onDesktopFrame).not.toHaveBeenCalled();
    expect(sink.onFileFrame).not.toHaveBeenCalled();
  });

  it('routes a file frame to onFileFrame only', () => {
    const sink = makeSink();
    frameDispatch(
      toBuffer({
        type: FRAME_FILE,
        frame: { offset: 5, total_size: 9, data: new Uint8Array([9]) },
      }),
      sink,
      vi.fn(),
    );
    expect(sink.onFileFrame.mock.calls[0]![0].offset).toBe(5);
    expect(sink.onDesktopFrame).not.toHaveBeenCalled();
    expect(sink.onTerminalFrame).not.toHaveBeenCalled();
  });

  it('answers a ping through sendPong', () => {
    const sink = makeSink();
    const sendPong = vi.fn();
    frameDispatch(toBuffer({ type: FRAME_PING }), sink, sendPong);
    expect(sendPong).toHaveBeenCalledTimes(1);
  });

  it('ignores a pong', () => {
    const sink = makeSink();
    const sendPong = vi.fn();
    frameDispatch(toBuffer({ type: FRAME_PONG }), sink, sendPong);
    expect(sendPong).not.toHaveBeenCalled();
    expect(sink.onError).not.toHaveBeenCalled();
  });

  it('reports a malformed frame through onError', () => {
    const sink = makeSink();
    frameDispatch(new Uint8Array([0xff, 0, 0, 0, 0]).buffer, sink, vi.fn());
    expect(sink.onError).toHaveBeenCalledTimes(1);
    expect(sink.onError.mock.calls[0]![0]).toBeInstanceOf(Error);
  });

  it('reports a throwing sendPong through onError', () => {
    const sink = makeSink();
    const sendPong = vi.fn(() => {
      throw new Error('not connected');
    });
    frameDispatch(toBuffer({ type: FRAME_PING }), sink, sendPong);
    expect(sink.onError).toHaveBeenCalledWith(expect.objectContaining({ message: 'not connected' }));
  });
});
