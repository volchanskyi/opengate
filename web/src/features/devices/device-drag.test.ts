import { describe, it, expect, vi } from 'vitest';
import { DEVICE_DRAG_MIME, isDeviceDrag, readDraggedDeviceId, startDeviceDrag } from './device-drag';

/** Minimal DataTransfer stand-in: jsdom does not implement the real one. */
function fakeTransfer(entries: Record<string, string> = {}): DataTransfer {
  const store = new Map(Object.entries(entries));
  return {
    get types() { return [...store.keys()]; },
    setData: vi.fn((type: string, value: string) => { store.set(type, value); }),
    getData: (type: string) => store.get(type) ?? '',
    effectAllowed: 'none',
    dropEffect: 'none',
  } as unknown as DataTransfer;
}

describe('device drag payload', () => {
  it('startDeviceDrag writes the id under the private type and the hostname as text', () => {
    const dt = fakeTransfer();

    startDeviceDrag(dt, { id: 'd1', hostname: 'web-01' });

    expect(dt.getData(DEVICE_DRAG_MIME)).toBe('d1');
    expect(dt.getData('text/plain')).toBe('web-01');
    expect(dt.effectAllowed).toBe('move');
  });

  it('isDeviceDrag recognises a device drag from the types list alone', () => {
    // Drag-over handlers may read `types` but never `getData` (protected mode),
    // so the highlight decision must not depend on the payload.
    expect(isDeviceDrag(fakeTransfer({ [DEVICE_DRAG_MIME]: 'd1' }))).toBe(true);
  });

  it('isDeviceDrag rejects a foreign drag and a missing transfer', () => {
    expect(isDeviceDrag(fakeTransfer({ 'text/plain': 'some text' }))).toBe(false);
    expect(isDeviceDrag(fakeTransfer())).toBe(false);
    expect(isDeviceDrag(null)).toBe(false);
  });

  it('readDraggedDeviceId returns the id, trimmed, or empty for a foreign drag', () => {
    expect(readDraggedDeviceId(fakeTransfer({ [DEVICE_DRAG_MIME]: ' d1 ' }))).toBe('d1');
    expect(readDraggedDeviceId(fakeTransfer({ 'text/plain': 'nope' }))).toBe('');
    expect(readDraggedDeviceId(null)).toBe('');
  });
});
