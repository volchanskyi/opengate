import { create } from 'zustand';
import { api } from '../../../lib/api';
import { fireAndForget } from '../../../lib/fire-and-forget';
import {
  SessionChannel,
  type ConnectionState,
  type ReportedOutcome,
  type SessionChannelEvents,
  type SessionPath,
} from '../../../lib/transport/session-channel';
import type { ControlMessage, DesktopFrame, TerminalFrame, FileFrame } from '../../../lib/protocol/types';

interface ConnectionStore {
  state: ConnectionState;
  token: string | null;
  error: string | null;
  /** The session's sending point; every feature sends through it. */
  transport: SessionChannel | null;
  /** Direct or through the server, with the reason in plain words. */
  path: SessionPath;
  /** Why the relay closed, as the server said it: "Opened elsewhere", "Ended by <name>". */
  closedReason: string | null;

  connect: (token: string, relayUrl: string, helpers: string[]) => void;
  disconnect: () => void;

  /** Every feature listening for control messages; each takes the types it handles. */
  controlListeners: ReadonlySet<ControlListener>;
  onDesktopFrame: ((frame: DesktopFrame) => void) | null;
  onTerminalFrame: ((frame: TerminalFrame) => void) | null;
  onFileFrame: ((frame: FileFrame) => void) | null;

  /** Adds a control-message listener and returns what removes it. */
  listenControl: (cb: ControlListener) => () => void;
  setOnDesktopFrame: (cb: ((frame: DesktopFrame) => void) | null) => void;
  setOnTerminalFrame: (cb: ((frame: TerminalFrame) => void) | null) => void;
  setOnFileFrame: (cb: ((frame: FileFrame) => void) | null) => void;
}

type ControlListener = (msg: ControlMessage) => void;

const THROUGH_SERVER: SessionPath = { direct: false };

/** Counts an outcome into the fleet-wide counters; a failed report changes nothing on screen. */
function report(token: string, outcome: ReportedOutcome): void {
  fireAndForget(api.POST('/api/v1/sessions/{token}/outcome', { params: { path: { token } }, body: { outcome } }));
}

export const useConnectionStore = create<ConnectionStore>((set, get) => ({
  state: 'disconnected',
  token: null,
  error: null,
  transport: null,
  path: THROUGH_SERVER,
  closedReason: null,

  controlListeners: new Set(),
  onDesktopFrame: null,
  onTerminalFrame: null,
  onFileFrame: null,

  listenControl: (cb) => {
    set({ controlListeners: new Set(get().controlListeners).add(cb) });
    return () => {
      const rest = new Set(get().controlListeners);
      rest.delete(cb);
      set({ controlListeners: rest });
    };
  },
  setOnDesktopFrame: (cb) => set({ onDesktopFrame: cb }),
  setOnTerminalFrame: (cb) => set({ onTerminalFrame: cb }),
  setOnFileFrame: (cb) => set({ onFileFrame: cb }),

  connect: (token, relayUrl, helpers) => {
    get().transport?.disconnect();

    const events: SessionChannelEvents = {
      onStateChange: (state) => set({ state }),
      onControlMessage: (msg) => {
        for (const listener of get().controlListeners) listener(msg);
      },
      onDesktopFrame: (frame) => get().onDesktopFrame?.(frame),
      onTerminalFrame: (frame) => get().onTerminalFrame?.(frame),
      onFileFrame: (frame) => get().onFileFrame?.(frame),
      onError: (err) => set({ error: err.message }),
      onPath: (path) => set({ path }),
      onOutcome: (outcome) => report(token, outcome),
      onClosed: (reason) => set({ closedReason: reason }),
    };

    const transport = new SessionChannel(events, helpers);
    set({ transport, token, error: null, closedReason: null, path: THROUGH_SERVER });
    transport.connect(relayUrl);
  },

  disconnect: () => {
    get().transport?.disconnect();
    set({
      transport: null,
      token: null,
      state: 'disconnected',
      error: null,
      path: THROUGH_SERVER,
      closedReason: null,
      controlListeners: new Set(),
      onDesktopFrame: null,
      onTerminalFrame: null,
      onFileFrame: null,
    });
  },
}));
