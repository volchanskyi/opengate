import { describe, it, expect, beforeEach, vi } from 'vitest';
import { api } from '../../../lib/api';
import { useOrganizationStore } from '../../organizations';
import { useRoomStore } from './room-store';
import type { components } from '../../../types/api';

vi.mock('../../../lib/api', () => ({ api: { GET: vi.fn(), POST: vi.fn() } }));

const mockedGet = vi.mocked(api.GET);
const mockedPost = vi.mocked(api.POST);

type Incident = components['schemas']['Incident'];
type IncidentAlert = components['schemas']['IncidentAlert'];
type IncidentDetail = components['schemas']['IncidentDetail'];
type IncidentEvent = components['schemas']['IncidentEvent'];
type AlertEvidence = components['schemas']['AlertEvidence'];

function incident(over: Partial<Incident> = {}): Incident {
  return {
    id: 'i1', organization_id: 'org-1', rule_id: 'cpu.sustained', scope: 'organization',
    scope_key: 'org-1', severity: 'critical', status: 'new',
    opened_at: '2026-08-12T09:00:00Z', first_seen: '2026-08-12T08:59:00Z',
    last_seen: '2026-08-12T09:30:00Z', occurrences: 312, device_count: 40, ...over,
  };
}

function alert(over: Partial<IncidentAlert> = {}): IncidentAlert {
  return {
    id: 'a1', device_id: 'dev-7', rule_id: 'cpu.sustained', rule_version: 3, severity: 'critical',
    window_start: '2026-08-12T09:00:00Z', window_end: '2026-08-12T09:01:00Z',
    observed_at: '2026-08-12T09:00:30Z', received_at: '2026-08-12T09:00:45Z',
    backfilled: false, evidence_bytes: 4096, ...over,
  };
}

function detail(over: Partial<IncidentDetail> = {}): IncidentDetail {
  return {
    incident: incident(), alerts: [alert()], alerts_total: 1,
    events: [], events_total: 0, ...over,
  };
}

const evidence: AlertEvidence = {
  ranked: [{ dim: 'cpu.busy_pct', score: 0.94 }],
  series: [{ dim: 'cpu.busy_pct', points: [{ ts: 1, value: 90 }, { ts: 2, value: 96 }] }],
  processes: [{ rank: 1, basename: 'chrome', pid: 4242, cpu: 88.5, mem: 12.5 }],
  log_samples: ['kernel: task blocked for 120s'],
  truncated: false,
};

const ok = <T,>(data: T) => ({ data, error: undefined, response: { ok: true, status: 200 } });
const fail = (message: string, status = 400) => ({
  data: undefined, error: { error: message }, response: { ok: false, status },
});

interface RecordedCall {
  path: string;
  params?: { path?: Record<string, string>; query?: Record<string, unknown> };
  body?: unknown;
}

function recorded(mock: typeof mockedGet | typeof mockedPost): RecordedCall[] {
  return (mock.mock.calls as unknown as [string, Omit<RecordedCall, 'path'>][])
    .map(([path, init]) => ({ path, ...init }));
}

const allPaths = () => [...recorded(mockedGet), ...recorded(mockedPost)].map((c) => c.path);
const lastPost = () => recorded(mockedPost).at(-1);

function resetRoom() {
  useRoomStore.setState({
    detail: null, loading: false, error: null, actionError: null, acting: false,
    evidence: new Map(), evidenceLoading: new Map(), evidenceErrors: new Map(),
  });
}

beforeEach(() => {
  vi.clearAllMocks();
  useOrganizationStore.setState({ selectedOrganizationId: null });
  resetRoom();
});

