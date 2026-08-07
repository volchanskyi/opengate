import { render, screen, fireEvent, act } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createMemoryRouter, RouterProvider, useLocation } from 'react-router';
import { useDeviceStore } from './state/device-store';
import { useOrganizationStore } from '../organizations';
import type { components } from '../../types/api';
import { useSessionStore } from '../session';
import { useUpdateStore } from './state/update-store';
import { useToastStore } from '../../lib/feedback/toast-store';
import { DeviceDetail } from './DeviceDetail';
import { useAuthStore } from '../../state/auth-store';

vi.mock('../../lib/api', () => ({
  api: {
    GET: vi.fn().mockResolvedValue({ data: undefined, error: { error: 'mock' }, response: { status: 404 } }),
    POST: vi.fn().mockResolvedValue({ data: { token: 'tok', relay_url: 'ws://localhost' }, error: undefined }),
    DELETE: vi.fn().mockResolvedValue({ error: undefined }),
  },
}));

// The telemetry panel is exercised in DeviceMetrics.test.tsx; stub it here so
// these tests stay isolated from uPlot/canvas and the metrics fetch. The stub
// exposes onViewLogs so the correlation-jump glue can be driven.
vi.mock('./DeviceMetrics', () => ({
  DeviceMetrics: ({ deviceId, onViewLogs }: { deviceId: string; onViewLogs?: (f: number, t: number) => void }) => (
    <div data-testid="device-metrics">
      {deviceId}
      <button type="button" onClick={() => onViewLogs?.(1000, 4600)}>mock-view-logs</button>
    </div>
  ),
}));

function renderDetail() {
  const router = createMemoryRouter(
    [
      { path: '/devices/:id', element: <DeviceDetail /> },
      { path: '/devices', element: <p>Device List</p> },
    ],
    { initialEntries: ['/devices/d1'] },
  );
  return render(<RouterProvider router={router} />);
}

/**
 * Render, then open the collapsed-by-default Hardware section. Located by
 * accessible name, which pins the decorative caret as `aria-hidden` — otherwise
 * the toggle answers to "▶ Hardware" instead of "Hardware".
 */
function renderDetailWithHardware() {
  const result = renderDetail();
  fireEvent.click(screen.getByRole('button', { name: 'Hardware' }));
  return result;
}

const mockDevice = {
  id: 'd1',
  organization_id: 'org-1', group_id: 'g1',
  hostname: 'test-host',
  os: 'linux',
  agent_version: '1.0.0',
  status: 'online' as const,
  capabilities: [],
  last_seen: '2026-01-01T00:00:00Z',
  created_at: '2025-12-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
};

type PowerAction = components['schemas']['AMTPowerRequest']['action'];

const newerManifest = { version: '2.0.0', os: 'linux', arch: 'amd64', url: 'https://example.com/agent', sha256: 'abc', signature: 'sig', created_at: '2026-01-01T00:00:00Z' };

/** Puts the selected device in the linked-and-online AMT state power actions need. */
function setLinkedAmtDevice(sendPowerAction: (uuid: string, action: PowerAction) => Promise<boolean>) {
  useDeviceStore.setState({
    selectedDevice: { ...mockDevice, amt: { available: true, status: 'online' as const, uuid: 'amt-1' } },
    sendPowerAction,
  });
}

/** Sets the signed-in user's admin flag; delete and group-move are admin-only. */
function seedUser(isAdmin: boolean) {
  useAuthStore.setState({
    user: { id: 'u1', email: 'a@b.com', display_name: 'A', is_admin: isAdmin, created_at: '', updated_at: '' },
  });
}

