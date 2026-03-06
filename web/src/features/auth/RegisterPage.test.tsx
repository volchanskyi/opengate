import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { useAuthStore } from '../../state/auth-store';
import { RegisterPage } from './RegisterPage';

vi.mock('../../lib/api', () => ({
  api: {
    GET: vi.fn().mockResolvedValue({ data: undefined, error: { error: 'mock' }, response: { status: 401 } }),
    POST: vi.fn().mockResolvedValue({ data: undefined, error: { error: 'mock' } }),
  },
}));

function renderRegister(initialEntries = ['/register']) {
  const router = createMemoryRouter(
    [
      { path: '/register', element: <RegisterPage /> },
      { path: '/login', element: <p>Login Page</p> },
      { path: '/devices', element: <p>Devices Page</p> },
    ],
    { initialEntries },
  );
  return render(<RouterProvider router={router} />);
}

describe('RegisterPage', () => {
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

  it('renders email, display name, and password inputs', () => {
    renderRegister();
    expect(screen.getByLabelText('Email')).toBeInTheDocument();
    expect(screen.getByLabelText('Display Name')).toBeInTheDocument();
    expect(screen.getByLabelText('Password')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Register' })).toBeInTheDocument();
  });

  it('shows error message', () => {
    useAuthStore.setState({ error: 'email already exists' });
    renderRegister();
    expect(screen.getByText('email already exists')).toBeInTheDocument();
  });

  it('links to login page', () => {
    renderRegister();
    expect(screen.getByText('Login')).toHaveAttribute('href', '/login');
  });

  it('redirects to /devices if already authenticated', () => {
    useAuthStore.setState({
      token: 'valid',
      user: { id: '1', email: 'a@b.com', display_name: 'A', is_admin: false, created_at: '', updated_at: '' },
    });
    renderRegister();
    expect(screen.getByText('Devices Page')).toBeInTheDocument();
  });

  it('submits register form', async () => {
    const user = userEvent.setup();
    const registerFn = vi.fn();
    useAuthStore.setState({ register: registerFn });

    renderRegister();

    await user.type(screen.getByLabelText('Email'), 'new@example.com');
    await user.type(screen.getByLabelText('Display Name'), 'New User');
    await user.type(screen.getByLabelText('Password'), 'password123');
    await user.click(screen.getByRole('button', { name: 'Register' }));

    expect(registerFn).toHaveBeenCalledWith('new@example.com', 'password123', 'New User');
  });
});
