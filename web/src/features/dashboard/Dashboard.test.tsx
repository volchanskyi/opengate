import { render, screen } from '@testing-library/react';
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { useDeviceStore } from '../../state/device-store';
import { useAuthStore } from '../../state/auth-store';
import { useAdminStore } from '../../state/admin-store';
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
      fetchDevices: vi.fn(),
      fetchGroups: vi.fn(),
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
});
