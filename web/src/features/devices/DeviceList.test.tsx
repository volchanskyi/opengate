import { render, screen } from '@testing-library/react';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { useDeviceStore } from '../../state/device-store';
import { DeviceList } from './DeviceList';

vi.mock('../../lib/api', () => ({
  api: {
    GET: vi.fn().mockResolvedValue({ data: [], error: undefined }),
    POST: vi.fn().mockResolvedValue({ data: { id: 'new', name: 'New' }, error: undefined }),
    DELETE: vi.fn().mockResolvedValue({ error: undefined }),
  },
}));

function renderDeviceList() {
  const router = createMemoryRouter(
    [
      { path: '/devices', element: <DeviceList /> },
      { path: '/devices/:id', element: <p>Device Detail</p> },
    ],
    { initialEntries: ['/devices'] },
  );
  return render(<RouterProvider router={router} />);
}

describe('DeviceList', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useDeviceStore.setState({
      groups: [{ id: 'g1', name: 'Group A', owner_id: 'u1', created_at: '', updated_at: '' }],
      devices: [],
      selectedGroupId: null,
      selectedDevice: null,
      isLoading: false,
      error: null,
      fetchGroups: vi.fn(),
      fetchDevices: vi.fn(),
    });
  });

  it('shows welcome message when no devices exist', () => {
    renderDeviceList();
    expect(screen.getByText('Welcome to OpenGate')).toBeInTheDocument();
    expect(screen.getAllByText('Quick Setup').length).toBeGreaterThan(0);
  });

  it('shows empty group message when group selected but empty', () => {
    useDeviceStore.setState({ selectedGroupId: 'g1' });
    renderDeviceList();
    expect(screen.getByText('No devices in this group')).toBeInTheDocument();
    expect(screen.getAllByText('Quick Setup').length).toBeGreaterThan(0);
  });

  it('renders devices', () => {
    useDeviceStore.setState({
      devices: [
        { id: 'd1', group_id: 'g1', hostname: 'host-1', os: 'linux', agent_version: '1.0.0', status: 'online', last_seen: new Date().toISOString(), created_at: '', updated_at: '' },
        { id: 'd2', group_id: 'g1', hostname: 'host-2', os: 'windows', agent_version: '', status: 'offline', last_seen: new Date().toISOString(), created_at: '', updated_at: '' },
      ],
    });
    renderDeviceList();
    expect(screen.getByText('host-1')).toBeInTheDocument();
    expect(screen.getByText('host-2')).toBeInTheDocument();
  });

  it('shows loading skeleton', () => {
    useDeviceStore.setState({ isLoading: true });
    renderDeviceList();
    const skeletons = document.querySelectorAll('.animate-pulse');
    expect(skeletons.length).toBeGreaterThan(0);
  });

  it('fetches groups and devices on mount', () => {
    const fetchGroupsFn = vi.fn();
    const fetchDevicesFn = vi.fn();
    useDeviceStore.setState({ fetchGroups: fetchGroupsFn, fetchDevices: fetchDevicesFn });
    renderDeviceList();
    expect(fetchGroupsFn).toHaveBeenCalled();
    expect(fetchDevicesFn).toHaveBeenCalled();
  });
});
