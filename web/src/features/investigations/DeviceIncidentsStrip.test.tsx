import { render, screen, within } from '@testing-library/react';
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createMemoryRouter, RouterProvider } from 'react-router';
import { DeviceIncidentsStrip } from './DeviceIncidentsStrip';
import { DEFAULT_QUEUE_FILTERS, useQueueStore } from './state/queue-store';
import type { components } from '../../types/api';

type Incident = components['schemas']['Incident'];

function incident(over: Partial<Incident> = {}): Incident {
  return {
    id: 'i1', organization_id: 'org-1', rule_id: 'cpu.sustained', scope: 'organization',
    scope_key: 'org-1', severity: 'critical', status: 'new',
    opened_at: '2026-08-12T09:00:00Z', first_seen: '2026-08-12T09:00:00Z',
    last_seen: '2026-08-12T11:05:00Z', occurrences: 312, device_count: 40, ...over,
  };
}

const fetchDeviceIncidents = vi.fn().mockResolvedValue(undefined);

function renderStrip(deviceId = 'dev-7') {
  const router = createMemoryRouter(
    [
      { path: '/', element: <DeviceIncidentsStrip deviceId={deviceId} /> },
      { path: '/investigations/:id', element: <p>Room</p> },
    ],
    { initialEntries: ['/'] },
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
  useQueueStore.setState({
    items: [], nextCursor: null, loading: false, loaded: false, error: null,
    filters: DEFAULT_QUEUE_FILTERS, byDevice: new Map(), deviceErrors: new Map(),
    fetchDeviceIncidents,
  });
});

afterEach(() => {
  vi.useRealTimers();
});

describe('DeviceIncidentsStrip', () => {
  it('asks what this machine is caught up in', () => {
    renderStrip();
    expect(fetchDeviceIncidents).toHaveBeenCalledWith('dev-7');
  });

  it('is absent, not an empty box, while nothing has been read', () => {
    const { container } = renderStrip();
    expect(container).toBeEmptyDOMElement();
  });

  it('is absent, not an empty box, when the machine is in no open incident', () => {
    useQueueStore.setState({ byDevice: new Map([['dev-7', []]]) });
    const { container } = renderStrip();
    expect(container).toBeEmptyDOMElement();
  });

  it('is absent when the read failed — a strip is not the place to report one', () => {
    useQueueStore.setState({ deviceErrors: new Map([['dev-7', 'unavailable']]) });
    const { container } = renderStrip();
    expect(container).toBeEmptyDOMElement();
  });

  it('names each incident and links into the room', () => {
    useQueueStore.setState({ byDevice: new Map([['dev-7', [incident()]]]) });
    renderStrip();

    const link = screen.getByRole('link', { name: /cpu\.sustained/ });
    expect(link).toHaveAttribute('href', '/investigations/i1');
  });

  it('says how bad each one is and where it stands', () => {
    useQueueStore.setState({ byDevice: new Map([['dev-7', [incident()]]]) });
    renderStrip();

    const strip = within(screen.getByRole('region', { name: /open incidents/i }));
    expect(strip.getByText('Critical')).toBeInTheDocument();
    expect(strip.getByText('New')).toBeInTheDocument();
  });

  it('says how many rooms this machine is in', () => {
    useQueueStore.setState({
      byDevice: new Map([['dev-7', [incident({ id: 'i1' }), incident({ id: 'i2', rule_id: 'disk.await' })]]]),
    });
    renderStrip();
    expect(screen.getByText(/2 open incidents/i)).toBeInTheDocument();
  });

  it('re-reads on its own beat while the tab is watched, and never while it is hidden', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    useQueueStore.setState({ byDevice: new Map([['dev-7', [incident()]]]) });
    renderStrip();
    const afterMount = fetchDeviceIncidents.mock.calls.length;

    await vi.advanceTimersByTimeAsync(60_000);
    expect(fetchDeviceIncidents.mock.calls.length).toBeGreaterThan(afterMount);

    setVisibility('hidden');
    const beforeHidden = fetchDeviceIncidents.mock.calls.length;
    await vi.advanceTimersByTimeAsync(180_000);
    expect(fetchDeviceIncidents.mock.calls.length).toBe(beforeHidden);
  });

  it('re-reads when the page moves to another machine', () => {
    const { rerender } = renderStrip('dev-7');
    rerender(
      <RouterProvider
        router={createMemoryRouter([{ path: '/', element: <DeviceIncidentsStrip deviceId="dev-9" /> }], { initialEntries: ['/'] })}
      />,
    );
    expect(fetchDeviceIncidents).toHaveBeenCalledWith('dev-9');
  });
});
