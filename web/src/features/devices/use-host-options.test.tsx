import { renderHook, waitFor } from '@testing-library/react';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { api } from '../../lib/api';
import { useDeviceStore } from './state/device-store';
import { useHostOptions } from './use-host-options';

vi.mock('../../lib/api', () => ({ api: { GET: vi.fn() } }));

const GET = vi.mocked(api.GET);

function device(id: string, hostname: string, status: 'online' | 'offline' | 'connecting') {
  return {
    id, hostname, status, organization_id: 'org-1', site_id: '', os: 'linux', agent_version: '',
    capabilities: [], last_seen: '', created_at: '', updated_at: '',
  };
}

describe('useHostOptions', () => {
  beforeEach(() => {
    GET.mockReset();
    useDeviceStore.setState({ devices: [], selectedSiteId: 'site-front-desk' });
  });

  it('offers nothing and reads nothing until a customer is chosen', () => {
    const { result } = renderHook(() => useHostOptions(null));
    expect(result.current).toEqual([]);
    expect(GET).not.toHaveBeenCalled();
  });

  it("lists the customer's hosts by name, ignoring case, with each one's state", async () => {
    GET.mockResolvedValue({
      data: [
        device('d3', 'reception-pc', 'offline'),
        device('d1', 'Backup-01', 'online'),
        device('d4', 'reception-pc', 'online'),
        device('d2', 'archive', 'connecting'),
      ],
      response: { ok: true, status: 200 },
    } as never);

    const { result } = renderHook(() => useHostOptions('org-1'));

    await waitFor(() => { expect(result.current).toHaveLength(4); });
    expect(result.current).toEqual([
      { id: 'd2', name: 'archive', online: false },
      { id: 'd1', name: 'Backup-01', online: true },
      { id: 'd3', name: 'reception-pc', online: false },
      { id: 'd4', name: 'reception-pc', online: true },
    ]);
    expect(GET).toHaveBeenCalledWith('/api/v1/devices', { params: { query: { organization_id: 'org-1' } } });
  });

  it("leaves the Devices page's own list and site filter alone", async () => {
    GET.mockResolvedValue({ data: [device('d1', 'web-01', 'online')], response: { ok: true, status: 200 } } as never);

    const { result } = renderHook(() => useHostOptions('org-1'));

    await waitFor(() => { expect(result.current).toHaveLength(1); });
    expect(useDeviceStore.getState().devices).toEqual([]);
    expect(useDeviceStore.getState().selectedSiteId).toBe('site-front-desk');
  });

  it('offers nothing when the read is refused, rather than a stale list', async () => {
    GET.mockResolvedValue({ data: undefined, error: { error: 'unavailable' }, response: { ok: false, status: 503 } } as never);

    const { result } = renderHook(() => useHostOptions('org-refused'));

    await waitFor(() => { expect(GET).toHaveBeenCalled(); });
    expect(result.current).toEqual([]);
  });
});
