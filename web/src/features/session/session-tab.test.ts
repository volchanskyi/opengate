import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  announceSessionTab,
  focusSessionTab,
  holdsSessionTab,
  openSessionTab,
  sessionTabName,
  watchSignIn,
} from './session-tab';
import { identityOf, SIGNED_IN_KEY } from '../../lib/console-session';

const ALICE = identityOf('alice', 'northwind');

describe('session tab', () => {
  afterEach(() => {
    vi.restoreAllMocks();
    vi.useRealTimers();
    localStorage.clear();
  });

  it('names a host’s tab after the host, so any page of this browser can reach it', () => {
    expect(sessionTabName('dev-1')).toBe('opengate-session-dev-1');
    expect(sessionTabName('dev-2')).not.toBe(sessionTabName('dev-1'));
  });

  it('opens the host’s tab at once under its name', () => {
    const open = vi.spyOn(window, 'open').mockReturnValue(null);
    openSessionTab('dev-1');
    expect(open).toHaveBeenCalledWith('/devices/dev-1/session', 'opengate-session-dev-1');
  });

  describe('presence', () => {
    beforeEach(() => {
      vi.useFakeTimers();
    });

    it('says this browser holds the host while its tab keeps announcing', () => {
      expect(holdsSessionTab('dev-1')).toBe(false);
      const stop = announceSessionTab('dev-1');
      expect(holdsSessionTab('dev-1')).toBe(true);
      expect(holdsSessionTab('dev-2')).toBe(false);

      vi.advanceTimersByTime(30_000);
      expect(holdsSessionTab('dev-1')).toBe(true);

      stop();
      expect(holdsSessionTab('dev-1')).toBe(false);
    });

    it('forgets a tab that stopped announcing without saying so', () => {
      announceSessionTab('dev-1');
      vi.clearAllTimers();
      vi.advanceTimersByTime(10_000);
      expect(holdsSessionTab('dev-1')).toBe(false);
    });
  });

  describe('bringing the tab forward', () => {
    const existing = () => ({ location: { href: 'https://app.test/devices/dev-1/session' }, focus: vi.fn(), close: vi.fn() });

    it('focuses the existing tab without reloading it, and knows it came forward when this page lost focus', async () => {
      vi.useFakeTimers();
      const tab = existing();
      const open = vi.spyOn(window, 'open').mockReturnValue(tab as unknown as Window);
      vi.spyOn(document, 'hasFocus').mockReturnValue(false);

      const forward = focusSessionTab('dev-1');
      expect(open).toHaveBeenCalledWith('', 'opengate-session-dev-1');
      expect(tab.focus).toHaveBeenCalled();
      expect(tab.close).not.toHaveBeenCalled();
      await vi.advanceTimersByTimeAsync(500);
      await expect(forward).resolves.toBe(true);
    });

    it('says it could not when this page still holds focus after asking, as a browser that refuses leaves it', async () => {
      vi.useFakeTimers();
      vi.spyOn(window, 'open').mockReturnValue(existing() as unknown as Window);
      vi.spyOn(document, 'hasFocus').mockReturnValue(true);

      const forward = focusSessionTab('dev-1');
      await vi.advanceTimersByTimeAsync(500);
      await expect(forward).resolves.toBe(false);
    });

    it('closes the blank tab a browser opened instead, and says it could not', async () => {
      const blank = { location: { href: 'about:blank' }, focus: vi.fn(), close: vi.fn() };
      vi.spyOn(window, 'open').mockReturnValue(blank as unknown as Window);
      await expect(focusSessionTab('dev-1')).resolves.toBe(false);
      expect(blank.close).toHaveBeenCalled();
    });

    it('says it could not when the browser opens nothing', async () => {
      vi.spyOn(window, 'open').mockReturnValue(null);
      await expect(focusSessionTab('dev-1')).resolves.toBe(false);
    });
  });

  describe('signing out', () => {
    function change(key: string | null, value: string | null) {
      if (key) {
        if (value === null) localStorage.removeItem(key);
        else localStorage.setItem(key, value);
      } else {
        localStorage.clear();
      }
      window.dispatchEvent(new StorageEvent('storage', { key, newValue: value }));
    }

    beforeEach(() => {
      localStorage.setItem(SIGNED_IN_KEY, ALICE);
    });

    it('ends the tab’s session when this browser signs out', () => {
      const signedOut = vi.fn();
      watchSignIn(ALICE, signedOut);
      change(SIGNED_IN_KEY, null);
      expect(signedOut).toHaveBeenCalledOnce();
    });

    it('ends it when another account or tenant signs in', () => {
      const signedOut = vi.fn();
      watchSignIn(ALICE, signedOut);
      change(SIGNED_IN_KEY, identityOf('bob', 'northwind'));
      change(SIGNED_IN_KEY, identityOf('alice', 'fabrikam'));
      expect(signedOut).toHaveBeenCalledTimes(2);
    });

    it('ends it when this browser’s storage is cleared', () => {
      const signedOut = vi.fn();
      watchSignIn(ALICE, signedOut);
      change(null, null);
      expect(signedOut).toHaveBeenCalledOnce();
    });

    it('ends it at once when this browser signed out before the watch began', () => {
      localStorage.removeItem(SIGNED_IN_KEY);
      const signedOut = vi.fn();
      watchSignIn(ALICE, signedOut);
      expect(signedOut).toHaveBeenCalledOnce();
    });

    it('ends it at once when another account signed in before the watch began', () => {
      localStorage.setItem(SIGNED_IN_KEY, identityOf('bob', 'northwind'));
      const signedOut = vi.fn();
      watchSignIn(ALICE, signedOut);
      expect(signedOut).toHaveBeenCalledOnce();
    });

    it('leaves it running when this browser’s storage cannot be read', () => {
      vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
        throw new DOMException('storage is blocked', 'SecurityError');
      });
      const signedOut = vi.fn();
      watchSignIn(ALICE, signedOut);
      window.dispatchEvent(new StorageEvent('storage', { key: null, newValue: null }));
      expect(signedOut).not.toHaveBeenCalled();
    });

    it('leaves it running when the same account signs in again or another key changes', () => {
      const signedOut = vi.fn();
      const stop = watchSignIn(ALICE, signedOut);
      change(SIGNED_IN_KEY, identityOf('alice', 'northwind'));
      change('opengate-organization', 'fabrikam');
      expect(signedOut).not.toHaveBeenCalled();

      stop();
      change(SIGNED_IN_KEY, null);
      expect(signedOut).not.toHaveBeenCalled();
    });
  });
});
