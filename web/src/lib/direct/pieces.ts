import {
  FRAME_CONTROL,
  FRAME_DESKTOP,
  FRAME_FILE,
  FRAME_PING,
  FRAME_PONG,
  FRAME_TERMINAL,
} from '../protocol/types';

/** Largest piece sent on the link (RFC 8831 §6.6, for an association without interleaving). */
export const MAX_PIECE = 16 * 1024;

/** Largest frame the link reassembles: the relay's limit, so both paths carry the same frames. */
export const MAX_LINK_FRAME = 4 * 1024 * 1024;

// Bytes before a frame's payload: one type byte and a four-byte length.
const HEADER = 5;

/** A piece that cannot be put back into a frame. */
export class ReassemblyError extends Error {
  override name = 'ReassemblyError';
}

/** Cuts an encoded frame into pieces of at most {@link MAX_PIECE} bytes, sent back to back. */
export function split(frame: Uint8Array): Uint8Array[] {
  const pieces: Uint8Array[] = [];
  for (let at = 0; at < frame.length; at += MAX_PIECE) {
    pieces.push(frame.subarray(at, Math.min(at + MAX_PIECE, frame.length)));
  }
  return pieces;
}

/** Puts pieces back into whole frames, holding at most one frame in part. */
export class Reassembler {
  private buf = new Uint8Array(0);

  /** Appends a piece and returns the frames it completes; throws {@link ReassemblyError}. */
  push(piece: Uint8Array): Uint8Array[] {
    const joined = new Uint8Array(this.buf.length + piece.length);
    joined.set(this.buf);
    joined.set(piece, this.buf.length);
    this.buf = joined;

    const frames: Uint8Array[] = [];
    for (let len = wholeFrameLength(this.buf); len !== null; len = wholeFrameLength(this.buf)) {
      frames.push(this.buf.slice(0, len));
      this.buf = this.buf.slice(len);
    }
    return frames;
  }

  /** Discards a frame received in part, which its sender re-sends whole. */
  reset(): void {
    this.buf = new Uint8Array(0);
  }

  /** Bytes held of a frame not yet complete. */
  pending(): number {
    return this.buf.length;
  }
}

/** The length of the frame at the start of buf once it is all there, or null while it is not. */
function wholeFrameLength(buf: Uint8Array): number | null {
  if (buf.length === 0) return null;
  const kind = buf[0] as number;
  switch (kind) {
    case FRAME_PING:
    case FRAME_PONG:
      return 1;
    case FRAME_CONTROL:
    case FRAME_DESKTOP:
    case FRAME_TERMINAL:
    case FRAME_FILE: {
      if (buf.length < HEADER) return null;
      const total = HEADER + new DataView(buf.buffer, buf.byteOffset, buf.byteLength).getUint32(1, false);
      if (total > MAX_LINK_FRAME) {
        throw new ReassemblyError(`a link frame of ${total} bytes is past the ${MAX_LINK_FRAME}-byte limit`);
      }
      return buf.length >= total ? total : null;
    }
    default:
      throw new ReassemblyError(`unknown frame type 0x${kind.toString(16).padStart(2, '0')} on the link`);
  }
}
