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
