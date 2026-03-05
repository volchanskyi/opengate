import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useDeviceStore } from './device-store';

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

describe('device store', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useDeviceStore.setState({
      devices: [],
      groups: [],
      selectedGroupId: null,
      selectedDevice: null,
      isLoading: false,
      error: null,
    });
  });

  it('fetchGroups populates groups', async () => {
    mockGet.mockResolvedValueOnce({
      data: [{ id: 'g1', name: 'Group 1' }],
      error: undefined,
    });

    await useDeviceStore.getState().fetchGroups();

    expect(useDeviceStore.getState().groups).toHaveLength(1);
    expect(useDeviceStore.getState().groups[0]?.name).toBe('Group 1');
  });

  it('fetchDevices with groupId', async () => {
    mockGet.mockResolvedValueOnce({
      data: [{ id: 'd1', hostname: 'host1', status: 'online' }],
      error: undefined,
    });

    await useDeviceStore.getState().fetchDevices('g1');

    expect(useDeviceStore.getState().devices).toHaveLength(1);
    expect(mockGet).toHaveBeenCalledWith('/api/v1/devices', {
      params: { query: { group_id: 'g1' } },
    });
  });

  it('selectGroup triggers fetchDevices', async () => {
    mockGet.mockResolvedValueOnce({
      data: [{ id: 'd1', hostname: 'host1' }],
      error: undefined,
    });

    useDeviceStore.getState().selectGroup('g1');

    expect(useDeviceStore.getState().selectedGroupId).toBe('g1');
    // fetchDevices was called
    expect(mockGet).toHaveBeenCalledWith('/api/v1/devices', {
      params: { query: { group_id: 'g1' } },
    });
  });

  it('createGroup appends to list', async () => {
    useDeviceStore.setState({ groups: [{ id: 'g1', name: 'Existing', owner_id: 'u1', created_at: '', updated_at: '' }] });
    mockPost.mockResolvedValueOnce({
      data: { id: 'g2', name: 'New Group', owner_id: 'u1', created_at: '', updated_at: '' },
      error: undefined,
    });

    await useDeviceStore.getState().createGroup('New Group');

    expect(useDeviceStore.getState().groups).toHaveLength(2);
    expect(useDeviceStore.getState().groups[1]?.name).toBe('New Group');
  });

  it('deleteGroup removes from list', async () => {
    useDeviceStore.setState({
      groups: [
        { id: 'g1', name: 'A', owner_id: 'u1', created_at: '', updated_at: '' },
        { id: 'g2', name: 'B', owner_id: 'u1', created_at: '', updated_at: '' },
      ],
      selectedGroupId: 'g1',
    });
    mockDelete.mockResolvedValueOnce({ error: undefined });

    await useDeviceStore.getState().deleteGroup('g1');

    expect(useDeviceStore.getState().groups).toHaveLength(1);
    expect(useDeviceStore.getState().groups[0]?.id).toBe('g2');
    expect(useDeviceStore.getState().selectedGroupId).toBeNull();
  });

  it('fetchGroups error sets error state', async () => {
    mockGet.mockResolvedValueOnce({
      data: undefined,
      error: { error: 'unauthorized' },
    });

    await useDeviceStore.getState().fetchGroups();

    expect(useDeviceStore.getState().error).toBe('unauthorized');
  });

  it('fetchDevice populates selectedDevice', async () => {
    mockGet.mockResolvedValueOnce({
      data: { id: 'd1', hostname: 'host1', os: 'linux', status: 'online' },
      error: undefined,
    });

    await useDeviceStore.getState().fetchDevice('d1');

    expect(useDeviceStore.getState().selectedDevice?.hostname).toBe('host1');
  });

  it('deleteDevice removes from list', async () => {
    useDeviceStore.setState({
      devices: [
        { id: 'd1', group_id: 'g1', hostname: 'h1', os: 'linux', status: 'online', last_seen: '', created_at: '', updated_at: '' },
        { id: 'd2', group_id: 'g1', hostname: 'h2', os: 'linux', status: 'offline', last_seen: '', created_at: '', updated_at: '' },
      ],
    });
    mockDelete.mockResolvedValueOnce({ error: undefined });

    await useDeviceStore.getState().deleteDevice('d1');

    expect(useDeviceStore.getState().devices).toHaveLength(1);
    expect(useDeviceStore.getState().devices[0]?.id).toBe('d2');
  });
});
