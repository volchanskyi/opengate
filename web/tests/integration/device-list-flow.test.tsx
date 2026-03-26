import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { useAuthStore } from '../../src/state/auth-store';
import { useDeviceStore } from '../../src/state/device-store';
import { AuthGuard } from '../../src/features/auth/AuthGuard';
import { Layout } from '../../src/components/Layout';
import { DeviceList } from '../../src/features/devices/DeviceList';

vi.mock('../../src/lib/api', () => ({
  api: {
    GET: vi.fn().mockResolvedValue({ data: [], error: undefined }),
    POST: vi.fn().mockResolvedValue({ data: { id: 'new-g', name: 'New Group', owner_id: 'u1', created_at: '', updated_at: '' }, error: undefined }),
    DELETE: vi.fn().mockResolvedValue({ error: undefined }),
  },
}));

const mockUser = {
  id: 'u1',
  email: 'test@example.com',
  display_name: 'Test User',
  is_admin: false,
  created_at: '',
  updated_at: '',
};

function renderDeviceListFlow() {
  const router = createMemoryRouter(
    [
      {
        path: '/',
        element: <AuthGuard />,
        children: [
          {
            element: <Layout />,
            children: [
              { path: 'devices', element: <DeviceList /> },
              { path: 'devices/:id', element: <p>Device Detail</p> },
            ],
          },
        ],
      },
      { path: '/login', element: <p>Login Page</p> },
    ],
    { initialEntries: ['/devices'] },
  );
  return render(<RouterProvider router={router} />);
}

describe('Device List Flow (integration)', () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    useAuthStore.setState({ token: 'tok', user: mockUser, isLoading: false, error: null });
    useDeviceStore.setState({
      devices: [],
      groups: [
        { id: 'g1', name: 'Production', owner_id: 'u1', created_at: '', updated_at: '' },
        { id: 'g2', name: 'Staging', owner_id: 'u1', created_at: '', updated_at: '' },
      ],
      selectedGroupId: null,
      selectedDevice: null,
      isLoading: false,
      error: null,
      fetchGroups: vi.fn(),
      fetchDevices: vi.fn(),
    });
  });

  it('shows groups in sidebar and empty state for main area', () => {
    renderDeviceListFlow();
    expect(screen.getByText('Production')).toBeInTheDocument();
    expect(screen.getByText('Staging')).toBeInTheDocument();
    expect(screen.getByText('Welcome to OpenGate')).toBeInTheDocument();
  });

  it('selects group and shows no devices message', async () => {
    const fetchDevicesFn = vi.fn();
    useDeviceStore.setState({
      fetchDevices: fetchDevicesFn,
      selectGroup: (id: string | null) => {
        useDeviceStore.setState({ selectedGroupId: id });
        if (id) fetchDevicesFn(id);
      },
    });

    const user = userEvent.setup();
    renderDeviceListFlow();

    await user.click(screen.getByText('Production'));
    expect(fetchDevicesFn).toHaveBeenCalledWith('g1');
  });

  it('renders devices when group is selected with devices', () => {
    useDeviceStore.setState({
      selectedGroupId: 'g1',
      devices: [
        { id: 'd1', group_id: 'g1', hostname: 'server-01', os: 'linux', agent_version: '1.0.0', capabilities: [], status: 'online', last_seen: new Date().toISOString(), created_at: '', updated_at: '' },
        { id: 'd2', group_id: 'g1', hostname: 'server-02', os: 'windows', agent_version: '', capabilities: [], status: 'offline', last_seen: new Date().toISOString(), created_at: '', updated_at: '' },
      ],
    });

    renderDeviceListFlow();
    expect(screen.getByText('server-01')).toBeInTheDocument();
    expect(screen.getByText('server-02')).toBeInTheDocument();
  });

  it('shows layout nav bar with user info', () => {
    renderDeviceListFlow();
    expect(screen.getByText('OpenGate')).toBeInTheDocument();
    expect(screen.getByText('Test User')).toBeInTheDocument();
    expect(screen.getByText('Logout')).toBeInTheDocument();
  });
});
