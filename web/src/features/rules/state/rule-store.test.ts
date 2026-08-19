import { describe, it, expect, beforeEach, vi } from 'vitest';
import { api } from '../../../lib/api';
import { useOrganizationStore } from '../../organizations';
import { useRuleStore } from './rule-store';
import type { components } from '../../../types/api';

vi.mock('../../../lib/api', () => ({
  api: { GET: vi.fn(), PUT: vi.fn(), POST: vi.fn(), DELETE: vi.fn() },
}));

const mockedGet = vi.mocked(api.GET);
const mockedPut = vi.mocked(api.PUT);
const mockedPost = vi.mocked(api.POST);
const mockedDelete = vi.mocked(api.DELETE);

type RuleDetail = components['schemas']['RuleDetail'];

function detail(threshold = 90): RuleDetail {
  return {
    rule: {
      id: 'disk-critical', version: 1, summary: 'A disk about to fill',
      metric: 'disk.used_percent', comparator: 'gte', threshold,
      group_by: ['device'], group_window_secs: 300, evidence: ['vitals'],
      coverage_requires: ['disk.used_percent'],
      tunable: { threshold: { min: 50, max: 99, shipped: 90 } },
      rollout: {
        enabled: true, rollout_percent: 100, kill: false, stage: 'full',
        canary_percent: 1, staged_percent: 10, canary_hold_secs: 3600, staged_hold_secs: 21600,
      },
      coverage: { active: 10, throttled: 0, unsupported: 0, unknown: 0 },
      noise: { recent: 0, baseline_per_hour: 0, level: 'unknown' },
    },
    bindings: [],
    clamps: [],
  };
}

const ok = (data: unknown) => ({ data, error: undefined, response: { ok: true, status: 200 } });
const noContent = () => ({ data: undefined, error: undefined, response: { ok: true, status: 204 } });
const refused = (message: string) => ({
  data: undefined, error: { error: message }, response: { ok: false, status: 400 },
});

function lastQuery(mock: { mock: { calls: unknown[][] } }): Record<string, unknown> {
  const call = mock.mock.calls.at(-1) as unknown as [string, { params?: { query?: Record<string, unknown> } }] | undefined;
  return call?.[1].params?.query ?? {};
}

/** The body of the most recent call, which is what a scope assertion is about. */
function lastBody(mock: { mock: { calls: unknown[][] } }): unknown {
  const call = mock.mock.calls.at(-1) as unknown as [string, { body?: unknown }] | undefined;
  return call?.[1].body;
}

beforeEach(() => {
  vi.clearAllMocks();
  useOrganizationStore.setState({ selectedOrganizationId: null });
  useRuleStore.setState({ detail: null, resolved: null, isLoading: false, error: null });
});

describe('rule-store', () => {
  it('reads one rule with its tuning and anything a version change moved', async () => {
    mockedGet.mockResolvedValue(ok(detail()) as never);
    await useRuleStore.getState().fetchRule('disk-critical');

    expect(mockedGet).toHaveBeenCalledWith('/api/v1/rules/{rule_id}', expect.anything());
    expect(useRuleStore.getState().detail?.rule.id).toBe('disk-critical');
    expect(useRuleStore.getState().isLoading).toBe(false);
  });

  it('reads it against the customer the picker has selected', async () => {
    useOrganizationStore.setState({ selectedOrganizationId: 'org-9' });
    mockedGet.mockResolvedValue(ok(detail()) as never);
    await useRuleStore.getState().fetchRule('disk-critical');
    expect(lastQuery(mockedGet)).toEqual({ organization_id: 'org-9' });
  });

  it('re-reads the rule after a write, because a page of resolved state cannot be patched locally', async () => {
    mockedPut.mockResolvedValue(ok({}) as never);
    mockedGet.mockResolvedValue(ok(detail(95)) as never);

    const saved = await useRuleStore.getState().saveBinding('disk-critical', {
      level: 'organization', level_key: 'org-9', params: { threshold: 95 },
    });

    expect(saved).toBe(true);
    expect(mockedGet).toHaveBeenCalledTimes(1);
    expect(useRuleStore.getState().detail?.rule.threshold).toBe(95);
  });

  it('reports a refused value and leaves the page as it was', async () => {
    useRuleStore.setState({ detail: detail() });
    mockedPut.mockResolvedValue(refused('threshold 100 is outside [50, 99]') as never);

    const saved = await useRuleStore.getState().saveBinding('disk-critical', {
      level: 'organization', level_key: 'org-9', params: { threshold: 100 },
    });

    expect(saved).toBe(false);
    expect(useRuleStore.getState().error).toContain('outside');
    expect(mockedGet).not.toHaveBeenCalled();
    expect(useRuleStore.getState().detail?.rule.threshold).toBe(90);
  });

  it('removes a tuned value and re-reads', async () => {
    mockedDelete.mockResolvedValue(noContent() as never);
    mockedGet.mockResolvedValue(ok(detail()) as never);

    expect(await useRuleStore.getState().removeBinding('disk-critical', 'binding-1')).toBe(true);
    expect(mockedDelete).toHaveBeenCalledWith(
      '/api/v1/rules/{rule_id}/bindings/{binding_id}', expect.anything());
    expect(mockedGet).toHaveBeenCalledTimes(1);
  });

  it('sets the pace a rule spreads at', async () => {
    mockedPut.mockResolvedValue(ok({}) as never);
    mockedGet.mockResolvedValue(ok(detail()) as never);

    expect(await useRuleStore.getState().saveRollout('disk-critical', {
      enabled: true, canary_percent: 5, staged_percent: 25,
      canary_hold_secs: 7200, staged_hold_secs: 43200,
    })).toBe(true);
    expect(mockedPut).toHaveBeenCalledWith('/api/v1/rules/{rule_id}/rollout', expect.anything());
  });

  it('stops a rule for one customer, and for every customer at once', async () => {
    mockedPost.mockResolvedValue(noContent() as never);
    mockedGet.mockResolvedValue(ok(detail()) as never);

    await useRuleStore.getState().setStopped('disk-critical', 'organization', true);
    expect(lastBody(mockedPost)).toEqual({ scope: 'organization', stopped: true });

    await useRuleStore.getState().setStopped('disk-critical', 'tenant', true);
    expect(lastBody(mockedPost)).toEqual({ scope: 'tenant', stopped: true });
  });

  it('acknowledges a value a new rule version had to move', async () => {
    mockedPost.mockResolvedValue(noContent() as never);
    mockedGet.mockResolvedValue(ok(detail()) as never);

    expect(await useRuleStore.getState().acknowledgeClamp('disk-critical', 'clamp-1')).toBe(true);
    expect(mockedPost).toHaveBeenCalledWith(
      '/api/v1/rules/{rule_id}/clamps/{clamp_id}', expect.anything());
  });

  it('resolves the rule for one named machine, and forgets it on request', async () => {
    mockedGet.mockResolvedValue(ok({
      rule_id: 'disk-critical', device_id: 'fs01', delivered: true,
      params: { threshold: { value: 95, level: 'site', source: "set on this machine's office" } },
    }) as never);

    await useRuleStore.getState().resolveFor('disk-critical', 'fs01');
    expect(useRuleStore.getState().resolved?.params.threshold?.value).toBe(95);

    useRuleStore.getState().clearResolved();
    expect(useRuleStore.getState().resolved).toBeNull();
  });
});
