import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createMemoryRouter, RouterProvider } from 'react-router';
import { api } from '../../lib/api';
import { useOrganizationStore } from '../organizations';
import { InvestigationList } from './InvestigationList';
import { DEFAULT_QUEUE_FILTERS, useQueueStore } from './state/queue-store';
import { useCatalogueStore } from './state/catalogue-store';
import type { components } from '../../types/api';

vi.mock('../../lib/api', () => ({ api: { GET: vi.fn() } }));

const mockedGet = vi.mocked(api.GET);

type Incident = components['schemas']['Incident'];

function incident(over: Partial<Incident> = {}): Incident {
  return {
    id: 'i1', organization_id: 'org-1', rule_id: 'cpu.sustained', scope: 'organization',
    scope_key: 'org-1', severity: 'critical', status: 'new',
    opened_at: '2026-08-12T09:00:00Z', first_seen: '2026-08-12T09:00:00Z',
    last_seen: '2026-08-12T11:05:00Z', occurrences: 312, device_count: 40, ...over,
  };
}

const page = (items: Incident[], nextCursor?: string) => ({
  data: { items, ...(nextCursor ? { next_cursor: nextCursor } : {}) },
  error: undefined,
  response: { ok: true, status: 200 },
});

function lastQuery(): Record<string, unknown> {
  const call = (mockedGet.mock.calls as unknown as [string, { params?: { query?: Record<string, unknown> } }][]).at(-1);
  return call?.[1].params?.query ?? {};
}

function renderList() {
  const router = createMemoryRouter(
    [
      { path: '/investigations', element: <InvestigationList /> },
      { path: '/investigations/:id', element: <p>Room</p> },
    ],
    { initialEntries: ['/investigations'] },
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
  useCatalogueStore.setState({ rules: [], fleetSize: 0, loaded: false, loading: false, error: null });
  useQueueStore.setState({
    items: [], nextCursor: null, loading: false, loaded: false, error: null, pagedOn: false,
    filters: DEFAULT_QUEUE_FILTERS, byDevice: new Map(), deviceErrors: new Map(),
  });
  mockedGet.mockResolvedValue(page([]) as never);
});

afterEach(() => {
  vi.useRealTimers();
});

describe('InvestigationList — the three states before there are rows', () => {
  it('says it is reading rather than showing an empty table', () => {
    useQueueStore.setState({ loading: true });
    renderList();
    expect(screen.getByText(/Reading the queue/i)).toBeInTheDocument();
    expect(screen.queryByRole('table')).toBeNull();
  });

  it('renders a real empty state once the queue is read and holds nothing', async () => {
    renderList();
    expect(await screen.findByText(/Nothing to work/i)).toBeInTheDocument();
    expect(screen.queryByText(/Reading the queue/i)).toBeNull();
  });

  it('surfaces a failed read as an error, not as an empty queue', async () => {
    mockedGet.mockResolvedValue({ data: undefined, error: { error: 'the queue is unavailable' }, response: { ok: false, status: 503 } } as never);
    renderList();

    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('the queue is unavailable');
    expect(screen.queryByText(/Nothing to work/i)).toBeNull();
  });
});

describe('InvestigationList — the queue', () => {
  it('reads the queue on arrival', async () => {
    renderList();
    await screen.findByText(/Nothing to work/i);
    expect(mockedGet).toHaveBeenCalledWith('/api/v1/investigations', expect.anything());
  });

  it('shows what an incident is, how big it is and how long it has run', async () => {
    mockedGet.mockResolvedValue(page([incident()]) as never);
    renderList();

    const row = within(await screen.findByRole('table')).getByRole('row', { name: /cpu\.sustained/ });
    expect(within(row).getByText('Critical')).toBeInTheDocument();
    expect(within(row).getByText('New')).toBeInTheDocument();
    expect(within(row).getByText('312 alerts')).toBeInTheDocument();
    expect(within(row).getByText('40 machines')).toBeInTheDocument();
    expect(within(row).getByText('2 h 5 m')).toBeInTheDocument();
  });

  it('opens the room from the row', async () => {
    mockedGet.mockResolvedValue(page([incident()]) as never);
    renderList();

    const link = await screen.findByRole('link', { name: 'cpu.sustained' });
    expect(link).toHaveAttribute('href', '/investigations/i1');
  });

  it('says how many rows are on screen, so a bounded page says what it is a page of', async () => {
    mockedGet.mockResolvedValue(page([incident({ id: 'i1' }), incident({ id: 'i2', rule_id: 'disk.await' })]) as never);
    renderList();
    expect(await screen.findByText('2 incidents')).toBeInTheDocument();
  });
});

