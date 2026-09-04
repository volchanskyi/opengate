import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { useDeviceStore } from './state/device-store';
import { useSessionStore } from '../session';
import { useUpdateStore } from './state/update-store';
import { useToastStore } from '../../lib/feedback/toast-store';
import { mockDevice, newerManifest, renderDetail, seedDeviceDetailStores } from './DeviceDetail.testkit';

vi.mock('../../lib/api', () => ({
  api: {
    GET: vi.fn().mockResolvedValue({ data: undefined, error: { error: 'mock' }, response: { status: 404 } }),
    POST: vi.fn().mockResolvedValue({ data: { token: 'tok', relay_url: 'ws://localhost' }, error: undefined }),
    DELETE: vi.fn().mockResolvedValue({ error: undefined }),
  },
}));

// The incidents strip owns its own read and is exercised in
// DeviceIncidentsStrip.test.tsx; stub it here so these tests assert only that
// the device page carries it, keyed to the device on screen.
vi.mock('../investigations', () => ({
  DeviceIncidentsStrip: ({ deviceId }: { deviceId: string }) => (
    <div data-testid="incidents-strip">{deviceId}</div>
  ),
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

describe('DeviceDetail — agent version, upgrade and restart', () => {
  beforeEach(seedDeviceDetailStores);

  afterEach(() => {
    vi.useRealTimers();
  });

  it('shows agent version', () => {
    renderDetail();
    expect(screen.getByText('1.0.0')).toBeInTheDocument();
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
});
