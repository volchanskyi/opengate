import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { createMemoryRouter, RouterProvider, Navigate } from 'react-router-dom';
import { useSecurityGroupsStore } from '../../src/state/security-groups-store';
import { useAuthStore } from '../../src/state/auth-store';
import { AuthGuard } from '../../src/features/auth/AuthGuard';
import { AdminGuard } from '../../src/features/admin/AdminGuard';
import { AdminLayout } from '../../src/features/admin/AdminLayout';
import { Permissions } from '../../src/features/admin/Permissions';
import { Layout } from '../../src/components/Layout';

vi.mock('../../src/lib/api', () => ({
  api: {
    GET: vi.fn().mockResolvedValue({ data: undefined, error: { error: 'mock' }, response: { status: 401 } }),
    POST: vi.fn().mockResolvedValue({ data: undefined, error: undefined }),
    DELETE: vi.fn().mockResolvedValue({ data: undefined, error: undefined }),
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
  email: 'regular@test.com',
  display_name: 'Regular',
  is_admin: false,
  created_at: '2024-01-01T00:00:00Z',
  updated_at: '2024-01-01T00:00:00Z',
};

const adminsGroup = {
  id: 'g1',
  name: 'Administrators',
  description: 'Full system access',
  is_system: true,
  created_at: '2024-01-01T00:00:00Z',
  updated_at: '2024-01-01T00:00:00Z',
};

const routes = [
  { path: '/login', element: <p>Login Page</p> },
  {
    path: '/',
    element: <AuthGuard />,
    children: [
      {
        element: <Layout />,
        children: [
          { index: true, element: <Navigate to="/devices" replace /> },
          { path: 'devices', element: <p>Devices</p> },
          {
            path: 'settings',
            element: <AdminGuard />,
            children: [
              {
                element: <AdminLayout />,
                children: [
                  { index: true, element: <Navigate to="/settings/users" replace /> },
                  { path: 'users', element: <p>Users Page</p> },
                  { path: 'security/permissions', element: <Permissions /> },
                ],
              },
            ],
          },
        ],
      },
    ],
  },
];

function renderRoute(path: string) {
  const router = createMemoryRouter(routes, { initialEntries: [path] });
  return render(<RouterProvider router={router} />);
}

describe('Permissions Flow (integration)', () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    useAuthStore.setState({
      token: 'test-token',
      user: adminUser,
      isLoading: false,
      error: null,
    });
    useSecurityGroupsStore.setState({
      groups: [adminsGroup],
      selectedGroup: { ...adminsGroup, members: [adminUser] },
      users: [adminUser, regularUser],
      isLoading: false,
      error: null,
      fetchGroups: vi.fn(),
      fetchUsers: vi.fn(),
      fetchGroupDetail: vi.fn(),
    });
  });

  it('admin can navigate to Permissions page', () => {
    renderRoute('/settings/security/permissions');
    expect(screen.getByRole('heading', { name: 'Permissions' })).toBeInTheDocument();
  });

  it('sidebar shows Security section with Permissions link', () => {
    renderRoute('/settings/security/permissions');
    expect(screen.getByText('Security')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Permissions' })).toBeInTheDocument();
  });

  it('sidebar shows Management section links', () => {
    renderRoute('/settings/security/permissions');
    expect(screen.getByText('Management')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Users' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Audit Log' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Agent Updates' })).toBeInTheDocument();
  });

  it('renders group with members and add member controls', () => {
    renderRoute('/settings/security/permissions');

    // Members table
    expect(screen.getByText('admin@test.com')).toBeInTheDocument();
    // "Admin" appears in nav, user display, and member table — check within the table
    const table = screen.getByRole('table');
    expect(within(table).getByText('Admin')).toBeInTheDocument();

    // Add member dropdown has only non-members
    const select = screen.getByRole('combobox');
    const options = within(select).getAllByRole('option');
    expect(options).toHaveLength(2); // placeholder + regular user
    expect(options[1]?.textContent).toContain('regular@test.com');
  });

  it('add member flow calls store action', async () => {
    const addMember = vi.fn();
    useSecurityGroupsStore.setState({ addMember });

    const user = userEvent.setup();
    renderRoute('/settings/security/permissions');

    await user.selectOptions(screen.getByRole('combobox'), 'u2');
    await user.click(screen.getByRole('button', { name: 'Add Member' }));

    expect(addMember).toHaveBeenCalledWith('g1', 'u2');
  });

  it('non-admin is redirected away from admin routes', () => {
    useAuthStore.setState({ user: regularUser });
    renderRoute('/settings/security/permissions');
    // AdminGuard should redirect non-admin users
    expect(screen.queryByRole('heading', { name: 'Permissions' })).not.toBeInTheDocument();
  });

  it('shows error from store', () => {
    useSecurityGroupsStore.setState({ error: 'Failed to load groups' });
    renderRoute('/settings/security/permissions');
    expect(screen.getByText('Failed to load groups')).toBeInTheDocument();
  });
});
