import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { useAuthStore } from '../state/auth-store';
import { Layout } from './Layout';

vi.mock('../lib/api', () => ({
  api: {
    GET: vi.fn().mockResolvedValue({ data: undefined, error: { error: 'mock' }, response: { status: 401 } }),
    POST: vi.fn(),
  },
}));

function renderLayout() {
  const router = createMemoryRouter(
    [
      {
        path: '/',
        element: <Layout />,
        children: [{ index: true, element: <p>Child Content</p> }],
      },
    ],
    { initialEntries: ['/'] },
  );
  return render(<RouterProvider router={router} />);
}

describe('Layout', () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    useAuthStore.setState({
      token: 'valid',
      user: { id: '1', email: 'a@b.com', display_name: 'Test User', is_admin: false, created_at: '', updated_at: '' },
      isLoading: false,
      error: null,
    });
  });

  it('renders app title and user name', () => {
    renderLayout();
    expect(screen.getByText('OpenGate')).toBeInTheDocument();
    expect(screen.getByText('Test User')).toBeInTheDocument();
  });

  it('renders child content via Outlet', () => {
    renderLayout();
    expect(screen.getByText('Child Content')).toBeInTheDocument();
  });

  it('calls logout on button click', async () => {
    const user = userEvent.setup();
    const logoutFn = vi.fn();
    useAuthStore.setState({ logout: logoutFn });

    renderLayout();
    await user.click(screen.getByText('Logout'));

    expect(logoutFn).toHaveBeenCalled();
  });
});
