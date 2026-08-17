import { describe, it, expect, beforeEach, vi } from 'vitest';
import { api } from '../../../lib/api';
import { useOrganizationStore } from '../../organizations';
import { DEFAULT_QUEUE_FILTERS, useQueueStore } from './queue-store';
import type { components } from '../../../types/api';

vi.mock('../../../lib/api', () => ({ api: { GET: vi.fn() } }));

const mockedGet = vi.mocked(api.GET);

type Incident = components['schemas']['Incident'];

function incident(over: Partial<Incident> = {}): Incident {
  return {
    id: 'i1', organization_id: 'org-1', rule_id: 'cpu.sustained', scope: 'organization',
    scope_key: 'org-1', severity: 'critical', status: 'new',
    opened_at: '2026-08-12T09:00:00Z', first_seen: '2026-08-12T08:59:00Z',
    last_seen: '2026-08-12T09:30:00Z', occurrences: 312, device_count: 40, ...over,
  };
}

function page(items: Incident[], nextCursor?: string) {
  return {
    data: { items, ...(nextCursor ? { next_cursor: nextCursor } : {}) },
    error: undefined,
    response: { ok: true, status: 200 },
  };
}

const failure = {
  data: undefined,
  error: { error: 'the queue is unavailable' },
  response: { ok: false, status: 503 },
};

/** `api.GET` is overloaded per path, so its recorded calls need one loose shape. */
interface RecordedGet {
  path: string;
  params?: { path?: Record<string, string>; query?: Record<string, unknown> };
  querySerializer?: unknown;
}

function lastGet(): RecordedGet {
  const call = (mockedGet.mock.calls as unknown as [string, Omit<RecordedGet, 'path'>][]).at(-1);
  if (!call) throw new Error('no request was issued');
  return { path: call[0], ...call[1] };
}

/** The query the last GET carried. */
function lastQuery(): Record<string, unknown> {
  return lastGet().params?.query ?? {};
}

beforeEach(() => {
  vi.clearAllMocks();
  useOrganizationStore.setState({ selectedOrganizationId: null });
  useQueueStore.setState({
    items: [], nextCursor: null, loading: false, loaded: false, error: null, pagedOn: false,
    filters: DEFAULT_QUEUE_FILTERS, byDevice: new Map(), deviceErrors: new Map(),
  });
});

describe('queue-store — reading the queue', () => {
  it('opens on the three statuses that are still somebody’s problem', () => {
    expect(DEFAULT_QUEUE_FILTERS.status).toEqual(['new', 'acknowledged', 'investigating']);
    expect(DEFAULT_QUEUE_FILTERS.severity).toEqual([]);
  });

  it('reads the triage queue and keeps where the next page starts', async () => {
    mockedGet.mockResolvedValue(page([incident()], 'cursor-2') as never);
    await useQueueStore.getState().fetchQueue();

    expect(mockedGet).toHaveBeenCalledWith('/api/v1/investigations', expect.anything());
    expect(useQueueStore.getState().items).toHaveLength(1);
    expect(useQueueStore.getState().nextCursor).toBe('cursor-2');
    expect(useQueueStore.getState().loading).toBe(false);
    expect(useQueueStore.getState().error).toBeNull();
  });

  it('marks an empty queue as read, so "nothing to work" and "not asked" never look alike', async () => {
    mockedGet.mockResolvedValue(page([]) as never);
    await useQueueStore.getState().fetchQueue();
    expect(useQueueStore.getState().loaded).toBe(true);
    expect(useQueueStore.getState().items).toEqual([]);
  });

  it('stays unread after a failure, so a failed first read is not an empty queue', async () => {
    mockedGet.mockResolvedValue(failure as never);
    await useQueueStore.getState().fetchQueue();
    expect(useQueueStore.getState().loaded).toBe(false);
  });

  it('reports the end of the queue as no cursor rather than an empty one', async () => {
    mockedGet.mockResolvedValue(page([incident()]) as never);
    await useQueueStore.getState().fetchQueue();
    expect(useQueueStore.getState().nextCursor).toBeNull();
  });

  it('composes every filter into one read', async () => {
    mockedGet.mockResolvedValue(page([]) as never);
    useQueueStore.getState().setFilters({
      status: ['investigating'], severity: ['critical', 'warning'],
      ruleId: 'cpu.sustained', deviceId: 'dev-7',
    });
    await useQueueStore.getState().fetchQueue();

    expect(lastQuery()).toMatchObject({
      status: ['investigating'],
      severity: ['critical', 'warning'],
      rule_id: 'cpu.sustained',
      device_id: 'dev-7',
    });
  });

  it('omits a filter nobody set instead of sending an empty one', async () => {
    mockedGet.mockResolvedValue(page([]) as never);
    useQueueStore.getState().setFilters({ status: [], severity: [], ruleId: '', deviceId: '' });
    await useQueueStore.getState().fetchQueue();

    const query = lastQuery();
    expect(query).not.toHaveProperty('status');
    expect(query).not.toHaveProperty('severity');
    expect(query).not.toHaveProperty('rule_id');
    expect(query).not.toHaveProperty('device_id');
  });

  it('narrows to the customer the picker has selected', async () => {
    useOrganizationStore.setState({ selectedOrganizationId: 'org-9' });
    mockedGet.mockResolvedValue(page([]) as never);
    await useQueueStore.getState().fetchQueue();
    expect(lastQuery()).toMatchObject({ organization_id: 'org-9' });
  });

  it('reads the whole tenant when no customer is selected', async () => {
    mockedGet.mockResolvedValue(page([]) as never);
    await useQueueStore.getState().fetchQueue();
    expect(lastQuery()).not.toHaveProperty('organization_id');
  });

  it('surfaces the server’s message and keeps the queue it already had', async () => {
    useQueueStore.setState({ items: [incident()] });
    mockedGet.mockResolvedValue(failure as never);
    await useQueueStore.getState().fetchQueue();

    expect(useQueueStore.getState().error).toBe('the queue is unavailable');
    expect(useQueueStore.getState().items).toHaveLength(1);
    expect(useQueueStore.getState().loading).toBe(false);
  });

  it('re-reading from the top drops the cursor, so a filter change cannot page into the old queue', async () => {
    useQueueStore.setState({ nextCursor: 'cursor-2', items: [incident({ id: 'stale' })] });
    mockedGet.mockResolvedValue(page([incident({ id: 'fresh' })]) as never);
    await useQueueStore.getState().fetchQueue();

    expect(lastQuery()).not.toHaveProperty('cursor');
    expect(useQueueStore.getState().items.map((i) => i.id)).toEqual(['fresh']);
  });
});

