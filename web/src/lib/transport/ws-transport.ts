import { encodeFrame, decodeFrame } from '../protocol/codec';
import {
  FRAME_CONTROL,
  FRAME_DESKTOP,
  FRAME_TERMINAL,
  FRAME_FILE,
  FRAME_PING,
  FRAME_PONG,
} from '../protocol/types';
import type { ControlMessage, DesktopFrame, TerminalFrame, FileFrame } from '../protocol/types';

/** WebSocket connection lifecycle state. */
export type ConnectionState = 'disconnected' | 'connecting' | 'connected' | 'error';

/** Event callbacks for frame dispatch. */
export interface TransportEvents {
  onStateChange: (state: ConnectionState) => void;
  onControlMessage: (msg: ControlMessage) => void;
  onDesktopFrame: (frame: DesktopFrame) => void;
  onTerminalFrame: (frame: TerminalFrame) => void;
  onFileFrame: (frame: FileFrame) => void;
  onError: (error: Error) => void;
}

/** WebSocket transport that speaks the binary frame protocol. */
export class WSTransport {
  private ws: WebSocket | null = null;
  private _state: ConnectionState = 'disconnected';
  private readonly events: TransportEvents;

  constructor(events: TransportEvents) {
    this.events = events;
  }

  get state(): ConnectionState {
    return this._state;
  }

  /** Connect to relay WebSocket with JWT auth via query param. */
  connect(relayUrl: string, authToken: string): void {
    if (this.ws) {
      this.ws.close();
    }

    this.setState('connecting');

    const separator = relayUrl.includes('?') ? '&' : '?';
    const url = `${relayUrl}${separator}side=browser&auth=${encodeURIComponent(authToken)}`;

    const ws = new WebSocket(url);
    ws.binaryType = 'arraybuffer';
    this.ws = ws;

    ws.onopen = () => {
      this.setState('connected');
    };

    ws.onmessage = (event: MessageEvent) => {
      this.handleMessage(event.data as ArrayBuffer);
    };

    ws.onerror = () => {
      this.events.onError(new Error('WebSocket connection error'));
      this.setState('error');
    };

    ws.onclose = () => {
      if (this._state !== 'error') {
        this.setState('disconnected');
      }
      this.ws = null;
    };
  }

  /** Send a control message. */
  sendControl(msg: ControlMessage): void {
    this.sendRaw(encodeFrame({ type: FRAME_CONTROL, message: msg }));
  }

  /** Send terminal data. */
  sendTerminalData(data: Uint8Array): void {
    this.sendRaw(encodeFrame({ type: FRAME_TERMINAL, frame: { data } }));
  }

  /** Send a file frame. */
  sendFileFrame(frame: FileFrame): void {
    this.sendRaw(encodeFrame({ type: FRAME_FILE, frame }));
  }

  /** Close the connection. */
  disconnect(): void {
    if (this.ws) {
      this.ws.close();
      this.ws = null;
    }
    this.setState('disconnected');
  }

  private setState(state: ConnectionState): void {
    this._state = state;
    this.events.onStateChange(state);
  }

  private sendRaw(data: Uint8Array): void {
    if (this.ws?.readyState !== WebSocket.OPEN) {
      throw new Error('WebSocket not connected');
    }
    this.ws.send(data);
  }

  private handleMessage(data: ArrayBuffer): void {
    try {
      const { frame } = decodeFrame(new Uint8Array(data));
      switch (frame.type) {
        case FRAME_PING:
          // Auto-respond with Pong
          this.sendRaw(encodeFrame({ type: FRAME_PONG }));
          break;
        case FRAME_PONG:
          // Ignore Pong
          break;
        case FRAME_CONTROL:
          this.events.onControlMessage(frame.message);
          break;
        case FRAME_DESKTOP:
          this.events.onDesktopFrame(frame.frame);
          break;
        case FRAME_TERMINAL:
          this.events.onTerminalFrame(frame.frame);
          break;
        case FRAME_FILE:
          this.events.onFileFrame(frame.frame);
          break;
      }
    } catch (err) {
      this.events.onError(err instanceof Error ? err : new Error(String(err)));
    }
  }
}
