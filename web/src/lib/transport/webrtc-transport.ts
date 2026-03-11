import { encodeFrame, decodeFrame } from '../protocol/codec';
import {
  FRAME_CONTROL,
  FRAME_DESKTOP,
  FRAME_TERMINAL,
  FRAME_FILE,
  FRAME_PING,
  FRAME_PONG,
} from '../protocol/types';
import type { ControlMessage, FileFrame } from '../protocol/types';
import type { TransportEvents } from './ws-transport';

/** WebRTC upgrade lifecycle state. */
export type WebRTCState = 'idle' | 'offering' | 'answering' | 'connecting' | 'connected' | 'failed';

/** ICE server configuration for RTCPeerConnection. */
export interface RTCConfig {
  iceServers: RTCIceServer[];
}

/** WebRTC transport using data channels for frame delivery. */
export class WebRTCTransport {
  private pc: RTCPeerConnection | null = null;
  private controlChannel: RTCDataChannel | null = null;
  private desktopChannel: RTCDataChannel | null = null;
  private bulkChannel: RTCDataChannel | null = null;
  private _state: WebRTCState = 'idle';
  private events: TransportEvents;
  private pendingCandidates: Array<{ candidate: string; mid: string }> = [];
  private remoteDescSet = false;

  /** Callback fired when a local ICE candidate is gathered. */
  onLocalIceCandidate: ((candidate: string, mid: string) => void) | null = null;

  constructor(events: TransportEvents) {
    this.events = events;
  }

  get state(): WebRTCState {
    return this._state;
  }

  /** Create an SDP offer to initiate WebRTC upgrade. Returns the offer string. */
  async createOffer(config: RTCConfig): Promise<string> {
    this.pc = new RTCPeerConnection({ iceServers: config.iceServers });
    this.setupPeerConnection();

    // Create data channels (browser is the offerer)
    this.controlChannel = this.pc.createDataChannel('control', {
      ordered: true,
      id: 0,
    });
    this.desktopChannel = this.pc.createDataChannel('desktop', {
      ordered: false,
      maxRetransmits: 0,
      id: 1,
    });
    this.bulkChannel = this.pc.createDataChannel('bulk', {
      ordered: true,
      id: 2,
    });

    this.setupDataChannel(this.controlChannel);
    this.setupDataChannel(this.desktopChannel);
    this.setupDataChannel(this.bulkChannel);

    this._state = 'offering';

    const offer = await this.pc.createOffer();
    await this.pc.setLocalDescription(offer);
    return offer.sdp ?? '';
  }

  /** Handle the remote SDP answer from the agent. */
  async handleAnswer(sdpAnswer: string): Promise<void> {
    if (!this.pc) return;

    await this.pc.setRemoteDescription({
      type: 'answer',
      sdp: sdpAnswer,
    });
    this.remoteDescSet = true;
    this._state = 'answering';

    // Flush buffered ICE candidates
    for (const { candidate, mid } of this.pendingCandidates) {
      await this.pc.addIceCandidate({ candidate, sdpMid: mid });
    }
    this.pendingCandidates = [];
  }

  /** Add a remote ICE candidate from the agent. */
  async addIceCandidate(candidate: string, mid: string): Promise<void> {
    if (!this.pc) return;

    if (!this.remoteDescSet) {
      // Buffer until remote description is set
      this.pendingCandidates.push({ candidate, mid });
      return;
    }

    await this.pc.addIceCandidate({ candidate, sdpMid: mid });
  }

  /** Send a control message via the control data channel. */
  sendControl(msg: ControlMessage): void {
    this.sendOnChannel(this.controlChannel, encodeFrame({ type: FRAME_CONTROL, message: msg }));
  }

  /** Send terminal data via the bulk data channel. */
  sendTerminalData(data: Uint8Array): void {
    this.sendOnChannel(this.bulkChannel, encodeFrame({ type: FRAME_TERMINAL, frame: { data } }));
  }

  /** Send a file frame via the bulk data channel. */
  sendFileFrame(frame: FileFrame): void {
    this.sendOnChannel(this.bulkChannel, encodeFrame({ type: FRAME_FILE, frame }));
  }

  /** Close the peer connection and all channels. */
  close(): void {
    this.controlChannel?.close();
    this.desktopChannel?.close();
    this.bulkChannel?.close();
    this.pc?.close();
    this.pc = null;
    this.controlChannel = null;
    this.desktopChannel = null;
    this.bulkChannel = null;
    this._state = 'idle';
    this.pendingCandidates = [];
    this.remoteDescSet = false;
  }

  private setupPeerConnection(): void {
    if (!this.pc) return;

    this.pc.onicecandidate = (event) => {
      if (event.candidate && this.onLocalIceCandidate) {
        this.onLocalIceCandidate(
          event.candidate.candidate,
          event.candidate.sdpMid ?? '',
        );
      }
    };

    this.pc.oniceconnectionstatechange = () => {
      if (!this.pc) return;
      switch (this.pc.iceConnectionState) {
        case 'connected':
        case 'completed':
          this._state = 'connected';
          break;
        case 'failed':
        case 'disconnected':
          this._state = 'failed';
          this.events.onError(new Error(`ICE ${this.pc.iceConnectionState}`));
          break;
        case 'closed':
          this._state = 'idle';
          break;
      }
    };
  }

  private setupDataChannel(channel: RTCDataChannel): void {
    channel.binaryType = 'arraybuffer';

    channel.onmessage = (event: MessageEvent) => {
      this.handleChannelMessage(event.data as ArrayBuffer);
    };

    channel.onerror = () => {
      this.events.onError(new Error(`DataChannel error: ${channel.label}`));
    };
  }

  private handleChannelMessage(data: ArrayBuffer): void {
    try {
      const { frame } = decodeFrame(new Uint8Array(data));
      switch (frame.type) {
        case FRAME_PING:
          // Auto-respond with Pong on control channel
          this.sendOnChannel(this.controlChannel, encodeFrame({ type: FRAME_PONG }));
          break;
        case FRAME_PONG:
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

  private sendOnChannel(channel: RTCDataChannel | null, data: Uint8Array): void {
    if (!channel || channel.readyState !== 'open') {
      throw new Error(`DataChannel ${channel?.label ?? 'unknown'} not open`);
    }
    channel.send(data as Uint8Array<ArrayBuffer>);
  }
}
