import { useEffect, useRef } from 'react';
import { useConnectionStore } from '../../state/connection-store';
import { InputHandler } from './input-handler';
import { paintFrame, type CanvasContext } from './desktop-worker';
import { fireAndForget } from '../../lib/fire-and-forget';

/** Hook that wires the transport's desktop frames to a canvas and captures input. */
export function useRemoteDesktop(canvasRef: React.RefObject<HTMLCanvasElement | null>) {
  const transport = useConnectionStore((s) => s.transport);
  const setOnDesktopFrame = useConnectionStore((s) => s.setOnDesktopFrame);
  const setOnControlMessage = useConnectionStore((s) => s.setOnControlMessage);
  const inputHandlerRef = useRef<InputHandler | null>(null);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas || !transport) return;

    const ctx = canvas.getContext('2d');
    if (!ctx) return;

    // Set up frame rendering
    setOnDesktopFrame((frame) => {
      // Update canvas size to match frame if needed
      if (frame.x === 0 && frame.y === 0 && frame.width > 0 && frame.height > 0) {
        if (canvas.width !== frame.width || canvas.height !== frame.height) {
          canvas.width = frame.width;
          canvas.height = frame.height;
        }
      }
      fireAndForget(paintFrame(ctx as unknown as CanvasContext, frame));
    });

    // Set up input capture
    const handler = new InputHandler(canvas, (msg) => {
      transport.sendControl(msg);
    });
    inputHandlerRef.current = handler;

    return () => {
      handler.destroy();
      inputHandlerRef.current = null;
      setOnDesktopFrame(null);
      setOnControlMessage(null);
    };
  }, [transport, canvasRef, setOnDesktopFrame, setOnControlMessage]);
}