describe('room-store — opening a room', () => {
  it('reads the incident, its alerts and its history in one call', async () => {
    mockedGet.mockResolvedValue(ok(detail()) as never);
    await useRoomStore.getState().open('i1');

    expect(recorded(mockedGet).at(0)?.path).toBe('/api/v1/investigations/{id}');
    expect(recorded(mockedGet).at(0)?.params?.path).toEqual({ id: 'i1' });
    expect(useRoomStore.getState().detail?.incident.id).toBe('i1');
    expect(useRoomStore.getState().loading).toBe(false);
  });

  it('narrows to the customer the picker has selected', async () => {
    useOrganizationStore.setState({ selectedOrganizationId: 'org-9' });
    mockedGet.mockResolvedValue(ok(detail()) as never);
    await useRoomStore.getState().open('i1');
    expect(recorded(mockedGet).at(0)?.params?.query).toEqual({ organization_id: 'org-9' });
  });

  it('drops the previous room before reading the next, so evidence never leaks across rooms', async () => {
    useRoomStore.setState({
      detail: detail({ incident: incident({ id: 'other' }) }),
      evidence: new Map([['a1', evidence]]),
      evidenceErrors: new Map([['a2', 'gone']]),
    });
    mockedGet.mockResolvedValue(ok(detail()) as never);
    await useRoomStore.getState().open('i1');

    expect(useRoomStore.getState().evidence.size).toBe(0);
    expect(useRoomStore.getState().evidenceErrors.size).toBe(0);
  });

  it('surfaces the server’s message and leaves no half-room behind', async () => {
    mockedGet.mockResolvedValue(fail('no such incident', 404) as never);
    await useRoomStore.getState().open('i1');

    expect(useRoomStore.getState().error).toBe('no such incident');
    expect(useRoomStore.getState().detail).toBeNull();
    expect(useRoomStore.getState().loading).toBe(false);
  });

  it('refreshing keeps the evidence already fetched, unlike opening', async () => {
    useRoomStore.setState({ detail: detail(), evidence: new Map([['a1', evidence]]) });
    mockedGet.mockResolvedValue(ok(detail({ alerts_total: 9 })) as never);
    await useRoomStore.getState().refresh('i1');

    expect(useRoomStore.getState().detail?.alerts_total).toBe(9);
    expect(useRoomStore.getState().evidence.get('a1')).toEqual(evidence);
  });

  it('leaving the room drops everything it held', () => {
    useRoomStore.setState({ detail: detail(), evidence: new Map([['a1', evidence]]), error: 'x' });
    useRoomStore.getState().leave();

    expect(useRoomStore.getState().detail).toBeNull();
    expect(useRoomStore.getState().evidence.size).toBe(0);
    expect(useRoomStore.getState().error).toBeNull();
  });
});

describe('room-store — nothing is ever asked of the machine', () => {
  it('issues only investigation reads and writes, on every path the room offers', async () => {
    mockedGet.mockResolvedValue(ok(detail()) as never);
    mockedPost.mockResolvedValue(ok(incident({ status: 'acknowledged' })) as never);

    const room = useRoomStore.getState();
    await room.open('i1');
    await room.fetchEvidence('i1', 'a1');
    await room.setStatus('i1', 'acknowledged');
    await room.setAssignee('i1', 'user-3');
    mockedPost.mockResolvedValue(ok({ id: 'e9', at: '2026-08-12T10:00:00Z', kind: 'comment', body: { body: 'hi' } }) as never);
    await room.addComment('i1', 'hi');

    expect(allPaths().length).toBeGreaterThan(0);
    for (const path of allPaths()) {
      expect(path.startsWith('/api/v1/investigations')).toBe(true);
    }
  });
});

