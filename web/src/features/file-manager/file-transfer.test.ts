import { describe, it, expect } from 'vitest';
import { DownloadAccumulator, chunkFile, CHUNK_SIZE } from './file-transfer';

describe('DownloadAccumulator', () => {
  it('accumulates chunks and produces Blob on completion', () => {
    const acc = new DownloadAccumulator(10);

    acc.addChunk({ offset: 0, total_size: 10, data: new Uint8Array([1, 2, 3, 4, 5]) });
    expect(acc.isComplete()).toBe(false);
    expect(acc.progress()).toBeCloseTo(0.5);

    acc.addChunk({ offset: 5, total_size: 10, data: new Uint8Array([6, 7, 8, 9, 10]) });
    expect(acc.isComplete()).toBe(true);
    expect(acc.progress()).toBe(1);

    const blob = acc.toBlob();
    expect(blob.size).toBe(10);
  });

  it('reports progress as fraction', () => {
    const acc = new DownloadAccumulator(100);
    acc.addChunk({ offset: 0, total_size: 100, data: new Uint8Array(25) });
    expect(acc.progress()).toBeCloseTo(0.25);
  });

  it('handles single-chunk download', () => {
    const acc = new DownloadAccumulator(5);
    acc.addChunk({ offset: 0, total_size: 5, data: new Uint8Array([1, 2, 3, 4, 5]) });
    expect(acc.isComplete()).toBe(true);
  });
});

describe('chunkFile', () => {
  it('splits data into CHUNK_SIZE pieces', () => {
    const data = new Uint8Array(CHUNK_SIZE * 2 + 100);
    data.fill(0xab);
    const chunks = chunkFile(data);

    expect(chunks.length).toBe(3);
    expect(chunks[0]!.offset).toBe(0);
    expect(chunks[0]!.data.length).toBe(CHUNK_SIZE);
    expect(chunks[1]!.offset).toBe(CHUNK_SIZE);
    expect(chunks[1]!.data.length).toBe(CHUNK_SIZE);
    expect(chunks[2]!.offset).toBe(CHUNK_SIZE * 2);
    expect(chunks[2]!.data.length).toBe(100);
    expect(chunks[2]!.total_size).toBe(data.length);
  });

  it('handles empty data', () => {
    const chunks = chunkFile(new Uint8Array(0));
    expect(chunks.length).toBe(0);
  });

  it('handles exact chunk boundary', () => {
    const data = new Uint8Array(CHUNK_SIZE);
    const chunks = chunkFile(data);
    expect(chunks.length).toBe(1);
    expect(chunks[0]!.offset).toBe(0);
    expect(chunks[0]!.total_size).toBe(CHUNK_SIZE);
  });
});
