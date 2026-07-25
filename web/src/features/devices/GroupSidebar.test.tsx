import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useToastStore } from '../../lib/feedback/toast-store';
import { useDeviceStore } from './state/device-store';
import { DEVICE_DRAG_MIME, UNGROUPED_GROUP_ID } from './device-drag';
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

  describe('drop target for device drags', () => {
    const device = {
      id: 'd1', group_id: 'g1', hostname: 'web-01', os: 'linux', agent_version: '1.0.0',
      capabilities: [], status: 'online' as const, last_seen: '', created_at: '', updated_at: '',
    };

    /** A DataTransfer stand-in carrying a device drag. */
    const deviceTransfer = (id = 'd1') => ({
      types: [DEVICE_DRAG_MIME],
      getData: (type: string) => (type === DEVICE_DRAG_MIME ? id : ''),
      dropEffect: 'none',
    });

    const dropZone = (name: string) => screen.getByRole('listitem', { name });

    beforeEach(() => {
      useDeviceStore.setState({ devices: [device] });
    });

    it('dropping a device on a group moves it there', async () => {
      const updateDeviceGroup = vi.fn().mockResolvedValue(true);
      const fetchDevices = vi.fn().mockResolvedValue(undefined);
      useDeviceStore.setState({ updateDeviceGroup, fetchDevices });
      render(<GroupSidebar />);

      fireEvent.drop(dropZone('Group B'), { dataTransfer: deviceTransfer() });

      await waitFor(() => { expect(updateDeviceGroup).toHaveBeenCalledWith('d1', 'g2'); });
      // The list is re-pulled for the active filter so the card leaves the view.
      await waitFor(() => { expect(fetchDevices).toHaveBeenCalledWith('g1'); });
    });

    it('dropping on the Ungrouped zone clears the device group', async () => {
      const updateDeviceGroup = vi.fn().mockResolvedValue(true);
      useDeviceStore.setState({ updateDeviceGroup, fetchDevices: vi.fn() });
      render(<GroupSidebar />);

      fireEvent.drop(dropZone('Ungrouped'), { dataTransfer: deviceTransfer() });

      await waitFor(() => { expect(updateDeviceGroup).toHaveBeenCalledWith('d1', UNGROUPED_GROUP_ID); });
    });

    it('names the device and the destination in the success toast', async () => {
      const addToast = vi.fn();
      useToastStore.setState({ addToast });
      useDeviceStore.setState({ updateDeviceGroup: vi.fn().mockResolvedValue(true), fetchDevices: vi.fn() });
      render(<GroupSidebar />);

      fireEvent.drop(dropZone('Group B'), { dataTransfer: deviceTransfer() });

      await waitFor(() => { expect(addToast).toHaveBeenCalledWith('Moved web-01 to Group B', 'success'); });
    });

    it('reports a failed move and does not re-pull the list', async () => {
      const addToast = vi.fn();
      const fetchDevices = vi.fn();
      useToastStore.setState({ addToast });
      useDeviceStore.setState({ updateDeviceGroup: vi.fn().mockResolvedValue(false), fetchDevices });
      render(<GroupSidebar />);

      fireEvent.drop(dropZone('Group B'), { dataTransfer: deviceTransfer() });

      await waitFor(() => { expect(addToast).toHaveBeenCalledWith('Failed to move web-01 to Group B', 'error'); });
      expect(fetchDevices).not.toHaveBeenCalled();
    });

    it('dropping a device on the group it is already in is a no-op', async () => {
      const updateDeviceGroup = vi.fn();
      useDeviceStore.setState({ updateDeviceGroup, fetchDevices: vi.fn() });
      render(<GroupSidebar />);

      fireEvent.drop(dropZone('Group A'), { dataTransfer: deviceTransfer() });

      await waitFor(() => { expect(updateDeviceGroup).not.toHaveBeenCalled(); });
    });

    it('ignores a drop that carries no device', async () => {
      const updateDeviceGroup = vi.fn();
      useDeviceStore.setState({ updateDeviceGroup, fetchDevices: vi.fn() });
      render(<GroupSidebar />);

      fireEvent.drop(dropZone('Group B'), {
        dataTransfer: {
          types: ['text/plain'],
          getData: (type: string) => (type === 'text/plain' ? 'hello' : ''),
          dropEffect: 'none',
        },
      });

      await waitFor(() => { expect(updateDeviceGroup).not.toHaveBeenCalled(); });
    });

    it('highlights only the hovered zone while a device drag is over it', () => {
      useDeviceStore.setState({ updateDeviceGroup: vi.fn(), fetchDevices: vi.fn() });
      render(<GroupSidebar />);
      const target = dropZone('Group B');

      fireEvent.dragOver(target, { dataTransfer: deviceTransfer() });
      expect(target).toHaveClass('ring-2');
      expect(dropZone('Group A')).not.toHaveClass('ring-2');

      fireEvent.dragLeave(target);
      expect(target).not.toHaveClass('ring-2');
    });

    it('does not highlight for a drag that carries no device', () => {
      render(<GroupSidebar />);
      const target = dropZone('Group B');

      fireEvent.dragOver(target, { dataTransfer: { types: ['text/plain'], getData: () => '', dropEffect: 'none' } });

      expect(target).not.toHaveClass('ring-2');
    });
  });
});
