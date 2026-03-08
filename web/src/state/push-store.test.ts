import { describe, it, expect, beforeEach, vi } from 'vitest';
import { usePushStore } from './push-store';

const mockGet = vi.fn();
const mockPost = vi.fn();
const mockDelete = vi.fn();

vi.mock('../lib/api', () => ({
  api: {
    GET: (...args: unknown[]) => mockGet(...args),
    POST: (...args: unknown[]) => mockPost(...args),
    DELETE: (...args: unknown[]) => mockDelete(...args),
  },
}));

describe('push store', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    usePushStore.setState({
      vapidKey: null,
      isSubscribed: false,
      error: null,
    });
  });

  it('fetchVapidKey stores public key', async () => {
    mockGet.mockResolvedValueOnce({ data: { public_key: 'vapid-pub-123' }, error: undefined });

    await usePushStore.getState().fetchVapidKey();

    expect(usePushStore.getState().vapidKey).toBe('vapid-pub-123');
    expect(mockGet).toHaveBeenCalledWith('/api/v1/push/vapid-key');
  });

  it('fetchVapidKey handles error', async () => {
    mockGet.mockResolvedValueOnce({ data: undefined, error: { error: 'unauthorized' } });

    await usePushStore.getState().fetchVapidKey();

    expect(usePushStore.getState().vapidKey).toBeNull();
    expect(usePushStore.getState().error).toBe('unauthorized');
  });

  it('subscribe calls API and sets subscribed', async () => {
    mockPost.mockResolvedValueOnce({ error: undefined });

    await usePushStore.getState().subscribe('https://push.example.com/sub', 'p256dh-key', 'auth-key');

    expect(mockPost).toHaveBeenCalledWith('/api/v1/push/subscribe', {
      body: { endpoint: 'https://push.example.com/sub', p256dh: 'p256dh-key', auth: 'auth-key' },
    });
    expect(usePushStore.getState().isSubscribed).toBe(true);
  });

  it('subscribe handles error', async () => {
    mockPost.mockResolvedValueOnce({ error: { error: 'failed' } });

    await usePushStore.getState().subscribe('https://push.example.com/sub', 'p256dh-key', 'auth-key');

    expect(usePushStore.getState().isSubscribed).toBe(false);
    expect(usePushStore.getState().error).toBe('failed');
  });

  it('unsubscribe calls API and clears subscribed', async () => {
    usePushStore.setState({ isSubscribed: true });
    mockDelete.mockResolvedValueOnce({ error: undefined });

    await usePushStore.getState().unsubscribe('https://push.example.com/sub');

    expect(mockDelete).toHaveBeenCalledWith('/api/v1/push/subscribe', {
      body: { endpoint: 'https://push.example.com/sub' },
    });
    expect(usePushStore.getState().isSubscribed).toBe(false);
  });
});
