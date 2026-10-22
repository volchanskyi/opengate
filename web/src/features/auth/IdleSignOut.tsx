import { useEffect, useState } from 'react';
import { api } from '../../lib/api';
import { lastRequest } from '../../lib/console-session';
import { fireAndForget } from '../../lib/fire-and-forget';
import { useAuthStore } from '../../state/auth-store';

/** How long before the idle sign-out the console warns. */
export const IDLE_WARNING_MS = 2 * 60 * 1000;

/**
 * Warns two minutes before the server's idle limit and signs the console out when it passes;
 * any request renews the limit, so the warning's button asks the server for the session again.
 */
export function IdleSignOut() {
  const idleSeconds = useAuthStore((s) => s.idleTimeoutSeconds);
  const signedOutLocally = useAuthStore((s) => s.signedOutLocally);
  const [left, setLeft] = useState<number | null>(null);

  useEffect(() => {
    if (!idleSeconds) return undefined;
    const tick = () => {
      const remaining = idleSeconds * 1000 - (Date.now() - lastRequest());
      if (remaining <= 0) {
        signedOutLocally();
        return;
      }
      setLeft(remaining <= IDLE_WARNING_MS ? remaining : null);
    };
    tick();
    const timer = setInterval(tick, 1000);
    return () => clearInterval(timer);
  }, [idleSeconds, signedOutLocally]);

  if (left === null) return null;

  const stay = () => {
    fireAndForget(api.GET('/api/v1/auth/session').then(() => setLeft(null)));
  };

  return (
    <div
      role="alertdialog"
      aria-labelledby="idle-sign-out-title"
      className="fixed bottom-4 right-4 z-50 rounded-lg bg-gray-800 p-4 text-white shadow-lg"
    >
      <p id="idle-sign-out-title" className="mb-2">
        You will be signed out in {Math.ceil(left / 1000)} seconds for inactivity.
      </p>
      <button type="button" className="rounded bg-blue-600 px-3 py-1" onClick={stay}>
        Stay signed in
      </button>
    </div>
  );
}
