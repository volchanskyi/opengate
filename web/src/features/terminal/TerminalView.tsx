import { useRef } from 'react';
import { useConnectionStore } from '../../state/connection-store';
import { useTerminal } from './use-terminal';

export function TerminalView() {
  const containerRef = useRef<HTMLDivElement>(null);
  const connectionState = useConnectionStore((s) => s.state);

  useTerminal(containerRef);

  return (
    <div className="relative w-full h-full">
      <div
        ref={containerRef}
        data-testid="terminal-container"
        className="w-full h-full"
      />
      {connectionState !== 'connected' && (
        <div className="absolute inset-0 flex items-center justify-center bg-black/80">
          <p className="text-gray-400">Waiting for connection...</p>
        </div>
      )}
    </div>
  );
}
