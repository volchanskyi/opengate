import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router';
import type { components } from '../../types/api';
import { api } from '../../lib/api';
import { useAuthStore } from '../../state/auth-store';
import { useOrganizationStore } from '../organizations';
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

function device(id: string, hostname: string, status: 'online' | 'offline') {
  return {
    id, hostname, status, organization_id: 'org-1', site_id: '', os: 'linux', agent_version: '',
    capabilities: [], last_seen: '', created_at: '', updated_at: '',
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(api.GET).mockResolvedValue({
    data: [
      device('fs02', 'file-server-02', 'online'),
      device('fs01', 'file-server-01', 'online'),
      device('fs04', 'Reception-PC', 'offline'),
      device('fs03', 'backup-01', 'online'),
    ],
    response: { ok: true, status: 200 },
  } as never);
  useOrganizationStore.setState({ selectedOrganizationId: 'org-1', customerWanted: 0 });
});

async function checklist() {
  return within(await screen.findByRole('group', { name: 'Hosts to label' }));
}

describe('DeviceLabels', () => {
  it('counts the hosts carrying each label', () => {
    show(false);
    // The first table is the label list; the second is the per-host view.
    const labelList = screen.getAllByRole('table')[0];
    const row = within(labelList!).getByRole('row', { name: /role=file-server/ });
    expect(within(row).getByText('2')).toBeInTheDocument();
  });

  it('opening the page reads the labels and who carries them', () => {
    const fetchTags = vi.fn().mockResolvedValue(undefined);
    useAuthStore.setState({ user: { id: 'u1', email: 'x@example.com', is_admin: false } } as never);
    useDeviceTagsStore.setState({ labels: [fileServer], assignments: [], isLoading: false, error: null, fetchTags });
    render(<MemoryRouter><DeviceLabels /></MemoryRouter>);
    expect(fetchTags).toHaveBeenCalledTimes(1);
  });

  it('lets an administrator remove a label from the list', async () => {
    const deleteLabel = vi.fn().mockResolvedValue(true);
    show(true);
    useDeviceTagsStore.setState({ deleteLabel });

    const labelList = screen.getAllByRole('table')[0]!;
    const row = within(labelList).getByRole('row', { name: /role=file-server/ });
    await userEvent.click(within(row).getByRole('button', { name: 'Remove' }));

    expect(deleteLabel).toHaveBeenCalledWith('label-1');
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

  it('labels a set of hosts at once, picked from a checklist', async () => {
    const assignLabel = vi.fn().mockResolvedValue(true);
    show(true);
    useDeviceTagsStore.setState({ assignLabel });
    const user = userEvent.setup();

    const hosts = await checklist();
    await vi.waitFor(() => { expect(hosts.getAllByRole('checkbox')).toHaveLength(4); });
    await user.click(hosts.getByRole('checkbox', { name: /backup-01/ }));
    await user.click(hosts.getByRole('checkbox', { name: /Reception-PC/ }));
    await user.click(screen.getByRole('button', { name: 'Assign' }));

    expect(assignLabel).toHaveBeenCalledWith('label-1', ['fs03', 'fs04']);
    expect(hosts.getByRole('checkbox', { name: /backup-01/ })).not.toBeChecked();
  });

  it('lists the hosts by name with their state, so the right one is picked', async () => {
    show(true);
    const hosts = await checklist();
    await vi.waitFor(() => { expect(hosts.getAllByRole('checkbox')).toHaveLength(4); });
    expect(hosts.getAllByRole('checkbox').map((c) => c.closest('label')?.textContent))
      .toEqual(['backup-01online', 'file-server-01online', 'file-server-02online', 'Reception-PCoffline']);
  });

  it('waits on a customer under "All customers" before offering any host', () => {
    useOrganizationStore.setState({ selectedOrganizationId: null });
    show(true);
    expect(screen.getByText('Pick a customer first')).toBeInTheDocument();
    expect(screen.queryByRole('group', { name: 'Hosts to label' })).toBeNull();
    expect(useOrganizationStore.getState().customerWanted).toBe(1);
  });

  it('asks a reader with nothing to assign for no customer', () => {
    useOrganizationStore.setState({ selectedOrganizationId: null });
    show(false);
    expect(useOrganizationStore.getState().customerWanted).toBe(0);
  });

  it('says why a label a rule is aimed at could not be removed', () => {
    show(true, [fileServer], [], 'role=file-server is aimed at by 2 rule settings');
    expect(screen.getByRole('alert')).toHaveTextContent('aimed at by 2 rule settings');
  });

  it('takes one label key off one host, named by its hostname', async () => {
    const clearTag = vi.fn().mockResolvedValue(true);
    show(true);
    useDeviceTagsStore.setState({ clearTag });

    await userEvent.click(await screen.findByRole('button', { name: 'Take env off file-server-02' }));
    expect(clearTag).toHaveBeenCalledWith('fs02', 'env');
  });

  it('names each carrier by hostname, and an id it cannot name as a removed host', async () => {
    show(false, [fileServer], [
      { device_id: 'fs01', tags: { role: 'file-server' } },
      { device_id: 'gone-1', tags: { role: 'file-server' } },
    ]);
    const carriers = screen.getAllByRole('table')[1]!;
    expect(await within(carriers).findByText('file-server-01')).toBeInTheDocument();
    expect(within(carriers).getByText('a removed host')).toBeInTheDocument();
    expect(within(carriers).queryByText('gone-1')).toBeNull();
  });

  it('says an empty list is empty rather than showing nothing', () => {
    show(true, [], []);
    expect(screen.getByText('This customer has no labels yet.')).toBeInTheDocument();
    expect(screen.getByText('No host carries a label yet.')).toBeInTheDocument();
  });
});

describe('DeviceLabels — what it refuses and what it clears', () => {
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

    await checklist();
    await userEvent.click(screen.getByRole('button', { name: 'Assign' }));

    expect(assignLabel).not.toHaveBeenCalled();
  });
});

describe('DeviceLabels — counting and waiting', () => {
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

  it('keeps the list on screen while a refresh is in flight', () => {
    showState({ isLoading: true, labels: [fileServer] });
    const labelList = screen.getAllByRole('table')[0];
    expect(within(labelList!).getByRole('row', { name: /role=file-server/ })).toBeInTheDocument();
  });

  it('counts only the hosts carrying that exact key and value', () => {
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
