import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { useAuthStore } from '../../src/state/auth-store';
import { RegisterPage } from '../../src/features/auth/RegisterPage';

vi.mock('../../src/lib/api', () => ({
  api: {
    GET: vi.fn().mockResolvedValue({ data: undefined, error: { error: 'mock' }, response: { status: 401 } }),
    POST: vi.fn().mockResolvedValue({ data: { token: 'test-jwt' }, error: undefined }),
  },
}));

function renderRegisterFlow() {
  const router = createMemoryRouter(
    [
      { path: '/register', element: <RegisterPage /> },
      { path: '/devices', element: <p>Devices Page</p> },
      { path: '/login', element: <p>Login Page</p> },
    ],
    { initialEntries: ['/register'] },
  );
  return render(<RouterProvider router={router} />);
}

describe('Register Flow (integration)', () => {
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

  it('renders register form with email, display name, and password fields', () => {
    renderRegisterFlow();
    expect(screen.getByRole('heading', { name: 'Register' })).toBeInTheDocument();
    expect(screen.getByLabelText('Email')).toBeInTheDocument();
    expect(screen.getByLabelText('Display Name')).toBeInTheDocument();
    expect(screen.getByLabelText('Password')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Register' })).toBeInTheDocument();
  });

  it('navigates to login page via link', async () => {
    const user = userEvent.setup();
    renderRegisterFlow();

    await user.click(screen.getByText('Login'));
    expect(screen.getByText('Login Page')).toBeInTheDocument();
  });

  it('shows error on registration failure', async () => {
    const { api } = await import('../../src/lib/api');
    vi.mocked(api.POST).mockResolvedValueOnce({
      data: undefined,
      error: { error: 'Email already exists' },
      response: { status: 409 },
    } as never);

    const user = userEvent.setup();
    renderRegisterFlow();

    await user.type(screen.getByLabelText('Email'), 'test@example.com');
    await user.type(screen.getByLabelText('Password'), 'password123');
    await user.click(screen.getByRole('button', { name: 'Register' }));

    expect(await screen.findByText('Email already exists')).toBeInTheDocument();
  });

  it('redirects authenticated users to /devices', () => {
    useAuthStore.setState({
      token: 'existing-token',
      user: { id: 'u1', email: 'test@example.com', display_name: 'Test', is_admin: false, created_at: '', updated_at: '' },
    });
    renderRegisterFlow();
    expect(screen.getByText('Devices Page')).toBeInTheDocument();
  });
});
