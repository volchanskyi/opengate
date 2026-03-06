import type { DesktopFrame } from '../../lib/protocol/types';

/** Minimal canvas context interface for painting. */
export interface CanvasContext {
  width: number;
  height: number;
  putImageData: (imageData: ImageData, dx: number, dy: number) => void;
  createImageData: (sw: number, sh: number) => ImageData;
}

/** Paint a raw RGBA desktop frame onto a canvas context. */
export function paintFrame(ctx: CanvasContext, frame: DesktopFrame): void {
  if (frame.encoding !== 'Raw') {
    // Only Raw encoding supported for now
    return;
  }

  const imageData = ctx.createImageData(frame.width, frame.height);
  imageData.data.set(frame.data.subarray(0, frame.width * frame.height * 4));
  ctx.putImageData(imageData, frame.x, frame.y);
}
