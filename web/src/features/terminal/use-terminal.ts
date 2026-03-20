import { useEffect, useRef } from 'react';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { useConnectionStore } from '../../state/connection-store';

const textEncoder = new TextEncoder();
const textDecoder = new TextDecoder();

/** Hook that wires xterm.js to the relay transport's terminal frames. */
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

    // Intercept Ctrl+letter so the browser does not consume them for
    // clipboard operations. We send the control character directly and
    // block both browser and xterm.js default handling.
    // Ctrl+Shift+C/V are left for clipboard copy/paste.
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

    // Terminal input → send to relay
    term.onData((data) => {
      transport.sendTerminalData(textEncoder.encode(data));
    });

    // Terminal resize → send control message
    term.onResize(({ cols, rows }) => {
      transport.sendControl({ type: 'TerminalResize', cols, rows });
    });

    // Incoming terminal data → write to terminal
    setOnTerminalFrame((frame) => {
      term.write(textDecoder.decode(frame.data));
    });

    // Handle window resize
    const handleResize = () => fitAddon.fit();
    window.addEventListener('resize', handleResize);

    return () => {
      window.removeEventListener('resize', handleResize);
      setOnTerminalFrame(null);
      term.dispose();
      termRef.current = null;
    };
  }, [transport, containerRef, setOnTerminalFrame]);
}
