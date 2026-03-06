import { render, screen } from '@testing-library/react';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { useAuthStore } from '../../src/state/auth-store';
import { useDeviceStore } from '../../src/state/device-store';
import { useSessionStore } from '../../src/state/session-store';
import { LoginPage } from '../../src/features/auth/LoginPage';
import { RegisterPage } from '../../src/features/auth/RegisterPage';
import { AuthGuard } from '../../src/features/auth/AuthGuard';
import { Layout } from '../../src/components/Layout';
import { DeviceList } from '../../src/features/devices/DeviceList';
import { DeviceDetail } from '../../src/features/devices/DeviceDetail';
import { Navigate } from 'react-router-dom';

vi.mock('../../src/lib/api', () => ({
  api: {
    GET: vi.fn().mockResolvedValue({ data: undefined, error: { error: 'mock' }, response: { status: 401 } }),
    POST: vi.fn().mockResolvedValue({ data: undefined, error: { error: 'mock' }, response: { status: 400 } }),
    DELETE: vi.fn().mockResolvedValue({ error: undefined }),
  },
}));

const routes = [
  { path: '/login', element: <LoginPage /> },
  { path: '/register', element: <RegisterPage /> },
  {
    path: '/',
    element: <AuthGuard />,
    children: [
      {
        element: <Layout />,
        children: [
          { index: true, element: <Navigate to="/devices" replace /> },
          { path: 'devices', element: <DeviceList /> },
          { path: 'devices/:id', element: <DeviceDetail /> },
        ],
      },
    ],
  },
];

function renderRoute(path: string) {
  const router = createMemoryRouter(routes, { initialEntries: [path] });
  return render(<RouterProvider router={router} />);
}

const mockUser = {
  id: 'u1',
  email: 'test@example.com',
  display_name: 'Test User',
  is_admin: false,
  created_at: '',
  updated_at: '',
};

describe('Routing (integration)', () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    useAuthStore.setState({
      token: null,
      user: null,
      isLoading: false,
      error: null,
    });
    useDeviceStore.setState({
      devices: [],
      groups: [],
      selectedGroupId: null,
      selectedDevice: null,
      isLoading: false,
      error: null,
      fetchGroups: vi.fn(),
    });
    useSessionStore.setState({
      sessions: [],
      isLoading: false,
      error: null,
      fetchSessions: vi.fn(),
    });
  });

  it('redirects unauthenticated user from / to /login', () => {
    renderRoute('/');
    expect(screen.getByRole('heading', { name: 'Login' })).toBeInTheDocument();
  });

  it('redirects unauthenticated user from /devices to /login', () => {
    renderRoute('/devices');
    expect(screen.getByRole('heading', { name: 'Login' })).toBeInTheDocument();
  });

  it('shows login page at /login', () => {
    renderRoute('/login');
    expect(screen.getByRole('heading', { name: 'Login' })).toBeInTheDocument();
  });

  it('shows register page at /register', () => {
    renderRoute('/register');
    expect(screen.getByRole('heading', { name: 'Register' })).toBeInTheDocument();
  });

  it('authenticated user sees devices page at /devices', () => {
    useAuthStore.setState({ token: 'tok', user: mockUser });
    renderRoute('/devices');
    expect(screen.getByText('OpenGate')).toBeInTheDocument();
    expect(screen.getByText('Select a group to view devices')).toBeInTheDocument();
  });

  it('authenticated user at / gets redirected to /devices', () => {
    useAuthStore.setState({ token: 'tok', user: mockUser });
    renderRoute('/');
    expect(screen.getByText('Select a group to view devices')).toBeInTheDocument();
  });

  it('shows layout with user info for authenticated user', () => {
    useAuthStore.setState({ token: 'tok', user: mockUser });
    renderRoute('/devices');
    expect(screen.getByText('Test User')).toBeInTheDocument();
    expect(screen.getByText('Logout')).toBeInTheDocument();
  });
});
