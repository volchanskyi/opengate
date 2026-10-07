import { act, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createMemoryRouter, RouterProvider } from 'react-router';
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

function renderDeviceList(initialEntry = '/devices') {
  const router = createMemoryRouter(
    [
      { path: '/devices', element: <DeviceList /> },
      { path: '/devices/:id', element: <p>Device Detail</p> },
    ],
    { initialEntries: [initialEntry] },
  );
  return render(<RouterProvider router={router} />);
}

describe('DeviceList', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useDeviceStore.setState({
      sites: [{ id: 'g1', organization_id: 'org-1', name: 'Site A', created_at: '', updated_at: '' }],
      devices: [],
      selectedSiteId: null,
      selectedDevice: null,
      isLoading: false,
      error: null,
      fetchSites: vi.fn(),
      fetchDevices: vi.fn(),
    });
    useInventoryStore.setState({ byDevice: new Map(), loading: new Map(), errors: new Map(), fetchInventory: vi.fn() });
  });

  it('shows welcome message when no devices exist', () => {
    renderDeviceList();
    expect(screen.getByText('Welcome to OpenGate')).toBeInTheDocument();
    expect(screen.getByText('Add Device')).toBeInTheDocument();
  });

  it('shows empty site message when site selected but empty', () => {
    useDeviceStore.setState({ selectedSiteId: 'g1' });
    renderDeviceList();
    expect(screen.getByText('No devices in this site')).toBeInTheDocument();
    expect(screen.getByText('Add Device')).toBeInTheDocument();
  });

  it('renders devices', () => {
    useDeviceStore.setState({
      devices: [
        { id: 'd1', organization_id: 'org-1', site_id: 'g1', hostname: 'host-1', os: 'linux', agent_version: '1.0.0', capabilities: [], status: 'online', last_seen: new Date().toISOString(), created_at: '', updated_at: '' },
        { id: 'd2', organization_id: 'org-1', site_id: 'g1', hostname: 'host-2', os: 'windows', agent_version: '', capabilities: [], status: 'offline', last_seen: new Date().toISOString(), created_at: '', updated_at: '' },
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
        { id: 'd1', organization_id: 'org-1', site_id: 'g1', hostname: 'host-1', os: 'linux', agent_version: '1.0.0', capabilities: [], status: 'online', last_seen: new Date().toISOString(), created_at: '', updated_at: '' },
        { id: 'd2', organization_id: 'org-1', site_id: 'g1', hostname: 'host-2', os: 'windows', agent_version: '', capabilities: [], status: 'offline', last_seen: new Date().toISOString(), created_at: '', updated_at: '' },
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

  it('fetches sites and devices on mount', () => {
    const fetchGroupsFn = vi.fn();
    const fetchDevicesFn = vi.fn();
    useDeviceStore.setState({ fetchSites: fetchGroupsFn, fetchDevices: fetchDevicesFn });
    renderDeviceList();
    expect(fetchGroupsFn).toHaveBeenCalled();
    expect(fetchDevicesFn).toHaveBeenCalled();
  });

  it('polls devices every 15 seconds', () => {
    vi.useFakeTimers();
    const fetchDevicesFn = vi.fn();
    useDeviceStore.setState({ fetchDevices: fetchDevicesFn });
    renderDeviceList();

    expect(fetchDevicesFn).toHaveBeenCalledTimes(1);

    vi.advanceTimersByTime(15_000);
    expect(fetchDevicesFn).toHaveBeenCalledTimes(2);

    vi.advanceTimersByTime(15_000);
    expect(fetchDevicesFn).toHaveBeenCalledTimes(3);

    vi.useRealTimers();
  });

  describe('search filter', () => {
    beforeEach(() => {
      useDeviceStore.setState({
        devices: [
          { id: 'd1', organization_id: 'org-1', site_id: 'g1', hostname: 'web-01', os: 'linux', agent_version: '1.0.0', capabilities: [], status: 'online', last_seen: '', created_at: '', updated_at: '' },
          { id: 'd2', organization_id: 'org-1', site_id: 'g1', hostname: 'db-01', os: 'windows', agent_version: '1.0.0', capabilities: [], status: 'online', last_seen: '', created_at: '', updated_at: '' },
          { id: 'd3', organization_id: 'org-1', site_id: 'g1', hostname: 'cache-01', os: 'darwin', agent_version: '1.0.0', capabilities: [], status: 'online', last_seen: '', created_at: '', updated_at: '' },
        ],
      });
    });

    it('filters by hostname substring (case-insensitive)', async () => {
      renderDeviceList();
      expect(screen.getByText('web-01')).toBeInTheDocument();
      expect(screen.getByText('db-01')).toBeInTheDocument();
      expect(screen.getByText('cache-01')).toBeInTheDocument();

      const search = screen.getByPlaceholderText(/search/i);
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
      useDeviceStore.setState({
        devices: [
          { id: 'old', organization_id: 'org-1', site_id: 'g1', hostname: 'outdated', os: 'linux', agent_version: '1.0.0', capabilities: [], status: 'online', last_seen: '', created_at: '', updated_at: '' },
          { id: 'cur', organization_id: 'org-1', site_id: 'g1', hostname: 'current', os: 'linux', agent_version: '2.0.0', capabilities: [], status: 'online', last_seen: '', created_at: '', updated_at: '' },
          { id: 'off', organization_id: 'org-1', site_id: 'g1', hostname: 'offline-old', os: 'linux', agent_version: '1.0.0', capabilities: [], status: 'offline', last_seen: '', created_at: '', updated_at: '' },
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
      expect(screen.getByText(/Upgrade All Agents \(1\)/)).toBeInTheDocument();
    });

    it('hides Upgrade-All button when no outdated online devices', () => {
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

      expect(upgradeAgentFn).toHaveBeenCalledTimes(1);
      expect(upgradeAgentFn).toHaveBeenCalledWith('old', '2.0.0', 'linux', 'amd64');
    });

    it('shows success toast when all upgrades succeed', async () => {
      const addToastFn = vi.fn();
      useToastStore.setState({ addToast: addToastFn });
      renderDeviceList();
      const button = screen.getByText(/Upgrade All Agents/);
      await userEvent.click(button);
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
      expect(addToastFn).toHaveBeenCalledWith(expect.stringMatching(/Upgraded 0, failed 1/), 'error');
    });

    it('pluralizes the success toast count (2 devices → "2 devices", succeeded !== 1)', async () => {
      const addToastFn = vi.fn();
      useToastStore.setState({ addToast: addToastFn });
      useDeviceStore.setState({
        devices: [
          { id: 'old1', organization_id: 'org-1', site_id: 'g1', hostname: 'h1', os: 'linux', agent_version: '1.0.0', capabilities: [], status: 'online', last_seen: '', created_at: '', updated_at: '' },
          { id: 'old2', organization_id: 'org-1', site_id: 'g1', hostname: 'h2', os: 'linux', agent_version: '1.0.0', capabilities: [], status: 'online', last_seen: '', created_at: '', updated_at: '' },
        ],
        upgradeAgent: vi.fn().mockResolvedValue(true),
      });
      renderDeviceList();
      await userEvent.click(screen.getByText(/Upgrade All Agents/));
      expect(addToastFn).toHaveBeenCalledWith('Upgrade pushed to 2 devices', 'success');
    });

    it('uses singular phrasing when exactly one device upgrades', async () => {
      const addToastFn = vi.fn();
      useToastStore.setState({ addToast: addToastFn });
      renderDeviceList();
      await userEvent.click(screen.getByText(/Upgrade All Agents/));
      expect(addToastFn).toHaveBeenCalledWith('Upgrade pushed to 1 device', 'success');
    });

    it('Upgrade All button label shows the outdated count', () => {
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
      expect(await screen.findByText('Upgrading...')).toBeInTheDocument();
      const btn = screen.getByText('Upgrading...').closest('button') as HTMLButtonElement;
      expect(btn.disabled).toBe(true);
      resolve(true);
    });

    it('outdated filter uses numeric version comparison ("10.0.0" > "2.0.0")', async () => {
      const upgradeAgentFn = vi.fn().mockResolvedValue(true);
      useDeviceStore.setState({
        devices: [
          { id: 'd', organization_id: 'org-1', site_id: 'g1', hostname: 'h', os: 'linux', agent_version: '2.0.0', capabilities: [], status: 'online', last_seen: '', created_at: '', updated_at: '' },
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
          { id: 'win', organization_id: 'org-1', site_id: 'g1', hostname: 'winhost', os: 'windows', agent_version: '1.0.0', capabilities: [], status: 'online', last_seen: '', created_at: '', updated_at: '' },
        ],
      });
      useUpdateStore.setState({
        manifests: [
          { version: '99.0.0', os: 'linux', arch: 'amd64', sha256: '', url: '', released_at: '', signed: true } as never,
        ],
        fetchManifests: vi.fn(),
      });
      renderDeviceList();
      expect(screen.queryByText(/Upgrade All/)).toBeNull();
    });

    it('outdated filter excludes offline outdated devices', () => {
      useDeviceStore.setState({
        devices: [
          { id: 'on', organization_id: 'org-1', site_id: 'g1', hostname: 'on', os: 'linux', agent_version: '1.0.0', capabilities: [], status: 'online', last_seen: '', created_at: '', updated_at: '' },
          { id: 'off', organization_id: 'org-1', site_id: 'g1', hostname: 'off', os: 'linux', agent_version: '1.0.0', capabilities: [], status: 'offline', last_seen: '', created_at: '', updated_at: '' },
        ],
      });
      renderDeviceList();
      expect(screen.getByText(/Upgrade All Agents \(1\)/)).toBeInTheDocument();
    });

    it('handleUpgradeAll is a no-op when there are no outdated devices', async () => {
      const upgradeAgentFn = vi.fn().mockResolvedValue(true);
      useUpdateStore.setState({ manifests: [], fetchManifests: vi.fn() });
      useDeviceStore.setState({ upgradeAgent: upgradeAgentFn });
      renderDeviceList();
      expect(upgradeAgentFn).not.toHaveBeenCalled();
    });
  });

  describe('empty-state messaging', () => {
    it('uses different copy when filtering vs. browsing', async () => {
      useDeviceStore.setState({
        devices: [
          { id: 'd', organization_id: 'org-1', site_id: 'g1', hostname: 'h', os: 'linux', agent_version: '1.0.0', capabilities: [], status: 'online', last_seen: '', created_at: '', updated_at: '' },
        ],
      });
      renderDeviceList();
      const search = screen.getByPlaceholderText(/search/i);
      await userEvent.type(search, 'zzznomatch');
      await waitFor(() => {
        expect(screen.getByText('Try a different search term.')).toBeInTheDocument();
      });
      expect(screen.queryByText(/Download and install/)).toBeNull();
    });

    it('uses site-specific copy when a site is selected but empty', () => {
      useDeviceStore.setState({ selectedSiteId: 'g1', devices: [] });
      renderDeviceList();
      expect(screen.getByText('No devices in this site')).toBeInTheDocument();
      expect(screen.getByText('Download and install the agent to add devices.')).toBeInTheDocument();
    });

    it('uses welcome copy when no site is selected and no devices exist', () => {
      useDeviceStore.setState({ selectedSiteId: null, devices: [] });
      renderDeviceList();
      expect(screen.getByText('Welcome to OpenGate')).toBeInTheDocument();
      expect(screen.getByText('Select a site to filter devices, or add a new device to get started.')).toBeInTheDocument();
    });
  });

  describe('URL filter', () => {
    beforeEach(() => {
      useDeviceStore.setState({
        devices: [
          { id: 'd1', organization_id: 'org-1', site_id: 'g1', hostname: 'online-host', os: 'linux', agent_version: '1.0.0', capabilities: [], status: 'online', last_seen: '', created_at: '', updated_at: '' },
          { id: 'd2', organization_id: 'org-1', site_id: 'g1', hostname: 'offline-host', os: 'linux', agent_version: '1.0.0', capabilities: [], status: 'offline', last_seen: '', created_at: '', updated_at: '' },
          { id: 'd3', organization_id: 'org-1', site_id: 'g1', hostname: 'maint-host', os: 'linux', agent_version: '1.0.0', capabilities: [], status: 'online', last_seen: '', created_at: '', updated_at: '', maintenance_on: true },
          { id: 'd4', organization_id: 'org-1', site_id: 'g1', hostname: 'anomalous-host', os: 'linux', agent_version: '1.0.0', capabilities: [], status: 'online', last_seen: '', created_at: '', updated_at: '', anomaly_rate: 0.9 },
        ],
      });
    });

    it('filters by status=online from the URL', () => {
      renderDeviceList('/devices?status=online');
      expect(screen.getByText('online-host')).toBeInTheDocument();
      expect(screen.queryByText('offline-host')).toBeNull();
    });

    it('filters by status=offline (excludes online devices)', () => {
      renderDeviceList('/devices?status=offline');
      expect(screen.getByText('offline-host')).toBeInTheDocument();
      expect(screen.queryByText('online-host')).toBeNull();
      expect(screen.queryByText('maint-host')).toBeNull();
    });

    it('filters by maintenance=true', () => {
      renderDeviceList('/devices?maintenance=true');
      expect(screen.getByText('maint-host')).toBeInTheDocument();
      expect(screen.queryByText('online-host')).toBeNull();
    });

    it('filters by health=anomalous', () => {
      renderDeviceList('/devices?health=anomalous');
      expect(screen.getByText('anomalous-host')).toBeInTheDocument();
      expect(screen.queryByText('online-host')).toBeNull();
    });

    it('renders a removable filter chip describing the active filter', () => {
      renderDeviceList('/devices?status=online');
      const chip = screen.getByRole('button', { name: /clear filter/i });
      expect(chip).toBeInTheDocument();
      expect(chip).toHaveTextContent('Online');
    });

    it('clearing the chip removes the filter and shows all devices', async () => {
      renderDeviceList('/devices?status=online');
      expect(screen.queryByText('offline-host')).toBeNull();
      await userEvent.click(screen.getByRole('button', { name: /clear filter/i }));
      expect(screen.getByText('offline-host')).toBeInTheDocument();
      expect(screen.queryByRole('button', { name: /clear filter/i })).toBeNull();
    });

    it('ignores a garbage filter param (no chip, no narrowing)', () => {
      renderDeviceList('/devices?status=bogus');
      expect(screen.getByText('online-host')).toBeInTheDocument();
      expect(screen.getByText('offline-host')).toBeInTheDocument();
      expect(screen.queryByRole('button', { name: /clear filter/i })).toBeNull();
    });
  });

  describe('virtualization', () => {
    it('windows a large device list (renders a subset, not every card)', () => {
      const many = Array.from({ length: 300 }, (_, i) => ({
        id: 'd' + String(i),
        organization_id: 'org-1', site_id: 'g1',
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

      expect(screen.getByText('host-0')).toBeInTheDocument();
      expect(screen.queryByText('host-299')).toBeNull();
      const renderedHostnames = document.querySelectorAll('h3');
      expect(renderedHostnames.length).toBeGreaterThan(0);
      expect(renderedHostnames.length).toBeLessThan(300);
    });
  });

  it('Device grid is hidden while isLoading is true', () => {
    useDeviceStore.setState({
      isLoading: true,
      devices: [
        { id: 'd', organization_id: 'org-1', site_id: 'g1', hostname: 'visible-host', os: 'linux', agent_version: '1.0.0', capabilities: [], status: 'online', last_seen: '', created_at: '', updated_at: '' },
      ],
    });
    renderDeviceList();
    expect(screen.queryByText('visible-host')).toBeNull();
  });

  describe('virtualized grid layout', () => {
    function makeDevices(n: number) {
      return Array.from({ length: n }, (_, i) => ({
        id: 'd' + String(i),
        organization_id: 'org-1', site_id: 'g1',
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
      useDeviceStore.setState({ devices: makeDevices(6) });
      renderDeviceList();

      const firstRow = screen.getByText('host-0').closest('div.grid.gap-4') as HTMLElement;
      expect(firstRow).not.toBeNull();
      expect(firstRow).toHaveStyle({ gridTemplateColumns: 'repeat(3, minmax(0, 1fr))' });
      expect(within(firstRow).getByText('host-0')).toBeInTheDocument();
      expect(within(firstRow).getByText('host-2')).toBeInTheDocument();
      expect(within(firstRow).queryByText('host-3')).toBeNull();

      const container = firstRow.parentElement as HTMLElement;
      const rows = container.querySelectorAll(':scope > div.grid.gap-4');
      expect(rows).toHaveLength(2);
    });

    it('absolutely positions each virtual row by translateY inside a relative, sized container', () => {
      useDeviceStore.setState({ devices: makeDevices(6) });
      renderDeviceList();

      const firstRow = screen.getByText('host-0').closest('div.grid.gap-4') as HTMLElement;
      const container = firstRow.parentElement as HTMLElement;

      expect(container).toHaveStyle({ position: 'relative', width: '100%' });
      expect(Number.parseFloat(container.style.height)).toBeGreaterThan(0);

      expect(firstRow).toHaveStyle({
        position: 'absolute',
        width: '100%',
        transform: 'translateY(0px)',
      });
    });

    it('drops to 2 columns at the md breakpoint (>= is inclusive; > would fall through to 1)', () => {
      const originalRect = Element.prototype.getBoundingClientRect;
      try {
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

    describe('sizing the grid', () => {
      class RecordingObserver {
        static instances: RecordingObserver[] = [];
        readonly targets = new Set<Element>();
        disconnected = false;
        private readonly callback: ResizeObserverCallback;
        constructor(callback: ResizeObserverCallback) {
          this.callback = callback;
          RecordingObserver.instances.push(this);
        }
        observe(target: Element) { this.targets.add(target); }
        unobserve(target: Element) { this.targets.delete(target); }
        disconnect() { this.disconnected = true; }
        static resize(target: Element) {
          for (const observer of RecordingObserver.instances) {
            if (!observer.targets.has(target)) continue;
            observer.callback(
              [{ target, contentRect: target.getBoundingClientRect() } as unknown as ResizeObserverEntry],
              observer as unknown as ResizeObserver,
            );
          }
        }
      }

      const gridColumns = () =>
        (screen.getByText('host-0').closest('div.grid.gap-4') as HTMLElement).style.gridTemplateColumns;

      beforeEach(() => {
        RecordingObserver.instances = [];
        vi.stubGlobal('ResizeObserver', RecordingObserver);
        useDeviceStore.setState({ devices: makeDevices(2) });
      });

      afterEach(() => {
        vi.unstubAllGlobals();
      });

      it('measures itself on mount, before any resize is reported', () => {
        renderDeviceList();
        expect(gridColumns()).toBe('repeat(3, minmax(0, 1fr))');
      });

      it('follows its own width when it is resized', () => {
        renderDeviceList();
        const scrollParent = screen.getByText('host-0').closest('div.overflow-auto') as HTMLElement;
        const originalRect = Element.prototype.getBoundingClientRect;
        try {
          Element.prototype.getBoundingClientRect = () =>
            ({ width: 768, height: 800, top: 0, left: 0, right: 768, bottom: 800, x: 0, y: 0, toJSON: () => ({}) }) as DOMRect;
          act(() => { RecordingObserver.resize(scrollParent); });
          expect(gridColumns()).toBe('repeat(2, minmax(0, 1fr))');
        } finally {
          Element.prototype.getBoundingClientRect = originalRect;
        }
      });

      it('stops watching when it leaves', () => {
        const { unmount } = renderDeviceList();
        unmount();
        const watching = RecordingObserver.instances.filter((o) => o.targets.size > 0 && !o.disconnected);
        expect(watching).toEqual([]);
      });
    });
  });

  it('opening the list requests the update manifests', () => {
    const fetchManifests = vi.fn();
    useUpdateStore.setState({ manifests: [], fetchManifests });
    renderDeviceList();
    expect(fetchManifests).toHaveBeenCalledTimes(1);
  });
});
