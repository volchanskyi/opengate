import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { useDeviceStore } from '../../state/device-store';
import { useUpdateStore } from '../../state/update-store';
import { useToastStore } from '../../state/toast-store';
import { DeviceList } from './DeviceList';

vi.mock('../../lib/api', () => ({
  api: {
    GET: vi.fn().mockResolvedValue({ data: [], error: undefined }),
    POST: vi.fn().mockResolvedValue({ data: { id: 'new', name: 'New' }, error: undefined }),
    DELETE: vi.fn().mockResolvedValue({ error: undefined }),
  },
}));

function renderDeviceList() {
  const router = createMemoryRouter(
    [
      { path: '/devices', element: <DeviceList /> },
      { path: '/devices/:id', element: <p>Device Detail</p> },
    ],
    { initialEntries: ['/devices'] },
  );
  return render(<RouterProvider router={router} />);
}

describe('DeviceList', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useDeviceStore.setState({
      groups: [{ id: 'g1', name: 'Group A', owner_id: 'u1', created_at: '', updated_at: '' }],
      devices: [],
      selectedGroupId: null,
      selectedDevice: null,
      isLoading: false,
      error: null,
      fetchGroups: vi.fn(),
      fetchDevices: vi.fn(),
    });
  });

  it('shows welcome message when no devices exist', () => {
    renderDeviceList();
    expect(screen.getByText('Welcome to OpenGate')).toBeInTheDocument();
    expect(screen.getByText('Add Device')).toBeInTheDocument();
  });

  it('shows empty group message when group selected but empty', () => {
    useDeviceStore.setState({ selectedGroupId: 'g1' });
    renderDeviceList();
    expect(screen.getByText('No devices in this group')).toBeInTheDocument();
    expect(screen.getByText('Add Device')).toBeInTheDocument();
  });

  it('renders devices', () => {
    useDeviceStore.setState({
      devices: [
        { id: 'd1', group_id: 'g1', hostname: 'host-1', os: 'linux', agent_version: '1.0.0', capabilities: [], status: 'online', last_seen: new Date().toISOString(), created_at: '', updated_at: '' },
        { id: 'd2', group_id: 'g1', hostname: 'host-2', os: 'windows', agent_version: '', capabilities: [], status: 'offline', last_seen: new Date().toISOString(), created_at: '', updated_at: '' },
      ],
    });
    renderDeviceList();
    expect(screen.getByText('host-1')).toBeInTheDocument();
    expect(screen.getByText('host-2')).toBeInTheDocument();
  });

  it('shows loading skeleton', () => {
    useDeviceStore.setState({ isLoading: true });
    renderDeviceList();
    const skeletons = document.querySelectorAll('.animate-pulse');
    expect(skeletons.length).toBeGreaterThan(0);
  });

  it('fetches groups and devices on mount', () => {
    const fetchGroupsFn = vi.fn();
    const fetchDevicesFn = vi.fn();
    useDeviceStore.setState({ fetchGroups: fetchGroupsFn, fetchDevices: fetchDevicesFn });
    renderDeviceList();
    expect(fetchGroupsFn).toHaveBeenCalled();
    expect(fetchDevicesFn).toHaveBeenCalled();
  });

  it('polls devices every 15 seconds', () => {
    vi.useFakeTimers();
    const fetchDevicesFn = vi.fn();
    useDeviceStore.setState({ fetchDevices: fetchDevicesFn });
    renderDeviceList();

    // Initial fetch on mount
    expect(fetchDevicesFn).toHaveBeenCalledTimes(1);

    // Advance 15s — should trigger second fetch
    vi.advanceTimersByTime(15_000);
    expect(fetchDevicesFn).toHaveBeenCalledTimes(2);

    // Advance another 15s — third fetch
    vi.advanceTimersByTime(15_000);
    expect(fetchDevicesFn).toHaveBeenCalledTimes(3);

    vi.useRealTimers();
  });

  describe('search filter', () => {
    beforeEach(() => {
      useDeviceStore.setState({
        devices: [
          { id: 'd1', group_id: 'g1', hostname: 'web-01', os: 'linux', agent_version: '1.0.0', capabilities: [], status: 'online', last_seen: '', created_at: '', updated_at: '' },
          { id: 'd2', group_id: 'g1', hostname: 'db-01', os: 'windows', agent_version: '1.0.0', capabilities: [], status: 'online', last_seen: '', created_at: '', updated_at: '' },
          { id: 'd3', group_id: 'g1', hostname: 'cache-01', os: 'darwin', agent_version: '1.0.0', capabilities: [], status: 'online', last_seen: '', created_at: '', updated_at: '' },
        ],
      });
    });

    it('filters by hostname substring (case-insensitive)', async () => {
      renderDeviceList();
      // Initially all three rendered.
      expect(screen.getByText('web-01')).toBeInTheDocument();
      expect(screen.getByText('db-01')).toBeInTheDocument();
      expect(screen.getByText('cache-01')).toBeInTheDocument();

      const search = screen.getByPlaceholderText(/search/i);
      // Type uppercase to prove case insensitivity (kills toLowerCase → toUpperCase mutant).
      // The bar debounces by 300ms, so we waitFor the filter to settle.
      await userEvent.type(search, 'WEB');

      await waitFor(() => {
        expect(screen.queryByText('db-01')).toBeNull();
      });
      expect(screen.getByText('web-01')).toBeInTheDocument();
      expect(screen.queryByText('cache-01')).toBeNull();
    });

    it('filters by os (matches when hostname does not)', async () => {
      renderDeviceList();
      const search = screen.getByPlaceholderText(/search/i);
      // 'linux' matches d1.os but no hostname — kills the OR-arm collapse mutants
      // (ConditionalExpression: 'false', LogicalOperator: '&&').
      await userEvent.type(search, 'linux');
      await waitFor(() => {
        expect(screen.queryByText('db-01')).toBeNull();
      });
      expect(screen.getByText('web-01')).toBeInTheDocument();
      expect(screen.queryByText('cache-01')).toBeNull();
    });

    it('shows search-no-match message when query matches nothing', async () => {
      renderDeviceList();
      const search = screen.getByPlaceholderText(/search/i);
      await userEvent.type(search, 'nonexistent-xyz');
      await waitFor(() => {
        expect(screen.getByText('No devices match your search')).toBeInTheDocument();
      });
    });
  });

  describe('upgrade-all flow', () => {
    beforeEach(() => {
      useToastStore.setState({ toasts: [] });
      // Two devices: one outdated online (will upgrade), one current (will not).
      useDeviceStore.setState({
        devices: [
          { id: 'old', group_id: 'g1', hostname: 'outdated', os: 'linux', agent_version: '1.0.0', capabilities: [], status: 'online', last_seen: '', created_at: '', updated_at: '' },
          { id: 'cur', group_id: 'g1', hostname: 'current', os: 'linux', agent_version: '2.0.0', capabilities: [], status: 'online', last_seen: '', created_at: '', updated_at: '' },
          { id: 'off', group_id: 'g1', hostname: 'offline-old', os: 'linux', agent_version: '1.0.0', capabilities: [], status: 'offline', last_seen: '', created_at: '', updated_at: '' },
        ],
        upgradeAgent: vi.fn().mockResolvedValue(true),
      });
      useUpdateStore.setState({
        manifests: [
          { version: '2.0.0', os: 'linux', arch: 'amd64', sha256: '', url: '', released_at: '', signed: true } as never,
        ],
        fetchManifests: vi.fn(),
      });
    });

    it('shows Upgrade-All button only when there are outdated online devices', () => {
      renderDeviceList();
      // Only one device (id=old) is outdated AND online — kills:
      // - status `===` → `!==` mutant (would include offline-old, count=2)
      // - localeCompare `<` → `>=` / `<=` mutant (would zero count)
      expect(screen.getByText(/Upgrade All Agents \(1\)/)).toBeInTheDocument();
    });

    it('hides Upgrade-All button when no outdated online devices', () => {
      // Wipe the manifests list — nothing to upgrade to.
      useUpdateStore.setState({ manifests: [] });
      renderDeviceList();
      expect(screen.queryByText(/Upgrade All/)).toBeNull();
    });

    it('upgradeAgent is called once for the outdated device on Upgrade-All click', async () => {
      const upgradeAgentFn = vi.fn().mockResolvedValue(true);
      useDeviceStore.setState({ upgradeAgent: upgradeAgentFn });
      renderDeviceList();

      const button = screen.getByText(/Upgrade All Agents/);
      await userEvent.click(button);

      // Exactly one call — for the single outdated device.
      // Args: (deviceId, version, os, arch). Pins the manifest fields.
      expect(upgradeAgentFn).toHaveBeenCalledTimes(1);
      expect(upgradeAgentFn).toHaveBeenCalledWith('old', '2.0.0', 'linux', 'amd64');
    });

    it('shows success toast when all upgrades succeed', async () => {
      const addToastFn = vi.fn();
      useToastStore.setState({ addToast: addToastFn });
      renderDeviceList();
      const button = screen.getByText(/Upgrade All Agents/);
      await userEvent.click(button);
      // Pins the success path's toast level → kills the level-mutant on 'success'/'error'.
      expect(addToastFn).toHaveBeenCalledWith(expect.stringContaining('Upgrade pushed to 1'), 'success');
    });

    it('shows error toast when at least one upgrade fails', async () => {
      const addToastFn = vi.fn();
      useToastStore.setState({ addToast: addToastFn });
      const upgradeAgentFn = vi.fn().mockResolvedValue(false);
      useDeviceStore.setState({ upgradeAgent: upgradeAgentFn });
      renderDeviceList();
      const button = screen.getByText(/Upgrade All Agents/);
      await userEvent.click(button);
      // Branch: failed > 0 → 'error' toast — kills `if (failed === 0)` flip mutants.
      expect(addToastFn).toHaveBeenCalledWith(expect.stringMatching(/Upgraded 0, failed 1/), 'error');
    });
  });
});
