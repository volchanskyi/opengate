import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { useDeviceStore } from './state/device-store';
import { useUpdateStore } from './state/update-store';
import { useInventoryStore } from './state/inventory-store';
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
    useInventoryStore.setState({ byDevice: new Map(), loading: new Map(), errors: new Map(), fetchInventory: vi.fn() });
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

  it('lazily fetches inventory for the rendered devices', async () => {
    const fetchInventory = vi.fn();
    useInventoryStore.setState({ fetchInventory });
    useDeviceStore.setState({
      devices: [
        { id: 'd1', group_id: 'g1', hostname: 'host-1', os: 'linux', agent_version: '1.0.0', capabilities: [], status: 'online', last_seen: new Date().toISOString(), created_at: '', updated_at: '' },
        { id: 'd2', group_id: 'g1', hostname: 'host-2', os: 'windows', agent_version: '', capabilities: [], status: 'offline', last_seen: new Date().toISOString(), created_at: '', updated_at: '' },
      ],
    });
    renderDeviceList();
    await waitFor(() => {
      expect(fetchInventory).toHaveBeenCalledWith('d1');
      expect(fetchInventory).toHaveBeenCalledWith('d2');
    });
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

  describe('virtualized grid layout', () => {
    function makeDevices(n: number) {
      return Array.from({ length: n }, (_, i) => ({
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
    }

    it('lays out cards in 3 responsive columns and ceil(n/columns) rows at desktop width', () => {
      // vitest.setup mocks every element to 1200px wide → the >= 1024 breakpoint → 3 columns.
      useDeviceStore.setState({ devices: makeDevices(6) });
      renderDeviceList();

      const firstRow = screen.getByText('host-0').closest('div.grid.gap-4') as HTMLElement;
      expect(firstRow).not.toBeNull();
      // 3 columns: kills the useColumnCount effect/update BlockStatement removals (which leave
      // the default 1 column), the COLUMN_BREAKPOINTS.find predicate mutants, and the
      // gridTemplateColumns StringLiteral on the row style.
      expect(firstRow).toHaveStyle({ gridTemplateColumns: 'repeat(3, minmax(0, 1fr))' });
      // Row 0 holds the first 3 devices (slice [0,3)); row 1 holds the next 3 — validates the
      // `index * columns` slice offset and the column count together.
      expect(within(firstRow).getByText('host-0')).toBeInTheDocument();
      expect(within(firstRow).getByText('host-2')).toBeInTheDocument();
      expect(within(firstRow).queryByText('host-3')).toBeNull();

      // 6 devices / 3 columns = 2 virtual rows. The `length / columns` → `length * columns`
      // ArithmeticOperator mutant inflates rowCount and renders extra (empty) row elements.
      const container = firstRow.parentElement as HTMLElement;
      const rows = container.querySelectorAll(':scope > div.grid.gap-4');
      expect(rows).toHaveLength(2);
    });

    it('absolutely positions each virtual row by translateY inside a relative, sized container', () => {
      useDeviceStore.setState({ devices: makeDevices(6) });
      renderDeviceList();

      const firstRow = screen.getByText('host-0').closest('div.grid.gap-4') as HTMLElement;
      const container = firstRow.parentElement as HTMLElement;

      // Wrapper: position relative + full width + non-zero height (the virtualizer total
      // size). Kills the ObjectLiteral→{} and the position/width StringLiteral mutants.
      expect(container).toHaveStyle({ position: 'relative', width: '100%' });
      expect(Number.parseFloat(container.style.height)).toBeGreaterThan(0);

      // Row: absolutely positioned, full width, transformed to its virtual offset (0px for the
      // first row). Kills the transform/position/width StringLiteral and ObjectLiteral mutants.
      expect(firstRow).toHaveStyle({
        position: 'absolute',
        width: '100%',
        transform: 'translateY(0px)',
      });
    });

    it('drops to 2 columns at the md breakpoint (>= is inclusive; > would fall through to 1)', () => {
      const originalRect = Element.prototype.getBoundingClientRect;
      try {
        // Exactly 768px → matches the 768 breakpoint via `>=` (2 columns). The
        // `width > b.minWidth` EqualityOperator mutant fails 768 > 768 and falls to 1 column;
        // the `(b) => true` ConditionalExpression mutant would match the first (1024 → 3) entry.
        Element.prototype.getBoundingClientRect = () =>
          ({ width: 768, height: 800, top: 0, left: 0, right: 768, bottom: 800, x: 0, y: 0, toJSON: () => ({}) }) as DOMRect;
        useDeviceStore.setState({ devices: makeDevices(2) });
        renderDeviceList();
        const firstRow = screen.getByText('host-0').closest('div.grid.gap-4') as HTMLElement;
        expect(firstRow).toHaveStyle({ gridTemplateColumns: 'repeat(2, minmax(0, 1fr))' });
      } finally {
        Element.prototype.getBoundingClientRect = originalRect;
      }
    });
  });
});
