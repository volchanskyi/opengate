import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { useSecurityGroupsStore } from './state/security-groups-store';
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

  it('auto-selects the first group on mount when no group is selected', () => {
    const fetchGroupDetail = vi.fn();
    useSecurityGroupsStore.setState({
      groups: [{ ...fakeGroup, id: 'g2', name: 'Auditors' }, fakeGroup],
      selectedGroup: null,
      fetchGroups: vi.fn(),
      fetchUsers: vi.fn(),
      fetchGroupDetail,
    });
    // Render without renderPermissions (which overrides fetchGroupDetail to a no-op).
    const router = createMemoryRouter(
      [{ path: '/admin/security/permissions', element: <Permissions /> }],
      { initialEntries: ['/admin/security/permissions'] },
    );
    render(<RouterProvider router={router} />);

    // The effect picks groups[0] (Auditors) and fetches its detail.
    expect(fetchGroupDetail).toHaveBeenCalledWith('g2');
  });

  it('does not auto-select when a group is already selected', () => {
    const fetchGroupDetail = vi.fn();
    useSecurityGroupsStore.setState({
      groups: [fakeGroup],
      selectedGroup: { ...fakeGroup, members: [fakeAdmin] },
      fetchGroups: vi.fn(),
      fetchUsers: vi.fn(),
      fetchGroupDetail,
    });
    const router = createMemoryRouter(
      [{ path: '/admin/security/permissions', element: <Permissions /> }],
      { initialEntries: ['/admin/security/permissions'] },
    );
    render(<RouterProvider router={router} />);
    // selectedGroup is truthy → no auto-fetch call.
    expect(fetchGroupDetail).not.toHaveBeenCalled();
  });

  it('selected group tab has the blue active class; unselected tabs have the gray class', () => {
    useSecurityGroupsStore.setState({
      groups: [fakeGroup, { ...fakeGroup, id: 'g2', name: 'Auditors', is_system: false }],
      selectedGroup: { ...fakeGroup, members: [fakeAdmin] },
    });
    renderPermissions();
    const adminsTab = screen.getByRole('button', { name: /Administrators/i });
    expect(adminsTab.className).toContain('bg-blue-600');
    expect(adminsTab.className).not.toContain('bg-gray-700');
    const auditorsTab = screen.getByRole('button', { name: /Auditors/i });
    expect(auditorsTab.className).toContain('bg-gray-700');
    expect(auditorsTab.className).not.toContain('bg-blue-600');
  });

  it('handleAdd clears the selected user after a successful add', async () => {
    const addMember = vi.fn().mockResolvedValue(undefined);
    useSecurityGroupsStore.setState({ addMember });

    const user = userEvent.setup();
    renderPermissions();

    const select = screen.getByRole('combobox') as HTMLSelectElement;
    await user.selectOptions(select, 'u2');
    expect(select.value).toBe('u2');

    await user.click(screen.getByRole('button', { name: 'Add Member' }));
    // After the add resolves, the dropdown resets to '' (the placeholder) and the button is disabled again.
    expect((screen.getByRole('combobox') as HTMLSelectElement).value).toBe('');
    expect(screen.getByRole('button', { name: 'Add Member' })).toBeDisabled();
  });

  it('Add Member is a no-op when no user is selected', async () => {
    const addMember = vi.fn();
    useSecurityGroupsStore.setState({ addMember });
    const user = userEvent.setup();
    renderPermissions();
    // Force-click the disabled button via the DOM:
    const btn = screen.getByRole('button', { name: 'Add Member' });
    expect(btn).toBeDisabled();
    await user.click(btn);
    expect(addMember).not.toHaveBeenCalled();
  });

  it('Remove tooltip text changes when self is last admin vs. other member', () => {
    useSecurityGroupsStore.setState({
      selectedGroup: { ...fakeGroup, members: [fakeAdmin, fakeRegularUser] },
    });
    useAuthStore.setState({ user: fakeAdmin });
    renderPermissions();
    const buttons = screen.getAllByRole('button', { name: 'Remove' });
    // First member is the current user, but since members.length > 1, isLastAdmin = false.
    // Both buttons should have the "Remove from group" tooltip.
    for (const b of buttons) {
      expect(b.getAttribute('title')).toBe('Remove from group');
      expect((b as HTMLButtonElement).disabled).toBe(false);
    }
  });

  it('Last-admin self Remove button shows protective tooltip', () => {
    useSecurityGroupsStore.setState({
      selectedGroup: { ...fakeGroup, members: [fakeAdmin] },
    });
    useAuthStore.setState({ user: fakeAdmin });
    renderPermissions();
    const btn = screen.getByRole('button', { name: 'Remove' });
    expect(btn).toBeDisabled();
    expect(btn.getAttribute('title')).toBe('Cannot remove the last administrator');
  });

  it('description paragraph hidden when selectedGroup.description is empty', () => {
    useSecurityGroupsStore.setState({
      selectedGroup: { ...fakeGroup, description: '', members: [fakeAdmin] },
    });
    renderPermissions();
    expect(screen.queryByText('Full system access')).toBeNull();
    // The group h3 heading is still present (the tab button also has "Administrators" text).
    expect(screen.getByRole('heading', { level: 3, name: 'Administrators' })).toBeInTheDocument();
  });

  it('renders display_name parenthetical in user dropdown option text when present', () => {
    useSecurityGroupsStore.setState({
      selectedGroup: { ...fakeGroup, members: [] },
      users: [fakeRegularUser],
    });
    renderPermissions();
    const select = screen.getByRole('combobox');
    const options = within(select).getAllByRole('option');
    // Option text must include "(Regular User)" — kills the empty-string mutant on the parenthetical.
    expect(options[1]?.textContent).toMatch(/regular@test\.com\s*\(Regular User\)/);
  });

  it('omits parenthetical when display_name is empty', () => {
    const userWithoutName = { ...fakeRegularUser, display_name: '' };
    useSecurityGroupsStore.setState({
      selectedGroup: { ...fakeGroup, members: [] },
      users: [userWithoutName],
    });
    renderPermissions();
    const select = screen.getByRole('combobox');
    const options = within(select).getAllByRole('option');
    expect(options[1]?.textContent?.trim()).toBe('regular@test.com');
    expect(options[1]?.textContent).not.toContain('(');
  });

  it('display_name "-" fallback when member lacks a display name', () => {
    useSecurityGroupsStore.setState({
      selectedGroup: { ...fakeGroup, members: [{ ...fakeAdmin, display_name: '' }] },
    });
    renderPermissions();
    // The members table cell falls back to "-" when display_name is empty.
    expect(screen.getByText('-')).toBeInTheDocument();
  });
});
