import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router';
import type { components } from '../../types/api';
import { useAuthStore } from '../../state/auth-store';
import { DeviceLabels } from './DeviceLabels';
import { useDeviceTagsStore } from './state/device-tags-store';

vi.mock('../../lib/api', () => ({
  api: { GET: vi.fn(), POST: vi.fn(), PUT: vi.fn(), DELETE: vi.fn() },
}));

type Label = components['schemas']['DeviceTagLabel'];
type Assignment = components['schemas']['DeviceTagAssignment'];

const fileServer: Label = { id: 'label-1', key: 'role', value: 'file-server', created_by: 'ivan' };

function show(isAdmin: boolean, labels: Label[] = [fileServer], assignments: Assignment[] = [
  { device_id: 'fs01', tags: { role: 'file-server' } },
  { device_id: 'fs02', tags: { role: 'file-server', env: 'production' } },
], error: string | null = null) {
  useAuthStore.setState({
    user: { id: 'u1', email: 'x@example.com', display_name: 'X', is_admin: isAdmin },
  } as never);
  useDeviceTagsStore.setState({
    labels, assignments, isLoading: false, error,
    fetchTags: async () => {},
  });
  render(
    <MemoryRouter>
      <DeviceLabels />
    </MemoryRouter>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe('DeviceLabels', () => {
  it('counts the machines carrying each label', () => {
    show(false);
    // The label list is the first table; the second is the machine-by-machine
    // view, whose rows name the same label.
    const labelList = screen.getAllByRole('table')[0];
    const row = within(labelList!).getByRole('row', { name: /role=file-server/ });
    expect(within(row).getByText('2')).toBeInTheDocument();
  });

  it('gives an ordinary member the list to read and nothing to change', () => {
    show(false);
    expect(screen.queryByRole('button', { name: 'Add label' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Remove' })).not.toBeInTheDocument();
  });

  it('lets an administrator add a label to the list', async () => {
    const createLabel = vi.fn().mockResolvedValue(true);
    show(true);
    useDeviceTagsStore.setState({ createLabel });

    await userEvent.type(screen.getByLabelText('Key'), 'env');
    await userEvent.type(screen.getByLabelText('Value'), 'production');
    await userEvent.click(screen.getByRole('button', { name: 'Add label' }));

    expect(createLabel).toHaveBeenCalledWith('env', 'production');
  });

  it('labels a set of machines at once rather than one page at a time', async () => {
    const assignLabel = vi.fn().mockResolvedValue(true);
    show(true);
    useDeviceTagsStore.setState({ assignLabel });

    await userEvent.type(screen.getByLabelText('Machines to label'), 'fs03, fs04');
    await userEvent.click(screen.getByRole('button', { name: 'Assign' }));

    expect(assignLabel).toHaveBeenCalledWith('label-1', ['fs03', 'fs04']);
  });

  it('says why a label a rule is aimed at could not be removed', () => {
    show(true, [fileServer], [], 'role=file-server is aimed at by 2 rule settings');
    expect(screen.getByRole('alert')).toHaveTextContent('aimed at by 2 rule settings');
  });

  it('takes one label key off one machine', async () => {
    const clearTag = vi.fn().mockResolvedValue(true);
    show(true);
    useDeviceTagsStore.setState({ clearTag });

    await userEvent.click(screen.getByRole('button', { name: 'Take env off fs02' }));
    expect(clearTag).toHaveBeenCalledWith('fs02', 'env');
  });

  it('says an empty list is empty rather than showing nothing', () => {
    show(true, [], []);
    expect(screen.getByText('This customer has no labels yet.')).toBeInTheDocument();
    expect(screen.getByText('No machine carries a label yet.')).toBeInTheDocument();
  });
});