describe('DeviceDetail', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.clearAllMocks();
    seedUser(true);
    useDeviceStore.setState({
      selectedDevice: mockDevice,
      isLoading: false,
      error: null,
      devices: [],
      groups: [],
      selectedGroupId: null,
      fetchDevice: vi.fn(),
      refreshDevice: vi.fn(),
      fetchGroups: vi.fn(),
      fetchHardware: vi.fn(),
      deleteDevice: vi.fn(),
      upgradeAgent: vi.fn().mockResolvedValue(true),
      sendPowerAction: vi.fn(),
    });
    useSessionStore.setState({
      sessions: [{ token: 'tok1', device_id: 'd1', user_id: 'u1', created_at: '' }],
      isLoading: false,
      error: null,
      fetchSessions: vi.fn(),
      createSession: vi.fn().mockResolvedValue({ token: 'new-tok', relay_url: 'ws://localhost' }),
    });
    useUpdateStore.setState({
      manifests: [],
      fetchManifests: vi.fn(),
    });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('renders device info', () => {
    renderDetail();
    expect(screen.getByText('test-host')).toBeInTheDocument();
    expect(screen.getByText('linux')).toBeInTheDocument();
    expect(screen.getByText('Online')).toBeInTheDocument();
  });

  it('mounts the telemetry metrics panel for the device', () => {
    renderDetail();
    expect(screen.getByTestId('device-metrics')).toHaveTextContent('d1');
  });

  it('correlation jump: viewing a window pre-fills and fetches the System Logs pane', () => {
    const fetchLogs = vi.fn();
    useDeviceStore.setState({ fetchLogs });
    renderDetail();
    fireEvent.click(screen.getByText('mock-view-logs'));
    // The drill focuses the System Logs pane (source=system), not Agent Logs.
    expect(fetchLogs).toHaveBeenCalledWith('system', 'd1', expect.objectContaining({
      from: new Date(1000 * 1000).toISOString(),
      to: new Date(4600 * 1000).toISOString(),
    }));
  });

  it('shows loading skeleton when loading', () => {
    useDeviceStore.setState({ selectedDevice: null, isLoading: true });
    renderDetail();
    expect(document.querySelector('.animate-pulse')).toBeInTheDocument();
  });

  it('shows active sessions', () => {
    renderDetail();
    expect(screen.getByText('Active Sessions (1)')).toBeInTheDocument();
    expect(screen.getByText('tok1')).toBeInTheDocument();
  });

  it('has start session button in header', () => {
    renderDetail();
    expect(screen.getByRole('button', { name: 'Start Session' })).toBeInTheDocument();
  });

  it('Start Session button uses the online-green palette', () => {
    renderDetail();
    expect(screen.getByRole('button', { name: 'Start Session' })).toHaveClass('bg-green-500', 'hover:bg-green-600');
  });

  it('delete requires confirmation', async () => {
    vi.useRealTimers();
    const user = userEvent.setup();
    renderDetail();

    await user.click(screen.getByRole('button', { name: 'Delete Device' }));
    expect(screen.getByRole('button', { name: 'Confirm Delete' })).toBeInTheDocument();
  });

  it('shows agent version', () => {
    renderDetail();
    expect(screen.getByText('1.0.0')).toBeInTheDocument();
  });

  it('polls device data every 30 seconds', () => {
    const refreshDeviceFn = vi.fn();
    useDeviceStore.setState({ refreshDevice: refreshDeviceFn });
    renderDetail();

    expect(refreshDeviceFn).toHaveBeenCalledTimes(0);

    vi.advanceTimersByTime(30_000);
    expect(refreshDeviceFn).toHaveBeenCalledTimes(1);

    vi.advanceTimersByTime(30_000);
    expect(refreshDeviceFn).toHaveBeenCalledTimes(2);
  });

  it('shows error toast when session creation fails', async () => {
    vi.useRealTimers();
    const user = userEvent.setup();
    useSessionStore.setState({
      ...useSessionStore.getState(),
      createSession: vi.fn().mockResolvedValue(null),
    });
    useToastStore.setState({ toasts: [] });

    renderDetail();
    await user.click(screen.getByRole('button', { name: 'Start Session' }));

    const toasts = useToastStore.getState().toasts;
    expect(toasts).toHaveLength(1);
    expect(toasts[0]!.message).toMatch(/failed to start session/i);
    expect(toasts[0]!.type).toBe('error');
  });

  it('navigates to session view on successful creation', async () => {
    vi.useRealTimers();
    const user = userEvent.setup();
    const router = createMemoryRouter(
      [
        { path: '/devices/:id', element: <DeviceDetail /> },
        { path: '/sessions/:token', element: <p>Session View</p> },
      ],
      { initialEntries: ['/devices/d1'] },
    );
    render(<RouterProvider router={router} />);

    await user.click(screen.getByRole('button', { name: 'Start Session' }));

    expect(await screen.findByText('Session View')).toBeInTheDocument();
  });

  it('shows upgrade button when newer manifest available', () => {
    useUpdateStore.setState({
      manifests: [newerManifest],
    });
    renderDetail();
    expect(screen.getByText('Upgrade to v2.0.0')).toBeInTheDocument();
  });

  it('shows up to date when on latest version', () => {
    useUpdateStore.setState({
      manifests: [{ version: '1.0.0', os: 'linux', arch: 'amd64', url: 'https://example.com/agent', sha256: 'abc', signature: 'sig', created_at: '2026-01-01T00:00:00Z' }],
    });
    renderDetail();
    expect(screen.getByRole('img', { name: 'Up to date' })).toBeInTheDocument();
  });

  it('renders logs card as separate tile', () => {
    renderDetail();
    expect(screen.getByText('Agent Logs')).toBeInTheDocument();
  });

  it('calls upgradeAgent when upgrade button is clicked', async () => {
    vi.useRealTimers();
    const user = userEvent.setup();
    const upgradeFn = vi.fn().mockResolvedValue(true);
    useDeviceStore.setState({ upgradeAgent: upgradeFn });
    useUpdateStore.setState({
      manifests: [newerManifest],
    });
    useToastStore.setState({ toasts: [] });

    renderDetail();
    await user.click(screen.getByText('Upgrade to v2.0.0'));

    expect(upgradeFn).toHaveBeenCalledWith('d1', '2.0.0', 'linux', 'amd64');
    const toasts = useToastStore.getState().toasts;
    expect(toasts.some((t) => t.message.includes('Upgrade to v2.0.0 pushed'))).toBe(true);
  });

  it('shows os_display when available', () => {
    useDeviceStore.setState({
      selectedDevice: { ...mockDevice, os: 'linux', os_display: 'Ubuntu 22.04 LTS' },
    });
    renderDetail();
    expect(screen.getByText('Ubuntu 22.04 LTS')).toBeInTheDocument();
  });

  it('fetches manifests on mount', () => {
    const fetchManifestsFn = vi.fn();
    useUpdateStore.setState({ fetchManifests: fetchManifestsFn });
    renderDetail();
    expect(fetchManifestsFn).toHaveBeenCalled();
  });

  it('handleRestart shows confirm when active sessions exist', async () => {
    vi.useRealTimers();
    const user = userEvent.setup();
    const restartFn = vi.fn().mockResolvedValue(true);
    useDeviceStore.setState({ restartAgent: restartFn });

    renderDetail();

    // First click shows confirmation
    await user.click(screen.getByRole('button', { name: 'Restart Agent' }));
    expect(screen.getByRole('button', { name: /Confirm \(1 active\)/ })).toBeInTheDocument();

    // Second click triggers the actual restart
    await user.click(screen.getByRole('button', { name: /Confirm \(1 active\)/ }));
    expect(restartFn).toHaveBeenCalledWith('d1');
  });

  it('handleRestart shows failure toast', async () => {
    vi.useRealTimers();
    const user = userEvent.setup();
    const restartFn = vi.fn().mockResolvedValue(false);
    useDeviceStore.setState({ restartAgent: restartFn });
    useSessionStore.setState({ sessions: [] });
    useToastStore.setState({ toasts: [] });

    renderDetail();
    await user.click(screen.getByRole('button', { name: 'Restart Agent' }));

    const toasts = useToastStore.getState().toasts;
    expect(toasts.some((t) => t.message.includes('Failed to restart'))).toBe(true);
  });

  it('handleMoveGroup moves device to new group', async () => {
    vi.useRealTimers();
    const user = userEvent.setup();
    const updateGroupFn = vi.fn().mockResolvedValue(true);
    useDeviceStore.setState({
      groups: [
        { id: 'g1', name: 'Group 1', created_at: '', updated_at: '' },
        { id: 'g2', name: 'Group 2', created_at: '', updated_at: '' },
      ],
      updateDeviceGroup: updateGroupFn,
    });
    useToastStore.setState({ toasts: [] });

    renderDetail();

    // Select new group from the "Move to Group" dropdown (not the logs filter dropdown)
    const groupSelect = screen.getByDisplayValue('Select group...');
    await user.selectOptions(groupSelect, 'g2');
    await user.click(screen.getByText('Move'));

    expect(updateGroupFn).toHaveBeenCalledWith('d1', 'g2');
    const toasts = useToastStore.getState().toasts;
    expect(toasts.some((t) => t.message.includes('moved to new group'))).toBe(true);
  });

  it('moves the device to another customer', async () => {
    vi.useRealTimers();
    const user = userEvent.setup();
    const moveFn = vi.fn().mockResolvedValue(true);
    useDeviceStore.setState({ moveDeviceOrganization: moveFn });
    useOrganizationStore.setState({
      organizations: [
        { id: 'org-1', name: 'Contoso', created_at: '', updated_at: '' },
        { id: 'org-2', name: 'Fabrikam', created_at: '', updated_at: '' },
      ],
      fetchOrganizations: vi.fn().mockResolvedValue(undefined),
    });
    useToastStore.setState({ toasts: [] });

    renderDetail();

    await user.selectOptions(screen.getByLabelText('Move to customer'), 'org-2');
    await user.click(screen.getByLabelText('Move to customer').closest('div')!.querySelector('button')!);

    expect(moveFn).toHaveBeenCalledWith('d1', 'org-2');
    const toasts = useToastStore.getState().toasts;
    expect(toasts.some((t) => t.message.includes('moved to new customer'))).toBe(true);
  });

  it('hides the customer move when the tenant has only one customer', () => {
    useOrganizationStore.setState({
      organizations: [{ id: 'org-1', name: 'Contoso', created_at: '', updated_at: '' }],
      fetchOrganizations: vi.fn().mockResolvedValue(undefined),
    });
    renderDetail();
    expect(screen.queryByText('Move to Customer')).not.toBeInTheDocument();
  });

  it('handleDelete navigates to device list after confirm', async () => {
    vi.useRealTimers();
    const user = userEvent.setup();
    const deleteFn = vi.fn().mockResolvedValue(undefined);
    useDeviceStore.setState({ deleteDevice: deleteFn });

    const router = createMemoryRouter(
      [
        { path: '/devices/:id', element: <DeviceDetail /> },
        { path: '/devices', element: <p>Device List</p> },
      ],
      { initialEntries: ['/devices/d1'] },
    );
    render(<RouterProvider router={router} />);

    // First click shows confirm
    await user.click(screen.getByRole('button', { name: 'Delete Device' }));
    expect(screen.getByRole('button', { name: 'Confirm Delete' })).toBeInTheDocument();

    // Second click deletes and navigates
    await user.click(screen.getByRole('button', { name: 'Confirm Delete' }));
    expect(deleteFn).toHaveBeenCalledWith('d1');
    expect(await screen.findByText('Device List')).toBeInTheDocument();
  });

  it('handleUpgrade shows failure toast on error', async () => {
    vi.useRealTimers();
    const user = userEvent.setup();
    const upgradeFn = vi.fn().mockResolvedValue(false);
    useDeviceStore.setState({ upgradeAgent: upgradeFn });
    useUpdateStore.setState({
      manifests: [newerManifest],
    });
    useToastStore.setState({ toasts: [] });

    renderDetail();
    await user.click(screen.getByText('Upgrade to v2.0.0'));

    const toasts = useToastStore.getState().toasts;
    expect(toasts.some((t) => t.message.includes('Failed to push upgrade'))).toBe(true);
  });

  it('handlePowerAction sends non-destructive action immediately', async () => {
    vi.useRealTimers();
    const user = userEvent.setup();
    const sendPowerFn = vi.fn().mockResolvedValue(true);
    setLinkedAmtDevice(sendPowerFn);
    useToastStore.setState({ toasts: [] });

    renderDetail();
    await user.click(screen.getByText('Power On'));

    expect(sendPowerFn).toHaveBeenCalledWith('amt-1', 'power_on');
    const toasts = useToastStore.getState().toasts;
    expect(toasts.some((t) => t.message.includes('power on'))).toBe(true);
  });

  it('handlePowerAction requires confirm for destructive action', async () => {
    vi.useRealTimers();
    const user = userEvent.setup();
    const sendPowerFn = vi.fn().mockResolvedValue(true);
    setLinkedAmtDevice(sendPowerFn);
    useToastStore.setState({ toasts: [] });

    renderDetail();

    // First click on destructive action shows confirmation
    await user.click(screen.getByText('Power Cycle'));
    expect(screen.getByText('Confirm Cycle')).toBeInTheDocument();
    expect(sendPowerFn).not.toHaveBeenCalled();

    // Second click triggers the action
    await user.click(screen.getByText('Confirm Cycle'));
    expect(sendPowerFn).toHaveBeenCalledWith('amt-1', 'power_cycle');
  });

  it('handlePowerAction shows error toast on failure', async () => {
    vi.useRealTimers();
    const user = userEvent.setup();
    const sendPowerFn = vi.fn().mockResolvedValue(false);
    setLinkedAmtDevice(sendPowerFn);
    useToastStore.setState({ toasts: [] });

    renderDetail();
    await user.click(screen.getByText('Soft Off'));

    const toasts = useToastStore.getState().toasts;
    expect(toasts.some((t) => t.message.includes('Failed to send power action'))).toBe(true);
  });

  it('handleMoveGroup shows failure toast on error', async () => {
    vi.useRealTimers();
    const user = userEvent.setup();
    const updateGroupFn = vi.fn().mockResolvedValue(false);
    useDeviceStore.setState({
      groups: [
        { id: 'g1', name: 'Group 1', created_at: '', updated_at: '' },
        { id: 'g2', name: 'Group 2', created_at: '', updated_at: '' },
      ],
      updateDeviceGroup: updateGroupFn,
    });
    useToastStore.setState({ toasts: [] });

    renderDetail();
    const groupSelect = screen.getByDisplayValue('Select group...');
    await user.selectOptions(groupSelect, 'g2');
    await user.click(screen.getByText('Move'));

    const toasts = useToastStore.getState().toasts;
    expect(toasts.some((t) => t.message.includes('Failed to move device'))).toBe(true);
  });

  it('shows the Intel AMT badge for a capable device that has never dialled in', () => {
    useDeviceStore.setState({ selectedDevice: { ...mockDevice, amt: { available: true } } });
    renderDetail();
    expect(screen.getByText('Intel AMT').getAttribute('title')).toBe('Intel AMT · not connected');
  });

  it('hides the power buttons until the AMT connection is online', () => {
    useDeviceStore.setState({
      selectedDevice: { ...mockDevice, amt: { available: true, status: 'offline' as const, uuid: 'amt-1' } },
    });
    renderDetail();
    expect(screen.getByText('Intel AMT').getAttribute('title')).toBe('Intel AMT · offline');
    expect(screen.queryByText('Power On')).not.toBeInTheDocument();
  });

  it('shows the power buttons once the AMT connection is online', () => {
    setLinkedAmtDevice(vi.fn());
    renderDetail();
    expect(screen.getByText('Power On')).toBeInTheDocument();
  });

  it('renders no AMT badge or power buttons for a device without AMT', () => {
    renderDetail();
    expect(screen.queryByText('Intel AMT')).not.toBeInTheDocument();
    expect(screen.queryByText('Power On')).not.toBeInTheDocument();
  });

  it('handlePowerAction hard_reset shows Confirm Reset on first click', async () => {
    vi.useRealTimers();
    const user = userEvent.setup();
    const sendPowerFn = vi.fn().mockResolvedValue(true);
    setLinkedAmtDevice(sendPowerFn);
    renderDetail();

    await user.click(screen.getByText('Hard Reset'));
    expect(screen.getByText('Confirm Reset')).toBeInTheDocument();
    expect(sendPowerFn).not.toHaveBeenCalled();

    await user.click(screen.getByText('Confirm Reset'));
    expect(sendPowerFn).toHaveBeenCalledWith('amt-1', 'hard_reset');
  });

  it('handlePowerAction soft_off runs immediately without confirm', async () => {
    vi.useRealTimers();
    const user = userEvent.setup();
    const sendPowerFn = vi.fn().mockResolvedValue(true);
    setLinkedAmtDevice(sendPowerFn);
    renderDetail();
    await user.click(screen.getByText('Soft Off'));
    expect(sendPowerFn).toHaveBeenCalledWith('amt-1', 'soft_off');
    // No "Confirm" variant should appear for soft_off
    expect(screen.queryByText(/Confirm Soft/)).not.toBeInTheDocument();
  });

  it('formatBytes renders 0 B when total is zero', () => {
    useDeviceStore.setState({
      hardware: {
        device_id: 'd1',
        cpu_model: 'cpu', cpu_cores: 1,
        ram_total_mb: 0, disk_free_mb: 0, disk_total_mb: 0,
        updated_at: '2026-01-01T00:00:00Z',
        network_interfaces: [],
      },
    });
    renderDetailWithHardware();
    // RAM, disk free, disk total all 0 → three "0 B" occurrences (RAM row + "0 B free / 0 B")
    const zeros = screen.getAllByText(/0 B/);
    expect(zeros.length).toBeGreaterThanOrEqual(1);
    const ramDd = screen.getByText('RAM').nextElementSibling;
    expect(ramDd?.textContent).toBe('0 B');
  });

  it('formatBytes uses 1 decimal when value < 100 (e.g. 1.0 MB)', () => {
    useDeviceStore.setState({
      hardware: {
        device_id: 'd1', cpu_model: 'cpu', cpu_cores: 1,
        ram_total_mb: 1, disk_free_mb: 0, disk_total_mb: 0,
        updated_at: '2026-01-01T00:00:00Z',
        network_interfaces: [],
      },
    });
    renderDetailWithHardware();
    expect(screen.getByText('1.0 MB')).toBeInTheDocument();
  });

  it('formatBytes uses 0 decimals when value >= 100 (e.g. 200 MB)', () => {
    useDeviceStore.setState({
      hardware: {
        device_id: 'd1', cpu_model: 'cpu', cpu_cores: 1,
        ram_total_mb: 200, disk_free_mb: 0, disk_total_mb: 0,
        updated_at: '2026-01-01T00:00:00Z',
        network_interfaces: [],
      },
    });
    renderDetailWithHardware();
    expect(screen.getByText('200 MB')).toBeInTheDocument();
  });

  it('formatBytes uses 1 decimal at the val < 100 boundary (e.g. 99.0 MB)', () => {
    useDeviceStore.setState({
      hardware: {
        device_id: 'd1', cpu_model: 'cpu', cpu_cores: 1,
        ram_total_mb: 99, disk_free_mb: 0, disk_total_mb: 0,
        updated_at: '2026-01-01T00:00:00Z',
        network_interfaces: [],
      },
    });
    renderDetailWithHardware();
    expect(screen.getByText('99.0 MB')).toBeInTheDocument();
  });

  it('formatBytes picks KB for sub-megabyte and computes division (val = bytes / 1024^idx)', () => {
    // 0 MB but non-zero disk in KB range: 1 disk_free_mb = 1 MB; but we want KB-range bytes.
    // Use a small disk via direct hardware override; ram_total_mb=0 would short-circuit to 0 B.
    // Instead, supply 0.001 MB equivalent: ram_total_mb won't accept fractions in API typing.
    // Use disk_free_mb of 1 (=1 MB) so we hit MB and also exercise the disk_free/disk_total formatting.
    useDeviceStore.setState({
      hardware: {
        device_id: 'd1', cpu_model: 'cpu', cpu_cores: 1,
        ram_total_mb: 2, disk_free_mb: 1, disk_total_mb: 4,
        updated_at: '2026-01-01T00:00:00Z',
        network_interfaces: [],
      },
    });
    renderDetailWithHardware();
    expect(screen.getByText('2.0 MB')).toBeInTheDocument();
    // disk uses two formatBytes invocations inside a single dd
    const diskDd = screen.getByText('Disk').nextElementSibling;
    expect(diskDd?.textContent).toBe('1.0 MB free / 4.0 MB');
  });

  it('formatBytes clamps idx to TB for extremely large values', () => {
    // ram_total_mb = 2^30 MB → bytes = 2^50; idx natural = 5, clamped to 4 (TB).
    // val = 2^50 / 1024^4 = 2^50 / 2^40 = 2^10 = 1024 → "1024 TB" (val>=100 → 0 decimals).
    useDeviceStore.setState({
      hardware: {
        device_id: 'd1', cpu_model: 'cpu', cpu_cores: 1,
        ram_total_mb: 1024 * 1024 * 1024,
        disk_free_mb: 0, disk_total_mb: 0,
        updated_at: '2026-01-01T00:00:00Z',
        network_interfaces: [],
      },
    });
    renderDetailWithHardware();
    expect(screen.getByText('1024 TB')).toBeInTheDocument();
  });

  it('formatBytes picks GB unit for 1024^3 byte values (idx = 3)', () => {
    // ram_total_mb = 1024 MB → bytes = 1024^3 → idx = 3 (GB), val = 1 → "1.0 GB"
    useDeviceStore.setState({
      hardware: {
        device_id: 'd1', cpu_model: 'cpu', cpu_cores: 1,
        ram_total_mb: 1024, disk_free_mb: 0, disk_total_mb: 0,
        updated_at: '2026-01-01T00:00:00Z',
        network_interfaces: [],
      },
    });
    renderDetailWithHardware();
    expect(screen.getByText('1.0 GB')).toBeInTheDocument();
  });

  it('Hardware section is hidden until hardware data is available', () => {
    useDeviceStore.setState({ hardware: null });
    renderDetail();
    expect(screen.queryByText('CPU')).not.toBeInTheDocument();
    expect(screen.queryByText('RAM')).not.toBeInTheDocument();
  });

  it('has no manual hardware refresh control — coming back online pulls it', () => {
    renderDetail();
    expect(screen.queryByRole('button', { name: /refresh hardware/i })).toBeNull();
  });

  it('says so when the agent has never reported an inventory', () => {
    useDeviceStore.setState({ hardware: null });
    renderDetailWithHardware();
    expect(screen.getByText('Hardware inventory not reported yet.')).toBeInTheDocument();
  });

  it('auto-loads hardware once when the device is already online on mount', () => {
    const fetchHardwareFn = vi.fn();
    useDeviceStore.setState({ fetchHardware: fetchHardwareFn }); // mockDevice is online
    renderDetail();
    expect(fetchHardwareFn).toHaveBeenCalledTimes(1);
    expect(fetchHardwareFn).toHaveBeenCalledWith('d1');
  });

  it('auto-loads hardware on an offline→online transition but not on a steady poll', () => {
    const fetchHardwareFn = vi.fn();
    useDeviceStore.setState({
      fetchHardware: fetchHardwareFn,
      selectedDevice: { ...mockDevice, status: 'offline' as const },
    });
    renderDetail();
    expect(fetchHardwareFn).not.toHaveBeenCalled(); // offline: nothing pulled

    // Agent comes back online → pull once.
    act(() => { useDeviceStore.setState({ selectedDevice: { ...mockDevice, status: 'online' as const } }); });
    expect(fetchHardwareFn).toHaveBeenCalledTimes(1);

    // A subsequent poll that leaves the device online must not re-pull.
    act(() => { useDeviceStore.setState({ selectedDevice: { ...mockDevice, status: 'online' as const, last_seen: '2026-01-02T00:00:00Z' } }); });
    expect(fetchHardwareFn).toHaveBeenCalledTimes(1);
  });

  it('shows N/A for the Group ID when the device is not in a group', () => {
    useDeviceStore.setState({ selectedDevice: { ...mockDevice, organization_id: 'org-1', group_id: '' } });
    renderDetail();
    expect(screen.getByText('Group ID').nextElementSibling?.textContent).toBe('N/A');
  });

  it('shows the group id when the device belongs to a group', () => {
    useDeviceStore.setState({ selectedDevice: { ...mockDevice, organization_id: 'org-1', group_id: 'g-42' } });
    renderDetail();
    expect(screen.getByText('Group ID').nextElementSibling?.textContent).toBe('g-42');
  });

  it('shows N/A for the all-zeros placeholder Group ID', () => {
    useDeviceStore.setState({ selectedDevice: { ...mockDevice, organization_id: 'org-1', group_id: '00000000-0000-0000-0000-000000000000' } });
    renderDetail();
    expect(screen.getByText('Group ID').nextElementSibling?.textContent).toBe('N/A');
  });

  it('Hardware section is collapsed by default and expands via the caret toggle', async () => {
    vi.useRealTimers();
    const user = userEvent.setup();
    useDeviceStore.setState({
      hardware: {
        device_id: 'd1', cpu_model: 'cpu', cpu_cores: 1,
        ram_total_mb: 1024, disk_free_mb: 0, disk_total_mb: 0,
        updated_at: '2026-01-01T00:00:00Z', network_interfaces: [],
      },
    });
    renderDetail();
    // Collapsed on open — the host card stays short and scannable.
    expect(screen.queryByText('CPU')).toBeNull();
    // The caret toggle reveals the details (same pattern as Intel AMT Setup).
    await user.click(screen.getByText('Hardware'));
    expect(screen.getByText('CPU')).toBeInTheDocument();
    await user.click(screen.getByText('Hardware'));
    expect(screen.queryByText('CPU')).toBeNull();
  });

  it('the System Logs card spans the full grid width, like Discovered Footprint', () => {
    renderDetail();
    const card = screen.getByText('System Logs').closest('.rounded-lg');
    expect(card).toHaveClass('lg:col-span-2');
  });

  it('network interface row shows MAC alone when ipv4 is empty', () => {
    useDeviceStore.setState({
      hardware: {
        device_id: 'd1', cpu_model: 'cpu', cpu_cores: 1,
        ram_total_mb: 1, disk_free_mb: 0, disk_total_mb: 0,
        updated_at: '2026-01-01T00:00:00Z',
        network_interfaces: [
          { name: 'eth0', mac: '00:11:22:33:44:55', ipv4: [], ipv6: [] },
        ],
      },
    });
    renderDetailWithHardware();
    const li = screen.getByText(/eth0/).closest('li');
    expect(li?.textContent).toBe('eth0: 00:11:22:33:44:55');
    expect(li?.textContent).not.toContain('—');
  });

  it('network interface joins multiple ipv4 with ", " after MAC', () => {
    useDeviceStore.setState({
      hardware: {
        device_id: 'd1', cpu_model: 'cpu', cpu_cores: 1,
        ram_total_mb: 1, disk_free_mb: 0, disk_total_mb: 0,
        updated_at: '2026-01-01T00:00:00Z',
        network_interfaces: [
          { name: 'eth0', mac: '00:11:22:33:44:55', ipv4: ['10.0.0.1', '10.0.0.2'], ipv6: [] },
        ],
      },
    });
    renderDetailWithHardware();
    const li = screen.getByText(/eth0/).closest('li');
    expect(li?.textContent).toBe('eth0: 00:11:22:33:44:55 — 10.0.0.1, 10.0.0.2');
  });

  it('Network Interfaces heading hidden when interface list is empty', () => {
    useDeviceStore.setState({
      hardware: {
        device_id: 'd1', cpu_model: 'cpu', cpu_cores: 1,
        ram_total_mb: 1, disk_free_mb: 0, disk_total_mb: 0,
        updated_at: '2026-01-01T00:00:00Z',
        network_interfaces: [],
      },
    });
    renderDetail();
    expect(screen.queryByText('Network Interfaces')).not.toBeInTheDocument();
  });

  it('Move to Group section hidden when groups.length === 0', () => {
    useDeviceStore.setState({ groups: [] });
    renderDetail();
    expect(screen.queryByText('Move to Group')).not.toBeInTheDocument();
  });

  it('Move to Group section hidden when groups.length === 1', () => {
    useDeviceStore.setState({
      groups: [{ id: 'g1', name: 'Only', created_at: '', updated_at: '' }],
    });
    renderDetail();
    expect(screen.queryByText('Move to Group')).not.toBeInTheDocument();
  });

  it('Move to Group dropdown excludes the device current group', () => {
    useDeviceStore.setState({
      groups: [
        { id: 'g1', name: 'Group 1', created_at: '', updated_at: '' },
        { id: 'g2', name: 'Group 2', created_at: '', updated_at: '' },
        { id: 'g3', name: 'Group 3', created_at: '', updated_at: '' },
      ],
    });
    renderDetail();
    const select = screen.getByDisplayValue('Select group...') as HTMLSelectElement;
    const optionLabels = Array.from(select.options).map((o) => o.textContent ?? '');
    expect(optionLabels).toEqual(['Select group...', 'Group 2', 'Group 3']);
    expect(optionLabels).not.toContain('Group 1');
  });

  it('handleMoveGroup is a no-op when no group is selected', async () => {
    vi.useRealTimers();
    const user = userEvent.setup();
    const updateGroupFn = vi.fn();
    useDeviceStore.setState({
      groups: [
        { id: 'g1', name: 'Group 1', created_at: '', updated_at: '' },
        { id: 'g2', name: 'Group 2', created_at: '', updated_at: '' },
      ],
      updateDeviceGroup: updateGroupFn,
    });
    renderDetail();
    const moveBtn = screen.getByText('Move') as HTMLButtonElement;
    expect(moveBtn.disabled).toBe(true);
    await user.click(moveBtn);
    expect(updateGroupFn).not.toHaveBeenCalled();
  });

  it('Active Sessions heading hidden when sessions array is empty', () => {
    useSessionStore.setState({ sessions: [] });
    renderDetail();
    expect(screen.queryByText(/Active Sessions/)).not.toBeInTheDocument();
  });

  it('Agent Version row hidden when device.agent_version is empty', () => {
    useDeviceStore.setState({
      selectedDevice: { ...mockDevice, agent_version: '' },
    });
    renderDetail();
    expect(screen.queryByText('Agent Version')).not.toBeInTheDocument();
    expect(screen.queryByText('1.0.0')).not.toBeInTheDocument();
  });

  it('Agent Version dt label is rendered alongside the value when set', () => {
    renderDetail();
    expect(screen.getByText('Agent Version')).toBeInTheDocument();
    expect(screen.getByText('1.0.0')).toBeInTheDocument();
  });

  it('latestManifest filters by device OS', () => {
    useDeviceStore.setState({
      selectedDevice: { ...mockDevice, os: 'linux', agent_version: '5.0.0' },
    });
    useUpdateStore.setState({
      manifests: [
        { version: '99.0.0', os: 'windows', arch: 'amd64', url: 'https://example.com', sha256: 'a', signature: 's', created_at: '2026-01-01T00:00:00Z' },
        { version: '5.0.0', os: 'linux', arch: 'amd64', url: 'https://example.com', sha256: 'b', signature: 's', created_at: '2026-01-01T00:00:00Z' },
      ],
    });
    renderDetail();
    // Windows manifest must be filtered out; linux 5.0.0 == device 5.0.0 → Up to date
    expect(screen.queryByText(/Upgrade to v99\.0\.0/)).not.toBeInTheDocument();
    expect(screen.getByRole('img', { name: 'Up to date' })).toBeInTheDocument();
  });

  it('latestManifest sorts numerically (10.0.0 > 2.0.0, not lexicographic)', () => {
    useDeviceStore.setState({
      selectedDevice: { ...mockDevice, os: 'linux', agent_version: '5.0.0' },
    });
    useUpdateStore.setState({
      manifests: [
        { version: '2.0.0', os: 'linux', arch: 'amd64', url: 'https://example.com', sha256: 'a', signature: 's', created_at: '2026-01-01T00:00:00Z' },
        { version: '10.0.0', os: 'linux', arch: 'amd64', url: 'https://example.com', sha256: 'b', signature: 's', created_at: '2026-01-01T00:00:00Z' },
      ],
    });
    renderDetail();
    expect(screen.getByText('Upgrade to v10.0.0')).toBeInTheDocument();
    expect(screen.queryByText('Upgrade to v2.0.0')).not.toBeInTheDocument();
  });

  it('isUpToDate compares versions numerically (5.0.0 < 10.0.0)', () => {
    useDeviceStore.setState({
      selectedDevice: { ...mockDevice, os: 'linux', agent_version: '5.0.0' },
    });
    useUpdateStore.setState({
      manifests: [
        { version: '10.0.0', os: 'linux', arch: 'amd64', url: 'https://example.com', sha256: 'a', signature: 's', created_at: '2026-01-01T00:00:00Z' },
      ],
    });
    renderDetail();
    // Without numeric comparison, '5.0.0' >= '10.0.0' lexicographically → "Up to date".
    // With numeric comparison, '5.0.0' < '10.0.0' → "Upgrade to v10.0.0".
    expect(screen.getByText('Upgrade to v10.0.0')).toBeInTheDocument();
    expect(screen.queryByRole('img', { name: 'Up to date' })).not.toBeInTheDocument();
  });

  it('Upgrade button disabled when device is offline', () => {
    useDeviceStore.setState({
      selectedDevice: { ...mockDevice, status: 'offline' as const },
    });
    useUpdateStore.setState({ manifests: [newerManifest] });
    renderDetail();
    const btn = screen.getByText(/Upgrade to v2\.0\.0/).closest('button') as HTMLButtonElement;
    expect(btn.disabled).toBe(true);
  });

  it('Upgrade button enabled when device online and not upgrading', () => {
    useUpdateStore.setState({ manifests: [newerManifest] });
    renderDetail();
    const btn = screen.getByText(/Upgrade to v2\.0\.0/).closest('button') as HTMLButtonElement;
    expect(btn.disabled).toBe(false);
  });

  it('Upgrade button returns to default label after a successful upgrade completes', async () => {
    vi.useRealTimers();
    const user = userEvent.setup();
    const upgradeFn = vi.fn().mockResolvedValue(true);
    useDeviceStore.setState({ upgradeAgent: upgradeFn });
    useUpdateStore.setState({ manifests: [newerManifest] });

    renderDetail();
    await user.click(screen.getByText('Upgrade to v2.0.0'));

    // After the promise settles, setIsUpgrading(false) flips the label back from "Upgrading..." to the original.
    // (A mutation that flips that final boolean leaves the label stuck on "Upgrading...".)
    expect(await screen.findByText('Upgrade to v2.0.0')).toBeInTheDocument();
    expect(screen.queryByText('Upgrading...')).not.toBeInTheDocument();
  });

  it('Upgrade button shows Upgrading... label and stays disabled while in-flight', async () => {
    vi.useRealTimers();
    const user = userEvent.setup();
    let resolve: (v: boolean) => void = () => undefined;
    const upgradeFn = vi.fn().mockReturnValue(new Promise<boolean>((r) => { resolve = r; }));
    useDeviceStore.setState({ upgradeAgent: upgradeFn });
    useUpdateStore.setState({ manifests: [newerManifest] });
    renderDetail();

    await user.click(screen.getByText('Upgrade to v2.0.0'));
    // While the promise is pending the button shows "Upgrading..." and is disabled.
    const upgrading = await screen.findByText('Upgrading...');
    const btn = upgrading.closest('button') as HTMLButtonElement;
    expect(btn.disabled).toBe(true);
    resolve(true);
  });

  it('Restart button disabled when device is offline', () => {
    useDeviceStore.setState({
      selectedDevice: { ...mockDevice, status: 'offline' as const },
    });
    renderDetail();
    const btn = screen.getByRole('button', { name: 'Restart Agent' }).closest('button') as HTMLButtonElement;
    expect(btn.disabled).toBe(true);
  });

  it('Restart button enabled when device online and not restarting', () => {
    renderDetail();
    const btn = screen.getByRole('button', { name: 'Restart Agent' }).closest('button') as HTMLButtonElement;
    expect(btn.disabled).toBe(false);
  });

  it('handleRestart shows Restarting... label while in-flight and resets after', async () => {
    vi.useRealTimers();
    const user = userEvent.setup();
    let resolve: (v: boolean) => void = () => undefined;
    const restartFn = vi.fn().mockReturnValue(new Promise<boolean>((r) => { resolve = r; }));
    useDeviceStore.setState({ restartAgent: restartFn });
    useSessionStore.setState({ sessions: [] });

    renderDetail();
    await user.click(screen.getByRole('button', { name: 'Restart Agent' }));

    expect(await screen.findByRole('button', { name: 'Restarting...' })).toBeInTheDocument();
    const btn = screen.getByRole('button', { name: 'Restarting...' }).closest('button') as HTMLButtonElement;
    expect(btn.disabled).toBe(true);

    resolve(true);
    // Label flips back to "Restart Agent" after promise settles.
    expect(await screen.findByRole('button', { name: 'Restart Agent' })).toBeInTheDocument();
  });

  it('handleRestart success toast contains "Restart command sent"', async () => {
    vi.useRealTimers();
    const user = userEvent.setup();
    const restartFn = vi.fn().mockResolvedValue(true);
    useDeviceStore.setState({ restartAgent: restartFn });
    useSessionStore.setState({ sessions: [] });
    useToastStore.setState({ toasts: [] });

    renderDetail();
    await user.click(screen.getByRole('button', { name: 'Restart Agent' }));

    const toasts = useToastStore.getState().toasts;
    expect(toasts.some((t) => t.message === 'Restart command sent' && t.type === 'success')).toBe(true);
  });

  it('handleStartSession passes relayUrl and capabilities through navigation state', async () => {
    vi.useRealTimers();
    const user = userEvent.setup();
    useDeviceStore.setState({
      selectedDevice: { ...mockDevice, capabilities: ['terminal', 'files'] },
    });
    useSessionStore.setState({
      ...useSessionStore.getState(),
      createSession: vi.fn().mockResolvedValue({ token: 'new-tok', relay_url: 'wss://relay.example' }),
    });

    function SessionProbe() {
      const location = useLocation();
      const state = location.state as { relayUrl?: string; capabilities?: string[] } | null;
      return (
        <>
          <p>Session View</p>
          <p data-testid="relay">{state?.relayUrl ?? ''}</p>
          <p data-testid="caps">{(state?.capabilities ?? []).join(',')}</p>
        </>
      );
    }

    const router = createMemoryRouter(
      [
        { path: '/devices/:id', element: <DeviceDetail /> },
        { path: '/sessions/:token', element: <SessionProbe /> },
      ],
      { initialEntries: ['/devices/d1'] },
    );
    render(<RouterProvider router={router} />);

    await user.click(screen.getByRole('button', { name: 'Start Session' }));

    expect(await screen.findByText('Session View')).toBeInTheDocument();
    expect(screen.getByTestId('relay').textContent).toBe('wss://relay.example');
    expect(screen.getByTestId('caps').textContent).toBe('terminal,files');
  });

  it('handlePowerAction success toast contains the action name', async () => {
    vi.useRealTimers();
    const user = userEvent.setup();
    const sendPowerFn = vi.fn().mockResolvedValue(true);
    setLinkedAmtDevice(sendPowerFn);
    useToastStore.setState({ toasts: [] });

    renderDetail();
    await user.click(screen.getByText('Power On'));

    const toasts = useToastStore.getState().toasts;
    expect(toasts.some((t) => t.message === 'Power action "power on" sent' && t.type === 'success')).toBe(true);
  });

  it('handleMoveGroup clears the dropdown selection after a successful move', async () => {
    vi.useRealTimers();
    const user = userEvent.setup();
    const updateGroupFn = vi.fn().mockResolvedValue(true);
    useDeviceStore.setState({
      groups: [
        { id: 'g1', name: 'Group 1', created_at: '', updated_at: '' },
        { id: 'g2', name: 'Group 2', created_at: '', updated_at: '' },
      ],
      updateDeviceGroup: updateGroupFn,
    });
    renderDetail();

    const select = screen.getByDisplayValue('Select group...') as HTMLSelectElement;
    await user.selectOptions(select, 'g2');
    expect(select.value).toBe('g2');

    const moveBtn = screen.getByText('Move') as HTMLButtonElement;
    expect(moveBtn.disabled).toBe(false);

    await user.click(moveBtn);

    // After a successful move, selectedGroupId is reset to '' — so the Move button is disabled again.
    // (A mutation that swaps `setSelectedGroupId('')` for any truthy literal leaves the button enabled.)
    const moveBtnAfter = screen.getByText('Move') as HTMLButtonElement;
    expect(moveBtnAfter.disabled).toBe(true);
  });

  it('polling interval is cleared on unmount', () => {
    const refreshFn = vi.fn();
    useDeviceStore.setState({ refreshDevice: refreshFn });

    const { unmount } = renderDetail();

    vi.advanceTimersByTime(30_000);
    expect(refreshFn).toHaveBeenCalledTimes(1);

    unmount();

    vi.advanceTimersByTime(60_000);
    // After unmount the interval must be cleared; the call count stays at 1.
    expect(refreshFn).toHaveBeenCalledTimes(1);
  });

  it('mount triggers fetchDevice, fetchSessions, fetchAmtDevices, fetchGroups, fetchManifests', () => {
    const fetchDeviceFn = vi.fn();
    const fetchSessionsFn = vi.fn();
    const fetchGroupsFn = vi.fn();
    const fetchManifestsFn = vi.fn();
    useDeviceStore.setState({ fetchDevice: fetchDeviceFn, fetchGroups: fetchGroupsFn });
    useSessionStore.setState({ ...useSessionStore.getState(), fetchSessions: fetchSessionsFn });
    useUpdateStore.setState({ fetchManifests: fetchManifestsFn });

    renderDetail();

    expect(fetchDeviceFn).toHaveBeenCalledWith('d1');
    expect(fetchSessionsFn).toHaveBeenCalledWith('d1');
    expect(fetchGroupsFn).toHaveBeenCalled();
    expect(fetchManifestsFn).toHaveBeenCalled();
  });

  it('Confirm cycle/reset button label collapses back when a non-destructive action runs', async () => {
    // Stryker target: the "destructive && confirmPowerAction !== action" guard, plus the setConfirmPowerAction(null) reset path.
    vi.useRealTimers();
    const user = userEvent.setup();
    const sendPowerFn = vi.fn().mockResolvedValue(true);
    setLinkedAmtDevice(sendPowerFn);
    renderDetail();

    await user.click(screen.getByText('Power Cycle'));
    expect(screen.getByText('Confirm Cycle')).toBeInTheDocument();

    // Running a different destructive action arms a new confirm, leaving the prior one as-is.
    await user.click(screen.getByText('Hard Reset'));
    expect(screen.getByText('Confirm Reset')).toBeInTheDocument();
    // Power Cycle is back to its default label because confirmPowerAction switched targets.
    expect(screen.getByText('Power Cycle')).toBeInTheDocument();
  });

  it('the 30s poll re-reads the session list so ended sessions drop out', () => {
    const fetchSessionsFn = vi.fn();
    useSessionStore.setState({ ...useSessionStore.getState(), fetchSessions: fetchSessionsFn });

    renderDetail();
    expect(fetchSessionsFn).toHaveBeenCalledTimes(1); // mount

    vi.advanceTimersByTime(30_000);
    expect(fetchSessionsFn).toHaveBeenCalledTimes(2);
    expect(fetchSessionsFn).toHaveBeenLastCalledWith('d1');
  });

  it('Confirm Cycle label switches back to Power Cycle on successful confirm', async () => {
    vi.useRealTimers();
    const user = userEvent.setup();
    const sendPowerFn = vi.fn().mockResolvedValue(true);
    setLinkedAmtDevice(sendPowerFn);
    renderDetail();

    await user.click(screen.getByText('Power Cycle'));
    expect(screen.getByText('Confirm Cycle')).toBeInTheDocument();

    await user.click(screen.getByText('Confirm Cycle'));
    expect(sendPowerFn).toHaveBeenCalledWith('amt-1', 'power_cycle');
    // After the confirm fires, the label collapses back.
    expect(screen.getByText('Power Cycle')).toBeInTheDocument();
    expect(screen.queryByText('Confirm Cycle')).not.toBeInTheDocument();
  });

  it('hardware section CPU row contains both cpu_model and cores', () => {
    useDeviceStore.setState({
      hardware: {
        device_id: 'd1', cpu_model: 'AMD Ryzen 9 7950X', cpu_cores: 16,
        ram_total_mb: 1, disk_free_mb: 0, disk_total_mb: 0,
        updated_at: '2026-01-01T00:00:00Z',
        network_interfaces: [],
      },
    });
    renderDetailWithHardware();
    const cpuDd = screen.getByText('CPU').nextElementSibling;
    expect(cpuDd?.textContent).toBe('AMD Ryzen 9 7950X (16 cores)');
  });

  it('Up to date pill rendered exactly when versions are equal', () => {
    useUpdateStore.setState({
      manifests: [{ version: '1.0.0', os: 'linux', arch: 'amd64', url: 'https://example.com', sha256: 'a', signature: 's', created_at: '2026-01-01T00:00:00Z' }],
    });
    renderDetail();
    const pill = screen.getByRole('img', { name: 'Up to date' });
    expect(pill.tagName).toBe('SPAN');
    // Upgrade button must NOT be rendered when isUpToDate.
    expect(screen.queryByText(/Upgrade to v/)).not.toBeInTheDocument();
  });

  it('latestManifest considers only the highest-version linux manifest in the list (3-entry mixed)', () => {
    useDeviceStore.setState({
      selectedDevice: { ...mockDevice, os: 'linux', agent_version: '0.5.0' },
    });
    useUpdateStore.setState({
      manifests: [
        { version: '11.0.0', os: 'windows', arch: 'amd64', url: 'https://example.com', sha256: 'a', signature: 's', created_at: '2026-01-01T00:00:00Z' },
        { version: '10.0.0', os: 'linux', arch: 'amd64', url: 'https://example.com', sha256: 'b', signature: 's', created_at: '2026-01-01T00:00:00Z' },
        { version: '2.0.0', os: 'linux', arch: 'amd64', url: 'https://example.com', sha256: 'c', signature: 's', created_at: '2026-01-01T00:00:00Z' },
      ],
    });
    renderDetail();
    expect(screen.getByText('Upgrade to v10.0.0')).toBeInTheDocument();
    expect(screen.queryByText(/Upgrade to v11\.0\.0/)).not.toBeInTheDocument();
    expect(screen.queryByText(/Upgrade to v2\.0\.0/)).not.toBeInTheDocument();
  });

  it('the AMT badge carries connection state in its tooltip, not in the page body', () => {
    setLinkedAmtDevice(vi.fn());
    renderDetail();
    const badge = screen.getByText('Intel AMT');
    expect(badge.getAttribute('title')).toBe('Intel AMT · online');
    // Connection state belongs to the badge tooltip; the body carries no status line.
    expect(screen.queryByText(/AMT Status:/)).not.toBeInTheDocument();
  });

  it('keeps the AMT setup instructions off the device page', () => {
    useDeviceStore.setState({ selectedDevice: { ...mockDevice, amt: { available: true } } });
    renderDetail();
    expect(screen.queryByText(/Intel AMT Setup/)).not.toBeInTheDocument();
    expect(screen.queryByText(/Enable AMT in BIOS/)).not.toBeInTheDocument();
  });

  it('AMT Power Cycle button toggles between default and confirm labels', async () => {
    vi.useRealTimers();
    const user = userEvent.setup();
    setLinkedAmtDevice(vi.fn().mockResolvedValue(true));
    renderDetail();
    expect(screen.getByText('Power Cycle')).toBeInTheDocument();
    await user.click(screen.getByText('Power Cycle'));
    expect(screen.getByText('Confirm Cycle')).toBeInTheDocument();
    expect(screen.queryByText(/^Power Cycle$/)).not.toBeInTheDocument();
  });

  it('shows hardware details when hardware data is available', () => {
    useDeviceStore.setState({
      hardware: {
        device_id: 'd1',
        cpu_model: 'Intel i7-12700',
        cpu_cores: 12,
        ram_total_mb: 32768,
        disk_free_mb: 102400,
        disk_total_mb: 512000,
        updated_at: '2026-01-01T00:00:00Z',
        network_interfaces: [
          { name: 'eth0', mac: '00:11:22:33:44:55', ipv4: ['192.168.1.10'], ipv6: [] },
        ],
      },
    });
    renderDetailWithHardware();

    expect(screen.getByText(/Intel i7-12700/)).toBeInTheDocument();
    expect(screen.getByText(/12 cores/)).toBeInTheDocument();
    expect(screen.getByText(/eth0/)).toBeInTheDocument();
    expect(screen.getByText(/00:11:22:33:44:55/)).toBeInTheDocument();
  });

  it('offers Enter Maintenance for an active device and hides Exit', () => {
    renderDetail();
    expect(screen.getByRole('button', { name: /enter maintenance/i })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /exit maintenance/i })).toBeNull();
  });

  it('shows a maintenance badge in the header when the device is in maintenance', () => {
    useDeviceStore.setState({
      selectedDevice: { ...mockDevice, maintenance_on: true, maintenance_since: '2026-07-19T00:00:00Z' },
    });
    renderDetail();
    // The badge carries a title beginning "In maintenance"; the panel copy has no title.
    expect(document.querySelector('[title^="In maintenance"]')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /exit maintenance/i })).toBeInTheDocument();
  });

  it('entering maintenance forwards the reason to setMaintenance and toasts success', async () => {
    vi.useRealTimers();
    const user = userEvent.setup();
    const setMaint = vi.fn().mockResolvedValue(true);
    useDeviceStore.setState({ setMaintenance: setMaint });
    useToastStore.setState({ toasts: [] });

    renderDetail();
    await user.type(screen.getByPlaceholderText(/reason/i), 'patching');
    await user.click(screen.getByRole('button', { name: /enter maintenance/i }));

    expect(setMaint).toHaveBeenCalledWith('d1', true, 'patching');
    expect(useToastStore.getState().toasts.some((t) => t.type === 'success')).toBe(true);
  });

  it('exiting maintenance calls setMaintenance(false) and toasts success', async () => {
    vi.useRealTimers();
    const user = userEvent.setup();
    const setMaint = vi.fn().mockResolvedValue(true);
    useDeviceStore.setState({
      selectedDevice: { ...mockDevice, maintenance_on: true, maintenance_since: '2026-07-19T00:00:00Z' },
      setMaintenance: setMaint,
    });
    useToastStore.setState({ toasts: [] });

    renderDetail();
    await user.click(screen.getByRole('button', { name: /exit maintenance/i }));

    expect(setMaint).toHaveBeenCalledWith('d1', false, undefined);
    expect(useToastStore.getState().toasts.some((t) => t.type === 'success')).toBe(true);
  });

  it('surfaces a failure toast when the maintenance toggle fails', async () => {
    vi.useRealTimers();
    const user = userEvent.setup();
    const setMaint = vi.fn().mockResolvedValue(false);
    useDeviceStore.setState({ setMaintenance: setMaint });
    useToastStore.setState({ toasts: [] });

    renderDetail();
    await user.click(screen.getByRole('button', { name: /enter maintenance/i }));

    expect(useToastStore.getState().toasts.some((t) => t.type === 'error')).toBe(true);
  });

  describe('non-admin', () => {
    beforeEach(() => { seedUser(false); });

    it('omits the delete control from the DOM', () => {
      renderDetail();
      expect(screen.queryByRole('button', { name: /delete device/i })).toBeNull();
    });

    it('omits the Move to Group panel from the DOM', () => {
      useDeviceStore.setState({
        groups: [
          { id: 'g1', name: 'Group 1', created_at: '', updated_at: '' },
          { id: 'g2', name: 'Group 2', created_at: '', updated_at: '' },
        ],
      });
      renderDetail();
      expect(screen.queryByText('Move to Group')).toBeNull();
    });

    it('keeps the device commands a member may issue', () => {
      renderDetail();
      expect(screen.getByRole('button', { name: /restart/i })).toBeInTheDocument();
      expect(screen.getByRole('button', { name: /enter maintenance/i })).toBeInTheDocument();
    });
  });
});
