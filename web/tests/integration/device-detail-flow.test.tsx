import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { useAuthStore } from '../../src/state/auth-store';
import { useDeviceStore } from '../../src/state/device-store';
import { useSessionStore } from '../../src/state/session-store';
import { AuthGuard } from '../../src/features/auth/AuthGuard';
import { Layout } from '../../src/components/Layout';
import { DeviceDetail } from '../../src/features/devices/DeviceDetail';
import { DeviceList } from '../../src/features/devices/DeviceList';

vi.mock('../../src/lib/api', () => ({
  api: {
    GET: vi.fn().mockResolvedValue({ data: undefined, error: { error: 'mock' }, response: { status: 404 } }),
    POST: vi.fn().mockResolvedValue({ data: { token: 'new-tok', relay_url: 'ws://localhost' }, error: undefined }),
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

const mockDevice = {
  id: 'd1',
  group_id: 'g1',
  hostname: 'prod-server',
  os: 'linux',
  status: 'online' as const,
  last_seen: '2026-01-01T00:00:00Z',
  created_at: '2025-12-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
};

function renderDeviceDetailFlow() {
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
              { path: 'devices/:id', element: <DeviceDetail /> },
            ],
          },
        ],
      },
      { path: '/login', element: <p>Login Page</p> },
    ],
    { initialEntries: ['/devices/d1'] },
  );
  return render(<RouterProvider router={router} />);
}

describe('Device Detail Flow (integration)', () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    useAuthStore.setState({ token: 'tok', user: mockUser, isLoading: false, error: null });
    useDeviceStore.setState({
      selectedDevice: mockDevice,
      devices: [],
      groups: [],
      selectedGroupId: null,
      isLoading: false,
      error: null,
      fetchDevice: vi.fn(),
      deleteDevice: vi.fn(),
      fetchGroups: vi.fn(),
    });
    useSessionStore.setState({
      sessions: [{ token: 'sess-1', device_id: 'd1', user_id: 'u1', created_at: '' }],
      isLoading: false,
      error: null,
      fetchSessions: vi.fn(),
      createSession: vi.fn().mockResolvedValue({ token: 'new-tok', relay_url: 'ws://localhost' }),
    });
  });

  it('shows device info within layout', () => {
    renderDeviceDetailFlow();
    expect(screen.getByText('OpenGate')).toBeInTheDocument();
    expect(screen.getByText('prod-server')).toBeInTheDocument();
    expect(screen.getByText('linux')).toBeInTheDocument();
    expect(screen.getByText('Online')).toBeInTheDocument();
  });

  it('shows active sessions', () => {
    renderDeviceDetailFlow();
    expect(screen.getByText('Active Sessions (1)')).toBeInTheDocument();
    expect(screen.getByText('sess-1')).toBeInTheDocument();
  });

  it('navigates back to device list', async () => {
    useDeviceStore.setState({ fetchGroups: vi.fn() });
    const user = userEvent.setup();
    renderDeviceDetailFlow();

    await user.click(screen.getByText(/Back to devices/));
    expect(screen.getByText('Select a group to view devices')).toBeInTheDocument();
  });

  it('delete requires confirmation click', async () => {
    const user = userEvent.setup();
    renderDeviceDetailFlow();

    await user.click(screen.getByText('Delete Device'));
    expect(screen.getByText('Confirm Delete')).toBeInTheDocument();
  });

  it('redirects to login when unauthenticated', () => {
    useAuthStore.setState({ token: null, user: null });
    renderDeviceDetailFlow();
    expect(screen.getByText('Login Page')).toBeInTheDocument();
  });
});
