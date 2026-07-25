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

describe('Dashboard', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.clearAllMocks();
    useDeviceStore.setState({
      devices: [
        { id: 'd1', group_id: 'g1', hostname: 'host-1', os: 'linux', agent_version: '1.0.0', capabilities: [], status: 'online', last_seen: '', created_at: '', updated_at: '' },
        { id: 'd2', group_id: 'g1', hostname: 'host-2', os: 'linux', agent_version: '1.0.0', capabilities: [], status: 'offline', last_seen: '', created_at: '', updated_at: '' },
      ],
      groups: [{ id: 'g1', name: 'Group A', owner_id: 'u1', created_at: '', updated_at: '' }],
      isLoading: false,
      error: null,
      maintenanceCount: 0,
      fetchDevices: vi.fn(),
      fetchGroups: vi.fn(),
      fetchMaintenanceSummary: vi.fn(),
    });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('renders device stats', () => {
    renderDashboard();
    expect(screen.getByText('Total Devices')).toBeInTheDocument();
    expect(screen.getByText('2')).toBeInTheDocument();
    expect(screen.getByText('Online')).toBeInTheDocument();
  });

  it('renders the fleet health overview', () => {
    renderDashboard();
    expect(screen.getByText('Fleet Health')).toBeInTheDocument();
  });

  it('rolls up device anomaly rates into the fleet health bands', () => {
    useDeviceStore.setState({
      devices: [
        { id: 'd1', group_id: 'g1', hostname: 'h1', os: 'linux', agent_version: '1', capabilities: [], status: 'online', last_seen: '', created_at: '', updated_at: '', anomaly_rate: 0.9 },
      ],
    });
    renderDashboard();
    expect(screen.getByLabelText('Fleet health distribution')).toBeInTheDocument();
    expect(screen.getByText('Anomalous').closest('div')).toHaveTextContent('1');
  });

  it('renders the fleet in-maintenance count from the summary endpoint', () => {
    useDeviceStore.setState({ maintenanceCount: 3 });
    renderDashboard();
    const card = screen.getByText('In Maintenance').closest('a')!;
    expect(card).toHaveTextContent('3');
  });

  it('does not render the Device Groups card', () => {
    renderDashboard();
    expect(screen.queryByText('Device Groups')).toBeNull();
  });

  it('does not fetch groups on mount (FleetHealth needs no groups)', () => {
    const fetchGroups = vi.fn();
    useDeviceStore.setState({ fetchGroups });
    renderDashboard();
    expect(fetchGroups).not.toHaveBeenCalled();
  });

  it('Online tile deep-links to the online filter', () => {
    renderDashboard();
    expect(screen.getByText('Online').closest('a')).toHaveAttribute('href', '/devices?status=online');
  });

  it('Offline tile deep-links to the offline filter', () => {
    renderDashboard();
    expect(screen.getByText('Offline').closest('a')).toHaveAttribute('href', '/devices?status=offline');
  });

  it('fetches the maintenance summary on mount and on each 15s poll', () => {
    const fetchSummary = vi.fn();
    useDeviceStore.setState({ fetchMaintenanceSummary: fetchSummary });
    renderDashboard();
    expect(fetchSummary).toHaveBeenCalledTimes(1);
    vi.advanceTimersByTime(15_000);
    expect(fetchSummary).toHaveBeenCalledTimes(2);
  });

  it('In Maintenance tile deep-links to the maintenance filter', () => {
    renderDashboard();
    const link = screen.getByText('In Maintenance').closest('a');
    expect(link).toHaveAttribute('href', '/devices?maintenance=true');
  });

  it('polls devices every 15 seconds', () => {
    const fetchDevicesFn = vi.fn();
    useDeviceStore.setState({ fetchDevices: fetchDevicesFn });
    renderDashboard();

    // Initial fetch on mount
    expect(fetchDevicesFn).toHaveBeenCalledTimes(1);

    // Advance 15s — should trigger second fetch
    vi.advanceTimersByTime(15_000);
    expect(fetchDevicesFn).toHaveBeenCalledTimes(2);

    // Advance another 15s — third fetch
    vi.advanceTimersByTime(15_000);
    expect(fetchDevicesFn).toHaveBeenCalledTimes(3);
  });

  it('Total Devices tile links to /devices', () => {
    renderDashboard();
    const totalDevicesLink = screen.getByText('Total Devices').closest('a');
    expect(totalDevicesLink).toBeInTheDocument();
    expect(totalDevicesLink).toHaveAttribute('href', '/devices');
  });

  it('does not render View All Devices button', () => {
    renderDashboard();
    expect(screen.queryByText('View All Devices')).not.toBeInTheDocument();
  });

  it('does not render an Add Device link', () => {
    renderDashboard();
    expect(screen.queryByText('Add Device')).toBeNull();
  });

  it('online and offline counts add up to total devices', () => {
    renderDashboard();
    const totals = screen.getAllByText('2');
    // 2 total appears once; 1 online appears once; 1 offline appears once.
    expect(totals.length).toBeGreaterThanOrEqual(1);
    // Find each labelled value
    const totalCard = screen.getByText('Total Devices').closest('a')!;
    const onlineCard = screen.getByText('Online').closest('a')!;
    const offlineCard = screen.getByText('Offline').closest('a')!;
    expect(totalCard.textContent).toContain('2');
    expect(onlineCard.textContent).toContain('1');
    expect(offlineCard.textContent).toContain('1');
  });

  it('online count uses status === "online" filter (not !==)', () => {
    useDeviceStore.setState({
      devices: [
        { id: 'a', group_id: 'g', hostname: 'a', os: 'l', agent_version: '', capabilities: [], status: 'online', last_seen: '', created_at: '', updated_at: '' },
        { id: 'b', group_id: 'g', hostname: 'b', os: 'l', agent_version: '', capabilities: [], status: 'online', last_seen: '', created_at: '', updated_at: '' },
        { id: 'c', group_id: 'g', hostname: 'c', os: 'l', agent_version: '', capabilities: [], status: 'offline', last_seen: '', created_at: '', updated_at: '' },
      ],
    });
    renderDashboard();
    const onlineCard = screen.getByText('Online').closest('a')!;
    expect(onlineCard.textContent).toContain('2');
    const offlineCard = screen.getByText('Offline').closest('a')!;
    expect(offlineCard.textContent).toContain('1');
  });

  it('Dashboard heading is rendered', () => {
    renderDashboard();
    expect(screen.getByRole('heading', { name: 'Dashboard' })).toBeInTheDocument();
  });
});
