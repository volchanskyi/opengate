import { encode, decode } from '@msgpack/msgpack';
import type { Frame, ControlMessage, DesktopFrame, TerminalFrame, FileFrame } from './types';
import {
  FRAME_CONTROL,
  FRAME_DESKTOP,
  FRAME_TERMINAL,
  FRAME_FILE,
  FRAME_PING,
  FRAME_PONG,
  MAX_FRAME_SIZE,
} from './types';

/** Encode a Frame to wire format: [1-byte type][4-byte BE length][msgpack payload]. */
export function encodeFrame(frame: Frame): Uint8Array<ArrayBuffer> {
  if (frame.type === FRAME_PING) return new Uint8Array([FRAME_PING]);
  if (frame.type === FRAME_PONG) return new Uint8Array([FRAME_PONG]);

  let payload: Uint8Array;
  switch (frame.type) {
    case FRAME_CONTROL:
      payload = encode(frame.message);
      break;
    case FRAME_DESKTOP:
      payload = encode(frame.frame);
      break;
    case FRAME_TERMINAL:
      payload = encode(frame.frame);
      break;
    case FRAME_FILE:
      payload = encode(frame.frame);
      break;
    default:
      throw new Error(`unexpected frame type: ${(frame as { type: number }).type}`);
  }

  if (payload.length > MAX_FRAME_SIZE) {
    throw new Error(`frame payload too large: ${payload.length} > ${MAX_FRAME_SIZE}`);
  }

  const buf = new Uint8Array(5 + payload.length);
  buf[0] = frame.type;
  const view = new DataView(buf.buffer);
  view.setUint32(1, payload.length, false); // big-endian
  buf.set(payload, 5);
  return buf;
}

/** Decode a Frame from wire format. Returns the frame and number of bytes consumed. */
export function decodeFrame(data: Uint8Array): { frame: Frame; bytesConsumed: number } {
  if (data.length === 0) {
    throw new Error('incomplete frame: empty data');
  }

  const typeByte = data[0] as number;

  if (typeByte === FRAME_PING) return { frame: { type: FRAME_PING }, bytesConsumed: 1 };
  if (typeByte === FRAME_PONG) return { frame: { type: FRAME_PONG }, bytesConsumed: 1 };

  if (
    typeByte !== FRAME_CONTROL &&
    typeByte !== FRAME_DESKTOP &&
    typeByte !== FRAME_TERMINAL &&
    typeByte !== FRAME_FILE
  ) {
    throw new Error(`unknown frame type: 0x${typeByte.toString(16).padStart(2, '0')}`);
  }

  if (data.length < 5) {
    throw new Error(`incomplete frame: need 5 bytes, have ${data.length}`);
  }

  const view = new DataView(data.buffer, data.byteOffset, data.byteLength);
  const length = view.getUint32(1, false); // big-endian

  if (length > MAX_FRAME_SIZE) {
    throw new Error(`frame too large: ${length} > ${MAX_FRAME_SIZE}`);
  }

  const total = 5 + length;
  if (data.length < total) {
    throw new Error(`incomplete frame: need ${total} bytes, have ${data.length}`);
  }

  const payload = data.subarray(5, total);
  const decoded = decode(payload);

  let frame: Frame;
  switch (typeByte) {
    case FRAME_CONTROL:
      frame = { type: FRAME_CONTROL, message: decoded as ControlMessage };
      break;
    case FRAME_DESKTOP:
      frame = { type: FRAME_DESKTOP, frame: decoded as DesktopFrame };
      break;
    case FRAME_TERMINAL:
      frame = { type: FRAME_TERMINAL, frame: decoded as TerminalFrame };
      break;
    case FRAME_FILE:
      frame = { type: FRAME_FILE, frame: decoded as FileFrame };
      break;
    default:
      throw new Error(`unexpected frame type: 0x${(typeByte as number).toString(16).padStart(2, '0')}`);
  }

  return { frame, bytesConsumed: total };
}
