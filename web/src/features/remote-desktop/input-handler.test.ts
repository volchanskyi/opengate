import { describe, it, expect, vi } from 'vitest';
import { InputHandler } from './input-handler';
import type { ControlMessage } from '../../lib/protocol/types';

function createMockCanvas(): HTMLCanvasElement {
  const canvas = document.createElement('canvas');
  canvas.width = 1920;
  canvas.height = 1080;
  // Mock getBoundingClientRect for coordinate normalization
  canvas.getBoundingClientRect = () => ({
    x: 0,
    y: 0,
    width: 960,
    height: 540,
    top: 0,
    right: 960,
    bottom: 540,
    left: 0,
    toJSON: () => {},
  });
  return canvas;
}

/** Point the canvas at a rect with the given origin, keeping the 960x540 size. */
function placeCanvasAt(canvas: HTMLCanvasElement, left: number, top: number): void {
  canvas.getBoundingClientRect = () => ({
    x: left,
    y: top,
    width: 960,
    height: 540,
    top,
    right: left + 960,
    bottom: top + 540,
    left,
    toJSON: () => {},
  });
}

/** The coordinates of the single MouseMove the handler emitted. */
function movedTo(onMessage: ReturnType<typeof vi.fn>): { x: number; y: number } {
  const call = onMessage.mock.calls[0]?.[0] as ControlMessage | undefined;
  if (call?.type !== 'MouseMove') throw new Error(`expected a MouseMove, got ${String(call?.type)}`);
  return { x: call.x, y: call.y };
}

