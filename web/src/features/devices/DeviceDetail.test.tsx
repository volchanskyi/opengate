import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { useDeviceStore } from '../../state/device-store';
import { useSessionStore } from '../../state/session-store';
import { useAMTStore } from '../../state/amt-store';
import { useUpdateStore } from '../../state/update-store';
import { useToastStore } from '../../state/toast-store';
import { DeviceDetail } from './DeviceDetail';

vi.mock('../../lib/api', () => ({
  api: {
    GET: vi.fn().mockResolvedValue({ data: undefined, error: { error: 'mock' }, response: { status: 404 } }),
    POST: vi.fn().mockResolvedValue({ data: { token: 'tok', relay_url: 'ws://localhost' }, error: undefined }),
    DELETE: vi.fn().mockResolvedValue({ error: undefined }),
  },
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

const mockDevice = {
  id: 'd1',
  group_id: 'g1',
  hostname: 'test-host',
  os: 'linux',
  agent_version: '1.0.0',
  status: 'online' as const,
  capabilities: [],
  last_seen: '2026-01-01T00:00:00Z',
  created_at: '2025-12-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
};

const newerManifest = { version: '2.0.0', os: 'linux', arch: 'amd64', url: 'https://example.com/agent', sha256: 'abc', signature: 'sig', created_at: '2026-01-01T00:00:00Z' };

describe('DeviceDetail', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.clearAllMocks();
    useDeviceStore.setState({
      selectedDevice: mockDevice,
      isLoading: false,
      error: null,
      devices: [],
      groups: [],
      selectedGroupId: null,
      fetchDevice: vi.fn(),
      fetchGroups: vi.fn(),
      deleteDevice: vi.fn(),
      upgradeAgent: vi.fn().mockResolvedValue(true),
    });
    useAMTStore.setState({
      amtDevices: [],
      selectedAmtDevice: null,
      isLoading: false,
      error: null,
      fetchAmtDevices: vi.fn(),
      fetchAmtDevice: vi.fn(),
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
    expect(screen.getByText('Start Session')).toBeInTheDocument();
  });

  it('delete requires confirmation', async () => {
    vi.useRealTimers();
    const user = userEvent.setup();
    renderDetail();

    await user.click(screen.getByText('Delete Device'));
    expect(screen.getByText('Confirm Delete')).toBeInTheDocument();
  });

  it('shows agent version', () => {
    renderDetail();
    expect(screen.getByText('1.0.0')).toBeInTheDocument();
  });

  it('polls device data every 30 seconds', () => {
    const fetchDeviceFn = vi.fn();
    useDeviceStore.setState({ fetchDevice: fetchDeviceFn });
    renderDetail();

    expect(fetchDeviceFn).toHaveBeenCalledTimes(1);

    vi.advanceTimersByTime(30_000);
    expect(fetchDeviceFn).toHaveBeenCalledTimes(2);

    vi.advanceTimersByTime(30_000);
    expect(fetchDeviceFn).toHaveBeenCalledTimes(3);
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
    await user.click(screen.getByText('Start Session'));

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

    await user.click(screen.getByText('Start Session'));

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
    expect(screen.getByText('Up to date')).toBeInTheDocument();
  });

  it('renders logs card as separate tile', () => {
    renderDetail();
    expect(screen.getByText('Fetch Logs')).toBeInTheDocument();
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
    await user.click(screen.getByText('Restart Agent'));
    expect(screen.getByText(/Confirm \(1 active\)/)).toBeInTheDocument();

    // Second click triggers the actual restart
    await user.click(screen.getByText(/Confirm \(1 active\)/));
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
    await user.click(screen.getByText('Restart Agent'));

    const toasts = useToastStore.getState().toasts;
    expect(toasts.some((t) => t.message.includes('Failed to restart'))).toBe(true);
  });

  it('handleMoveGroup moves device to new group', async () => {
    vi.useRealTimers();
    const user = userEvent.setup();
    const updateGroupFn = vi.fn().mockResolvedValue(true);
    useDeviceStore.setState({
      groups: [
        { id: 'g1', name: 'Group 1', owner_id: 'u1', created_at: '', updated_at: '' },
        { id: 'g2', name: 'Group 2', owner_id: 'u1', created_at: '', updated_at: '' },
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
    await user.click(screen.getByText('Delete Device'));
    expect(screen.getByText('Confirm Delete')).toBeInTheDocument();

    // Second click deletes and navigates
    await user.click(screen.getByText('Confirm Delete'));
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
});
