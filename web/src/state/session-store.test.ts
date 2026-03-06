import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useSessionStore } from './session-store';

const mockPost = vi.fn();
const mockGet = vi.fn();
const mockDelete = vi.fn();

vi.mock('../lib/api', () => ({
  api: {
    POST: (...args: unknown[]) => mockPost(...args),
    GET: (...args: unknown[]) => mockGet(...args),
    DELETE: (...args: unknown[]) => mockDelete(...args),
  },
}));

describe('session store', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useSessionStore.setState({
      sessions: [],
      isLoading: false,
      error: null,
    });
  });

  it('fetchSessions with deviceId', async () => {
    mockGet.mockResolvedValueOnce({
      data: [{ token: 't1', device_id: 'd1', user_id: 'u1', created_at: '' }],
      error: undefined,
    });

    await useSessionStore.getState().fetchSessions('d1');

    expect(useSessionStore.getState().sessions).toHaveLength(1);
    expect(useSessionStore.getState().sessions[0]?.token).toBe('t1');
    expect(mockGet).toHaveBeenCalledWith('/api/v1/sessions', {
      params: { query: { device_id: 'd1' } },
    });
  });

  it('createSession returns token and relay_url', async () => {
    mockPost.mockResolvedValueOnce({
      data: { token: 'session-token', relay_url: 'ws://localhost/ws/relay/session-token' },
      error: undefined,
    });

    const result = await useSessionStore.getState().createSession('d1');

    expect(result).toEqual({
      token: 'session-token',
      relay_url: 'ws://localhost/ws/relay/session-token',
    });
  });

  it('createSession returns null on error', async () => {
    mockPost.mockResolvedValueOnce({
      data: undefined,
      error: { error: 'agent not connected' },
    });

    const result = await useSessionStore.getState().createSession('d1');

    expect(result).toBeNull();
    expect(useSessionStore.getState().error).toBe('agent not connected');
  });

  it('deleteSession removes from list', async () => {
    useSessionStore.setState({
      sessions: [
        { token: 't1', device_id: 'd1', user_id: 'u1', created_at: '' },
        { token: 't2', device_id: 'd1', user_id: 'u1', created_at: '' },
      ],
    });
    mockDelete.mockResolvedValueOnce({ error: undefined });

    await useSessionStore.getState().deleteSession('t1');

    expect(useSessionStore.getState().sessions).toHaveLength(1);
    expect(useSessionStore.getState().sessions[0]?.token).toBe('t2');
  });

  it('fetchSessions error sets error state', async () => {
    mockGet.mockResolvedValueOnce({
      data: undefined,
      error: { error: 'unauthorized' },
    });

    await useSessionStore.getState().fetchSessions('d1');

    expect(useSessionStore.getState().error).toBe('unauthorized');
  });
});
