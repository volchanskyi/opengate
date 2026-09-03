import { render, screen, waitFor } from '@testing-library/react';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { createMemoryRouter, RouterProvider } from 'react-router';
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

  // A technician who reloads the browser mid-incident arrives with the token
  // still in local storage and no user in memory. Nothing else asks the server
  // who they are, so if the guard does not, "Loading…" is the whole session.
  it('asks the server who the technician is when a reload leaves a token and no user', async () => {
    const fetchMe = vi.fn().mockResolvedValue(undefined);
    useAuthStore.setState({ token: 'valid-token', user: null, fetchMe });

    renderGuard();

    await waitFor(() => expect(fetchMe).toHaveBeenCalledTimes(1));
  });

  it('shows the page once the reloaded session has been identified', async () => {
    const user = {
      id: '1',
      email: 'tech@example.com',
      display_name: 'Tech',
      is_admin: false,
      created_at: '',
      updated_at: '',
    };
    const fetchMe = vi.fn().mockImplementation(async () => {
      useAuthStore.setState({ user });
    });
    useAuthStore.setState({ token: 'valid-token', user: null, fetchMe });

    renderGuard();

    expect(await screen.findByText('Protected Content')).toBeInTheDocument();
  });

  it('asks nobody who the technician is when the user is already loaded', () => {
    const fetchMe = vi.fn().mockResolvedValue(undefined);
    useAuthStore.setState({
      token: 'valid-token',
      user: { id: '1', email: 'a@b.com', display_name: 'A', is_admin: false, created_at: '', updated_at: '' },
      fetchMe,
    });

    renderGuard();

    expect(fetchMe).not.toHaveBeenCalled();
  });

  it('asks nobody who the technician is when there is no token to identify', () => {
    const fetchMe = vi.fn().mockResolvedValue(undefined);
    useAuthStore.setState({ token: null, user: null, fetchMe });

    renderGuard();

    expect(fetchMe).not.toHaveBeenCalled();
  });
});
