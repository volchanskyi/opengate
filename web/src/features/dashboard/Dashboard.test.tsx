import { render, screen } from '@testing-library/react';
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createMemoryRouter, RouterProvider } from 'react-router';
import { useDeviceStore } from '../devices';
import { Dashboard } from './Dashboard';

vi.mock('../../lib/api', () => ({
  api: {
    GET: vi.fn().mockResolvedValue({ data: [], error: undefined }),
    POST: vi.fn().mockResolvedValue({ data: undefined, error: undefined }),
    DELETE: vi.fn().mockResolvedValue({ error: undefined }),
  },
}));

function renderDashboard() {
  const router = createMemoryRouter(
    [
      { path: '/', element: <Dashboard /> },
      { path: '/devices', element: <p>Devices</p> },
      { path: '/setup', element: <p>Setup</p> },
    ],
    { initialEntries: ['/'] },
  );
  return render(<RouterProvider router={router} />);
}

/** Drives document.visibilityState and fires the matching event. */
function setVisibility(state: DocumentVisibilityState) {
  Object.defineProperty(document, 'visibilityState', { value: state, configurable: true });
  document.dispatchEvent(new Event('visibilitychange'));
}

describe('Dashboard', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.clearAllMocks();
    setVisibility('visible');
    useDeviceStore.setState({
      devices: [],
      groups: [{ id: 'g1', name: 'Group A', created_at: '', updated_at: '' }],
      isLoading: false,
      error: null,
      summary: {
        total: 2, online: 1, offline: 1, maintenance: 0,
        health: { anomalous: 0, watch: 0, healthy: 0, unknown: 2 },
      },
      fetchDevices: vi.fn(),
      fetchGroups: vi.fn(),
      fetchSummary: vi.fn(),
    });
  });

  afterEach(() => {
    vi.useRealTimers();
    setVisibility('visible');
  });

  it('renders device stats', () => {
    renderDashboard();
    expect(screen.getByText('Total Devices')).toBeInTheDocument();
    expect(screen.getByText('Online')).toBeInTheDocument();
  });

  it('renders the fleet health overview', () => {
    renderDashboard();
    expect(screen.getByText('Fleet Health')).toBeInTheDocument();
  });

  it('renders the server-counted health bands', () => {
    useDeviceStore.setState({
      summary: {
        total: 1, online: 1, offline: 0, maintenance: 0,
        health: { anomalous: 1, watch: 0, healthy: 0, unknown: 0 },
      },
    });
    renderDashboard();
    expect(screen.getByLabelText('Fleet health distribution')).toBeInTheDocument();
    expect(screen.getByText('Anomalous').closest('a')).toHaveTextContent('1');
  });

  it('renders the fleet in-maintenance count from the summary endpoint', () => {
    useDeviceStore.setState({
      summary: {
        total: 5, online: 5, offline: 0, maintenance: 3,
        health: { anomalous: 0, watch: 0, healthy: 0, unknown: 5 },
      },
    });
    renderDashboard();
    expect(screen.getByText('In Maintenance').closest('a')).toHaveTextContent('3');
  });

  it('renders zeroes before the first summary arrives', () => {
    useDeviceStore.setState({ summary: null });
    renderDashboard();
    expect(screen.getByText('Total Devices').closest('a')).toHaveTextContent('0');
    expect(screen.getByText(/no edge telemetry yet/i)).toBeInTheDocument();
  });

  it('Online tile deep-links to the online filter', () => {
    renderDashboard();
    expect(screen.getByText('Online').closest('a')).toHaveAttribute('href', '/devices?status=online');
  });

  it('Offline tile deep-links to the offline filter', () => {
    renderDashboard();
    expect(screen.getByText('Offline').closest('a')).toHaveAttribute('href', '/devices?status=offline');
  });

  it('In Maintenance tile deep-links to the maintenance filter', () => {
    renderDashboard();
    expect(screen.getByText('In Maintenance').closest('a')).toHaveAttribute('href', '/devices?maintenance=true');
  });

  it('Total Devices tile links to /devices', () => {
    renderDashboard();
    expect(screen.getByText('Total Devices').closest('a')).toHaveAttribute('href', '/devices');
  });

  it('fetches the summary on mount and on each 15s poll', () => {
    const fetchSummary = vi.fn();
    useDeviceStore.setState({ fetchSummary });
    renderDashboard();

    expect(fetchSummary).toHaveBeenCalledTimes(1);
    vi.advanceTimersByTime(15_000);
    expect(fetchSummary).toHaveBeenCalledTimes(2);
    vi.advanceTimersByTime(15_000);
    expect(fetchSummary).toHaveBeenCalledTimes(3);
  });

  it('never fetches the device list', () => {
    const fetchDevices = vi.fn();
    useDeviceStore.setState({ fetchDevices });
    renderDashboard();
    vi.advanceTimersByTime(60_000);
    expect(fetchDevices).not.toHaveBeenCalled();
  });

  it('stops polling while the tab is hidden and catches up on re-show', () => {
    const fetchSummary = vi.fn();
    useDeviceStore.setState({ fetchSummary });
    renderDashboard();
    expect(fetchSummary).toHaveBeenCalledTimes(1);

    setVisibility('hidden');
    vi.advanceTimersByTime(60_000);
    expect(fetchSummary).toHaveBeenCalledTimes(1);

    setVisibility('visible');
    expect(fetchSummary).toHaveBeenCalledTimes(2);
  });

  it('Dashboard heading is rendered', () => {
    renderDashboard();
    expect(screen.getByRole('heading', { name: 'Dashboard' })).toBeInTheDocument();
  });
});
