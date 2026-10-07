import { renderHook, waitFor } from '@testing-library/react';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { api } from '../../lib/api';
import { useHostNames } from './use-host-names';

vi.mock('../../lib/api', () => ({ api: { GET: vi.fn() } }));

const GET = vi.mocked(api.GET);

function device(id: string, hostname: string) {
  return {
    id, hostname, status: 'online', organization_id: 'org-1', site_id: '', os: 'linux', agent_version: '',
    capabilities: [], last_seen: '', created_at: '', updated_at: '',
  };
}

describe('useHostNames', () => {
  beforeEach(() => {
    GET.mockReset();
    GET.mockResolvedValue({ data: [device('fs01', 'file-server-01'), device('ws01', 'reception-pc')], response: { ok: true, status: 200 } } as never);
  });

  it('names the hosts of the chosen customer by id', async () => {
    const { result } = renderHook(() => useHostNames('org-1'));

    await waitFor(() => { expect(result.current.size).toBe(2); });
    expect(result.current.get('fs01')).toBe('file-server-01');
    expect(GET).toHaveBeenCalledWith('/api/v1/devices', { params: { query: { organization_id: 'org-1' } } });
  });

  it('names every host in the tenant when no customer is chosen', async () => {
    const { result } = renderHook(() => useHostNames(null));

    await waitFor(() => { expect(result.current.get('ws01')).toBe('reception-pc'); });
    expect(GET).toHaveBeenCalledWith('/api/v1/devices', { params: { query: {} } });
  });
});
