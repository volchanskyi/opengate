import { render, screen, within, fireEvent } from '@testing-library/react';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useAdminStore } from './state/admin-store';
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

  it('admin toggle button label is "Yes" when is_admin and "No" otherwise', () => {
    render(<UserManagement />);
    const rows = screen.getAllByRole('row');
    // First data row (after header) is adminUser → Yes; second is regular → No.
    const adminRow = rows[1];
    const regularRow = rows[2];
    expect(within(adminRow!).getByRole('button', { name: 'Yes' })).toBeInTheDocument();
    expect(within(regularRow!).getByRole('button', { name: 'No' })).toBeInTheDocument();
  });

  it('admin toggle uses green color class when is_admin and gray otherwise', () => {
    render(<UserManagement />);
    const yesBtn = screen.getByRole('button', { name: 'Yes' });
    const noBtn = screen.getByRole('button', { name: 'No' });
    expect(yesBtn.className).toContain('bg-green-900');
    expect(yesBtn.className).not.toContain('bg-gray-700');
    expect(noBtn.className).toContain('bg-gray-700');
    expect(noBtn.className).not.toContain('bg-green-900');
  });

  it('clicking admin toggle flips is_admin via updateUser', () => {
    const updateUserFn = vi.fn().mockResolvedValue(undefined);
    useAdminStore.setState({ updateUser: updateUserFn });
    render(<UserManagement />);
    const noBtn = screen.getByRole('button', { name: 'No' });
    expect((noBtn as HTMLButtonElement).disabled).toBe(false);
    fireEvent.click(noBtn);
    expect(updateUserFn).toHaveBeenCalledWith('u2', { is_admin: true });
  });

  it('clicking admin Yes flips to is_admin: false', () => {
    const updateUserFn = vi.fn().mockResolvedValue(undefined);
    // Make the second (regular) user the current user so we can click the admin's button.
    useAuthStore.setState({ user: regularUser, token: 'tok' });
    useAdminStore.setState({ updateUser: updateUserFn });
    render(<UserManagement />);
    const yesBtn = screen.getByRole('button', { name: 'Yes' });
    expect((yesBtn as HTMLButtonElement).disabled).toBe(false);
    fireEvent.click(yesBtn);
    expect(updateUserFn).toHaveBeenCalledWith('u1', { is_admin: false });
  });

  it('Delete on non-current user calls deleteUser with that id', () => {
    const deleteUserFn = vi.fn().mockResolvedValue(undefined);
    useAdminStore.setState({ deleteUser: deleteUserFn });
    render(<UserManagement />);
    const rows = screen.getAllByRole('row');
    const regularRow = rows[2];
    const delBtn = within(regularRow!).getByText('Delete');
    expect((delBtn as HTMLButtonElement).disabled).toBe(false);
    fireEvent.click(delBtn);
    expect(deleteUserFn).toHaveBeenCalledWith('u2');
  });

  it('fetchUsers is called once on mount', () => {
    const fetchUsersFn = vi.fn();
    useAdminStore.setState({ fetchUsers: fetchUsersFn });
    render(<UserManagement />);
    expect(fetchUsersFn).toHaveBeenCalledTimes(1);
  });

  it('table aria-hidden is undefined when not loading', () => {
    useAdminStore.setState({ isLoading: false, users: [adminUser] });
    render(<UserManagement />);
    const table = document.querySelector('table')!;
    expect(table.getAttribute('aria-hidden')).toBeNull();
  });

  it('table aria-hidden is true while loading-with-no-users', () => {
    useAdminStore.setState({ isLoading: true, users: [] });
    render(<UserManagement />);
    const table = document.querySelector('table')!;
    expect(table.getAttribute('aria-hidden')).toBe('true');
  });
});
