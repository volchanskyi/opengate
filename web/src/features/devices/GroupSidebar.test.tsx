import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useDeviceStore } from '../../state/device-store';
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
});
