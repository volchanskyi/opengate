import { describe, it, expect, vi } from 'vitest';
import { paintFrame, type CanvasContext } from './desktop-worker';
import type { DesktopFrame } from '../../lib/protocol/types';

function createMockContext(width = 1920, height = 1080): CanvasContext {
  return {
    width,
    height,
    putImageData: vi.fn(),
    createImageData: vi.fn((w: number, h: number) => ({
      width: w,
      height: h,
      data: new Uint8ClampedArray(w * h * 4),
      colorSpace: 'srgb' as const,
    })),
  };
}

describe('desktop-worker paintFrame', () => {
  it('paints Raw RGBA data to canvas context', () => {
    const ctx = createMockContext();
    const rgba = new Uint8Array(10 * 10 * 4);
    rgba.fill(255);

    const frame: DesktopFrame = {
      sequence: 1,
      x: 0,
      y: 0,
      width: 10,
      height: 10,
      encoding: 'Raw',
      data: rgba,
    };

    paintFrame(ctx, frame);

    expect(ctx.createImageData).toHaveBeenCalledWith(10, 10);
    expect(ctx.putImageData).toHaveBeenCalledTimes(1);
    const [imageData, x, y] = (ctx.putImageData as ReturnType<typeof vi.fn>).mock.calls[0]!;
    expect(x).toBe(0);
    expect(y).toBe(0);
    expect(imageData.width).toBe(10);
    expect(imageData.height).toBe(10);
  });

  it('places frame at correct x,y offset', () => {
    const ctx = createMockContext();
    const frame: DesktopFrame = {
      sequence: 2,
      x: 100,
      y: 200,
      width: 5,
      height: 5,
      encoding: 'Raw',
      data: new Uint8Array(5 * 5 * 4),
    };

    paintFrame(ctx, frame);

    const [, x, y] = (ctx.putImageData as ReturnType<typeof vi.fn>).mock.calls[0]!;
    expect(x).toBe(100);
    expect(y).toBe(200);
  });

  it('copies RGBA data into ImageData', () => {
    const ctx = createMockContext();
    const rgba = new Uint8Array(4);
    rgba[0] = 255; // R
    rgba[1] = 128; // G
    rgba[2] = 64;  // B
    rgba[3] = 255; // A

    const frame: DesktopFrame = {
      sequence: 3,
      x: 0,
      y: 0,
      width: 1,
      height: 1,
      encoding: 'Raw',
      data: rgba,
    };

    paintFrame(ctx, frame);

    const [imageData] = (ctx.putImageData as ReturnType<typeof vi.fn>).mock.calls[0]!;
    expect(imageData.data[0]).toBe(255);
    expect(imageData.data[1]).toBe(128);
    expect(imageData.data[2]).toBe(64);
    expect(imageData.data[3]).toBe(255);
  });

  it('handles non-Raw encoding by skipping (no paint)', () => {
    const ctx = createMockContext();
    const frame: DesktopFrame = {
      sequence: 4,
      x: 0,
      y: 0,
      width: 10,
      height: 10,
      encoding: 'Zlib',
      data: new Uint8Array(100),
    };

    paintFrame(ctx, frame);

    expect(ctx.putImageData).not.toHaveBeenCalled();
  });
});
