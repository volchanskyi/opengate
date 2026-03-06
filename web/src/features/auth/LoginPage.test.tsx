import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { useAuthStore } from '../../state/auth-store';
import { LoginPage } from './LoginPage';

vi.mock('../../lib/api', () => ({
  api: {
    GET: vi.fn().mockResolvedValue({ data: undefined, error: { error: 'mock' }, response: { status: 401 } }),
    POST: vi.fn().mockResolvedValue({ data: undefined, error: { error: 'mock' } }),
  },
}));

function renderLogin(initialEntries = ['/login']) {
  const router = createMemoryRouter(
    [
      { path: '/login', element: <LoginPage /> },
      { path: '/register', element: <p>Register Page</p> },
      { path: '/devices', element: <p>Devices Page</p> },
    ],
    { initialEntries },
  );
  return render(<RouterProvider router={router} />);
}

describe('LoginPage', () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    useAuthStore.setState({
      token: null,
      user: null,
      isLoading: false,
      error: null,
    });
  });

  it('renders email and password inputs', () => {
    renderLogin();
    expect(screen.getByLabelText('Email')).toBeInTheDocument();
    expect(screen.getByLabelText('Password')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Login' })).toBeInTheDocument();
  });

  it('shows error message', () => {
    useAuthStore.setState({ error: 'invalid credentials' });
    renderLogin();
    expect(screen.getByText('invalid credentials')).toBeInTheDocument();
  });

  it('links to register page', () => {
    renderLogin();
    expect(screen.getByText('Register')).toHaveAttribute('href', '/register');
  });

  it('redirects to /devices if already authenticated', () => {
    useAuthStore.setState({
      token: 'valid',
      user: { id: '1', email: 'a@b.com', display_name: 'A', is_admin: false, created_at: '', updated_at: '' },
    });
    renderLogin();
    expect(screen.getByText('Devices Page')).toBeInTheDocument();
  });

  it('submits login form', async () => {
    const user = userEvent.setup();
    const loginFn = vi.fn();
    useAuthStore.setState({ login: loginFn });

    renderLogin();

    await user.type(screen.getByLabelText('Email'), 'test@example.com');
    await user.type(screen.getByLabelText('Password'), 'password123');
    await user.click(screen.getByRole('button', { name: 'Login' }));

    expect(loginFn).toHaveBeenCalledWith('test@example.com', 'password123');
  });
});
