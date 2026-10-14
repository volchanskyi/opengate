import { useEffect, useRef } from 'react';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import '@xterm/xterm/css/xterm.css';
import { useConnectionStore } from '../session';

const textEncoder = new TextEncoder();
const textDecoder = new TextDecoder();

export function useTerminal(containerRef: React.RefObject<HTMLDivElement | null>) {
  const transport = useConnectionStore((s) => s.transport);
  const setOnTerminalFrame = useConnectionStore((s) => s.setOnTerminalFrame);
  const termRef = useRef<Terminal | null>(null);

  useEffect(() => {
    const container = containerRef.current;
    if (!container || !transport) return;

    const term = new Terminal({
      cursorBlink: true,
      theme: {
        background: '#1a1a2e',
        foreground: '#e0e0e0',
      },
    });
    const fitAddon = new FitAddon();
    term.loadAddon(fitAddon);
    term.open(container);
    fitAddon.fit();
    termRef.current = term;

    // Ctrl+letter goes to the relay as a control character and skips browser and xterm handling;
    // Ctrl+Shift+C/V keep their clipboard behaviour.
    term.attachCustomKeyEventHandler((ev) => {
      if (ev.type !== 'keydown') return true;
      if (ev.ctrlKey && !ev.altKey && !ev.metaKey && !ev.shiftKey) {
        const key = ev.key.toLowerCase();
        if (key.length === 1 && key >= 'a' && key <= 'z') {
          const ctrlChar = String.fromCharCode(key.charCodeAt(0) - 96);
          transport.sendTerminalData(textEncoder.encode(ctrlChar));
          ev.preventDefault();
          return false;
        }
      }
      return true;
    });

    term.onData((data) => {
      transport.sendTerminalData(textEncoder.encode(data));
    });

    term.onResize(({ cols, rows }) => {
      transport.sendControl({ type: 'TerminalResize', cols, rows });
    });

    setOnTerminalFrame((frame) => {
      term.write(textDecoder.decode(frame.data));
    });

    // The panel is hidden while another feature is shown, so it refits whenever its size changes.
    const panel = new ResizeObserver(() => fitAddon.fit());
    panel.observe(container);

    return () => {
      panel.disconnect();
      setOnTerminalFrame(null);
      term.dispose();
      termRef.current = null;
    };
  }, [transport, containerRef, setOnTerminalFrame]);
}
