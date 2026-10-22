import { SIGNED_IN_KEY } from '../../lib/console-session';

/** Where this browser keeps a host's presence mark, per host. */
const PRESENCE_PREFIX = 'opengate-session-tab:';

/** How often a session tab renews its mark, and how old a mark may be before it is forgotten. */
const HEARTBEAT_MS = 2_000;
const STALE_MS = 5_000;


/** The window name a host's session tab carries, so any page of this browser can reach it. */
export function sessionTabName(deviceId: string): string {
  return `opengate-session-${deviceId}`;
}

/** Where a host's session tab opens. */
export function sessionTabPath(deviceId: string): string {
  return `/devices/${deviceId}/session`;
}

/** Opens the host's session tab under its name; called in the click itself, so it is no popup. */
export function openSessionTab(deviceId: string): void {
  window.open(sessionTabPath(deviceId), sessionTabName(deviceId));
}

/** How long a page waits to give up focus to the session tab it asked forward. */
const FORWARD_WAIT_MS = 500;

/**
 * Brings the host's session tab forward without reloading it, and resolves whether it came: a
 * browser that will not hands back a blank tab of its own, or leaves this page holding focus.
 */
export function focusSessionTab(deviceId: string): Promise<boolean> {
  const tab = window.open('', sessionTabName(deviceId));
  if (!tab) return Promise.resolve(false);
  if (tab.location.href === 'about:blank') {
    tab.close();
    return Promise.resolve(false);
  }
  tab.focus();
  return new Promise((resolve) => setTimeout(() => resolve(!document.hasFocus()), FORWARD_WAIT_MS));
}

/** Marks this browser as holding a session tab for the host until the returned function runs. */
export function announceSessionTab(deviceId: string): () => void {
  const key = PRESENCE_PREFIX + deviceId;
  const mark = () => {
    try {
      localStorage.setItem(key, String(Date.now()));
    } catch {
      // Storage refused: the fleet page offers to move the session here, which replaces it.
    }
  };
  mark();
  const timer = setInterval(mark, HEARTBEAT_MS);
  return () => {
    clearInterval(timer);
    try {
      localStorage.removeItem(key);
    } catch {
      // A mark left behind goes stale on its own.
    }
  };
}

/** Whether a session tab of this browser holds the host now. */
export function holdsSessionTab(deviceId: string): boolean {
  try {
    const at = Number(localStorage.getItem(PRESENCE_PREFIX + deviceId));
    return at > 0 && Date.now() - at < STALE_MS;
  } catch {
    return false;
  }
}

/** The account this browser is signed in as, or the given one when storage cannot be read. */
function storedSignIn(fallback: string): string | null {
  try {
    return localStorage.getItem(SIGNED_IN_KEY);
  } catch {
    return fallback;
  }
}

/**
 * Calls onSignedOut when the account this browser is signed in as, now or later in any tab, is gone
 * or another account or tenant; `current` is the identity the session opened under.
 */
export function watchSignIn(current: string, onSignedOut: () => void): () => void {
  const changed = (next: string | null) => next !== current;
  const listener = (event: StorageEvent) => {
    if (event.key !== SIGNED_IN_KEY && event.key !== null) return;
    if (changed(event.key === null ? storedSignIn(current) : event.newValue)) onSignedOut();
  };
  window.addEventListener('storage', listener);
  if (changed(storedSignIn(current))) onSignedOut();
  return () => window.removeEventListener('storage', listener);
}
