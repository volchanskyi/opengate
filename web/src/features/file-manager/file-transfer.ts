import type { FileFrame } from '../../lib/protocol/types';

/** Chunk size for file uploads: 256 KiB. */
export const CHUNK_SIZE = 256 * 1024;

/** Accumulates incoming file chunks and produces a Blob on completion. */
export class DownloadAccumulator {
  private totalSize: number;
  private received = 0;
  private chunks: Uint8Array[] = [];

  constructor(totalSize: number) {
    this.totalSize = totalSize;
  }

  addChunk(frame: FileFrame): void {
    this.chunks.push(new Uint8Array(frame.data));
    this.received += frame.data.length;
  }

  isComplete(): boolean {
    return this.received >= this.totalSize;
  }

  progress(): number {
    return this.totalSize === 0 ? 1 : this.received / this.totalSize;
  }

  toBlob(): Blob {
    return new Blob(this.chunks.map((c) => c.slice().buffer as ArrayBuffer));
  }
}

/** Split a Uint8Array into FileFrame-shaped chunks. */
export function chunkFile(data: Uint8Array): FileFrame[] {
  if (data.length === 0) return [];

  const frames: FileFrame[] = [];
  for (let offset = 0; offset < data.length; offset += CHUNK_SIZE) {
    const end = Math.min(offset + CHUNK_SIZE, data.length);
    frames.push({
      offset,
      total_size: data.length,
      data: data.subarray(offset, end),
    });
  }
  return frames;
}
