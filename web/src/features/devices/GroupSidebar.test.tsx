import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useDeviceStore } from './state/device-store';
import { GroupSidebar } from './GroupSidebar';

vi.mock('../../lib/api', () => ({
  api: {
    GET: vi.fn().mockResolvedValue({ data: [], error: undefined }),
    POST: vi.fn().mockResolvedValue({ data: { id: 'new', name: 'New' }, error: undefined }),
    DELETE: vi.fn().mockResolvedValue({ error: undefined }),
  },
}));

describe('GroupSidebar', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useDeviceStore.setState({
      groups: [
        { id: 'g1', name: 'Group A', owner_id: 'u1', created_at: '', updated_at: '' },
        { id: 'g2', name: 'Group B', owner_id: 'u1', created_at: '', updated_at: '' },
      ],
      selectedGroupId: 'g1',
      devices: [],
      selectedDevice: null,
      isLoading: false,
      error: null,
    });
  });

  it('renders group names', () => {
    render(<GroupSidebar />);
    expect(screen.getByText('Group A')).toBeInTheDocument();
    expect(screen.getByText('Group B')).toBeInTheDocument();
  });

  it('highlights active group', () => {
    render(<GroupSidebar />);
    const groupA = screen.getByText('Group A').closest('div');
    expect(groupA?.className).toContain('bg-gray-700');
  });

  it('shows create form on + New click', async () => {
    const user = userEvent.setup();
    render(<GroupSidebar />);

    await user.click(screen.getByText('+ New'));
    expect(screen.getByPlaceholderText('Group name')).toBeInTheDocument();
  });

  it('calls selectGroup on group click', async () => {
    const user = userEvent.setup();
    const selectGroupFn = vi.fn();
    useDeviceStore.setState({ selectGroup: selectGroupFn });

    render(<GroupSidebar />);
    await user.click(screen.getByText('Group B'));

    expect(selectGroupFn).toHaveBeenCalledWith('g2');
  });

  it('delete requires confirmation', async () => {
    const user = userEvent.setup();
    render(<GroupSidebar />);

    // First click shows confirm
    const deleteButtons = screen.getAllByText('x');
    await user.click(deleteButtons[0]!);
    expect(screen.getByText('Confirm?')).toBeInTheDocument();
  });

  it('shows empty state', () => {
    useDeviceStore.setState({ groups: [] });
    render(<GroupSidebar />);
    expect(screen.getByText('No groups yet')).toBeInTheDocument();
  });

  it('calls createGroup with trimmed name on form submit, then clears input and hides form', async () => {
    const user = userEvent.setup();
    const createGroupFn = vi.fn().mockResolvedValue(undefined);
    useDeviceStore.setState({ createGroup: createGroupFn });

    render(<GroupSidebar />);
    await user.click(screen.getByText('+ New'));

    const input = screen.getByPlaceholderText('Group name') as HTMLInputElement;
    // Whitespace padding around 'New Group' — kills `newName.trim()` →
    // `newName` (no trim) mutant.
    await user.type(input, '  New Group  ');
    await user.click(screen.getByText('Add'));

    expect(createGroupFn).toHaveBeenCalledWith('New Group');
    // Input is cleared — kills `setNewName('')` → `'Stryker was here!'` mutant.
    // Form is hidden — kills `setShowForm(false)` → `setShowForm(true)` mutant.
    expect(screen.queryByPlaceholderText('Group name')).toBeNull();
  });

  it('does NOT call createGroup when name is whitespace-only', async () => {
    const user = userEvent.setup();
    const createGroupFn = vi.fn();
    useDeviceStore.setState({ createGroup: createGroupFn });

    render(<GroupSidebar />);
    await user.click(screen.getByText('+ New'));
    const input = screen.getByPlaceholderText('Group name');
    await user.type(input, '   ');
    await user.click(screen.getByText('Add'));

    // Kills `if (!newName.trim()) return;` → `if (false) return;` and
    // `if (newName.trim()) return;` mutants — only whitespace must short-circuit.
    expect(createGroupFn).not.toHaveBeenCalled();
  });

  it('first delete click shows Confirm, second click actually deletes', async () => {
    const user = userEvent.setup();
    const deleteGroupFn = vi.fn().mockResolvedValue(undefined);
    useDeviceStore.setState({ deleteGroup: deleteGroupFn });

    render(<GroupSidebar />);
    const deleteButtons = screen.getAllByText('x');

    // First click → Confirm shown.
    await user.click(deleteButtons[0]!);
    expect(screen.getByText('Confirm?')).toBeInTheDocument();
    expect(deleteGroupFn).not.toHaveBeenCalled();

    // Second click on same button → actual delete called.
    await user.click(screen.getByText('Confirm?'));
    expect(deleteGroupFn).toHaveBeenCalledWith('g1');
  });

  it('non-active groups use the gray text style; active uses white-on-gray', () => {
    render(<GroupSidebar />);
    const groupB = screen.getByText('Group B').closest('div');
    expect(groupB?.className).toContain('text-gray-400');
    expect(groupB?.className).not.toContain('bg-gray-700 text-white');
  });

  it('delete button title flips between default and confirm text', async () => {
    const user = userEvent.setup();
    render(<GroupSidebar />);
    const deleteButtons = screen.getAllByText('x');
    expect(deleteButtons[0]!.getAttribute('title')).toBe('Delete group');

    await user.click(deleteButtons[0]!);
    expect(screen.getByText('Confirm?').getAttribute('title')).toBe('Click again to confirm');
  });

  it('clicking another group delete button moves confirmation focus (only one Confirm? rendered)', async () => {
    const user = userEvent.setup();
    render(<GroupSidebar />);
    const deleteButtons = screen.getAllByText('x');
    await user.click(deleteButtons[0]!);
    expect(screen.getByText('Confirm?')).toBeInTheDocument();
    await user.click(screen.getByText('x'));
    expect(screen.getAllByText('Confirm?').length).toBe(1);
  });

  it('+ New button toggles label to Cancel when the form is open', async () => {
    const user = userEvent.setup();
    render(<GroupSidebar />);
    await user.click(screen.getByText('+ New'));
    expect(screen.getByText('Cancel')).toBeInTheDocument();
    expect(screen.queryByText('+ New')).toBeNull();

    await user.click(screen.getByText('Cancel'));
    expect(screen.getByText('+ New')).toBeInTheDocument();
    expect(screen.queryByText('Cancel')).toBeNull();
  });

  it('Groups heading is rendered as a heading element', () => {
    render(<GroupSidebar />);
    const heading = screen.getByRole('heading', { name: 'Groups' });
    expect(heading.tagName).toBe('H2');
  });
});
