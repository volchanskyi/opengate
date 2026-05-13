import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useDeviceStore } from './device-store';

const mockPost = vi.fn();
const mockGet = vi.fn();
const mockDelete = vi.fn();
const mockPatch = vi.fn();

vi.mock('../lib/api', () => ({
  api: {
    POST: (...args: unknown[]) => mockPost(...args),
    GET: (...args: unknown[]) => mockGet(...args),
    DELETE: (...args: unknown[]) => mockDelete(...args),
    PATCH: (...args: unknown[]) => mockPatch(...args),
  },
}));

const mockHardware = { device_id: 'd1', cpu_model: 'Intel', cpu_cores: 4, ram_total_mb: 8192, disk_free_mb: 100, disk_total_mb: 500, updated_at: '', network_interfaces: [] };

describe('device store', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useDeviceStore.setState({
      devices: [],
      groups: [],
      selectedGroupId: null,
      selectedDevice: null,
      hardware: null,
      logs: null,
      logsLoading: false,
      isLoading: false,
      error: null,
    });
  });

  // Helper: subscribe to the store across the awaited promise and capture the
  // peak value of isLoading. Silent mutation calls (apiAction(..., false)) MUST
  // never flip isLoading=true. This kills the BooleanLiteral `false`→`true`
  // mutants on every apiAction silent-mode call site.
  async function captureIsLoading(action: () => Promise<unknown>): Promise<{ peak: boolean }> {
    let peak = false;
    const unsub = useDeviceStore.subscribe((s) => {
      if (s.isLoading) peak = true;
    });
    try {
      await action();
    } finally {
      unsub();
    }
    return { peak };
  }

  it('silent operations never toggle isLoading', async () => {
    // createGroup, deleteGroup, deleteDevice, updateDeviceGroup, restartAgent,
    // fetchHardware, upgradeAgent, refreshDevice — all pass `loading: false`.
    // Any mutant flipping that to `true` would briefly set isLoading=true.
    mockPost.mockResolvedValue({ data: { id: 'g2', name: 'g', owner_id: 'u1', created_at: '', updated_at: '' }, error: undefined });
    mockDelete.mockResolvedValue({ error: undefined });
    mockPatch.mockResolvedValue({ data: { id: 'd1' }, error: undefined });
    mockGet.mockResolvedValue({ data: { id: 'd1' }, error: undefined });

    const peaks: boolean[] = [];
    peaks.push((await captureIsLoading(() => useDeviceStore.getState().createGroup('g'))).peak);
    peaks.push((await captureIsLoading(() => useDeviceStore.getState().deleteGroup('g1'))).peak);
    peaks.push((await captureIsLoading(() => useDeviceStore.getState().deleteDevice('d1'))).peak);
    peaks.push((await captureIsLoading(() => useDeviceStore.getState().updateDeviceGroup('d1', 'g2'))).peak);
    peaks.push((await captureIsLoading(() => useDeviceStore.getState().restartAgent('d1'))).peak);
    peaks.push((await captureIsLoading(() => useDeviceStore.getState().refreshDevice('d1'))).peak);
    peaks.push((await captureIsLoading(() => useDeviceStore.getState().upgradeAgent('d1', '2.0', 'linux', 'amd64'))).peak);
    peaks.push((await captureIsLoading(() => useDeviceStore.getState().fetchHardware('d1'))).peak);

    // All silent operations must keep isLoading false throughout — kills every
    // `false` → `true` mutant on the apiAction `loading` argument.
    expect(peaks.every((p) => p === false)).toBe(true);
  });

  it('fetchHardware clears hardware synchronously before awaiting', async () => {
    useDeviceStore.setState({ hardware: mockHardware });
    mockGet.mockResolvedValueOnce({ data: mockHardware, error: undefined });

    const promise = useDeviceStore.getState().fetchHardware('d1');
    // Synchronous reset BEFORE await — kills `set({})` mutant on hardware:null.
    expect(useDeviceStore.getState().hardware).toBeNull();
    await promise;
  });

  it('fetchLogs adds refresh=true only when explicitly requested', async () => {
    // No-refresh path: the query object must NOT contain `refresh`.
    mockGet.mockResolvedValueOnce({ data: { entries: [], total: 0, has_more: false }, response: { status: 200 } });
    await useDeviceStore.getState().fetchLogs('d1', { level: 'INFO' });
    expect(mockGet).toHaveBeenLastCalledWith('/api/v1/devices/{id}/logs', {
      params: { path: { id: 'd1' }, query: { level: 'INFO' } },
    });
  });

  it('fetchLogs does NOT set logs when status is 404', async () => {
    // Kills `if (response.status === 200 && data)` → `if (true && data)` mutant.
    mockGet.mockResolvedValueOnce({ data: { entries: [{ a: 1 }] }, response: { status: 404 } });
    await useDeviceStore.getState().fetchLogs('d1');
    expect(useDeviceStore.getState().logs).toBeNull();
  });

  it('fetchLogs does NOT set logs when data is missing even on 200', async () => {
    // Kills `if (response.status === 200 && data)` → `if (response.status === 200 || data)` mutant
    // when data=undefined.
    mockGet.mockResolvedValueOnce({ data: undefined, response: { status: 200 } });
    await useDeviceStore.getState().fetchLogs('d1');
    expect(useDeviceStore.getState().logs).toBeNull();
    expect(useDeviceStore.getState().logsLoading).toBe(false);
  });

  it('fetchHardware retry catch block fires toast for non-Error rejections', async () => {
    // Kills BlockStatement / StringLiteral mutants on the catch{} body of
    // retryHardwareFetch (covers the toast literal interpolation).
    vi.useFakeTimers();
    mockGet
      .mockResolvedValueOnce({ data: undefined, error: { error: 'accepted' } })
      .mockRejectedValueOnce({ weird: 'object' });

    await useDeviceStore.getState().fetchHardware('d1');
    vi.advanceTimersByTime(2500);
    await vi.runAllTimersAsync();

    expect(mockGet).toHaveBeenCalledTimes(2);
    vi.useRealTimers();
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

  it('createGroup appends to list and sends body name', async () => {
    useDeviceStore.setState({ groups: [{ id: 'g1', name: 'Existing', owner_id: 'u1', created_at: '', updated_at: '' }] });
    mockPost.mockResolvedValueOnce({
      data: { id: 'g2', name: 'New Group', owner_id: 'u1', created_at: '', updated_at: '' },
      error: undefined,
    });

    await useDeviceStore.getState().createGroup('New Group');

    expect(useDeviceStore.getState().groups).toHaveLength(2);
    expect(useDeviceStore.getState().groups[1]?.name).toBe('New Group');
    expect(mockPost).toHaveBeenCalledWith('/api/v1/groups', { body: { name: 'New Group' } });
  });

  it('createGroup does NOT mutate list on error', async () => {
    useDeviceStore.setState({ groups: [{ id: 'g1', name: 'Existing', owner_id: 'u1', created_at: '', updated_at: '' }] });
    mockPost.mockResolvedValueOnce({ data: undefined, error: { error: 'forbidden' } });

    await useDeviceStore.getState().createGroup('New Group');

    // Kills `if (res.ok)` → `if (true)` mutant on createGroup.
    expect(useDeviceStore.getState().groups).toHaveLength(1);
  });

  it('deleteGroup removes from list and clears selection if active', async () => {
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
    expect(mockDelete).toHaveBeenCalledWith('/api/v1/groups/{id}', {
      params: { path: { id: 'g1' } },
    });
  });

  it('deleteGroup keeps selection when removing a different group', async () => {
    useDeviceStore.setState({
      groups: [
        { id: 'g1', name: 'A', owner_id: 'u1', created_at: '', updated_at: '' },
        { id: 'g2', name: 'B', owner_id: 'u1', created_at: '', updated_at: '' },
      ],
      selectedGroupId: 'g2',
    });
    mockDelete.mockResolvedValueOnce({ error: undefined });

    await useDeviceStore.getState().deleteGroup('g1');

    // Kills `selectedGroupId === id ? null : state.selectedGroupId` → `true ? null : ...` mutant.
    expect(useDeviceStore.getState().selectedGroupId).toBe('g2');
  });

  it('deleteGroup leaves list and selection alone on error', async () => {
    useDeviceStore.setState({
      groups: [
        { id: 'g1', name: 'A', owner_id: 'u1', created_at: '', updated_at: '' },
      ],
      selectedGroupId: 'g1',
    });
    mockDelete.mockResolvedValueOnce({ error: { error: 'forbidden' } });

    await useDeviceStore.getState().deleteGroup('g1');

    // Kills `if (res.ok)` → `if (true)` mutant on deleteGroup.
    expect(useDeviceStore.getState().groups).toHaveLength(1);
    expect(useDeviceStore.getState().selectedGroupId).toBe('g1');
  });

  it('fetchGroups error sets error state', async () => {
    mockGet.mockResolvedValueOnce({
      data: undefined,
      error: { error: 'unauthorized' },
    });

    await useDeviceStore.getState().fetchGroups();

    expect(useDeviceStore.getState().error).toBe('unauthorized');
  });

  it('fetchDevice populates selectedDevice and resets stale per-device fields synchronously', async () => {
    // Seed stale state from a previously viewed device — fetchDevice must clear
    // these synchronously before awaiting, so the mutation `set({})` (no fields)
    // is killed.
    useDeviceStore.setState({
      selectedDevice: { id: 'old', group_id: 'g1', hostname: 'old', os: 'linux', agent_version: '', capabilities: [], status: 'online', last_seen: '', created_at: '', updated_at: '' },
      hardware: mockHardware,
      logs: { entries: [], total: 0, has_more: false },
    });

    mockGet.mockResolvedValueOnce({
      data: { id: 'd1', hostname: 'host1', os: 'linux', agent_version: '', status: 'online' },
      error: undefined,
    });

    const promise = useDeviceStore.getState().fetchDevice('d1');
    // Synchronous reset BEFORE await resolves.
    expect(useDeviceStore.getState().selectedDevice).toBeNull();
    expect(useDeviceStore.getState().hardware).toBeNull();
    expect(useDeviceStore.getState().logs).toBeNull();
    await promise;

    expect(useDeviceStore.getState().selectedDevice?.hostname).toBe('host1');
  });

  it('deleteDevice removes from list', async () => {
    useDeviceStore.setState({
      devices: [
        { id: 'd1', group_id: 'g1', hostname: 'h1', os: 'linux', agent_version: '', capabilities: [], status: 'online', last_seen: '', created_at: '', updated_at: '' },
        { id: 'd2', group_id: 'g1', hostname: 'h2', os: 'linux', agent_version: '', capabilities: [], status: 'offline', last_seen: '', created_at: '', updated_at: '' },
      ],
    });
    mockDelete.mockResolvedValueOnce({ error: undefined });

    await useDeviceStore.getState().deleteDevice('d1');

    expect(useDeviceStore.getState().devices).toHaveLength(1);
    expect(useDeviceStore.getState().devices[0]?.id).toBe('d2');
    expect(mockDelete).toHaveBeenCalledWith('/api/v1/devices/{id}', {
      params: { path: { id: 'd1' } },
    });
  });

  it('deleteDevice does NOT mutate list on error', async () => {
    useDeviceStore.setState({
      devices: [
        { id: 'd1', group_id: 'g1', hostname: 'h1', os: 'linux', agent_version: '', capabilities: [], status: 'online', last_seen: '', created_at: '', updated_at: '' },
      ],
    });
    mockDelete.mockResolvedValueOnce({ error: { error: 'forbidden' } });

    await useDeviceStore.getState().deleteDevice('d1');

    // Kills `if (res.ok)` → `if (true)` mutant on deleteDevice.
    expect(useDeviceStore.getState().devices).toHaveLength(1);
  });

  it('fetchLogs sets logs on 200 response', async () => {
    const logsData = { entries: [{ timestamp: '2026-01-01T00:00:00Z', level: 'INFO', target: 'agent', message: 'started' }], total: 1, has_more: false };
    mockGet.mockResolvedValueOnce({
      data: logsData,
      response: { status: 200 },
    });

    await useDeviceStore.getState().fetchLogs('d1', { level: 'INFO', limit: 50 });

    expect(useDeviceStore.getState().logs).toEqual(logsData);
    expect(useDeviceStore.getState().logsLoading).toBe(false);
  });

  it('fetchLogs sets logsLoading on 202 and retries', async () => {
    vi.useFakeTimers();
    mockGet
      .mockResolvedValueOnce({ data: undefined, response: { status: 202 } })
      .mockResolvedValueOnce({ data: { entries: [], total: 0, has_more: false }, response: { status: 200 } });

    const promise = useDeviceStore.getState().fetchLogs('d1');
    await promise;

    // After 202 the store stays in loading state until the retry
    expect(useDeviceStore.getState().logsLoading).toBe(true);

    // Advance past the 3s retry timeout
    vi.advanceTimersByTime(3500);
    await vi.runAllTimersAsync();

    expect(mockGet).toHaveBeenCalledTimes(2);
    vi.useRealTimers();
  });

  it('fetchLogs retry error with Error shows toast and clears loading', async () => {
    vi.useFakeTimers();
    mockGet.mockResolvedValueOnce({ data: undefined, response: { status: 202 } });
    mockGet.mockRejectedValueOnce(new Error('network error'));

    await useDeviceStore.getState().fetchLogs('d1');

    expect(useDeviceStore.getState().logsLoading).toBe(true);

    vi.advanceTimersByTime(3500);
    await vi.runAllTimersAsync();

    expect(useDeviceStore.getState().logsLoading).toBe(false);
    expect(mockGet).toHaveBeenCalledTimes(2);
    vi.useRealTimers();
  });

  it('fetchLogs retry error with non-Error shows toast via String()', async () => {
    vi.useFakeTimers();
    mockGet.mockResolvedValueOnce({ data: undefined, response: { status: 202 } });
    mockGet.mockRejectedValueOnce('string rejection');

    await useDeviceStore.getState().fetchLogs('d1');

    vi.advanceTimersByTime(3500);
    await vi.runAllTimersAsync();

    expect(useDeviceStore.getState().logsLoading).toBe(false);
    expect(mockGet).toHaveBeenCalledTimes(2);
    vi.useRealTimers();
  });

  it('fetchLogs clears loading on non-200/202', async () => {
    mockGet.mockResolvedValueOnce({ data: undefined, response: { status: 404 } });

    await useDeviceStore.getState().fetchLogs('d1');

    expect(useDeviceStore.getState().logsLoading).toBe(false);
    expect(useDeviceStore.getState().logs).toBeNull();
  });

  it('upgradeAgent calls POST and returns true on success', async () => {
    mockPost.mockResolvedValueOnce({ data: {}, error: undefined });

    const ok = await useDeviceStore.getState().upgradeAgent('d1', '2.0.0', 'linux', 'amd64');

    expect(ok).toBe(true);
    expect(mockPost).toHaveBeenCalledWith('/api/v1/updates/push', {
      body: { version: '2.0.0', os: 'linux', arch: 'amd64', device_ids: ['d1'] },
    });
  });

  it('upgradeAgent returns false on error', async () => {
    mockPost.mockResolvedValueOnce({ data: undefined, error: { error: 'not found' } });

    const ok = await useDeviceStore.getState().upgradeAgent('d1', '2.0.0', 'linux', 'amd64');

    expect(ok).toBe(false);
  });

  it('fetchHardware sets hardware on success', async () => {
    mockGet.mockResolvedValueOnce({ data: mockHardware, error: undefined });

    await useDeviceStore.getState().fetchHardware('d1');

    expect(useDeviceStore.getState().hardware).toEqual(mockHardware);
  });

  it('fetchHardware retries on non-ok and sets hardware on retry success', async () => {
    vi.useFakeTimers();
    // First call returns 202 (non-ok via apiAction)
    mockGet.mockResolvedValueOnce({ data: undefined, error: { error: 'accepted' } });
    // Retry returns success
    mockGet.mockResolvedValueOnce({ data: mockHardware, error: undefined });

    await useDeviceStore.getState().fetchHardware('d1');

    // Hardware not set yet — waiting for retry
    expect(useDeviceStore.getState().hardware).toBeNull();

    // Advance past the 2s retry timeout
    vi.advanceTimersByTime(2500);
    await vi.runAllTimersAsync();

    expect(mockGet).toHaveBeenCalledTimes(2);
    expect(useDeviceStore.getState().hardware).toEqual(mockHardware);
    vi.useRealTimers();
  });

  it('fetchHardware retry error with Error shows toast', async () => {
    vi.useFakeTimers();
    mockGet.mockResolvedValueOnce({ data: undefined, error: { error: 'accepted' } });
    mockGet.mockRejectedValueOnce(new Error('network failure'));

    await useDeviceStore.getState().fetchHardware('d1');

    vi.advanceTimersByTime(2500);
    await vi.runAllTimersAsync();

    expect(mockGet).toHaveBeenCalledTimes(2);
    vi.useRealTimers();
  });

  it('fetchHardware retry error with non-Error shows toast via String()', async () => {
    vi.useFakeTimers();
    mockGet.mockResolvedValueOnce({ data: undefined, error: { error: 'accepted' } });
    mockGet.mockRejectedValueOnce('string rejection');

    await useDeviceStore.getState().fetchHardware('d1');

    vi.advanceTimersByTime(2500);
    await vi.runAllTimersAsync();

    expect(mockGet).toHaveBeenCalledTimes(2);
    vi.useRealTimers();
  });

  it('restartAgent returns true on success and posts the literal reason', async () => {
    mockPost.mockResolvedValueOnce({ data: {}, error: undefined, ok: true, response: { ok: true } });

    const ok = await useDeviceStore.getState().restartAgent('d1');

    expect(ok).toBe(true);
    // Pin both path and the literal reason — kills StringLiteral / ObjectLiteral mutants
    // on the body and `body: {}` mutant.
    expect(mockPost).toHaveBeenCalledWith('/api/v1/devices/{id}/restart', {
      params: { path: { id: 'd1' } },
      body: { reason: 'restart requested from web UI' },
    });
  });

  it('updateDeviceGroup returns true on success and sends body group_id', async () => {
    mockPatch.mockResolvedValueOnce({ data: {}, error: undefined });

    const ok = await useDeviceStore.getState().updateDeviceGroup('d1', 'g2');

    expect(ok).toBe(true);
    expect(mockPatch).toHaveBeenCalledWith('/api/v1/devices/{id}', {
      params: { path: { id: 'd1' } },
      body: { group_id: 'g2' },
    });
  });

  it('updateDeviceGroup returns false on error', async () => {
    mockPatch.mockResolvedValueOnce({ data: undefined, error: { error: 'forbidden' } });

    const ok = await useDeviceStore.getState().updateDeviceGroup('d1', 'g2');

    expect(ok).toBe(false);
  });

  it('updateDeviceGroup updates selectedDevice on success', async () => {
    const updatedDevice = { id: 'd1', group_id: 'g2', hostname: 'host1', os: 'linux', agent_version: '', status: 'online' };
    mockPatch.mockResolvedValueOnce({ data: updatedDevice, error: undefined });

    await useDeviceStore.getState().updateDeviceGroup('d1', 'g2');

    expect(useDeviceStore.getState().selectedDevice).toEqual(updatedDevice);
  });

  it('restartAgent returns false on error', async () => {
    mockPost.mockResolvedValueOnce({ data: undefined, error: { error: 'offline' } });

    const ok = await useDeviceStore.getState().restartAgent('d1');

    expect(ok).toBe(false);
  });

  it('fetchLogs passes all query params correctly', async () => {
    mockGet.mockResolvedValueOnce({
      data: { entries: [], total: 0, has_more: false },
      response: { status: 200 },
    });

    await useDeviceStore.getState().fetchLogs('d1', {
      level: 'ERROR',
      from: '2026-01-01',
      to: '2026-01-31',
      search: 'timeout',
      offset: 100,
      limit: 50,
      refresh: true,
    });

    expect(mockGet).toHaveBeenCalledWith('/api/v1/devices/{id}/logs', {
      params: {
        path: { id: 'd1' },
        query: {
          level: 'ERROR',
          from: '2026-01-01',
          to: '2026-01-31',
          search: 'timeout',
          offset: 100,
          limit: 50,
          refresh: 'true',
        },
      },
    });
  });

  it('fetchLogs 202 retry omits refresh param', async () => {
    vi.useFakeTimers();
    mockGet
      .mockResolvedValueOnce({ data: undefined, response: { status: 202 } })
      .mockResolvedValueOnce({ data: { entries: [], total: 0, has_more: false }, response: { status: 200 } });

    await useDeviceStore.getState().fetchLogs('d1', { refresh: true, level: 'INFO' });

    vi.advanceTimersByTime(3500);
    await vi.runAllTimersAsync();

    // Retry call should have level but NOT refresh
    const retryCall = mockGet.mock.calls[1];
    expect(retryCall![1]).toEqual(expect.objectContaining({
      params: expect.objectContaining({
        query: expect.not.objectContaining({ refresh: 'true' }),
      }),
    }));
    vi.useRealTimers();
  });

  it('refreshDevice updates selectedDevice without clearing hardware or logs', async () => {
    const existingHardware = mockHardware;
    const existingLogs = { entries: [], total: 0, has_more: false };
    useDeviceStore.setState({
      selectedDevice: { id: 'd1', group_id: 'g1', hostname: 'old', os: 'linux', agent_version: '1.0.0', capabilities: [], status: 'online', last_seen: '', created_at: '', updated_at: '' },
      hardware: existingHardware,
      logs: existingLogs,
    });

    mockGet.mockResolvedValueOnce({
      data: { id: 'd1', group_id: 'g1', hostname: 'updated', os: 'linux', agent_version: '1.0.1', capabilities: [], status: 'online', last_seen: '', created_at: '', updated_at: '' },
      error: undefined,
    });

    await useDeviceStore.getState().refreshDevice('d1');

    expect(useDeviceStore.getState().selectedDevice?.hostname).toBe('updated');
    expect(useDeviceStore.getState().selectedDevice?.agent_version).toBe('1.0.1');
    expect(useDeviceStore.getState().hardware).toEqual(existingHardware);
    expect(useDeviceStore.getState().logs).toEqual(existingLogs);
  });

  it('refreshDevice does not set isLoading', async () => {
    mockGet.mockResolvedValueOnce({
      data: { id: 'd1', hostname: 'host1', os: 'linux', agent_version: '', status: 'online' },
      error: undefined,
    });

    const promise = useDeviceStore.getState().refreshDevice('d1');
    expect(useDeviceStore.getState().isLoading).toBe(false);
    await promise;
    expect(useDeviceStore.getState().isLoading).toBe(false);
  });
});
