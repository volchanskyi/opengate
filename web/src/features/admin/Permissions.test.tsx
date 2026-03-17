import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { useSecurityGroupsStore } from '../../state/security-groups-store';
import { useAuthStore } from '../../state/auth-store';
import { Permissions } from './Permissions';

vi.mock('../../lib/api', () => ({
  api: {
    GET: vi.fn().mockResolvedValue({ data: undefined, error: { error: 'mock' } }),
    POST: vi.fn().mockResolvedValue({ data: undefined, error: undefined }),
    DELETE: vi.fn().mockResolvedValue({ data: undefined, error: undefined }),
  },
}));

const fakeAdmin = {
  id: 'u1',
  email: 'admin@test.com',
  display_name: 'Admin User',
  is_admin: true,
  created_at: '2024-01-01T00:00:00Z',
  updated_at: '2024-01-01T00:00:00Z',
};

const fakeRegularUser = {
  id: 'u2',
  email: 'regular@test.com',
  display_name: 'Regular User',
  is_admin: false,
  created_at: '2024-01-01T00:00:00Z',
  updated_at: '2024-01-01T00:00:00Z',
};

const fakeGroup = {
  id: 'g1',
  name: 'Administrators',
  description: 'Full system access',
  is_system: true,
  created_at: '2024-01-01T00:00:00Z',
  updated_at: '2024-01-01T00:00:00Z',
};

function renderPermissions() {
  // Override fetchGroups and fetchUsers to no-op (we set state directly)
  useSecurityGroupsStore.setState({
    fetchGroups: vi.fn(),
    fetchUsers: vi.fn(),
    fetchGroupDetail: vi.fn(),
  });

  const router = createMemoryRouter(
    [{ path: '/admin/security/permissions', element: <Permissions /> }],
    { initialEntries: ['/admin/security/permissions'] },
  );
  return render(<RouterProvider router={router} />);
}

describe('Permissions', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useSecurityGroupsStore.setState({
      groups: [fakeGroup],
      selectedGroup: { ...fakeGroup, members: [fakeAdmin] },
      users: [fakeAdmin, fakeRegularUser],
      isLoading: false,
      error: null,
    });
    useAuthStore.setState({
      user: fakeAdmin,
      token: 'test-token',
    });
  });

  it('renders the Permissions heading', () => {
    renderPermissions();
    expect(screen.getByRole('heading', { name: 'Permissions' })).toBeInTheDocument();
  });

  it('renders group tabs', () => {
    renderPermissions();
    expect(screen.getByRole('button', { name: /Administrators/i })).toBeInTheDocument();
  });

  it('shows System badge for system groups', () => {
    renderPermissions();
    expect(screen.getByText('System')).toBeInTheDocument();
  });

  it('renders members table with email and display name', () => {
    renderPermissions();
    expect(screen.getByText('admin@test.com')).toBeInTheDocument();
    expect(screen.getByText('Admin User')).toBeInTheDocument();
  });

  it('shows group description', () => {
    renderPermissions();
    expect(screen.getByText('Full system access')).toBeInTheDocument();
  });

  it('filters out existing members from add dropdown', () => {
    renderPermissions();
    const select = screen.getByRole('combobox');
    const options = within(select).getAllByRole('option');
    // Should have placeholder + regular user only (admin is already a member)
    expect(options).toHaveLength(2);
    expect(options[1]?.textContent).toContain('regular@test.com');
  });

  it('add member button disabled when no user selected', () => {
    renderPermissions();
    expect(screen.getByRole('button', { name: 'Add Member' })).toBeDisabled();
  });

  it('calls addMember when user selected and button clicked', async () => {
    const addMember = vi.fn();
    useSecurityGroupsStore.setState({ addMember });

    const user = userEvent.setup();
    renderPermissions();

    await user.selectOptions(screen.getByRole('combobox'), 'u2');
    await user.click(screen.getByRole('button', { name: 'Add Member' }));

    expect(addMember).toHaveBeenCalledWith('g1', 'u2');
  });

  it('calls removeMember when Remove clicked', async () => {
    const removeMember = vi.fn();
    // Need 2 members so isLastAdmin is false and button is enabled
    useSecurityGroupsStore.setState({
      removeMember,
      selectedGroup: { ...fakeGroup, members: [fakeAdmin, fakeRegularUser] },
    });

    const user = userEvent.setup();
    renderPermissions();

    const removeButtons = screen.getAllByRole('button', { name: 'Remove' });
    await user.click(removeButtons[0]!);

    expect(removeMember).toHaveBeenCalledWith('g1', 'u1');
  });

  it('disables Remove button when last admin tries to remove self', () => {
    // Only one member, current user is that member
    useSecurityGroupsStore.setState({
      selectedGroup: { ...fakeGroup, members: [fakeAdmin] },
    });
    useAuthStore.setState({ user: fakeAdmin });

    renderPermissions();

    const removeBtn = screen.getByRole('button', { name: 'Remove' });
    expect(removeBtn).toBeDisabled();
  });

  it('shows error message when error exists', () => {
    useSecurityGroupsStore.setState({ error: 'Something went wrong' });
    renderPermissions();
    expect(screen.getByText('Something went wrong')).toBeInTheDocument();
  });

  it('shows loading state when loading with no groups', () => {
    useSecurityGroupsStore.setState({ isLoading: true, groups: [] });
    renderPermissions();
    expect(screen.getByText('Loading security groups...')).toBeInTheDocument();
  });

  it('shows empty members message when group has no members', () => {
    useSecurityGroupsStore.setState({
      selectedGroup: { ...fakeGroup, members: [] },
    });
    renderPermissions();
    expect(screen.getByText('No members in this group')).toBeInTheDocument();
  });

  it('calls fetchGroupDetail when a group tab is clicked', async () => {
    const fetchGroupDetail = vi.fn();
    useSecurityGroupsStore.setState({
      fetchGroups: vi.fn(),
      fetchUsers: vi.fn(),
      fetchGroupDetail,
    });

    const user = userEvent.setup();

    const router = createMemoryRouter(
      [{ path: '/admin/security/permissions', element: <Permissions /> }],
      { initialEntries: ['/admin/security/permissions'] },
    );
    render(<RouterProvider router={router} />);

    await user.click(screen.getByRole('button', { name: /Administrators/i }));

    expect(fetchGroupDetail).toHaveBeenCalledWith('g1');
  });
});