describe('room-store — evidence, frozen at write time', () => {
  it('fetches one alert’s evidence and caches it under that alert', async () => {
    mockedGet.mockResolvedValue(ok(evidence) as never);
    await useRoomStore.getState().fetchEvidence('i1', 'a1');

    const call = recorded(mockedGet).at(0);
    expect(call?.path).toBe('/api/v1/investigations/{id}/alerts/{alertId}/evidence');
    expect(call?.params?.path).toEqual({ id: 'i1', alertId: 'a1' });
    expect(useRoomStore.getState().evidence.get('a1')).toEqual(evidence);
    expect(useRoomStore.getState().evidenceLoading.get('a1')).toBe(false);
  });

  it('does not fetch the same evidence twice', async () => {
    useRoomStore.setState({ evidence: new Map([['a1', evidence]]) });
    await useRoomStore.getState().fetchEvidence('i1', 'a1');
    expect(mockedGet).not.toHaveBeenCalled();
  });

  it('does not stack a second fetch on one already in flight', async () => {
    let release!: (v: unknown) => void;
    mockedGet.mockReturnValue(new Promise((r) => { release = r; }) as never);
    const first = useRoomStore.getState().fetchEvidence('i1', 'a1');
    const second = useRoomStore.getState().fetchEvidence('i1', 'a1');
    release(ok(evidence));
    await Promise.all([first, second]);
    expect(mockedGet).toHaveBeenCalledTimes(1);
  });

  it('keeps the server’s own words for an alert that carries no evidence', async () => {
    mockedGet.mockResolvedValue(fail('the alert carries no evidence', 404) as never);
    await useRoomStore.getState().fetchEvidence('i1', 'a1');

    expect(useRoomStore.getState().evidenceErrors.get('a1')).toBe('the alert carries no evidence');
    expect(useRoomStore.getState().evidence.get('a1')).toBeUndefined();
    expect(useRoomStore.getState().evidenceLoading.get('a1')).toBe(false);
  });

  it('keeps the server’s own words for evidence this build cannot read', async () => {
    mockedGet.mockResolvedValue(fail('evidence codec unknown to this build', 422) as never);
    await useRoomStore.getState().fetchEvidence('i1', 'a1');
    expect(useRoomStore.getState().evidenceErrors.get('a1')).toBe('evidence codec unknown to this build');
  });

  it('retries after a failure — a cached failure is not a cached answer', async () => {
    mockedGet.mockResolvedValueOnce(fail('temporarily unavailable', 503) as never);
    await useRoomStore.getState().fetchEvidence('i1', 'a1');
    mockedGet.mockResolvedValueOnce(ok(evidence) as never);
    await useRoomStore.getState().fetchEvidence('i1', 'a1');

    expect(mockedGet).toHaveBeenCalledTimes(2);
    expect(useRoomStore.getState().evidence.get('a1')).toEqual(evidence);
    expect(useRoomStore.getState().evidenceErrors.get('a1')).toBeUndefined();
  });
});

describe('room-store — moving a room through its lifecycle', () => {
  it('sends a bare status for a move that is not a resolution', async () => {
    useRoomStore.setState({ detail: detail() });
    mockedPost.mockResolvedValue(ok(incident({ status: 'acknowledged' })) as never);
    mockedGet.mockResolvedValue(ok(detail({ incident: incident({ status: 'acknowledged' }) })) as never);

    await expect(useRoomStore.getState().setStatus('i1', 'acknowledged')).resolves.toBe(true);

    expect(lastPost()?.path).toBe('/api/v1/investigations/{id}/status');
    expect(lastPost()?.body).toEqual({ status: 'acknowledged' });
    expect(useRoomStore.getState().detail?.incident.status).toBe('acknowledged');
  });

  it('carries the cause code when resolving', async () => {
    useRoomStore.setState({ detail: detail() });
    mockedPost.mockResolvedValue(ok(incident({ status: 'resolved', cause_code: 'false_positive' })) as never);
    mockedGet.mockResolvedValue(ok(detail()) as never);

    await useRoomStore.getState().setStatus('i1', 'resolved', 'false_positive');
    expect(lastPost()?.body).toEqual({ status: 'resolved', cause_code: 'false_positive' });
  });

  it('re-reads the room after a move, so the timeline shows the line the move wrote', async () => {
    useRoomStore.setState({ detail: detail() });
    mockedPost.mockResolvedValue(ok(incident({ status: 'acknowledged' })) as never);
    mockedGet.mockResolvedValue(ok(detail({ events_total: 1 })) as never);

    await useRoomStore.getState().setStatus('i1', 'acknowledged');
    expect(recorded(mockedGet).map((c) => c.path)).toContain('/api/v1/investigations/{id}');
    expect(useRoomStore.getState().detail?.events_total).toBe(1);
  });

  it('surfaces a refused move and leaves the room exactly as it stood', async () => {
    useRoomStore.setState({ detail: detail() });
    mockedPost.mockResolvedValue(fail('illegal incident transition: resolved to new') as never);

    await expect(useRoomStore.getState().setStatus('i1', 'new')).resolves.toBe(false);

    expect(useRoomStore.getState().actionError).toBe('illegal incident transition: resolved to new');
    expect(useRoomStore.getState().detail?.incident.status).toBe('new');
    expect(mockedGet).not.toHaveBeenCalled();
    expect(useRoomStore.getState().acting).toBe(false);
  });

  it('clears a previous refusal when the next move is accepted', async () => {
    useRoomStore.setState({ detail: detail(), actionError: 'illegal incident transition' });
    mockedPost.mockResolvedValue(ok(incident({ status: 'acknowledged' })) as never);
    mockedGet.mockResolvedValue(ok(detail()) as never);

    await useRoomStore.getState().setStatus('i1', 'acknowledged');
    expect(useRoomStore.getState().actionError).toBeNull();
  });
});

