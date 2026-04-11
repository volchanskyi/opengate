import { render, screen } from '@testing-library/react';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { usePushStore } from '../../state/push-store';
import { NotificationCenter } from './NotificationCenter';

vi.mock('../../lib/fire-and-forget', () => ({
  fireAndForget: (p: Promise<unknown>) => { p.catch(() => {}); },
}));

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

  it('renders notification toggle button', () => {
    render(<NotificationCenter />);
    expect(screen.getByRole('button', { name: /enable notifications/i })).toBeInTheDocument();
  });

  it('shows subscribed state', () => {
    usePushStore.setState({ isSubscribed: true });
    render(<NotificationCenter />);
    expect(screen.getByRole('button', { name: /disable notifications/i })).toBeInTheDocument();
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
});
