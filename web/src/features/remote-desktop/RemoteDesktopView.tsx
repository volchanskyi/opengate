import { useRef } from 'react';
import { useConnectionStore } from '../../state/connection-store';
import { useRemoteDesktop } from './use-remote-desktop';

export function RemoteDesktopView() {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const connectionState = useConnectionStore((s) => s.state);

  useRemoteDesktop(canvasRef);

  return (
    <div className="relative w-full h-full bg-black">
      <canvas
        ref={canvasRef}
        tabIndex={0}
        className="w-full h-full object-contain outline-none"
      />
      {connectionState !== 'connected' && (
        <div className="absolute inset-0 flex items-center justify-center bg-black/80">
          <p className="text-gray-400">Waiting for connection...</p>
        </div>
      )}
    </div>
  );
}
