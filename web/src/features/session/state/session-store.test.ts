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
      sessionsDeviceId: null,
      isLoading: false,
      error: null,
    });
  });

  it('fetchSessions with deviceId', async () => {
    mockGet.mockResolvedValueOnce({
      data: [{ device_id: 'd1', user_id: 'u1', holder_name: 'Alice', created_at: '' }],
      error: undefined,
    });

    await useSessionStore.getState().fetchSessions('d1');

    expect(useSessionStore.getState().sessions).toHaveLength(1);
    expect(useSessionStore.getState().sessions[0]?.holder_name).toBe('Alice');
    expect(mockGet).toHaveBeenCalledWith('/api/v1/sessions', {
      params: { query: { device_id: 'd1' } },
    });
  });

  it('startSession opens a session with every permission and hands back where to meet', async () => {
    mockPost.mockResolvedValueOnce({
      data: {
        token: 'session-token',
        relay_url: 'wss://edge.test/ws/relay/session-token',
        ice_servers: [{ urls: ['stun:quic.example.test:9090'] }, { urls: ['stun:b.test:3478', 'stun:c.test:3478'] }],
      },
      error: undefined,
      response: { status: 201 },
    });

    const result = await useSessionStore.getState().startSession('d1');

    expect(result).toEqual({
      kind: 'opened',
      token: 'session-token',
      relayUrl: 'wss://edge.test/ws/relay/session-token',
      helpers: ['stun:quic.example.test:9090', 'stun:b.test:3478', 'stun:c.test:3478'],
    });
    expect(mockPost).toHaveBeenCalledWith('/api/v1/sessions', {
      body: {
        device_id: 'd1',
        permissions: { desktop: true, terminal: true, file_read: true, file_write: true, input: true },
      },
    });
  });

  it('startSession names the technician holding the host when it is refused', async () => {
    mockPost.mockResolvedValueOnce({
      data: undefined,
      error: { error: 'device has an open session', holder_name: 'Alice', started_at: '2026-10-12T14:02:00Z' },
      response: { status: 409 },
    });

    expect(await useSessionStore.getState().startSession('d1')).toEqual({
      kind: 'held',
      holderName: 'Alice',
      startedAt: '2026-10-12T14:02:00Z',
    });
  });

  it('startSession says why it failed otherwise', async () => {
    mockPost.mockResolvedValueOnce({
      data: undefined,
      error: { error: 'agent not connected' },
      response: { status: 404 },
    });

    expect(await useSessionStore.getState().startSession('d1')).toEqual({
      kind: 'failed',
      error: 'agent not connected',
    });
  });

  it('fetchSessions clears prior list before fetching new device', async () => {
    useSessionStore.setState({
      sessions: [{ device_id: 'd0', user_id: 'u1', holder_name: 'Alice', created_at: '' }],
      sessionsDeviceId: 'd0',
    });
    mockGet.mockResolvedValueOnce({
      data: [{ device_id: 'd1', user_id: 'u2', holder_name: 'Bob', created_at: '' }],
      error: undefined,
    });

    const promise = useSessionStore.getState().fetchSessions('d1');
    expect(useSessionStore.getState().sessions).toEqual([]);
    await promise;
    expect(useSessionStore.getState().sessions).toEqual([
      { device_id: 'd1', user_id: 'u2', holder_name: 'Bob', created_at: '' },
    ]);
  });

  it('a repeat fetch for the same device leaves the list up while it reloads', async () => {
    // The device page re-reads its sessions on every poll; blanking the list
    // first would flash the holder out of the card twice a minute.
    const held = { device_id: 'd1', user_id: 'u1', holder_name: 'Alice', created_at: '' };
    useSessionStore.setState({ sessions: [held], sessionsDeviceId: 'd1' });
    mockGet.mockResolvedValueOnce({ data: [held], error: undefined });

    const promise = useSessionStore.getState().fetchSessions('d1');
    expect(useSessionStore.getState().sessions).toHaveLength(1);
    await promise;
    expect(useSessionStore.getState().sessions).toHaveLength(1);
  });

  it('endDeviceSession ends the host’s session and clears the list', async () => {
    useSessionStore.setState({
      sessions: [{ device_id: 'd1', user_id: 'u1', holder_name: 'Alice', created_at: '' }],
      sessionsDeviceId: 'd1',
    });
    mockDelete.mockResolvedValueOnce({ error: undefined, response: { ok: true, status: 204 } });

    expect(await useSessionStore.getState().endDeviceSession('d1')).toBe(true);
    expect(mockDelete).toHaveBeenCalledWith('/api/v1/devices/{id}/session', { params: { path: { id: 'd1' } } });
    expect(useSessionStore.getState().sessions).toEqual([]);
  });

  it('endDeviceSession keeps the list when it is refused', async () => {
    useSessionStore.setState({
      sessions: [{ device_id: 'd1', user_id: 'u1', holder_name: 'Alice', created_at: '' }],
      sessionsDeviceId: 'd1',
    });
    mockDelete.mockResolvedValueOnce({ error: { error: 'forbidden' }, response: { ok: false, status: 403 } });

    expect(await useSessionStore.getState().endDeviceSession('d1')).toBe(false);
    expect(useSessionStore.getState().sessions).toHaveLength(1);
    expect(useSessionStore.getState().error).toBe('forbidden');
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
