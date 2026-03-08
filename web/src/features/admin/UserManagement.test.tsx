import { render, screen } from '@testing-library/react';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useAdminStore } from '../../state/admin-store';
import { useAuthStore } from '../../state/auth-store';
import { UserManagement } from './UserManagement';

vi.mock('../../lib/api', () => ({
  api: {
    GET: vi.fn().mockResolvedValue({ data: [], error: undefined }),
    PATCH: vi.fn().mockResolvedValue({ data: {}, error: undefined }),
    DELETE: vi.fn().mockResolvedValue({ error: undefined }),
    POST: vi.fn(),
  },
}));

const adminUser = {
  id: 'u1',
  email: 'admin@test.com',
  display_name: 'Admin',
  is_admin: true,
  created_at: '2024-01-01T00:00:00Z',
  updated_at: '2024-01-01T00:00:00Z',
};

const regularUser = {
  id: 'u2',
  email: 'user@test.com',
  display_name: 'User',
  is_admin: false,
  created_at: '2024-01-01T00:00:00Z',
  updated_at: '2024-01-01T00:00:00Z',
};

describe('UserManagement', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useAuthStore.setState({
      token: 'valid-token',
      user: adminUser,
      isLoading: false,
      error: null,
    });
    useAdminStore.setState({
      users: [adminUser, regularUser],
      auditEvents: [],
      isLoading: false,
      error: null,
    });
  });

  it('renders user list', () => {
    render(<UserManagement />);
    expect(screen.getByText('admin@test.com')).toBeInTheDocument();
    expect(screen.getByText('user@test.com')).toBeInTheDocument();
  });

  it('shows loading state when no users loaded yet', () => {
    useAdminStore.setState({ users: [], isLoading: true });
    render(<UserManagement />);
    expect(screen.getByText('Loading users...')).toBeInTheDocument();
  });

  it('disables toggle admin for current user', () => {
    render(<UserManagement />);
    const buttons = screen.getAllByRole('button', { name: /yes|no/i });
    // First button is for admin (current user) — should be disabled
    expect(buttons[0]).toBeDisabled();
  });

  it('enables delete for non-current users', () => {
    render(<UserManagement />);

    const deleteButtons = screen.getAllByText('Delete');
    // Second delete button is for regular user — should be enabled
    expect(deleteButtons[1]).not.toBeDisabled();
  });

  it('disables delete for current user', () => {
    render(<UserManagement />);
    const deleteButtons = screen.getAllByText('Delete');
    expect(deleteButtons[0]).toBeDisabled();
  });
});
