import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { useAuthStore } from '../../src/state/auth-store';
import { LoginPage } from '../../src/features/auth/LoginPage';

vi.mock('../../src/lib/api', () => ({
  api: {
    GET: vi.fn().mockResolvedValue({ data: undefined, error: { error: 'mock' }, response: { status: 401 } }),
    POST: vi.fn().mockResolvedValue({ data: { token: 'test-jwt' }, error: undefined }),
  },
}));

function renderLoginFlow() {
  const router = createMemoryRouter(
    [
      { path: '/login', element: <LoginPage /> },
      { path: '/devices', element: <p>Devices Page</p> },
      { path: '/register', element: <p>Register Page</p> },
    ],
    { initialEntries: ['/login'] },
  );
  return render(<RouterProvider router={router} />);
}

describe('Login Flow (integration)', () => {
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

  it('renders login form with email and password fields', () => {
    renderLoginFlow();
    expect(screen.getByRole('heading', { name: 'Login' })).toBeInTheDocument();
    expect(screen.getByLabelText('Email')).toBeInTheDocument();
    expect(screen.getByLabelText('Password')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Login' })).toBeInTheDocument();
  });

  it('navigates to register page via link', async () => {
    const user = userEvent.setup();
    renderLoginFlow();

    await user.click(screen.getByText('Register'));
    expect(screen.getByText('Register Page')).toBeInTheDocument();
  });

  it('shows error on login failure', async () => {
    const { api } = await import('../../src/lib/api');
    vi.mocked(api.POST).mockResolvedValueOnce({
      data: undefined,
      error: { error: 'Invalid credentials' },
      response: { status: 401 },
    } as never);

    const user = userEvent.setup();
    renderLoginFlow();

    await user.type(screen.getByLabelText('Email'), 'test@example.com');
    await user.type(screen.getByLabelText('Password'), 'wrong');
    await user.click(screen.getByRole('button', { name: 'Login' }));

    expect(await screen.findByText('Invalid credentials')).toBeInTheDocument();
  });

  it('redirects authenticated users to /devices', () => {
    useAuthStore.setState({
      token: 'existing-token',
      user: { id: 'u1', email: 'test@example.com', display_name: 'Test', is_admin: false, created_at: '', updated_at: '' },
    });
    renderLoginFlow();
    expect(screen.getByText('Devices Page')).toBeInTheDocument();
  });
});