describe('InvestigationList — narrowing', () => {
  it('re-reads with both filters when a second one is added', async () => {
    const user = userEvent.setup();
    mockedGet.mockResolvedValue(page([incident()]) as never);
    renderList();
    await screen.findByRole('table');

    await user.click(screen.getByRole('button', { name: 'Critical' }));

    expect(lastQuery()).toMatchObject({
      status: ['new', 'acknowledged', 'investigating'],
      severity: ['critical'],
    });
  });

  it('re-reads when the customer being looked at changes', async () => {
    mockedGet.mockResolvedValue(page([incident()]) as never);
    renderList();
    await screen.findByRole('table');
    const before = mockedGet.mock.calls.length;

    useOrganizationStore.setState({ selectedOrganizationId: 'org-9' });
    await vi.waitFor(() => { expect(mockedGet.mock.calls.length).toBeGreaterThan(before); });
    expect(lastQuery()).toMatchObject({ organization_id: 'org-9' });
  });
});

describe('InvestigationList — paging by position', () => {
  it('offers the next page only while there is one', async () => {
    mockedGet.mockResolvedValue(page([incident()]) as never);
    renderList();
    await screen.findByRole('table');
    expect(screen.queryByRole('button', { name: /Load more/i })).toBeNull();
  });

  it('reads on from where the page ended', async () => {
    const user = userEvent.setup();
    mockedGet.mockResolvedValue(page([incident({ id: 'i1' })], 'cursor-2') as never);
    renderList();
    await screen.findByRole('table');

    mockedGet.mockResolvedValue(page([incident({ id: 'i2', rule_id: 'disk.await' })]) as never);
    await user.click(screen.getByRole('button', { name: /Load more/i }));

    expect(await screen.findByRole('link', { name: 'disk.await' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'cpu.sustained' })).toBeInTheDocument();
  });
});

describe('InvestigationList — polling', () => {
  it('re-reads the queue on its own beat while the tab is watched', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    mockedGet.mockResolvedValue(page([incident()]) as never);
    renderList();
    await screen.findByRole('table');
    const before = mockedGet.mock.calls.length;

    await vi.advanceTimersByTimeAsync(30_000);
    expect(mockedGet.mock.calls.length).toBeGreaterThan(before);
  });

  it('stops the beat once somebody has read past the first page', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    mockedGet.mockResolvedValue(page([incident({ id: 'i1' })], 'cursor-2') as never);
    renderList();
    await screen.findByRole('table');

    mockedGet.mockResolvedValue(page([incident({ id: 'i2', rule_id: 'disk.await' })]) as never);
    await useQueueStore.getState().fetchMore();
    await screen.findByRole('link', { name: 'disk.await' });

    const before = mockedGet.mock.calls.length;
    await vi.advanceTimersByTimeAsync(120_000);
    expect(mockedGet.mock.calls.length).toBe(before);
    // The pages somebody walked to are still on screen.
    expect(screen.getByRole('link', { name: 'cpu.sustained' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'disk.await' })).toBeInTheDocument();
  });

  it('issues nothing at all while the tab is hidden', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    mockedGet.mockResolvedValue(page([incident()]) as never);
    renderList();
    await screen.findByRole('table');

    setVisibility('hidden');
    const before = mockedGet.mock.calls.length;
    await vi.advanceTimersByTimeAsync(120_000);
    expect(mockedGet.mock.calls.length).toBe(before);
  });
});
