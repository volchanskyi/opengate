import { describe, it, expect } from 'vitest';
import {
  FRAME_CONTROL,
  FRAME_DESKTOP,
  FRAME_TERMINAL,
  FRAME_FILE,
  FRAME_PING,
  FRAME_PONG,
  MAX_FRAME_SIZE,
} from './types';
import type { ControlMessage, DesktopFrame, TerminalFrame, FileFrame, Frame } from './types';

describe('frame type constants', () => {
  it('has correct byte values matching Rust/Go', () => {
    expect(FRAME_CONTROL).toBe(0x01);
    expect(FRAME_DESKTOP).toBe(0x02);
    expect(FRAME_TERMINAL).toBe(0x03);
    expect(FRAME_FILE).toBe(0x04);
    expect(FRAME_PING).toBe(0x05);
    expect(FRAME_PONG).toBe(0x06);
  });

  it('MAX_FRAME_SIZE is 16 MiB', () => {
    expect(MAX_FRAME_SIZE).toBe(16 * 1024 * 1024);
  });
});

describe('type construction', () => {
  it('creates a ControlMessage with RelayReady variant', () => {
    const msg: ControlMessage = { type: 'RelayReady' };
    expect(msg.type).toBe('RelayReady');
  });

  it('creates a ControlMessage with all fields', () => {
    const msg: ControlMessage = {
      type: 'SessionRequest',
      token: 'abc',
      relay_url: 'ws://host/relay',
      permissions: { desktop: true, terminal: false, file_read: true, file_write: false, input: true },
    };
    expect(msg.type).toBe('SessionRequest');
    expect(msg.permissions.desktop).toBe(true);
  });

  it('creates a DesktopFrame', () => {
    const frame: DesktopFrame = {
      sequence: 42,
      x: 0,
      y: 0,
      width: 1920,
      height: 1080,
      encoding: 'Raw',
      data: new Uint8Array([1, 2, 3]),
    };
    expect(frame.width).toBe(1920);
    expect(frame.data).toBeInstanceOf(Uint8Array);
  });

  it('creates a TerminalFrame', () => {
    const frame: TerminalFrame = { data: new Uint8Array([0x48, 0x65, 0x6c, 0x6c, 0x6f]) };
    expect(frame.data.length).toBe(5);
  });

  it('creates a FileFrame', () => {
    const frame: FileFrame = {
      offset: 0,
      total_size: 1024,
      data: new Uint8Array(256),
    };
    expect(frame.total_size).toBe(1024);
  });

  it('creates discriminated Frame union for Ping', () => {
    const frame: Frame = { type: FRAME_PING };
    expect(frame.type).toBe(FRAME_PING);
  });

  it('creates discriminated Frame union for Control', () => {
    const frame: Frame = {
      type: FRAME_CONTROL,
      message: { type: 'RelayReady' },
    };
    expect(frame.type).toBe(FRAME_CONTROL);
    if (frame.type === FRAME_CONTROL) {
      expect(frame.message.type).toBe('RelayReady');
    }
  });

  it('creates input control messages', () => {
    const move: ControlMessage = { type: 'MouseMove', x: 100, y: 200 };
    expect(move.type).toBe('MouseMove');

    const click: ControlMessage = { type: 'MouseClick', button: 'Left', pressed: true, x: 50, y: 60 };
    expect(click.type).toBe('MouseClick');

    const key: ControlMessage = { type: 'KeyPress', key: 'KeyA', pressed: true };
    expect(key.type).toBe('KeyPress');
  });

  it('creates chat control message', () => {
    const chat: ControlMessage = { type: 'ChatMessage', text: 'hello', sender: 'browser' };
    expect(chat.type).toBe('ChatMessage');
  });
});
