import { describe, it, expect } from 'vitest';
import fc from 'fast-check';
import { encodeFrame, decodeFrame } from './codec';
import {
  FRAME_CONTROL,
  FRAME_DESKTOP,
  FRAME_TERMINAL,
  FRAME_FILE,
  FRAME_PING,
  FRAME_PONG,
} from './types';
import type { Frame, FrameEncoding } from './types';

// Pinned runs + seed so any counterexample reproduces deterministically in the
// gauntlet (tests-determinism.md). No .skip / .only.
const RUNS = { numRuns: 500, seed: 0x0ac17a7e } as const;

describe('decodeFrame robustness properties', () => {
  it('never throws a non-Error on arbitrary bytes (controlled failure only)', () => {
    fc.assert(
      fc.property(fc.uint8Array({ maxLength: 256 }), (bytes) => {
        try {
          const { frame, bytesConsumed } = decodeFrame(bytes);
          // A successful decode must report a sane consumed-byte count.
          expect(bytesConsumed).toBeGreaterThanOrEqual(1);
          expect(bytesConsumed).toBeLessThanOrEqual(bytes.length);
          expect(typeof frame.type).toBe('number');
        } catch (err) {
          // The only acceptable failure mode is a thrown Error.
          expect(err).toBeInstanceOf(Error);
        }
      }),
      RUNS,
    );
  });

  it('decodes correctly when the frame is embedded in a larger byte-offset buffer', () => {
    // Guards the DataView byteOffset handling: a subarray view must decode the
    // same as a standalone buffer.
    fc.assert(
      fc.property(
        fc.uint8Array({ maxLength: 32 }),
        fc.nat({ max: 8 }),
        (payload, pad) => {
          const encoded = encodeFrame({ type: FRAME_TERMINAL, frame: { data: payload } });
          const padded = new Uint8Array(pad + encoded.length);
          padded.set(encoded, pad);
          const view = padded.subarray(pad);
          const { frame, bytesConsumed } = decodeFrame(view);
          expect(frame.type).toBe(FRAME_TERMINAL);
          expect(bytesConsumed).toBe(encoded.length);
        },
      ),
      RUNS,
    );
  });
});

const encodingArb = fc.constantFrom<FrameEncoding>(
  'Raw',
  'Zlib',
  'Zstd',
  'H264Idr',
  'H264Delta',
  'Jpeg',
);

const frameArb: fc.Arbitrary<Frame> = fc.oneof(
  fc.constant<Frame>({ type: FRAME_PING }),
  fc.constant<Frame>({ type: FRAME_PONG }),
  fc.uint8Array({ maxLength: 64 }).map<Frame>((data) => ({
    type: FRAME_TERMINAL,
    frame: { data },
  })),
  fc
    .record({ offset: fc.nat(), total_size: fc.nat(), data: fc.uint8Array({ maxLength: 64 }) })
    .map<Frame>((frame) => ({ type: FRAME_FILE, frame })),
  fc
    .record({
      sequence: fc.nat(),
      x: fc.nat(),
      y: fc.nat(),
      width: fc.nat(),
      height: fc.nat(),
      encoding: encodingArb,
      data: fc.uint8Array({ maxLength: 64 }),
    })
    .map<Frame>((frame) => ({ type: FRAME_DESKTOP, frame })),
  fc.constant<Frame>({ type: FRAME_CONTROL, message: { type: 'RelayReady' } }),
  fc
    .record({ text: fc.string(), sender: fc.string() })
    .map<Frame>(({ text, sender }) => ({
      type: FRAME_CONTROL,
      message: { type: 'ChatMessage', text, sender },
    })),
);

describe('encodeFrame/decodeFrame roundtrip', () => {
  it('decode(encode(f)) === f and consumes exactly the encoded bytes', () => {
    fc.assert(
      fc.property(frameArb, (original) => {
        const encoded = encodeFrame(original);
        const { frame, bytesConsumed } = decodeFrame(encoded);
        expect(bytesConsumed).toBe(encoded.length);
        expect(frame).toEqual(original);
      }),
      RUNS,
    );
  });
});
