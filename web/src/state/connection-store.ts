import { create } from 'zustand';
import { WSTransport, type ConnectionState, type TransportEvents } from '../lib/transport/ws-transport';
import type { ControlMessage, DesktopFrame, TerminalFrame, FileFrame } from '../lib/protocol/types';

interface ConnectionStore {
  state: ConnectionState;
  token: string | null;
  error: string | null;
  transport: WSTransport | null;

  connect: (token: string, relayUrl: string, authToken: string) => void;
  disconnect: () => void;

  // Frame event subscriptions
  onControlMessage: ((msg: ControlMessage) => void) | null;
  onDesktopFrame: ((frame: DesktopFrame) => void) | null;
  onTerminalFrame: ((frame: TerminalFrame) => void) | null;
  onFileFrame: ((frame: FileFrame) => void) | null;

  setOnControlMessage: (cb: ((msg: ControlMessage) => void) | null) => void;
  setOnDesktopFrame: (cb: ((frame: DesktopFrame) => void) | null) => void;
  setOnTerminalFrame: (cb: ((frame: TerminalFrame) => void) | null) => void;
  setOnFileFrame: (cb: ((frame: FileFrame) => void) | null) => void;
}

export const useConnectionStore = create<ConnectionStore>((set, get) => ({
  state: 'disconnected',
  token: null,
  error: null,
  transport: null,

  onControlMessage: null,
  onDesktopFrame: null,
  onTerminalFrame: null,
  onFileFrame: null,

  setOnControlMessage: (cb) => set({ onControlMessage: cb }),
  setOnDesktopFrame: (cb) => set({ onDesktopFrame: cb }),
  setOnTerminalFrame: (cb) => set({ onTerminalFrame: cb }),
  setOnFileFrame: (cb) => set({ onFileFrame: cb }),

  connect: (token, relayUrl, authToken) => {
    const current = get();
    if (current.transport) {
      current.transport.disconnect();
    }

    const events: TransportEvents = {
      onStateChange: (state) => set({ state }),
      onControlMessage: (msg) => get().onControlMessage?.(msg),
      onDesktopFrame: (frame) => get().onDesktopFrame?.(frame),
      onTerminalFrame: (frame) => get().onTerminalFrame?.(frame),
      onFileFrame: (frame) => get().onFileFrame?.(frame),
      onError: (err) => set({ error: err.message }),
    };

    const transport = new WSTransport(events);
    set({ transport, token, error: null });
    transport.connect(relayUrl, authToken);
  },

  disconnect: () => {
    const { transport } = get();
    if (transport) {
      transport.disconnect();
    }
    set({
      transport: null,
      token: null,
      state: 'disconnected',
      error: null,
      onControlMessage: null,
      onDesktopFrame: null,
      onTerminalFrame: null,
      onFileFrame: null,
    });
  },
}));
