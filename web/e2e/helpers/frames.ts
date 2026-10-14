/// <reference types="node" />
import { decode, encode } from "@msgpack/msgpack";

/** Control frame type byte, as in web/src/lib/protocol/types.ts. */
export const FRAME_CONTROL = 0x01;

/** A control message on the wire: `[type=0x01][4-byte big-endian length][msgpack]`. */
export function encodeControlFrame(message: object): Buffer {
  const payload = encode(message);
  const buf = Buffer.alloc(5 + payload.length);
  buf[0] = FRAME_CONTROL;
  buf.writeUInt32BE(payload.length, 1);
  buf.set(payload, 5);
  return buf;
}

/** The control message a wire frame carries, or null for any other frame. */
export function decodeControlFrame(data: Buffer): { type: string; [key: string]: unknown } | null {
  if (data.length < 5 || data[0] !== FRAME_CONTROL) return null;
  return decode(data.subarray(5, 5 + data.readUInt32BE(1))) as { type: string; [key: string]: unknown };
}
