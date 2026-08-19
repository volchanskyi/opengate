import { describe, it, expect, beforeEach, vi } from 'vitest';
import { api } from '../../../lib/api';
import { useOrganizationStore } from '../../organizations';
import { useCatalogueStore } from './catalogue-store';
import type { components } from '../../../types/api';

vi.mock('../../../lib/api', () => ({ api: { GET: vi.fn() } }));

const mockedGet = vi.mocked(api.GET);

type Rule = components['schemas']['Rule'];

function rule(over: Partial<Rule> = {}): Rule {
  return {
    id: 'cpu.sustained', version: 3, summary: 'CPU pinned for two minutes',
    metric: 'cpu.busy_pct', comparator: 'gt', threshold: 90, group_by: ['device_id'],
    group_window_secs: 900, evidence: ['series'], coverage_requires: ['cpu.busy_pct'],
    tunable: {},
    rollout: {
      enabled: true, rollout_percent: 100, kill: false, stage: 'full',
      canary_percent: 1, staged_percent: 10, canary_hold_secs: 3600, staged_hold_secs: 21600,
    },
    coverage: { active: 300, throttled: 5, unsupported: 6, unknown: 1 },
    noise: { recent: 0, baseline_per_hour: 0, level: 'unknown' },
    ...over,
  };
}

const catalogue = (rules: Rule[], fleetSize: number) => ({
  data: { fleet_size: fleetSize, rules },
  error: undefined,
  response: { ok: true, status: 200 },
});

function lastQuery(): Record<string, unknown> {
  const call = (mockedGet.mock.calls as unknown as [string, { params?: { query?: Record<string, unknown> } }][]).at(-1);
  return call?.[1].params?.query ?? {};
}

beforeEach(() => {
  vi.clearAllMocks();
  useOrganizationStore.setState({ selectedOrganizationId: null });
  useCatalogueStore.setState({ rules: [], fleetSize: 0, loaded: false, loading: false, error: null });
});

describe('catalogue-store', () => {
  it('reads the curated pack with the coverage each rule has over the estate', async () => {
    mockedGet.mockResolvedValue(catalogue([rule()], 312) as never);
    await useCatalogueStore.getState().fetchCatalogue();

    expect(mockedGet).toHaveBeenCalledWith('/api/v1/rules', expect.anything());
    expect(useCatalogueStore.getState().rules).toHaveLength(1);
    expect(useCatalogueStore.getState().fleetSize).toBe(312);
    expect(useCatalogueStore.getState().loaded).toBe(true);
    expect(useCatalogueStore.getState().loading).toBe(false);
  });

  it('takes the counts against the customer the picker has selected', async () => {
    useOrganizationStore.setState({ selectedOrganizationId: 'org-9' });
    mockedGet.mockResolvedValue(catalogue([], 0) as never);
    await useCatalogueStore.getState().fetchCatalogue();
    expect(lastQuery()).toEqual({ organization_id: 'org-9' });
  });

  it('takes them against the whole tenant when no customer is selected', async () => {
    mockedGet.mockResolvedValue(catalogue([], 0) as never);
    await useCatalogueStore.getState().fetchCatalogue();
    expect(lastQuery()).toEqual({});
  });

  it('marks an empty catalogue as read, so "no rules" and "not asked" never look alike', async () => {
    mockedGet.mockResolvedValue(catalogue([], 0) as never);
    await useCatalogueStore.getState().fetchCatalogue();
    expect(useCatalogueStore.getState().loaded).toBe(true);
    expect(useCatalogueStore.getState().rules).toEqual([]);
  });

  it('surfaces the server’s message and stays unread', async () => {
    mockedGet.mockResolvedValue({ data: undefined, error: { error: 'catalogue unavailable' }, response: { ok: false, status: 500 } } as never);
    await useCatalogueStore.getState().fetchCatalogue();

    expect(useCatalogueStore.getState().error).toBe('catalogue unavailable');
    expect(useCatalogueStore.getState().loaded).toBe(false);
    expect(useCatalogueStore.getState().loading).toBe(false);
  });

  it('does not stack a second read on one already in flight', async () => {
    let release!: (v: unknown) => void;
    mockedGet.mockReturnValue(new Promise((r) => { release = r; }) as never);
    const first = useCatalogueStore.getState().fetchCatalogue();
    const second = useCatalogueStore.getState().fetchCatalogue();
    release(catalogue([rule()], 312));
    await Promise.all([first, second]);
    expect(mockedGet).toHaveBeenCalledTimes(1);
  });
});
