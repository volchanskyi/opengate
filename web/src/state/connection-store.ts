import { create } from 'zustand';
import { WSTransport, type ConnectionState, type TransportEvents } from '../lib/transport/ws-transport';
import { WebRTCTransport, type RTCConfig } from '../lib/transport/webrtc-transport';
import type { ControlMessage, DesktopFrame, TerminalFrame, FileFrame } from '../lib/protocol/types';

/** Signaling state for the relay-to-WebRTC upgrade. */
export type SignalingState = 'relay-only' | 'upgrading' | 'webrtc' | 'fallback';

interface ConnectionStore {
  state: ConnectionState;
  token: string | null;
  error: string | null;
  transport: WSTransport | null;

  // WebRTC upgrade state
  webrtcTransport: WebRTCTransport | null;
  signalingState: SignalingState;
  iceServers: RTCIceServer[];

  connect: (token: string, relayUrl: string, authToken: string, iceServers?: RTCIceServer[]) => void;
  disconnect: () => void;
  initiateWebRTCUpgrade: () => void;

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

/** Handle signaling control messages arriving via the relay. */
function handleSignalingMessage(msg: ControlMessage, get: () => ConnectionStore, set: (state: Partial<ConnectionStore>) => void): boolean {
  const { webrtcTransport, transport } = get();

  switch (msg.type) {
    case 'SwitchToWebRTC': {
      // Agent's SDP answer
      if (webrtcTransport && get().signalingState === 'upgrading') {
        webrtcTransport.handleAnswer(msg.sdp_offer).catch((err) => {
          console.warn('[webrtc] handleAnswer failed:', err);
          set({ signalingState: 'fallback' });
        });
      }
      return true;
    }
    case 'IceCandidate': {
      if (webrtcTransport) {
        webrtcTransport.addIceCandidate(msg.candidate, msg.mid).catch((err) => {
          console.warn('[webrtc] addIceCandidate failed:', err);
        });
      }
      return true;
    }
    case 'SwitchAck': {
      if (webrtcTransport && get().signalingState === 'upgrading') {
        // Agent acknowledged — send our own ack and switch
        transport?.sendControl({ type: 'SwitchAck' });
        set({ signalingState: 'webrtc' });
      }
      return true;
    }
    default:
      return false;
  }
}

export const useConnectionStore = create<ConnectionStore>((set, get) => ({
  state: 'disconnected',
  token: null,
  error: null,
  transport: null,

  webrtcTransport: null,
  signalingState: 'relay-only',
  iceServers: [],

  onControlMessage: null,
  onDesktopFrame: null,
  onTerminalFrame: null,
  onFileFrame: null,

  setOnControlMessage: (cb) => set({ onControlMessage: cb }),
  setOnDesktopFrame: (cb) => set({ onDesktopFrame: cb }),
  setOnTerminalFrame: (cb) => set({ onTerminalFrame: cb }),
  setOnFileFrame: (cb) => set({ onFileFrame: cb }),

  connect: (token, relayUrl, authToken, iceServers) => {
    const current = get();
    if (current.transport) {
      current.transport.disconnect();
    }
    if (current.webrtcTransport) {
      current.webrtcTransport.close();
    }

    const events: TransportEvents = {
      onStateChange: (state) => set({ state }),
      onControlMessage: (msg) => {
        // Intercept signaling messages
        if (!handleSignalingMessage(msg, get, set)) {
          get().onControlMessage?.(msg);
        }
      },
      onDesktopFrame: (frame) => get().onDesktopFrame?.(frame),
      onTerminalFrame: (frame) => get().onTerminalFrame?.(frame),
      onFileFrame: (frame) => get().onFileFrame?.(frame),
      onError: (err) => set({ error: err.message }),
    };

    const transport = new WSTransport(events);
    set({
      transport,
      token,
      error: null,
      signalingState: 'relay-only',
      webrtcTransport: null,
      iceServers: iceServers ?? [],
    });
    transport.connect(relayUrl, authToken);
  },

  initiateWebRTCUpgrade: () => {
    const { transport, iceServers, signalingState } = get();
    if (!transport || signalingState !== 'relay-only' || iceServers.length === 0) {
      return;
    }

    // Create WebRTC transport with same event dispatch
    const webrtcEvents: TransportEvents = {
      onStateChange: () => {},
      onControlMessage: (msg) => get().onControlMessage?.(msg),
      onDesktopFrame: (frame) => get().onDesktopFrame?.(frame),
      onTerminalFrame: (frame) => get().onTerminalFrame?.(frame),
      onFileFrame: (frame) => get().onFileFrame?.(frame),
      onError: (err) => {
        set({ signalingState: 'fallback', error: err.message });
      },
    };

    const webrtc = new WebRTCTransport(webrtcEvents);

    // Set up ICE candidate forwarding via relay
    webrtc.onLocalIceCandidate = (candidate, mid) => {
      transport.sendControl({ type: 'IceCandidate', candidate, mid });
    };

    set({ webrtcTransport: webrtc, signalingState: 'upgrading' });

    const config: RTCConfig = { iceServers };
    webrtc.createOffer(config).then((sdpOffer) => {
      transport.sendControl({ type: 'SwitchToWebRTC', sdp_offer: sdpOffer });
    }).catch((err) => {
      console.warn('[webrtc] createOffer failed:', err);
      set({ signalingState: 'fallback' });
    });
  },

  disconnect: () => {
    const { transport, webrtcTransport } = get();
    if (transport) {
      transport.disconnect();
    }
    if (webrtcTransport) {
      webrtcTransport.close();
    }
    set({
      transport: null,
      webrtcTransport: null,
      token: null,
      state: 'disconnected',
      signalingState: 'relay-only',
      iceServers: [],
      error: null,
      onControlMessage: null,
      onDesktopFrame: null,
      onTerminalFrame: null,
      onFileFrame: null,
    });
  },
}));