describe('room-store — who is working it', () => {
  it('names the assignee', async () => {
    useRoomStore.setState({ detail: detail() });
    mockedPost.mockResolvedValue(ok(incident({ assignee_id: 'user-3' })) as never);
    mockedGet.mockResolvedValue(ok(detail({ incident: incident({ assignee_id: 'user-3' }) })) as never);

    await useRoomStore.getState().setAssignee('i1', 'user-3');

    expect(lastPost()?.path).toBe('/api/v1/investigations/{id}/assignee');
    expect(lastPost()?.body).toEqual({ assignee_id: 'user-3' });
    expect(useRoomStore.getState().detail?.incident.assignee_id).toBe('user-3');
  });

  it('keeps an accepted move on screen even when the re-read behind it fails', async () => {
    useRoomStore.setState({ detail: detail() });
    mockedPost.mockResolvedValue(ok(incident({ assignee_id: 'user-3' })) as never);
    mockedGet.mockResolvedValue(fail('the queue is unavailable', 503) as never);

    await expect(useRoomStore.getState().setAssignee('i1', 'user-3')).resolves.toBe(true);
    expect(useRoomStore.getState().detail?.incident.assignee_id).toBe('user-3');
  });

  it('hands a room back by omitting the assignee rather than sending an empty one', async () => {
    useRoomStore.setState({ detail: detail({ incident: incident({ assignee_id: 'user-3' }) }) });
    mockedPost.mockResolvedValue(ok(incident({ assignee_id: null })) as never);
    mockedGet.mockResolvedValue(ok(detail()) as never);

    await useRoomStore.getState().setAssignee('i1', null);
    expect(lastPost()?.body).toEqual({});
  });

  it('surfaces a refusal without changing who holds the room', async () => {
    useRoomStore.setState({ detail: detail({ incident: incident({ assignee_id: 'user-3' }) }) });
    mockedPost.mockResolvedValue(fail('no such assignee', 404) as never);

    await expect(useRoomStore.getState().setAssignee('i1', 'ghost')).resolves.toBe(false);
    expect(useRoomStore.getState().actionError).toBe('no such assignee');
    expect(useRoomStore.getState().detail?.incident.assignee_id).toBe('user-3');
  });
});

describe('room-store — notes on the history', () => {
  const created: IncidentEvent = {
    id: 'e9', at: '2026-08-12T10:00:00Z', kind: 'comment',
    actor_id: 'user-3', body: { body: 'Driver rollout rolled back' },
  };

  it('appends the line the server created rather than re-reading the room', async () => {
    useRoomStore.setState({ detail: detail({ events: [], events_total: 0 }) });
    mockedPost.mockResolvedValue(ok(created) as never);

    await expect(useRoomStore.getState().addComment('i1', 'Driver rollout rolled back')).resolves.toBe(true);

    expect(lastPost()?.path).toBe('/api/v1/investigations/{id}/comments');
    expect(lastPost()?.body).toEqual({ body: 'Driver rollout rolled back' });
    expect(useRoomStore.getState().detail?.events).toEqual([created]);
    expect(useRoomStore.getState().detail?.events_total).toBe(1);
    expect(mockedGet).not.toHaveBeenCalled();
  });

  it('trims a note before sending it', async () => {
    useRoomStore.setState({ detail: detail() });
    mockedPost.mockResolvedValue(ok(created) as never);
    await useRoomStore.getState().addComment('i1', '  spaced out  ');
    expect(lastPost()?.body).toEqual({ body: 'spaced out' });
  });

  it('refuses a note that says nothing without spending a request on it', async () => {
    useRoomStore.setState({ detail: detail() });
    await expect(useRoomStore.getState().addComment('i1', '   ')).resolves.toBe(false);
    expect(mockedPost).not.toHaveBeenCalled();
  });

  it('surfaces a refusal and adds no line', async () => {
    useRoomStore.setState({ detail: detail({ events: [], events_total: 0 }) });
    mockedPost.mockResolvedValue(fail('comment too long') as never);

    await expect(useRoomStore.getState().addComment('i1', 'x')).resolves.toBe(false);
    expect(useRoomStore.getState().actionError).toBe('comment too long');
    expect(useRoomStore.getState().detail?.events).toEqual([]);
  });
});
