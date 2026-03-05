import { render, screen } from '@testing-library/react';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { useAuthStore } from '../../state/auth-store';
import { AuthGuard } from './AuthGuard';

vi.mock('../../lib/api', () => ({
  api: {
    GET: vi.fn().mockResolvedValue({ data: undefined, error: { error: 'mock' }, response: { status: 401 } }),
    POST: vi.fn(),
  },
}));

function renderGuard(initialEntries = ['/']) {
  const router = createMemoryRouter(
    [
      {
        path: '/',
        element: <AuthGuard />,
        children: [{ index: true, element: <p>Protected Content</p> }],
      },
      { path: '/login', element: <p>Login Page</p> },
    ],
    { initialEntries },
  );
  return render(<RouterProvider router={router} />);
}

describe('AuthGuard', () => {
  beforeEach(() => {
    localStorage.clear();
    useAuthStore.setState({
      token: null,
      user: null,
      isLoading: false,
      error: null,
    });
  });

  it('redirects to /login when no token', () => {
    renderGuard();
    expect(screen.getByText('Login Page')).toBeInTheDocument();
  });

  it('renders children when authenticated', () => {
    useAuthStore.setState({
      token: 'valid-token',
      user: { id: '1', email: 'a@b.com', display_name: 'A', is_admin: false, created_at: '', updated_at: '' },
    });
    renderGuard();
    expect(screen.getByText('Protected Content')).toBeInTheDocument();
  });

  it('shows loading when token exists but no user yet', () => {
    useAuthStore.setState({ token: 'valid-token', user: null });
    renderGuard();
    expect(screen.getByText('Loading...')).toBeInTheDocument();
  });
});
