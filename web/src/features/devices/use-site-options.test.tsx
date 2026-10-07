import { renderHook, waitFor } from '@testing-library/react';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { api } from '../../lib/api';
import { useSiteOptions } from './use-site-options';

vi.mock('../../lib/api', () => ({ api: { GET: vi.fn() } }));

const GET = vi.mocked(api.GET);

function site(id: string, name: string) {
  return { id, name, organization_id: 'org-1', created_at: '', updated_at: '' };
}

describe('useSiteOptions', () => {
  beforeEach(() => { GET.mockReset(); });

  it('offers nothing and reads nothing until a customer is chosen', () => {
    const { result } = renderHook(() => useSiteOptions(null));
    expect(result.current).toEqual([]);
    expect(GET).not.toHaveBeenCalled();
  });

  it("lists the customer's sites by name, ignoring case", async () => {
    GET.mockResolvedValue({
      data: [site('s2', 'front desk'), site('s1', 'Back Office'), site('s3', 'Annex')],
      response: { ok: true, status: 200 },
    } as never);

    const { result } = renderHook(() => useSiteOptions('org-1'));

    await waitFor(() => { expect(result.current).toHaveLength(3); });
    expect(result.current).toEqual([
      { id: 's3', name: 'Annex' },
      { id: 's1', name: 'Back Office' },
      { id: 's2', name: 'front desk' },
    ]);
    expect(GET).toHaveBeenCalledWith('/api/v1/sites', { params: { query: { organization_id: 'org-1' } } });
  });

  it('offers nothing when the read is refused', async () => {
    GET.mockResolvedValue({ data: undefined, error: { error: 'unavailable' }, response: { ok: false, status: 503 } } as never);

    const { result } = renderHook(() => useSiteOptions('org-refused'));

    await waitFor(() => { expect(GET).toHaveBeenCalled(); });
    expect(result.current).toEqual([]);
  });
});
