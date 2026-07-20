import { render, screen } from '@testing-library/react';
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { useDeviceStore } from '../devices';
import { useAuthStore } from '../../state/auth-store';
import { useAdminStore } from '../admin';
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
    useAuthStore.setState({ user: { id: 'u1', email: 'a@b.c', is_admin: false, display_name: '', created_at: '', updated_at: '' } });
    useAdminStore.setState({
      auditEvents: [],
      isLoading: false,
      error: null,
      fetchAuditEvents: vi.fn(),
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

  it('fetches the maintenance summary on mount and on each 15s poll', () => {
    const fetchSummary = vi.fn();
    useDeviceStore.setState({ fetchMaintenanceSummary: fetchSummary });
    renderDashboard();
    expect(fetchSummary).toHaveBeenCalledTimes(1);
    vi.advanceTimersByTime(15_000);
    expect(fetchSummary).toHaveBeenCalledTimes(2);
  });

  it('In Maintenance tile links to /devices', () => {
    renderDashboard();
    const link = screen.getByText('In Maintenance').closest('a');
    expect(link).toHaveAttribute('href', '/devices');
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

  it('non-linked tiles are not clickable', () => {
    renderDashboard();
    const onlineTile = screen.getByText('Online').closest('a');
    expect(onlineTile).toBeNull();
  });

  it('does not render View All Devices button', () => {
    renderDashboard();
    expect(screen.queryByText('View All Devices')).not.toBeInTheDocument();
  });

  it('renders Add Device link', () => {
    renderDashboard();
    expect(screen.getByText('Add Device')).toBeInTheDocument();
  });

  it('non-admin user does NOT call fetchAuditEvents', () => {
    const fetchAuditFn = vi.fn();
    useAuthStore.setState({ user: { id: 'u1', email: 'a@b.c', is_admin: false, display_name: '', created_at: '', updated_at: '' } });
    useAdminStore.setState({ fetchAuditEvents: fetchAuditFn });
    renderDashboard();
    // Kills `if (user?.is_admin)` → `if (true)` mutant.
    expect(fetchAuditFn).not.toHaveBeenCalled();
  });

  it('admin user calls fetchAuditEvents with limit:10', () => {
    const fetchAuditFn = vi.fn();
    useAuthStore.setState({ user: { id: 'u1', email: 'a@b.c', is_admin: true, display_name: '', created_at: '', updated_at: '' } });
    useAdminStore.setState({ fetchAuditEvents: fetchAuditFn });
    renderDashboard();
    // Pin the limit:10 — kills `{ limit: 10 }` → `{}` mutant.
    expect(fetchAuditFn).toHaveBeenCalledWith({ limit: 10 });
  });

  it('hides Recent Activity section for admin when no events', () => {
    useAuthStore.setState({ user: { id: 'u1', email: 'a@b.c', is_admin: true, display_name: '', created_at: '', updated_at: '' } });
    useAdminStore.setState({ auditEvents: [] });
    renderDashboard();
    // Kills `auditEvents.length > 0` → `>= 0` (would render even with 0 events).
    expect(screen.queryByText('Recent Activity')).toBeNull();
  });

  it('shows Recent Activity section with admin + events', () => {
    useAuthStore.setState({ user: { id: 'u1', email: 'a@b.c', is_admin: true, display_name: '', created_at: '', updated_at: '' } });
    useAdminStore.setState({
      auditEvents: [
        { id: 1, user_id: 'u1', action: 'user.login', target: 'admin@test.com', details: '', created_at: '2024-01-01T12:00:00Z' } as never,
        { id: 2, user_id: 'u1', action: 'group.create', target: '', details: '', created_at: '2024-01-02T13:00:00Z' } as never,
      ],
      fetchAuditEvents: vi.fn(),
    });
    renderDashboard();
    expect(screen.getByText('Recent Activity')).toBeInTheDocument();
    expect(screen.getByText('user.login')).toBeInTheDocument();
    expect(screen.getByText('group.create')).toBeInTheDocument();
    // Empty target falls back to em-dash — kills `event.target || '—'`
    // → `event.target && '—'` mutant (which would render '' or undefined).
    expect(screen.getByText('—')).toBeInTheDocument();
    expect(screen.getByText('admin@test.com')).toBeInTheDocument();
  });

  it('shows at most 10 audit events even with 11 in store', () => {
    const events = Array.from({ length: 11 }, (_, i) => ({
      id: i + 1,
      user_id: 'u1',
      action: `action.${i}`,
      target: `target-${i}`,
      details: '',
      created_at: '2024-01-01T00:00:00Z',
    } as never));
    useAuthStore.setState({ user: { id: 'u1', email: 'a@b.c', is_admin: true, display_name: '', created_at: '', updated_at: '' } });
    useAdminStore.setState({ auditEvents: events, fetchAuditEvents: vi.fn() });
    renderDashboard();
    // Kills `slice(0, 10)` → `auditEvents.map(...)` (no slice) mutant.
    expect(screen.queryByText('action.10')).toBeNull();
    expect(screen.getByText('action.0')).toBeInTheDocument();
    expect(screen.getByText('action.9')).toBeInTheDocument();
  });

  it('online and offline counts add up to total devices', () => {
    renderDashboard();
    const totals = screen.getAllByText('2');
    // 2 total appears once; 1 online appears once; 1 offline appears once.
    expect(totals.length).toBeGreaterThanOrEqual(1);
    // Find each labelled value
    const totalCard = screen.getByText('Total Devices').closest('div')!;
    const onlineCard = screen.getByText('Online').closest('div')!;
    const offlineCard = screen.getByText('Offline').closest('div')!;
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
    const onlineCard = screen.getByText('Online').closest('div')!;
    expect(onlineCard.textContent).toContain('2');
    const offlineCard = screen.getByText('Offline').closest('div')!;
    expect(offlineCard.textContent).toContain('1');
  });

  it('Add Device link points to /setup', () => {
    renderDashboard();
    const link = screen.getByRole('link', { name: 'Add Device' });
    expect(link.getAttribute('href')).toBe('/setup');
  });

  it('Dashboard heading is rendered', () => {
    renderDashboard();
    expect(screen.getByRole('heading', { name: 'Dashboard' })).toBeInTheDocument();
  });

  it('Device Groups tile shows the groups count', () => {
    useDeviceStore.setState({
      groups: [
        { id: 'g1', name: 'Group A', owner_id: 'u1', created_at: '', updated_at: '' },
        { id: 'g2', name: 'Group B', owner_id: 'u1', created_at: '', updated_at: '' },
        { id: 'g3', name: 'Group C', owner_id: 'u1', created_at: '', updated_at: '' },
      ],
    });
    renderDashboard();
    const groupsCard = screen.getByText('Device Groups').closest('div')!;
    expect(groupsCard.textContent).toContain('3');
  });
});
