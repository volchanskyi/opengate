import type { DesktopFrame } from '../../lib/protocol/types';

/** Minimal canvas context interface for painting. */
export interface CanvasContext {
  width: number;
  height: number;
  putImageData: (imageData: ImageData, dx: number, dy: number) => void;
  createImageData: (sw: number, sh: number) => ImageData;
  drawImage: (image: ImageBitmap, dx: number, dy: number) => void;
}

/** Paint a desktop frame onto a canvas context. Supports Raw RGBA and JPEG. */
export async function paintFrame(ctx: CanvasContext, frame: DesktopFrame): Promise<void> {
  if (frame.encoding === 'Raw') {
    const imageData = ctx.createImageData(frame.width, frame.height);
    imageData.data.set(frame.data.subarray(0, frame.width * frame.height * 4));
    ctx.putImageData(imageData, frame.x, frame.y);
  } else if (frame.encoding === 'Jpeg') {
    const blob = new Blob([new Uint8Array(frame.data)], { type: 'image/jpeg' });
    const bitmap = await createImageBitmap(blob);
    ctx.drawImage(bitmap, frame.x, frame.y);
    bitmap.close();
  }
}
