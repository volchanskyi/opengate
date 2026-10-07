import { decodeFrame } from '../protocol/codec';
import {
  FRAME_CONTROL,
  FRAME_DESKTOP,
  FRAME_TERMINAL,
  FRAME_FILE,
  FRAME_PING,
  FRAME_PONG,
} from '../protocol/types';
import type { ControlMessage, DesktopFrame, TerminalFrame, FileFrame } from '../protocol/types';

/** The frame callbacks a transport hands to {@link frameDispatch}. */
export interface FrameEvents {
  onControlMessage: (msg: ControlMessage) => void;
  onDesktopFrame: (frame: DesktopFrame) => void;
  onTerminalFrame: (frame: TerminalFrame) => void;
  onFileFrame: (frame: FileFrame) => void;
  onError: (error: Error) => void;
}

/** Decodes one inbound frame and routes it to its callback; a failure goes to onError. */
export function frameDispatch(data: ArrayBuffer, events: FrameEvents, sendPong: () => void): void {
  try {
    const { frame } = decodeFrame(new Uint8Array(data));
    switch (frame.type) {
      case FRAME_PING:
        sendPong();
        break;
      case FRAME_PONG:
        break;
      case FRAME_CONTROL:
        events.onControlMessage(frame.message);
        break;
      case FRAME_DESKTOP:
        events.onDesktopFrame(frame.frame);
        break;
      case FRAME_TERMINAL:
        events.onTerminalFrame(frame.frame);
        break;
      case FRAME_FILE:
        events.onFileFrame(frame.frame);
        break;
    }
  } catch (err) {
    events.onError(err instanceof Error ? err : new Error(String(err)));
  }
}
