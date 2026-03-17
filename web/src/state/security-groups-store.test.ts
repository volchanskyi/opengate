import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useSecurityGroupsStore } from './security-groups-store';

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

const fakeGroup = {
  id: 'g1',
  name: 'Administrators',
  description: 'Full system access',
  is_system: true,
  created_at: '2024-01-01T00:00:00Z',
  updated_at: '2024-01-01T00:00:00Z',
};

const fakeUser = {
  id: 'u1',
  email: 'admin@test.com',
  display_name: 'Admin',
  is_admin: true,
  created_at: '2024-01-01T00:00:00Z',
  updated_at: '2024-01-01T00:00:00Z',
};

const fakeGroupWithMembers = {
  ...fakeGroup,
  members: [fakeUser],
};

describe('security-groups-store', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useSecurityGroupsStore.setState({
      groups: [],
      selectedGroup: null,
      users: [],
      isLoading: false,
      error: null,
    });
  });

  it('fetchGroups populates groups array', async () => {
    mockGet.mockResolvedValueOnce({ data: [fakeGroup], error: undefined });

    await useSecurityGroupsStore.getState().fetchGroups();

    expect(useSecurityGroupsStore.getState().groups).toEqual([fakeGroup]);
    expect(mockGet).toHaveBeenCalledWith('/api/v1/security-groups');
  });

  it('fetchGroups handles error', async () => {
    mockGet.mockResolvedValueOnce({ data: undefined, error: { error: 'forbidden' } });

    await useSecurityGroupsStore.getState().fetchGroups();

    expect(useSecurityGroupsStore.getState().error).toBe('forbidden');
    expect(useSecurityGroupsStore.getState().groups).toEqual([]);
  });

  it('fetchGroupDetail sets selectedGroup', async () => {
    mockGet.mockResolvedValueOnce({ data: fakeGroupWithMembers, error: undefined });

    await useSecurityGroupsStore.getState().fetchGroupDetail('g1');

    expect(useSecurityGroupsStore.getState().selectedGroup).toEqual(fakeGroupWithMembers);
    expect(mockGet).toHaveBeenCalledWith('/api/v1/security-groups/{id}', {
      params: { path: { id: 'g1' } },
    });
  });

  it('fetchGroupDetail handles error', async () => {
    mockGet.mockResolvedValueOnce({ data: undefined, error: { error: 'not found' } });

    await useSecurityGroupsStore.getState().fetchGroupDetail('bad');

    expect(useSecurityGroupsStore.getState().error).toBe('not found');
  });

  it('fetchUsers populates users array', async () => {
    mockGet.mockResolvedValueOnce({ data: [fakeUser], error: undefined });

    await useSecurityGroupsStore.getState().fetchUsers();

    expect(useSecurityGroupsStore.getState().users).toEqual([fakeUser]);
    expect(mockGet).toHaveBeenCalledWith('/api/v1/users');
  });

  it('fetchUsers handles error', async () => {
    mockGet.mockResolvedValueOnce({ data: undefined, error: { error: 'forbidden' } });

    await useSecurityGroupsStore.getState().fetchUsers();

    expect(useSecurityGroupsStore.getState().error).toBe('forbidden');
  });

  it('addMember posts and refreshes group detail', async () => {
    mockPost.mockResolvedValueOnce({ error: undefined });
    mockGet.mockResolvedValueOnce({ data: fakeGroupWithMembers, error: undefined });

    useSecurityGroupsStore.setState({ selectedGroup: { ...fakeGroup, members: [] } });

    await useSecurityGroupsStore.getState().addMember('g1', 'u1');

    expect(mockPost).toHaveBeenCalledWith('/api/v1/security-groups/{id}/members', {
      params: { path: { id: 'g1' } },
      body: { user_id: 'u1' },
    });
    expect(useSecurityGroupsStore.getState().selectedGroup).toEqual(fakeGroupWithMembers);
  });

  it('addMember handles error', async () => {
    mockPost.mockResolvedValueOnce({ error: { error: 'conflict' } });

    await useSecurityGroupsStore.getState().addMember('g1', 'u1');

    expect(useSecurityGroupsStore.getState().error).toBe('conflict');
  });

  it('removeMember deletes and refreshes group detail', async () => {
    mockDelete.mockResolvedValueOnce({ error: undefined });
    mockGet.mockResolvedValueOnce({ data: { ...fakeGroup, members: [] }, error: undefined });

    useSecurityGroupsStore.setState({ selectedGroup: fakeGroupWithMembers });

    await useSecurityGroupsStore.getState().removeMember('g1', 'u1');

    expect(mockDelete).toHaveBeenCalledWith('/api/v1/security-groups/{id}/members/{userId}', {
      params: { path: { id: 'g1', userId: 'u1' } },
    });
    expect(useSecurityGroupsStore.getState().selectedGroup?.members).toEqual([]);
  });

  it('removeMember handles error', async () => {
    mockDelete.mockResolvedValueOnce({ error: { error: 'last admin' } });

    await useSecurityGroupsStore.getState().removeMember('g1', 'u1');

    expect(useSecurityGroupsStore.getState().error).toBe('last admin');
  });
});
