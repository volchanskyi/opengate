import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useSessionStore } from './session-store';

const mockPost = vi.fn();
const mockGet = vi.fn();
const mockDelete = vi.fn();

vi.mock('../../../lib/api', () => ({
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

  it('initial state', () => {
    const fresh = useSessionStore.getState();
    expect(fresh.sessions).toEqual([]);
    expect(fresh.isLoading).toBe(false);
    expect(fresh.error).toBeNull();
  });

  it('createSession returns token and relay_url and sends ALL permissions=true', async () => {
    mockPost.mockResolvedValueOnce({
      data: { token: 'session-token', relay_url: 'ws://localhost/ws/relay/session-token' },
      error: undefined,
    });

    const result = await useSessionStore.getState().createSession('d1');

    expect(result).toEqual({
      token: 'session-token',
      relay_url: 'ws://localhost/ws/relay/session-token',
    });
    // Pin every permission flag literally — kills BooleanLiteral mutants on each.
    expect(mockPost).toHaveBeenCalledWith('/api/v1/sessions', {
      body: {
        device_id: 'd1',
        permissions: {
          desktop: true,
          terminal: true,
          file_read: true,
          file_write: true,
          input: true,
        },
      },
    });
  });

  it('fetchSessions clears prior list before fetching new device', async () => {
    // Seed prior sessions, then fetch with a new deviceId; after the synchronous
    // clear the prior list must be gone *before* the await resolves.
    useSessionStore.setState({
      sessions: [{ token: 'old', device_id: 'd0', user_id: 'u1', created_at: '' }],
    });
    mockGet.mockResolvedValueOnce({
      data: [{ token: 'new', device_id: 'd1', user_id: 'u1', created_at: '' }],
      error: undefined,
    });

    const promise = useSessionStore.getState().fetchSessions('d1');
    // Synchronous clear pins `set({ sessions: [] })` → kills `set({})` mutant.
    expect(useSessionStore.getState().sessions).toEqual([]);
    await promise;
    expect(useSessionStore.getState().sessions).toEqual([
      { token: 'new', device_id: 'd1', user_id: 'u1', created_at: '' },
    ]);
  });

  it('deleteSession does NOT mutate list on error', async () => {
    useSessionStore.setState({
      sessions: [
        { token: 't1', device_id: 'd1', user_id: 'u1', created_at: '' },
        { token: 't2', device_id: 'd1', user_id: 'u1', created_at: '' },
      ],
    });
    mockDelete.mockResolvedValueOnce({ error: { error: 'forbidden' } });

    await useSessionStore.getState().deleteSession('t1');

    // Both sessions must remain — kills `if (res.ok)` → `if (true)` mutant.
    expect(useSessionStore.getState().sessions).toHaveLength(2);
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
