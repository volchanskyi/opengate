import { render, screen, waitFor } from '@testing-library/react';
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { usePushStore } from '../profile';
import { NotificationCenter } from './NotificationCenter';

// Every promise the component hands to fireAndForget, so a test can assert the
// handler settled cleanly rather than throwing into a swallowed rejection.
const { forwarded } = vi.hoisted(() => ({ forwarded: [] as Promise<unknown>[] }));

vi.mock('../../lib/fire-and-forget', () => ({
  fireAndForget: (p: Promise<unknown>) => { forwarded.push(p); p.catch(() => {}); },
}));

/** A push subscription as the browser hands it back. */
function createSubscription(endpoint: string, keys?: { p256dh: string; auth: string }) {
  return {
    endpoint,
    unsubscribe: vi.fn().mockResolvedValue(true),
    toJSON: () => ({ endpoint, keys }),
  };
}

/** Install a service-worker registration whose push manager the test controls. */
function installServiceWorker(pushManager: {
  subscribe?: ReturnType<typeof vi.fn>;
  getSubscription?: ReturnType<typeof vi.fn>;
}) {
  Object.defineProperty(navigator, 'serviceWorker', {
    value: { ready: Promise.resolve({ pushManager }) },
    configurable: true,
  });
}

function removeServiceWorker() {
  // @ts-expect-error -- jsdom has no serviceWorker; the tests install their own
  delete navigator.serviceWorker;
}

describe('NotificationCenter', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    usePushStore.setState({
      vapidKey: 'test-key',
      isSubscribed: false,
      fetchVapidKey: vi.fn().mockResolvedValue(undefined),
      syncSubscriptionStatus: vi.fn().mockResolvedValue(undefined),
      subscribe: vi.fn(),
      unsubscribe: vi.fn(),
    });

    // Mock PushManager as available
    Object.defineProperty(globalThis, 'PushManager', { value: class {}, configurable: true });
  });

  afterEach(() => {
    removeServiceWorker();
  });

  it('renders notification toggle button', () => {
    render(<NotificationCenter />);
    const button = screen.getByRole('button', { name: /enable notifications/i });
    expect(button).toBeInTheDocument();
    expect(button).toHaveAttribute('title', 'Enable notifications');
    // Bell with a slash: notifications are off.
    expect(button).toHaveTextContent('\u{1F515}');
  });

  it('shows subscribed state', () => {
    usePushStore.setState({ isSubscribed: true });
    render(<NotificationCenter />);
    const button = screen.getByRole('button', { name: /disable notifications/i });
    expect(button).toBeInTheDocument();
    expect(button).toHaveAttribute('title', 'Disable notifications');
    expect(button).toHaveTextContent('\u{1F514}');
  });

  it('returns null when PushManager is unavailable', () => {
    const saved = globalThis.PushManager;
    // @ts-expect-error -- deliberately removing PushManager for test
    delete globalThis.PushManager;
    const { container } = render(<NotificationCenter />);
    expect(container.innerHTML).toBe('');
    // Restore for other tests
    Object.defineProperty(globalThis, 'PushManager', { value: saved, configurable: true });
  });

  it('subscribes with the VAPID key decoded from base64url', async () => {
    // '-_8' is base64url for the bytes 0xFB 0xFF: it exercises both character
    // substitutions and the one-character padding the decoder has to add.
    usePushStore.setState({ vapidKey: '-_8' });
    const subscribe = vi.fn().mockResolvedValue(undefined);
    usePushStore.setState({ subscribe });

    const pushSubscribe = vi.fn().mockResolvedValue(
      createSubscription('https://push.example.com/sub-1', { p256dh: 'key-material', auth: 'auth-secret' }),
    );
    installServiceWorker({ subscribe: pushSubscribe });

    render(<NotificationCenter />);
    screen.getByRole('button').click();

    await waitFor(() => {
      expect(subscribe).toHaveBeenCalledWith('https://push.example.com/sub-1', 'key-material', 'auth-secret');
    });

    const options = pushSubscribe.mock.calls[0]?.[0] as {
      userVisibleOnly: boolean;
      applicationServerKey: ArrayBuffer;
    };
    expect(options.userVisibleOnly).toBe(true);
    expect([...new Uint8Array(options.applicationServerKey)]).toEqual([0xfb, 0xff]);
  });

  it('unsubscribes the active subscription when already subscribed', async () => {
    usePushStore.setState({ isSubscribed: true });
    const unsubscribe = vi.fn().mockResolvedValue(undefined);
    usePushStore.setState({ unsubscribe });

    const subscription = createSubscription('https://push.example.com/sub-2');
    const pushSubscribe = vi.fn();
    installServiceWorker({
      getSubscription: vi.fn().mockResolvedValue(subscription),
      subscribe: pushSubscribe,
    });

    render(<NotificationCenter />);
    screen.getByRole('button').click();

    await waitFor(() => {
      expect(unsubscribe).toHaveBeenCalledWith('https://push.example.com/sub-2');
    });
    expect(subscription.unsubscribe).toHaveBeenCalled();
    expect(pushSubscribe).not.toHaveBeenCalled();
  });

  it('does nothing when subscribed with no active browser subscription', async () => {
    usePushStore.setState({ isSubscribed: true });
    const unsubscribe = vi.fn();
    usePushStore.setState({ unsubscribe });

    const getSubscription = vi.fn().mockResolvedValue(null);
    const pushSubscribe = vi.fn();
    installServiceWorker({ getSubscription, subscribe: pushSubscribe });

    render(<NotificationCenter />);
    screen.getByRole('button').click();

    await waitFor(() => {
      expect(getSubscription).toHaveBeenCalled();
    });
    expect(unsubscribe).not.toHaveBeenCalled();
    expect(pushSubscribe).not.toHaveBeenCalled();
  });

  it('does not subscribe before the VAPID key has arrived', async () => {
    usePushStore.setState({ vapidKey: null });
    const pushSubscribe = vi.fn();
    const ready = vi.fn();
    // A recording `ready` getter shows the handler reached the registration and
    // then stopped, rather than never having run at all.
    Object.defineProperty(navigator, 'serviceWorker', {
      value: { get ready() { ready(); return Promise.resolve({ pushManager: { subscribe: pushSubscribe } }); } },
      configurable: true,
    });

    render(<NotificationCenter />);
    screen.getByRole('button').click();

    await waitFor(() => {
      expect(ready).toHaveBeenCalled();
    });
    expect(pushSubscribe).not.toHaveBeenCalled();
  });

  it('does nothing when the browser has no service worker', async () => {
    removeServiceWorker();
    const subscribe = vi.fn();
    usePushStore.setState({ subscribe });

    render(<NotificationCenter />);
    forwarded.length = 0;
    screen.getByRole('button').click();

    // The guard has to return before touching navigator.serviceWorker —
    // reaching it would reject with a TypeError instead of settling.
    const toggle = forwarded.at(-1);
    expect(toggle).toBeDefined();
    await expect(toggle).resolves.toBeUndefined();
    expect(subscribe).not.toHaveBeenCalled();
  });

  it('skips the server call when the browser subscription carries no keys', async () => {
    const subscribe = vi.fn();
    usePushStore.setState({ subscribe });

    const pushSubscribe = vi.fn().mockResolvedValue(
      createSubscription('https://push.example.com/sub-3'),
    );
    installServiceWorker({ subscribe: pushSubscribe });

    render(<NotificationCenter />);
    screen.getByRole('button').click();

    await waitFor(() => {
      expect(pushSubscribe).toHaveBeenCalled();
    });
    expect(subscribe).not.toHaveBeenCalled();
  });
});
