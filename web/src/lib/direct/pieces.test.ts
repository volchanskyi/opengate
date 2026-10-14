import { describe, expect, it } from 'vitest';
import { encodeFrame } from '../protocol/codec';
import { FRAME_FILE, FRAME_PING, FRAME_PONG, FRAME_TERMINAL } from '../protocol/types';
import { MAX_LINK_FRAME, MAX_PIECE, Reassembler, ReassemblyError, split } from './pieces';

function terminal(len: number): Uint8Array {
  return encodeFrame({ type: FRAME_TERMINAL, frame: { data: new Uint8Array(len).fill(7) } });
}

function concat(parts: Uint8Array[]): Uint8Array {
  const out = new Uint8Array(parts.reduce((n, p) => n + p.length, 0));
  let at = 0;
  for (const part of parts) {
    out.set(part, at);
    at += part.length;
  }
  return out;
}

function header(kind: number, payload: number): Uint8Array {
  const bytes = new Uint8Array(5);
  bytes[0] = kind;
  new DataView(bytes.buffer).setUint32(1, payload, false);
  return bytes;
}

describe('split', () => {
  it('cuts a frame into pieces no larger than the limit', () => {
    const frame = terminal(40_000);
    const pieces = split(frame);
    expect(pieces).toHaveLength(3);
    expect(pieces.every((p) => p.length <= MAX_PIECE)).toBe(true);
    expect(concat(pieces)).toEqual(frame);
  });
});

describe('Reassembler', () => {
  it('gives back the frames the pieces were cut from', () => {
    const first = terminal(40_000);
    const second = terminal(10);
    const r = new Reassembler();
    const out = [...split(first), ...split(second)].flatMap((piece) => r.push(piece));
    expect(out).toEqual([first, second]);
    expect(r.pending()).toBe(0);
  });

  it('completes a frame only with its last piece', () => {
    const frame = terminal(40_000);
    const [a, b, c] = split(frame) as [Uint8Array, Uint8Array, Uint8Array];
    const r = new Reassembler();
    expect(r.push(a)).toEqual([]);
    expect(r.push(b.subarray(0, 1))).toEqual([]);
    expect(r.push(b.subarray(1))).toEqual([]);
    expect(r.push(c)).toEqual([frame]);
  });

  it('reads ping and pong as whole in one byte', () => {
    const r = new Reassembler();
    expect(r.push(new Uint8Array([FRAME_PING, FRAME_PONG]))).toEqual([
      new Uint8Array([FRAME_PING]),
      new Uint8Array([FRAME_PONG]),
    ]);
  });

  it('refuses a frame past the relay limit from its header, and allows the limit itself', () => {
    expect(() => new Reassembler().push(header(FRAME_FILE, MAX_LINK_FRAME - 5 + 1))).toThrow(ReassemblyError);
    expect(new Reassembler().push(header(FRAME_FILE, MAX_LINK_FRAME - 5))).toEqual([]);
  });

  it('refuses an unknown type byte', () => {
    expect(() => new Reassembler().push(new Uint8Array([0x7f, 0, 0]))).toThrow(/unknown frame type 0x7f/);
  });

  it('discards a frame held in part on reset', () => {
    const frame = terminal(40_000);
    const r = new Reassembler();
    r.push(frame.subarray(0, 100));
    expect(r.pending()).toBe(100);
    r.reset();
    expect(r.pending()).toBe(0);
    expect(r.push(terminal(3))).toEqual([terminal(3)]);
  });
});
