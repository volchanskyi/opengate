import { render, screen, within } from '@testing-library/react';
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createMemoryRouter, RouterProvider } from 'react-router';
import { api } from '../../lib/api';
import { useOrganizationStore } from '../organizations';
import { useAuthStore } from '../../state/auth-store';
import { InvestigationDetail } from './InvestigationDetail';
import { useRoomStore } from './state/room-store';
import type { components } from '../../types/api';

vi.mock('../../lib/api', () => ({ api: { GET: vi.fn(), POST: vi.fn() } }));

const mockedGet = vi.mocked(api.GET);
const mockedPost = vi.mocked(api.POST);

type Incident = components['schemas']['Incident'];
type IncidentAlert = components['schemas']['IncidentAlert'];
type IncidentDetail = components['schemas']['IncidentDetail'];

function incident(over: Partial<Incident> = {}): Incident {
  return {
    id: 'i1', organization_id: 'org-1', rule_id: 'cpu.sustained', scope: 'organization',
    scope_key: '6f2b9c31-1111-2222-3333-444455556666', severity: 'critical', status: 'new',
    opened_at: '2026-08-12T09:00:00Z', first_seen: '2026-08-12T09:00:00Z',
    last_seen: '2026-08-12T11:05:00Z', occurrences: 312, device_count: 40, ...over,
  };
}

function alert(over: Partial<IncidentAlert> = {}): IncidentAlert {
  return {
    id: 'a1', device_id: 'aaaaaaaa-1111-2222-3333-444455556666', rule_id: 'cpu.sustained',
    rule_version: 3, severity: 'critical', metric: 'cpu.busy_pct', value: 96.4,
    window_start: '2026-08-12T09:00:00Z', window_end: '2026-08-12T09:01:00Z',
    observed_at: '2026-08-12T09:00:30Z', received_at: '2026-08-12T09:00:45Z',
    backfilled: false, evidence_bytes: 4096, ...over,
  };
}

function detail(over: Partial<IncidentDetail> = {}): IncidentDetail {
  return { incident: incident(), alerts: [alert()], alerts_total: 1, events: [], events_total: 0, ...over };
}

const ok = <T,>(data: T) => ({ data, error: undefined, response: { ok: true, status: 200 } });

const allPaths = () => [
  ...(mockedGet.mock.calls as unknown as [string][]),
  ...(mockedPost.mock.calls as unknown as [string][]),
].map(([path]) => path);

function renderRoom(id = 'i1') {
  const router = createMemoryRouter(
    [
      { path: '/investigations/:id', element: <InvestigationDetail /> },
      { path: '/investigations', element: <p>Queue</p> },
      { path: '/devices/:id', element: <p>Device</p> },
    ],
    { initialEntries: [`/investigations/${id}`] },
  );
  return render(<RouterProvider router={router} />);
}

function setVisibility(state: DocumentVisibilityState) {
  Object.defineProperty(document, 'visibilityState', { value: state, configurable: true });
  document.dispatchEvent(new Event('visibilitychange'));
}

beforeEach(() => {
  vi.clearAllMocks();
  setVisibility('visible');
  useOrganizationStore.setState({ selectedOrganizationId: null });
  useAuthStore.setState({ user: { id: 'user-3', email: 'tech@example.com', is_admin: false } as never });
  useRoomStore.setState({
    detail: null, loading: false, error: null, actionError: null, acting: false,
    evidence: new Map(), evidenceLoading: new Map(), evidenceErrors: new Map(),
  });
  mockedGet.mockResolvedValue(ok(detail()) as never);
});

afterEach(() => {
  vi.useRealTimers();
});

describe('InvestigationDetail — nothing is asked of the machine', () => {
  it('issues no device-directed request when a room is opened', async () => {
    renderRoom();
    await screen.findByRole('heading', { name: 'cpu.sustained' });

    expect(allPaths().length).toBeGreaterThan(0);
    for (const path of allPaths()) {
      expect(path.startsWith('/api/v1/investigations')).toBe(true);
    }
  });

  it('reads the room the route names, narrowed to the customer being looked at', async () => {
    useOrganizationStore.setState({ selectedOrganizationId: 'org-9' });
    renderRoom('i7');
    await screen.findByRole('heading', { name: 'cpu.sustained' });

    const call = (mockedGet.mock.calls as unknown as [string, { params?: { path?: unknown; query?: unknown } }][]).at(0);
    expect(call?.[0]).toBe('/api/v1/investigations/{id}');
    expect(call?.[1].params?.path).toEqual({ id: 'i7' });
    expect(call?.[1].params?.query).toEqual({ organization_id: 'org-9' });
  });
});

