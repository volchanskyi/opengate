import { render, screen } from '@testing-library/react';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { useAuthStore } from '../../state/auth-store';
import { AdminGuard } from './AdminGuard';

vi.mock('../../lib/api', () => ({
  api: {
    GET: vi.fn().mockResolvedValue({ data: undefined, error: { error: 'mock' }, response: { status: 401 } }),
    POST: vi.fn(),
  },
}));

function renderGuard(initialEntries = ['/admin']) {
  const router = createMemoryRouter(
    [
      {
        path: '/admin',
        element: <AdminGuard />,
        children: [{ index: true, element: <p>Admin Content</p> }],
      },
      { path: '/devices', element: <p>Devices Page</p> },
    ],
    { initialEntries },
  );
  return render(<RouterProvider router={router} />);
}

describe('AdminGuard', () => {
  beforeEach(() => {
    useAuthStore.setState({
      token: 'valid-token',
      user: null,
      isLoading: false,
      hydrated: true,
      error: null,
    });
  });

  it('redirects non-admin users to /devices', () => {
    useAuthStore.setState({
      token: 'valid-token',
      hydrated: true,
      user: { id: '1', email: 'a@b.com', display_name: 'A', is_admin: false, created_at: '', updated_at: '' },
    });
    renderGuard();
    expect(screen.getByText('Devices Page')).toBeInTheDocument();
  });

  it('renders admin content for admin users', () => {
    useAuthStore.setState({
      token: 'valid-token',
      hydrated: true,
      user: { id: '1', email: 'a@b.com', display_name: 'A', is_admin: true, created_at: '', updated_at: '' },
    });
    renderGuard();
    expect(screen.getByText('Admin Content')).toBeInTheDocument();
  });

  it('redirects when user is null AND store is hydrated', () => {
    useAuthStore.setState({ token: 'valid-token', user: null, hydrated: true });
    renderGuard();
    expect(screen.getByText('Devices Page')).toBeInTheDocument();
  });

  // Regression: AdminGuard previously decided on first render with user=null
  // (before hydrate() finished reading the token from localStorage and
  // fetching /users/me). That caused an unconditional redirect on every
  // /settings/* navigation, surfacing as an intermittent e2e failure on the
  // a11y admin-user-management test under suite-level Docker contention.
  it('does NOT redirect while the auth store is still hydrating', () => {
    useAuthStore.setState({ token: 'valid-token', user: null, hydrated: false });
    renderGuard();
    // The protected outlet is suppressed while waiting, but no redirect to
    // /devices happens yet — the guard waits for hydration to complete.
    expect(screen.queryByText('Devices Page')).not.toBeInTheDocument();
    expect(screen.queryByText('Admin Content')).not.toBeInTheDocument();
  });
});
