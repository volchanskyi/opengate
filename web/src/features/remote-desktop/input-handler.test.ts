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
