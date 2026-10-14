import { renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useConnectionStore } from '../session';
import { useTerminal } from './use-terminal';

const fit = vi.fn();
const written: string[] = [];
let typed: ((data: string) => void) | null = null;

vi.mock('@xterm/xterm', () => ({
  Terminal: class {
    loadAddon() {}
    open() {}
    attachCustomKeyEventHandler() {}
    onData(cb: (data: string) => void) {
      typed = cb;
    }
    onResize() {}
    write(text: string) {
      written.push(text);
    }
    dispose() {}
  },
}));
vi.mock('@xterm/addon-fit', () => ({
  FitAddon: class {
    fit() {
      fit();
    }
  },
}));
vi.mock('@xterm/xterm/css/xterm.css', () => ({}));

describe('useTerminal', () => {
  const sendTerminalData = vi.fn();
  let resized: (() => void) | null = null;
  const disconnect = vi.fn();

  beforeEach(() => {
    fit.mockReset();
    sendTerminalData.mockReset();
    disconnect.mockReset();
    written.length = 0;
    typed = null;
    vi.stubGlobal(
      'ResizeObserver',
      class {
        constructor(cb: () => void) {
          resized = cb;
        }
        observe() {}
        disconnect() {
          disconnect();
        }
      },
    );
    useConnectionStore.setState({ transport: { sendTerminalData, sendControl: vi.fn() } as never });
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  function mount() {
    const container = { current: document.createElement('div') };
    return renderHook(() => useTerminal(container));
  }

  it('refits the terminal whenever its panel changes size, as when it is shown again', () => {
    const { unmount } = mount();
    const atOpen = fit.mock.calls.length;
    resized!();
    expect(fit.mock.calls.length).toBe(atOpen + 1);
    unmount();
    expect(disconnect).toHaveBeenCalledOnce();
  });

  it('sends what the technician types and shows what the host answers', () => {
    mount();
    typed!('ls\r');
    expect(new TextDecoder().decode(sendTerminalData.mock.calls[0]![0])).toBe('ls\r');
    useConnectionStore.getState().onTerminalFrame!({ data: new TextEncoder().encode('file.txt') });
    expect(written).toEqual(['file.txt']);
  });
});
