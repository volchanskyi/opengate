import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useAdminStore } from './admin-store';

const mockGet = vi.fn();
const mockPatch = vi.fn();
const mockDelete = vi.fn();

vi.mock('../lib/api', () => ({
  api: {
    GET: (...args: unknown[]) => mockGet(...args),
    PATCH: (...args: unknown[]) => mockPatch(...args),
    DELETE: (...args: unknown[]) => mockDelete(...args),
  },
}));

const fakeUser = {
  id: 'u1',
  email: 'admin@test.com',
  display_name: 'Admin',
  is_admin: true,
  created_at: '2024-01-01T00:00:00Z',
  updated_at: '2024-01-01T00:00:00Z',
};

const fakeAuditEvent = {
  id: 1,
  user_id: 'u1',
  action: 'user.login',
  target: 'admin@test.com',
  details: '',
  created_at: '2024-01-01T00:00:00Z',
};

describe('admin store', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useAdminStore.setState({
      users: [],
      auditEvents: [],
      isLoading: false,
      error: null,
    });
  });

  it('fetchUsers populates users array', async () => {
    mockGet.mockResolvedValueOnce({ data: [fakeUser], error: undefined });

    await useAdminStore.getState().fetchUsers();

    expect(useAdminStore.getState().users).toEqual([fakeUser]);
    expect(mockGet).toHaveBeenCalledWith('/api/v1/users');
  });

  it('fetchUsers handles error', async () => {
    mockGet.mockResolvedValueOnce({ data: undefined, error: { error: 'forbidden' } });

    await useAdminStore.getState().fetchUsers();

    expect(useAdminStore.getState().error).toBe('forbidden');
    expect(useAdminStore.getState().users).toEqual([]);
  });

  it('updateUser patches and refreshes users', async () => {
    mockPatch.mockResolvedValueOnce({ data: { ...fakeUser, display_name: 'Updated' }, error: undefined });
    mockGet.mockResolvedValueOnce({ data: [{ ...fakeUser, display_name: 'Updated' }], error: undefined });

    await useAdminStore.getState().updateUser('u1', { display_name: 'Updated' });

    expect(mockPatch).toHaveBeenCalledWith('/api/v1/users/{id}', {
      params: { path: { id: 'u1' } },
      body: { display_name: 'Updated' },
    });
  });

  it('deleteUser removes and refreshes users', async () => {
    mockDelete.mockResolvedValueOnce({ error: undefined });
    mockGet.mockResolvedValueOnce({ data: [], error: undefined });

    await useAdminStore.getState().deleteUser('u1');

    expect(mockDelete).toHaveBeenCalledWith('/api/v1/users/{id}', {
      params: { path: { id: 'u1' } },
    });
  });

  it('fetchAuditEvents populates events', async () => {
    mockGet.mockResolvedValueOnce({ data: [fakeAuditEvent], error: undefined });

    await useAdminStore.getState().fetchAuditEvents();

    expect(useAdminStore.getState().auditEvents).toEqual([fakeAuditEvent]);
    expect(mockGet).toHaveBeenCalledWith('/api/v1/audit', { params: { query: {} } });
  });

  it('fetchAuditEvents passes filters', async () => {
    mockGet.mockResolvedValueOnce({ data: [], error: undefined });

    await useAdminStore.getState().fetchAuditEvents({ action: 'user.login', limit: 10 });

    expect(mockGet).toHaveBeenCalledWith('/api/v1/audit', {
      params: { query: { action: 'user.login', limit: 10 } },
    });
  });

  it('fetchAuditEvents handles error', async () => {
    mockGet.mockResolvedValueOnce({ data: undefined, error: { error: 'forbidden' } });

    await useAdminStore.getState().fetchAuditEvents();

    expect(useAdminStore.getState().error).toBe('forbidden');
  });
});
