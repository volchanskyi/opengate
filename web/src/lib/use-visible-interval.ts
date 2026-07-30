import { useEffect, useRef } from 'react';

/**
 * Run `callback` every `delayMs` while the tab is visible.
 *
 * A background tab is not watching anything, so polling it only burns the
 * device's battery and the server's capacity. The interval is torn down when the
 * page hides and rebuilt when it shows again, with one immediate catch-up call
 * on the hidden -> visible edge so a returning user never reads stale data while
 * waiting out a full period.
 *
 * The first render does not fire a catch-up: mount-time loading belongs to the
 * caller, and this hook governs the repeat only.
 *
 * `callback` is held in a ref, so an inline arrow function may be passed without
 * restarting the interval on every render.
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
