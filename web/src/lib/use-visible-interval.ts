import { useEffect, useRef } from 'react';

/**
 * Runs `callback` every `delayMs` while the tab is visible, with a catch-up call on showing.
 * The callback is held in a ref, so an inline function does not restart the interval.
 */
export function useVisibleInterval(callback: () => void, delayMs: number): void {
  const savedCallback = useRef(callback);
  useEffect(() => {
    savedCallback.current = callback;
  }, [callback]);

  useEffect(() => {
    let timer: ReturnType<typeof setInterval> | undefined;

    const stop = () => {
      if (timer !== undefined) {
        clearInterval(timer);
        timer = undefined;
      }
    };

    const start = () => {
      stop();
      timer = setInterval(() => { savedCallback.current(); }, delayMs);
    };

    const onVisibilityChange = () => {
      if (document.visibilityState === 'visible') {
        savedCallback.current();
        start();
      } else {
        stop();
      }
    };

    if (document.visibilityState === 'visible') start();
    document.addEventListener('visibilitychange', onVisibilityChange);

    return () => {
      stop();
      document.removeEventListener('visibilitychange', onVisibilityChange);
    };
  }, [delayMs]);
}
