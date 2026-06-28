import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { useDeviceStore } from './state/device-store';
import { useUpdateStore } from './state/update-store';
import { useToastStore } from '../../lib/feedback/toast-store';
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

    it('pluralizes the success toast count (2 devices → "2 devices", succeeded !== 1)', async () => {
      const addToastFn = vi.fn();
      useToastStore.setState({ addToast: addToastFn });
      useDeviceStore.setState({
        devices: [
          { id: 'old1', group_id: 'g1', hostname: 'h1', os: 'linux', agent_version: '1.0.0', capabilities: [], status: 'online', last_seen: '', created_at: '', updated_at: '' },
          { id: 'old2', group_id: 'g1', hostname: 'h2', os: 'linux', agent_version: '1.0.0', capabilities: [], status: 'online', last_seen: '', created_at: '', updated_at: '' },
        ],
        upgradeAgent: vi.fn().mockResolvedValue(true),
      });
      renderDeviceList();
      await userEvent.click(screen.getByText(/Upgrade All Agents/));
      // Kills the `succeeded !== 1 ? 's' : ''` plural-suffix mutant: must say "2 devices", not "2 device".
      expect(addToastFn).toHaveBeenCalledWith('Upgrade pushed to 2 devices', 'success');
    });

    it('uses singular phrasing when exactly one device upgrades', async () => {
      const addToastFn = vi.fn();
      useToastStore.setState({ addToast: addToastFn });
      renderDeviceList();
      await userEvent.click(screen.getByText(/Upgrade All Agents/));
      // succeeded === 1 → no plural suffix; mutant `succeeded !== 1` becomes `succeeded === 1` → wrong branch.
      expect(addToastFn).toHaveBeenCalledWith('Upgrade pushed to 1 device', 'success');
    });

    it('Upgrade All button label shows the outdated count', () => {
      // The button text includes the literal count and the literal "Upgrade All Agents" string —
      // pins the StringLiteral mutant on the label template.
      renderDeviceList();
      expect(screen.getByRole('button', { name: 'Upgrade All Agents (1)' })).toBeInTheDocument();
    });

    it('Upgrade All button label flips to "Upgrading..." while in-flight', async () => {
      let resolve: (v: boolean) => void = () => undefined;
      useDeviceStore.setState({
        upgradeAgent: vi.fn().mockReturnValue(new Promise<boolean>((r) => { resolve = r; })),
      });
      renderDeviceList();
      await userEvent.click(screen.getByText(/Upgrade All Agents/));
      // Kills the `isUpgradingAll ? 'Upgrading...' : ...` ConditionalExpression mutant on both branches.
      expect(await screen.findByText('Upgrading...')).toBeInTheDocument();
      const btn = screen.getByText('Upgrading...').closest('button') as HTMLButtonElement;
      expect(btn.disabled).toBe(true);
      resolve(true);
    });

    it('outdated filter uses numeric version comparison ("10.0.0" > "2.0.0")', async () => {
      const upgradeAgentFn = vi.fn().mockResolvedValue(true);
      useDeviceStore.setState({
        devices: [
          // device.agent_version = "2.0.0" but latest = "10.0.0" — lexicographic compare would say
          // "2.0.0" >= "10.0.0" (since '2' > '1') and treat the device as up to date. Numeric
          // comparison correctly returns -1, so the device is outdated and gets upgraded.
          { id: 'd', group_id: 'g1', hostname: 'h', os: 'linux', agent_version: '2.0.0', capabilities: [], status: 'online', last_seen: '', created_at: '', updated_at: '' },
        ],
        upgradeAgent: upgradeAgentFn,
      });
      useUpdateStore.setState({
        manifests: [
          { version: '10.0.0', os: 'linux', arch: 'amd64', sha256: '', url: '', released_at: '', signed: true } as never,
        ],
        fetchManifests: vi.fn(),
      });
      renderDeviceList();
      expect(screen.getByText(/Upgrade All Agents \(1\)/)).toBeInTheDocument();
      await userEvent.click(screen.getByText(/Upgrade All Agents/));
      expect(upgradeAgentFn).toHaveBeenCalledWith('d', '10.0.0', 'linux', 'amd64');
    });

    it('outdated filter respects per-OS scoping (a linux manifest does not bump a windows device)', () => {
      useDeviceStore.setState({
        devices: [
          { id: 'win', group_id: 'g1', hostname: 'winhost', os: 'windows', agent_version: '1.0.0', capabilities: [], status: 'online', last_seen: '', created_at: '', updated_at: '' },
        ],
      });
      useUpdateStore.setState({
        manifests: [
          { version: '99.0.0', os: 'linux', arch: 'amd64', sha256: '', url: '', released_at: '', signed: true } as never,
        ],
        fetchManifests: vi.fn(),
      });
      renderDeviceList();
      // Manifest does not match windows → no outdated devices → button hidden.
      // Kills the `manifests.filter(m => m.os === d.os)` → `manifests` MethodExpression mutant.
      expect(screen.queryByText(/Upgrade All/)).toBeNull();
    });

    it('outdated filter excludes offline outdated devices', () => {
      useDeviceStore.setState({
        devices: [
          { id: 'on', group_id: 'g1', hostname: 'on', os: 'linux', agent_version: '1.0.0', capabilities: [], status: 'online', last_seen: '', created_at: '', updated_at: '' },
          { id: 'off', group_id: 'g1', hostname: 'off', os: 'linux', agent_version: '1.0.0', capabilities: [], status: 'offline', last_seen: '', created_at: '', updated_at: '' },
        ],
      });
      renderDeviceList();
      // Only the online outdated device counts. Kills `d.status === 'online'` → `d.status !== 'online'`.
      expect(screen.getByText(/Upgrade All Agents \(1\)/)).toBeInTheDocument();
    });

    it('handleUpgradeAll is a no-op when there are no outdated devices', async () => {
      const upgradeAgentFn = vi.fn().mockResolvedValue(true);
      useUpdateStore.setState({ manifests: [], fetchManifests: vi.fn() });
      useDeviceStore.setState({ upgradeAgent: upgradeAgentFn });
      renderDeviceList();
      // Button is hidden when outdatedDevices.length === 0 — but even if it were called the body
      // bails out. We verify the no-op by asserting no upgrade calls fire on render.
      expect(upgradeAgentFn).not.toHaveBeenCalled();
    });
  });

  describe('empty-state messaging', () => {
    it('uses different copy when filtering vs. browsing', async () => {
      useDeviceStore.setState({
        devices: [
          { id: 'd', group_id: 'g1', hostname: 'h', os: 'linux', agent_version: '1.0.0', capabilities: [], status: 'online', last_seen: '', created_at: '', updated_at: '' },
        ],
      });
      renderDeviceList();
      // Type a search term that matches nothing → "Try a different search term."
      const search = screen.getByPlaceholderText(/search/i);
      await userEvent.type(search, 'zzznomatch');
      await waitFor(() => {
        expect(screen.getByText('Try a different search term.')).toBeInTheDocument();
      });
      expect(screen.queryByText(/Download and install/)).toBeNull();
    });

    it('uses group-specific copy when a group is selected but empty', () => {
      useDeviceStore.setState({ selectedGroupId: 'g1', devices: [] });
      renderDeviceList();
      expect(screen.getByText('No devices in this group')).toBeInTheDocument();
      // Kills the StringLiteral mutant on the group-empty body text.
      expect(screen.getByText('Download and install the agent to add devices.')).toBeInTheDocument();
    });

    it('uses welcome copy when no group is selected and no devices exist', () => {
      useDeviceStore.setState({ selectedGroupId: null, devices: [] });
      renderDeviceList();
      expect(screen.getByText('Welcome to OpenGate')).toBeInTheDocument();
      expect(screen.getByText('Select a group to filter devices, or add a new device to get started.')).toBeInTheDocument();
    });
  });

  describe('virtualization', () => {
    it('windows a large device list (renders a subset, not every card)', () => {
      const many = Array.from({ length: 300 }, (_, i) => ({
        id: 'd' + String(i),
        group_id: 'g1',
        hostname: 'host-' + String(i),
        os: 'linux',
        agent_version: '1.0.0',
        capabilities: [],
        status: 'online' as const,
        last_seen: '',
        created_at: '',
        updated_at: '',
      }));
      useDeviceStore.setState({ devices: many });
      renderDeviceList();

      // The first card is in the rendered window...
      expect(screen.getByText('host-0')).toBeInTheDocument();
      // ...but a far-off card is virtualized away (not in the DOM).
      expect(screen.queryByText('host-299')).toBeNull();
      // Only a windowed subset of the 300 cards is mounted.
      const renderedHostnames = document.querySelectorAll('h3');
      expect(renderedHostnames.length).toBeGreaterThan(0);
      expect(renderedHostnames.length).toBeLessThan(300);
    });
  });

  it('Device grid is hidden while isLoading is true', () => {
    useDeviceStore.setState({
      isLoading: true,
      devices: [
        { id: 'd', group_id: 'g1', hostname: 'visible-host', os: 'linux', agent_version: '1.0.0', capabilities: [], status: 'online', last_seen: '', created_at: '', updated_at: '' },
      ],
    });
    renderDeviceList();
    // The DeviceCard for 'visible-host' must NOT render while loading — kills the
    // `!isLoading && filteredDevices.length > 0` LogicalOperator mutants that flip || ↔ &&.
    expect(screen.queryByText('visible-host')).toBeNull();
  });
});