describe('InvestigationDetail — what the room says about itself', () => {
  it('names the rule, how bad it is and where it stands', async () => {
    renderRoom();
    expect(await screen.findByRole('heading', { name: 'cpu.sustained' })).toBeInTheDocument();

    const summary = within(screen.getByRole('region', { name: 'Incident summary' }));
    expect(summary.getByText('Critical')).toBeInTheDocument();
    expect(summary.getByText('New')).toBeInTheDocument();
  });

  it('says how big it is and how long it has run', async () => {
    renderRoom();
    await screen.findByRole('heading', { name: 'cpu.sustained' });

    const summary = within(screen.getByRole('region', { name: 'Incident summary' }));
    expect(summary.getByText('312 alerts · across 40 machines · running for 2 h 5 m')).toBeInTheDocument();
  });

  it('says what rung the room is about', async () => {
    renderRoom();
    await screen.findByRole('heading', { name: 'cpu.sustained' });
    expect(screen.getByText(/organization · 6f2b9c31/)).toBeInTheDocument();
  });

  it('renders the history in the order it happened', async () => {
    mockedGet.mockResolvedValue(ok(detail({
      events: [
        { id: 'e1', at: '2026-08-12T09:05:00Z', kind: 'status_change', body: { from: 'new', to: 'acknowledged' } },
        { id: 'e2', at: '2026-08-12T09:07:00Z', kind: 'comment', actor_id: 'user-3', body: { body: 'driver rollout' } },
      ],
      events_total: 2,
    })) as never);
    renderRoom();

    const lines = within(await screen.findByRole('list', { name: 'Timeline' })).getAllByRole('listitem');
    expect(lines.at(0)).toHaveTextContent('New → Acknowledged');
    expect(lines.at(1)).toHaveTextContent('driver rollout');
  });

  it('renders the alerts it folded', async () => {
    renderRoom();
    expect(await screen.findByRole('link', { name: 'aaaaaaaa' })).toHaveAttribute(
      'href', '/devices/aaaaaaaa-1111-2222-3333-444455556666',
    );
  });

  it('offers a way back to the queue', async () => {
    renderRoom();
    await screen.findByRole('heading', { name: 'cpu.sustained' });
    expect(screen.getByRole('link', { name: /Back to the queue/i })).toHaveAttribute('href', '/investigations');
  });
});

describe('InvestigationDetail — a room whose machines are gone', () => {
  it('renders the machines that remain and says how many the incident covers', async () => {
    mockedGet.mockResolvedValue(ok(detail({
      incident: incident({ device_count: 3, occurrences: 2 }),
      alerts: [alert({ id: 'a1' }), alert({ id: 'a2' })],
      alerts_total: 2,
    })) as never);
    renderRoom();

    await screen.findByRole('heading', { name: 'cpu.sustained' });
    expect(screen.getByText(/1 of 3 machines/)).toBeInTheDocument();
    expect(screen.queryByRole('alert')).toBeNull();
  });

  it('renders a room left holding no alerts, with its history intact', async () => {
    mockedGet.mockResolvedValue(ok(detail({
      alerts: [], alerts_total: 0,
      events: [{ id: 'e1', at: '2026-08-12T09:05:00Z', kind: 'comment', body: { body: 'still worth reading' } }],
      events_total: 1,
    })) as never);
    renderRoom();

    await screen.findByRole('heading', { name: 'cpu.sustained' });
    expect(screen.getByText(/No alerts are held in this room/i)).toBeInTheDocument();
    expect(screen.getByText('still worth reading')).toBeInTheDocument();
  });
});

describe('InvestigationDetail — before there is a room', () => {
  it('says it is reading', () => {
    useRoomStore.setState({ loading: true });
    renderRoom();
    expect(screen.getByText(/Reading the incident/i)).toBeInTheDocument();
  });

  it('surfaces a room that is not there, with a way back', async () => {
    mockedGet.mockResolvedValue({ data: undefined, error: { error: 'no such incident' }, response: { ok: false, status: 404 } } as never);
    renderRoom();

    expect(await screen.findByRole('alert')).toHaveTextContent('no such incident');
    expect(screen.getByRole('link', { name: /Back to the queue/i })).toBeInTheDocument();
  });
});

describe('InvestigationDetail — polling', () => {
  it('re-reads the room on its own beat while the tab is watched', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    renderRoom();
    await screen.findByRole('heading', { name: 'cpu.sustained' });
    const before = mockedGet.mock.calls.length;

    await vi.advanceTimersByTimeAsync(30_000);
    expect(mockedGet.mock.calls.length).toBeGreaterThan(before);
  });

  it('issues nothing at all while the tab is hidden', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    renderRoom();
    await screen.findByRole('heading', { name: 'cpu.sustained' });

    setVisibility('hidden');
    const before = mockedGet.mock.calls.length;
    await vi.advanceTimersByTimeAsync(120_000);
    expect(mockedGet.mock.calls.length).toBe(before);
  });

  it('leaves nothing behind when the room is closed', async () => {
    const { unmount } = renderRoom();
    await screen.findByRole('heading', { name: 'cpu.sustained' });

    unmount();
    expect(useRoomStore.getState().detail).toBeNull();
  });
});
