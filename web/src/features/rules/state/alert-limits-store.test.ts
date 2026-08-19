import { describe, it, expect, beforeEach, vi } from 'vitest';
import { api } from '../../../lib/api';
import { useOrganizationStore } from '../../organizations';
import { useAlertLimitsStore } from './alert-limits-store';
import type { components } from '../../../types/api';

vi.mock('../../../lib/api', () => ({ api: { GET: vi.fn(), PUT: vi.fn() } }));

const mockedGet = vi.mocked(api.GET);
const mockedPut = vi.mocked(api.PUT);

type AlertLimits = components['schemas']['AlertLimits'];

function limits(over: Partial<AlertLimits> = {}): AlertLimits {
  return {
    organization_hourly: 500, device_hourly: 20,
    max_organization_hourly: 5000, max_device_hourly: 200,
    updated_by: '', ...over,
  };
}

const ok = (data: unknown) => ({ data, error: undefined, response: { ok: true, status: 200 } });
const refused = (message: string) => ({
  data: undefined, error: { error: message }, response: { ok: false, status: 400 },
});

function lastQuery(): Record<string, unknown> {
  const call = mockedGet.mock.calls.at(-1) as unknown as
    [string, { params?: { query?: Record<string, unknown> } }] | undefined;
  return call?.[1].params?.query ?? {};
}

beforeEach(() => {
  vi.clearAllMocks();
  useOrganizationStore.setState({ selectedOrganizationId: null });
  useAlertLimitsStore.setState({ limits: null, isLoading: false, error: null });
});

describe('alert-limits-store', () => {
  it('reads the budget with the maxima it may be raised to', async () => {
    mockedGet.mockResolvedValue(ok(limits()) as never);
    await useAlertLimitsStore.getState().fetchLimits();

    const got = useAlertLimitsStore.getState().limits;
    expect(got?.organization_hourly).toBe(500);
    expect(got?.max_organization_hourly).toBe(5000);
    expect(got?.max_device_hourly).toBe(200);
  });

  it('reads it for the customer the picker has selected — a budget is never the tenant\'s', async () => {
    useOrganizationStore.setState({ selectedOrganizationId: 'org-9' });
    mockedGet.mockResolvedValue(ok(limits()) as never);
    await useAlertLimitsStore.getState().fetchLimits();
    expect(lastQuery()).toEqual({ organization_id: 'org-9' });
  });

  it('keeps the stored budget the server answered with rather than what was typed', async () => {
    mockedPut.mockResolvedValue(ok(limits({ organization_hourly: 2000, device_hourly: 50 })) as never);

    expect(await useAlertLimitsStore.getState().saveLimits(2000, 50)).toBe(true);
    expect(useAlertLimitsStore.getState().limits?.organization_hourly).toBe(2000);
    expect(useAlertLimitsStore.getState().limits?.device_hourly).toBe(50);
  });

  it('reports a refused budget and leaves the stored one alone', async () => {
    useAlertLimitsStore.setState({ limits: limits() });
    mockedPut.mockResolvedValue(refused('the customer\'s hourly budget is 9000, outside 1–5000') as never);

    expect(await useAlertLimitsStore.getState().saveLimits(9000, 20)).toBe(false);
    expect(useAlertLimitsStore.getState().error).toContain('outside');
    expect(useAlertLimitsStore.getState().limits?.organization_hourly).toBe(500);
  });
});
