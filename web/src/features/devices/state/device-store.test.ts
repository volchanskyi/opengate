import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useToastStore } from '../../../lib/feedback/toast-store';
import { useDeviceStore } from './device-store';

const mockPost = vi.fn();
const mockGet = vi.fn();
const mockDelete = vi.fn();
const mockPatch = vi.fn();
const addToast = vi.fn();

vi.mock('../../../lib/api', () => ({
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
    useToastStore.setState({ addToast });
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

  it('fetchLogs never sends a refresh param (the broker is always live)', async () => {
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

  it('fetchLogs raises its dedicated loading flag before the broker responds', async () => {
    let resolve!: (value: unknown) => void;
    mockGet.mockReturnValueOnce(new Promise((r) => { resolve = r; }));

    const pending = useDeviceStore.getState().fetchLogs('d1');
    expect(useDeviceStore.getState().logsLoading).toBe(true);

    resolve({ data: { entries: [], total: 0, has_more: false }, response: { status: 200 } });
    await pending;
    expect(useDeviceStore.getState().logsLoading).toBe(false);
  });

  it.each([
    [403, 'Viewing device logs requires administrator access.'],
    [404, 'Logs unavailable — device offline or not found.'],
    [409, 'A log request is already in progress for this device.'],
    [504, 'The device did not return logs in time.'],
    [500, 'Failed to fetch logs.'],
  ])('fetchLogs reports the exact broker error for status %s', async (status, message) => {
    mockGet.mockResolvedValueOnce({ data: undefined, response: { status } });

    await useDeviceStore.getState().fetchLogs('d1');

    expect(addToast).toHaveBeenCalledExactlyOnceWith(message, 'error');
  });

  it.each([403, 404, 409, 504, 500])(
    'fetchLogs clears loading and leaves logs null on %s',
    async (status) => {
      mockGet.mockResolvedValueOnce({ data: undefined, response: { status } });

      await useDeviceStore.getState().fetchLogs('d1');

      // Synchronous broker: a single request, no retry.
      expect(mockGet).toHaveBeenCalledTimes(1);
      expect(useDeviceStore.getState().logsLoading).toBe(false);
      expect(useDeviceStore.getState().logs).toBeNull();
    },
  );

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
    expect(addToast).toHaveBeenCalledExactlyOnceWith(
      'Failed to refresh hardware: network failure',
      'error',
    );
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
    expect(addToast).toHaveBeenCalledExactlyOnceWith(
      'Failed to refresh hardware: string rejection',
      'error',
    );
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

  it('restartAgent returns false on an empty-bodied 409 (no JSON error)', async () => {
    // Regression: openapi-fetch leaves error undefined for an empty 409 body,
    // which a response-blind apiAction would misread as success.
    mockPost.mockResolvedValueOnce({ data: undefined, error: undefined, response: { ok: false, status: 409 } });

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
        },
      },
    });
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

  const deviceIn = (over: Partial<import('../../../types/api').components['schemas']['Device']> = {}) => ({
    id: 'd1', group_id: 'g1', hostname: 'host1', os: 'linux', agent_version: '1.0.0',
    capabilities: [], status: 'online' as const, last_seen: '', created_at: '', updated_at: '', ...over,
  });

  it('setMaintenance posts enabled + reason and updates selectedDevice', async () => {
    useDeviceStore.setState({ selectedDevice: deviceIn() });
    mockPost.mockResolvedValueOnce({
      data: deviceIn({ maintenance_on: true, maintenance_since: '2026-07-19T00:00:00Z', maintenance_reason: 'kernel upgrade' }),
      error: undefined,
    });

    const ok = await useDeviceStore.getState().setMaintenance('d1', true, 'kernel upgrade');

    expect(ok).toBe(true);
    expect(mockPost).toHaveBeenCalledWith('/api/v1/devices/{id}/maintenance', {
      params: { path: { id: 'd1' } },
      body: { enabled: true, reason: 'kernel upgrade' },
    });
    expect(useDeviceStore.getState().selectedDevice?.maintenance_on).toBe(true);
    expect(useDeviceStore.getState().selectedDevice?.maintenance_reason).toBe('kernel upgrade');
  });

  it('setMaintenance omits an empty reason from the request body', async () => {
    useDeviceStore.setState({ selectedDevice: deviceIn({ maintenance_on: true }) });
    mockPost.mockResolvedValueOnce({ data: deviceIn(), error: undefined });

    await useDeviceStore.getState().setMaintenance('d1', false);

    // Exit carries no reason — kills a mutant that always spreads a reason key.
    expect(mockPost).toHaveBeenCalledWith('/api/v1/devices/{id}/maintenance', {
      params: { path: { id: 'd1' } },
      body: { enabled: false },
    });
  });

  it('setMaintenance updates the matching device in the list', async () => {
    useDeviceStore.setState({
      devices: [deviceIn({ id: 'd1' }), deviceIn({ id: 'd2', hostname: 'other' })],
      selectedDevice: null,
    });
    mockPost.mockResolvedValueOnce({ data: deviceIn({ id: 'd1', maintenance_on: true }), error: undefined });

    await useDeviceStore.getState().setMaintenance('d1', true);

    const devices = useDeviceStore.getState().devices;
    expect(devices.find((d) => d.id === 'd1')?.maintenance_on).toBe(true);
    // The sibling device is untouched — kills a mutant that rewrites every row.
    expect(devices.find((d) => d.id === 'd2')?.maintenance_on).toBeUndefined();
  });

  it('setMaintenance returns false and does not mutate on error', async () => {
    const original = deviceIn({ maintenance_on: false });
    useDeviceStore.setState({ selectedDevice: original, devices: [original] });
    mockPost.mockResolvedValueOnce({ data: undefined, error: { error: 'forbidden' } });

    const ok = await useDeviceStore.getState().setMaintenance('d1', true);

    expect(ok).toBe(false);
    // Kills `if (res.ok)` → `if (true)` mutant on setMaintenance.
    expect(useDeviceStore.getState().selectedDevice?.maintenance_on).toBe(false);
    expect(useDeviceStore.getState().devices[0]?.maintenance_on).toBe(false);
  });

  it('setMaintenance never toggles isLoading (silent mutation)', async () => {
    mockPost.mockResolvedValueOnce({ data: deviceIn(), error: undefined });
    const { peak } = await captureIsLoading(() => useDeviceStore.getState().setMaintenance('d1', true));
    expect(peak).toBe(false);
  });

  it('fetchMaintenanceSummary sets the fleet in-maintenance count', async () => {
    mockGet.mockResolvedValueOnce({ data: { count: 4 }, error: undefined });

    await useDeviceStore.getState().fetchMaintenanceSummary();

    expect(mockGet).toHaveBeenCalledWith('/api/v1/devices/maintenance-summary');
    expect(useDeviceStore.getState().maintenanceCount).toBe(4);
  });

  it('fetchMaintenanceSummary leaves the count unchanged on error', async () => {
    useDeviceStore.setState({ maintenanceCount: 2 });
    mockGet.mockResolvedValueOnce({ data: undefined, error: { error: 'unauthorized' } });

    await useDeviceStore.getState().fetchMaintenanceSummary();

    // Kills `if (res.ok)` → `if (true)` mutant (would set count to undefined→NaN).
    expect(useDeviceStore.getState().maintenanceCount).toBe(2);
  });
});