describe('queue-store — paging by position', () => {
  it('appends the next page from the cursor the last one ended on', async () => {
    useQueueStore.setState({ items: [incident({ id: 'i1' })], nextCursor: 'cursor-2' });
    mockedGet.mockResolvedValue(page([incident({ id: 'i2' })], 'cursor-3') as never);
    await useQueueStore.getState().fetchMore();

    expect(lastQuery()).toMatchObject({ cursor: 'cursor-2' });
    expect(useQueueStore.getState().items.map((i) => i.id)).toEqual(['i1', 'i2']);
    expect(useQueueStore.getState().nextCursor).toBe('cursor-3');
  });

  it('records that somebody has read past the first page', async () => {
    useQueueStore.setState({ nextCursor: 'cursor-2' });
    mockedGet.mockResolvedValue(page([incident({ id: 'i2' })]) as never);
    await useQueueStore.getState().fetchMore();
    expect(useQueueStore.getState().pagedOn).toBe(true);
  });

  it('forgets it once the queue is read from the top again', async () => {
    useQueueStore.setState({ pagedOn: true });
    mockedGet.mockResolvedValue(page([incident()]) as never);
    await useQueueStore.getState().fetchQueue();
    expect(useQueueStore.getState().pagedOn).toBe(false);
  });

  it('does nothing at the end of the queue', async () => {
    useQueueStore.setState({ items: [incident()], nextCursor: null });
    await useQueueStore.getState().fetchMore();
    expect(mockedGet).not.toHaveBeenCalled();
  });

  it('does not stack a second page request on the first', async () => {
    useQueueStore.setState({ nextCursor: 'cursor-2' });
    let release!: (v: unknown) => void;
    mockedGet.mockReturnValue(new Promise((r) => { release = r; }) as never);
    const first = useQueueStore.getState().fetchMore();
    const second = useQueueStore.getState().fetchMore();
    release(page([]));
    await Promise.all([first, second]);
    expect(mockedGet).toHaveBeenCalledTimes(1);
  });
});

describe('queue-store — one machine’s incidents', () => {
  it('reads only the rooms still open, which is what a strip is for', async () => {
    mockedGet.mockResolvedValue(page([incident()]) as never);
    await useQueueStore.getState().fetchDeviceIncidents('dev-7');

    expect(lastGet().path).toBe('/api/v1/devices/{id}/incidents');
    expect(lastGet().params?.path).toEqual({ id: 'dev-7' });
    expect(lastQuery()).toMatchObject({ status: ['new', 'acknowledged', 'investigating'] });
    expect(useQueueStore.getState().byDevice.get('dev-7')).toHaveLength(1);
  });

  it('records an empty result as an answer, not as "not asked yet"', async () => {
    mockedGet.mockResolvedValue(page([]) as never);
    await useQueueStore.getState().fetchDeviceIncidents('dev-7');
    expect(useQueueStore.getState().byDevice.get('dev-7')).toEqual([]);
  });

  it('records a failure per device and caches nothing', async () => {
    mockedGet.mockResolvedValue(failure as never);
    await useQueueStore.getState().fetchDeviceIncidents('dev-7');

    expect(useQueueStore.getState().byDevice.get('dev-7')).toBeUndefined();
    expect(useQueueStore.getState().deviceErrors.get('dev-7')).toBe('the queue is unavailable');
  });
});
