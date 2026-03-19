import type { ControlMessage, MouseButton, KeyCode } from '../../lib/protocol/types';

const MOUSE_BUTTON_MAP: Record<number, MouseButton> = {
  0: 'Left',
  1: 'Middle',
  2: 'Right',
  3: 'Back',
  4: 'Forward',
};

/** All valid KeyCode values for filtering. */
const VALID_KEYS = new Set<string>([
  'KeyA','KeyB','KeyC','KeyD','KeyE','KeyF','KeyG','KeyH','KeyI','KeyJ','KeyK','KeyL','KeyM',
  'KeyN','KeyO','KeyP','KeyQ','KeyR','KeyS','KeyT','KeyU','KeyV','KeyW','KeyX','KeyY','KeyZ',
  'Digit0','Digit1','Digit2','Digit3','Digit4','Digit5','Digit6','Digit7','Digit8','Digit9',
  'ShiftLeft','ShiftRight','ControlLeft','ControlRight','AltLeft','AltRight','MetaLeft','MetaRight',
  'ArrowUp','ArrowDown','ArrowLeft','ArrowRight','Home','End','PageUp','PageDown',
  'Backspace','Delete','Enter','Tab','Escape','Space','Insert','CapsLock','NumLock','ScrollLock',
  'F1','F2','F3','F4','F5','F6','F7','F8','F9','F10','F11','F12',
  'Minus','Equal','BracketLeft','BracketRight','Backslash','Semicolon','Quote','Comma','Period','Slash','Backquote',
  'Numpad0','Numpad1','Numpad2','Numpad3','Numpad4','Numpad5','Numpad6','Numpad7','Numpad8','Numpad9',
  'NumpadAdd','NumpadSubtract','NumpadMultiply','NumpadDivide','NumpadDecimal','NumpadEnter',
  'PrintScreen','Pause',
]);

/** Captures mouse/keyboard events on a canvas and emits ControlMessages. */
export class InputHandler {
  private readonly canvas: HTMLCanvasElement;
  private readonly onMessage: (msg: ControlMessage) => void;
  private boundHandlers: Array<[string, EventListener]> = [];
  private boundWindowHandlers: Array<[string, EventListener]> = [];
  private cachedRect: DOMRect | null = null;

  constructor(canvas: HTMLCanvasElement, onMessage: (msg: ControlMessage) => void) {
    this.canvas = canvas;
    this.onMessage = onMessage;

    this.listen('mousemove', this.handleMouseMove);
    this.listen('mousedown', this.handleMouseButton);
    this.listen('mouseup', this.handleMouseButton);
    this.listen('keydown', this.handleKey);
    this.listen('keyup', this.handleKey);

    const invalidateRect = () => { this.cachedRect = null; };
    globalThis.addEventListener('resize', invalidateRect);
    globalThis.addEventListener('scroll', invalidateRect, true);
    this.boundWindowHandlers = [['resize', invalidateRect], ['scroll', invalidateRect]];
  }

  destroy(): void {
    for (const [event, handler] of this.boundHandlers) {
      this.canvas.removeEventListener(event, handler);
    }
    this.boundHandlers = [];
    for (const [event, handler] of this.boundWindowHandlers) {
      globalThis.removeEventListener(event, handler, true);
    }
    this.boundWindowHandlers = [];
  }

  private listen(event: string, handler: (e: Event) => void): void {
    const bound = handler.bind(this) as EventListener;
    this.canvas.addEventListener(event, bound);
    this.boundHandlers.push([event, bound]);
  }

  private scaleCoords(clientX: number, clientY: number): { x: number; y: number } {
    this.cachedRect ??= this.canvas.getBoundingClientRect();
    const rect = this.cachedRect;
    const scaleX = this.canvas.width / rect.width;
    const scaleY = this.canvas.height / rect.height;
    const x = Math.round(Math.max(0, Math.min(this.canvas.width, (clientX - rect.left) * scaleX)));
    const y = Math.round(Math.max(0, Math.min(this.canvas.height, (clientY - rect.top) * scaleY)));
    return { x, y };
  }

  private handleMouseMove(e: Event): void {
    const me = e as MouseEvent;
    const { x, y } = this.scaleCoords(me.clientX, me.clientY);
    this.onMessage({ type: 'MouseMove', x, y });
  }

  private handleMouseButton(e: Event): void {
    const me = e as MouseEvent;
    const button = MOUSE_BUTTON_MAP[me.button];
    if (!button) return;
    const { x, y } = this.scaleCoords(me.clientX, me.clientY);
    this.onMessage({
      type: 'MouseClick',
      button,
      pressed: me.type === 'mousedown',
      x,
      y,
    });
  }

  private handleKey(e: Event): void {
    const ke = e as KeyboardEvent;
    if (!VALID_KEYS.has(ke.code)) return;
    this.onMessage({
      type: 'KeyPress',
      key: ke.code as KeyCode,
      pressed: ke.type === 'keydown',
    });
  }
}