describe('InputHandler', () => {
  it('mousemove emits MouseMove with scaled coordinates', () => {
    const onMessage = vi.fn<(msg: ControlMessage) => void>();
    const canvas = createMockCanvas();
    const handler = new InputHandler(canvas, onMessage);

    // Simulate mouse at (480, 270) on a 960x540 client rect → (960, 540) in 1920x1080 remote
    canvas.dispatchEvent(new MouseEvent('mousemove', { clientX: 480, clientY: 270 }));

    expect(onMessage).toHaveBeenCalledWith({
      type: 'MouseMove',
      x: 960,
      y: 540,
    });

    handler.destroy();
  });

  it('mousedown emits MouseClick with pressed=true', () => {
    const onMessage = vi.fn<(msg: ControlMessage) => void>();
    const canvas = createMockCanvas();
    const handler = new InputHandler(canvas, onMessage);

    canvas.dispatchEvent(new MouseEvent('mousedown', { clientX: 0, clientY: 0, button: 0 }));

    expect(onMessage).toHaveBeenCalledWith(
      expect.objectContaining({ type: 'MouseClick', button: 'Left', pressed: true }),
    );

    handler.destroy();
  });

  it('mouseup emits MouseClick with pressed=false', () => {
    const onMessage = vi.fn<(msg: ControlMessage) => void>();
    const canvas = createMockCanvas();
    const handler = new InputHandler(canvas, onMessage);

    canvas.dispatchEvent(new MouseEvent('mouseup', { clientX: 0, clientY: 0, button: 0 }));

    expect(onMessage).toHaveBeenCalledWith(
      expect.objectContaining({ type: 'MouseClick', button: 'Left', pressed: false }),
    );

    handler.destroy();
  });

  it('maps mouse button indices to MouseButton names', () => {
    const onMessage = vi.fn<(msg: ControlMessage) => void>();
    const canvas = createMockCanvas();
    const handler = new InputHandler(canvas, onMessage);

    canvas.dispatchEvent(new MouseEvent('mousedown', { clientX: 0, clientY: 0, button: 2 }));
    expect(onMessage).toHaveBeenCalledWith(
      expect.objectContaining({ button: 'Right' }),
    );

    onMessage.mockClear();
    canvas.dispatchEvent(new MouseEvent('mousedown', { clientX: 0, clientY: 0, button: 1 }));
    expect(onMessage).toHaveBeenCalledWith(
      expect.objectContaining({ button: 'Middle' }),
    );

    handler.destroy();
  });

  it('keydown emits KeyPress with pressed=true', () => {
    const onMessage = vi.fn<(msg: ControlMessage) => void>();
    const canvas = createMockCanvas();
    const handler = new InputHandler(canvas, onMessage);

    canvas.dispatchEvent(new KeyboardEvent('keydown', { code: 'KeyA' }));

    expect(onMessage).toHaveBeenCalledWith({
      type: 'KeyPress',
      key: 'KeyA',
      pressed: true,
    });

    handler.destroy();
  });

  it('keyup emits KeyPress with pressed=false', () => {
    const onMessage = vi.fn<(msg: ControlMessage) => void>();
    const canvas = createMockCanvas();
    const handler = new InputHandler(canvas, onMessage);

    canvas.dispatchEvent(new KeyboardEvent('keyup', { code: 'KeyA' }));

    expect(onMessage).toHaveBeenCalledWith({
      type: 'KeyPress',
      key: 'KeyA',
      pressed: false,
    });

    handler.destroy();
  });

  it('ignores unmapped keyboard codes', () => {
    const onMessage = vi.fn<(msg: ControlMessage) => void>();
    const canvas = createMockCanvas();
    const handler = new InputHandler(canvas, onMessage);

    canvas.dispatchEvent(new KeyboardEvent('keydown', { code: 'UnknownKey' }));
    expect(onMessage).not.toHaveBeenCalled();

    handler.destroy();
  });

  it('destroy removes all event listeners', () => {
    const onMessage = vi.fn<(msg: ControlMessage) => void>();
    const canvas = createMockCanvas();
    const handler = new InputHandler(canvas, onMessage);

    handler.destroy();

    canvas.dispatchEvent(new MouseEvent('mousemove', { clientX: 100, clientY: 100 }));
    canvas.dispatchEvent(new KeyboardEvent('keydown', { code: 'KeyA' }));

    expect(onMessage).not.toHaveBeenCalled();
  });

  it('scales coordinates relative to the canvas origin, not the viewport', () => {
    const onMessage = vi.fn<(msg: ControlMessage) => void>();
    const canvas = createMockCanvas();
    placeCanvasAt(canvas, 100, 50);
    const handler = new InputHandler(canvas, onMessage);

    // 100px right and 50px below the canvas origin → (200, 100) after the 2x
    // scale. Adding the origin instead of subtracting it would land elsewhere.
    canvas.dispatchEvent(new MouseEvent('mousemove', { clientX: 200, clientY: 100 }));

    expect(movedTo(onMessage)).toEqual({ x: 200, y: 100 });

    handler.destroy();
  });

  it('re-reads the canvas rect after a window resize', () => {
    const onMessage = vi.fn<(msg: ControlMessage) => void>();
    const canvas = createMockCanvas();
    const handler = new InputHandler(canvas, onMessage);

    // Prime the cache against the original rect.
    canvas.dispatchEvent(new MouseEvent('mousemove', { clientX: 480, clientY: 270 }));
    expect(movedTo(onMessage)).toEqual({ x: 960, y: 540 });

    // The canvas moves; without invalidation the stale rect keeps producing the
    // old coordinates.
    onMessage.mockClear();
    placeCanvasAt(canvas, 480, 270);
    globalThis.dispatchEvent(new Event('resize'));

    canvas.dispatchEvent(new MouseEvent('mousemove', { clientX: 480, clientY: 270 }));
    expect(movedTo(onMessage)).toEqual({ x: 0, y: 0 });

    handler.destroy();
  });

  it('re-reads the canvas rect after a scroll', () => {
    const onMessage = vi.fn<(msg: ControlMessage) => void>();
    const canvas = createMockCanvas();
    const handler = new InputHandler(canvas, onMessage);

    canvas.dispatchEvent(new MouseEvent('mousemove', { clientX: 480, clientY: 270 }));
    expect(movedTo(onMessage)).toEqual({ x: 960, y: 540 });

    onMessage.mockClear();
    placeCanvasAt(canvas, 240, 135);
    // Scroll is captured, so it is observed even from a nested target.
    document.body.dispatchEvent(new Event('scroll', { bubbles: false }));

    canvas.dispatchEvent(new MouseEvent('mousemove', { clientX: 480, clientY: 270 }));
    expect(movedTo(onMessage)).toEqual({ x: 480, y: 270 });

    handler.destroy();
  });

  it('destroy removes every window listener it registered', () => {
    const onMessage = vi.fn<(msg: ControlMessage) => void>();
    const canvas = createMockCanvas();
    const addSpy = vi.spyOn(globalThis, 'addEventListener');
    const removeSpy = vi.spyOn(globalThis, 'removeEventListener');

    const handler = new InputHandler(canvas, onMessage);
    // Only the window registrations matter here; the canvas keeps its own.
    const registered = addSpy.mock.calls.map(([event, fn, capture]) => [event, fn, capture ?? false]);
    expect(registered.map(([event]) => event)).toEqual(['resize', 'scroll']);

    handler.destroy();

    const released = removeSpy.mock.calls.map(([event, fn, capture]) => [event, fn, capture ?? false]);
    // A listener is only released when the event, the function and the capture
    // flag all match what it was registered with.
    expect(released).toEqual(registered);

    addSpy.mockRestore();
    removeSpy.mockRestore();
  });

  it('clamps coordinates to remote resolution bounds', () => {
    const onMessage = vi.fn<(msg: ControlMessage) => void>();
    const canvas = createMockCanvas();
    const handler = new InputHandler(canvas, onMessage);

    // Move beyond canvas bounds
    canvas.dispatchEvent(new MouseEvent('mousemove', { clientX: 2000, clientY: 2000 }));

    const call = onMessage.mock.calls[0]![0];
    if (call.type === 'MouseMove') {
      expect(call.x).toBeLessThanOrEqual(1920);
      expect(call.y).toBeLessThanOrEqual(1080);
    }

    handler.destroy();
  });
});
