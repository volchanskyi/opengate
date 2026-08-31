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

describe('DeviceLabels — what it refuses and what it clears', () => {
  // A label is a key and a value together. Sending half of one would file a
  // label no rule can be aimed at, and the row would read as a broken entry.
  it('will not add a label that is missing half of itself', async () => {
    const createLabel = vi.fn().mockResolvedValue(true);
    show(true);
    useDeviceTagsStore.setState({ createLabel });

    await userEvent.type(screen.getByLabelText('Key'), 'env');
    await userEvent.click(screen.getByRole('button', { name: 'Add label' }));
    expect(createLabel).not.toHaveBeenCalled();

    await userEvent.clear(screen.getByLabelText('Key'));
    await userEvent.type(screen.getByLabelText('Value'), 'production');
    await userEvent.click(screen.getByRole('button', { name: 'Add label' }));
    expect(createLabel).not.toHaveBeenCalled();
  });

  // Adding role=file-server is usually followed by role=workstation, so the key
  // stays and only the value clears — retyping the key every time is the cost
  // of clearing both.
  it('keeps the key and clears the value once a label is added', async () => {
    show(true);
    useDeviceTagsStore.setState({ createLabel: vi.fn().mockResolvedValue(true) });

    await userEvent.type(screen.getByLabelText('Key'), 'env');
    await userEvent.type(screen.getByLabelText('Value'), 'production');
    await userEvent.click(screen.getByRole('button', { name: 'Add label' }));

    expect(screen.getByLabelText('Key')).toHaveValue('env');
    expect(screen.getByLabelText('Value')).toHaveValue('');
  });

  it('will not assign a label to nobody', async () => {
    const assignLabel = vi.fn().mockResolvedValue(true);
    show(true);
    useDeviceTagsStore.setState({ assignLabel });

    await userEvent.type(screen.getByLabelText('Machines to label'), '  ,  ');
    await userEvent.click(screen.getByRole('button', { name: 'Assign' }));

    expect(assignLabel).not.toHaveBeenCalled();
  });

  // A pasted list of machines arrives however the operator had it — commas,
  // spaces, newlines, or several at once. All of them name the same machines.
  it('takes a pasted list however it was separated', async () => {
    const assignLabel = vi.fn().mockResolvedValue(true);
    show(true);
    useDeviceTagsStore.setState({ assignLabel });

    await userEvent.type(screen.getByLabelText('Machines to label'), 'fs03 fs04,  fs05');
    await userEvent.click(screen.getByRole('button', { name: 'Assign' }));

    expect(assignLabel).toHaveBeenCalledWith('label-1', ['fs03', 'fs04', 'fs05']);
    expect(screen.getByLabelText('Machines to label')).toHaveValue('');
  });
});

describe('DeviceLabels — counting and waiting', () => {
  /** Renders with the store set exactly as given, which the spinner cases need. */
  function showState(over: Partial<ReturnType<typeof useDeviceTagsStore.getState>>) {
    useAuthStore.setState({
      user: { id: 'u1', email: 'x@example.com', display_name: 'X', is_admin: true },
    } as never);
    useDeviceTagsStore.setState({
      labels: [], assignments: [], isLoading: false, error: null,
      fetchTags: async () => {},
      ...over,
    });
    render(
      <MemoryRouter>
        <DeviceLabels />
      </MemoryRouter>,
    );
  }

  it('shows the wait instead of an empty list on the first read', () => {
    showState({ isLoading: true, labels: [] });
    expect(screen.queryByText('This customer has no labels yet.')).not.toBeInTheDocument();
  });

  // A refresh over a list already on screen keeps it there; swapping a good
  // list for a spinner on every poll would make the page flicker.
  it('keeps the list on screen while a refresh is in flight', () => {
    showState({ isLoading: true, labels: [fileServer] });
    const labelList = screen.getAllByRole('table')[0];
    expect(within(labelList!).getByRole('row', { name: /role=file-server/ })).toBeInTheDocument();
  });

  // A label is a key and a value together, so role=workstation is not
  // role=file-server. Counting by the key alone would report the whole estate
  // as carrying whichever value happened to be listed first.
  it('counts only the machines carrying that exact key and value', () => {
    showState({
      labels: [fileServer],
      assignments: [
        { device_id: 'fs01', tags: { role: 'file-server' } },
        { device_id: 'ws01', tags: { role: 'workstation' } },
        { device_id: 'ws02', tags: { env: 'file-server' } },
      ],
    });

    const labelList = screen.getAllByRole('table')[0];
    const row = within(labelList!).getByRole('row', { name: /role=file-server/ });
    expect(within(row).getByText('1')).toBeInTheDocument();
  });
});
