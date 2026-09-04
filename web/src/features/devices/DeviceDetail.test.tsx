import { render, screen, fireEvent } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createMemoryRouter, RouterProvider } from 'react-router';
import { useDeviceStore } from './state/device-store';
import { useSessionStore } from '../session';
import { useUpdateStore } from './state/update-store';
import { useToastStore } from '../../lib/feedback/toast-store';
import { DeviceDetail } from './DeviceDetail';
import { mockDevice, renderDetail, seedDeviceDetailStores, seedUser } from './DeviceDetail.testkit';

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

describe('DeviceDetail — the page', () => {
  beforeEach(seedDeviceDetailStores);

  afterEach(() => {
    vi.useRealTimers();
  });

  it('renders device info', () => {
    renderDetail();
    expect(screen.getByText('test-host')).toBeInTheDocument();
    expect(screen.getByText('linux')).toBeInTheDocument();
    expect(screen.getByText('Online')).toBeInTheDocument();
  });

  it('carries the open incidents this machine is caught up in', () => {
    renderDetail();
    expect(screen.getByTestId('incidents-strip')).toHaveTextContent('d1');
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

  it('delete requires confirmation', async () => {
    vi.useRealTimers();
    const user = userEvent.setup();
    renderDetail();

    await user.click(screen.getByRole('button', { name: 'Delete Device' }));
    expect(screen.getByRole('button', { name: 'Confirm Delete' })).toBeInTheDocument();
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

  it('renders logs card as separate tile', () => {
    renderDetail();
    expect(screen.getByText('Agent Logs')).toBeInTheDocument();
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

  it('mount triggers fetchDevice, fetchSessions, fetchAmtDevices, fetchSites, fetchManifests', () => {
    const fetchDeviceFn = vi.fn();
    const fetchSessionsFn = vi.fn();
    const fetchGroupsFn = vi.fn();
    const fetchManifestsFn = vi.fn();
    useDeviceStore.setState({ fetchDevice: fetchDeviceFn, fetchSites: fetchGroupsFn });
    useSessionStore.setState({ ...useSessionStore.getState(), fetchSessions: fetchSessionsFn });
    useUpdateStore.setState({ fetchManifests: fetchManifestsFn });

    renderDetail();

    expect(fetchDeviceFn).toHaveBeenCalledWith('d1');
    expect(fetchSessionsFn).toHaveBeenCalledWith('d1');
    expect(fetchGroupsFn).toHaveBeenCalled();
    expect(fetchManifestsFn).toHaveBeenCalled();
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

    it('omits the Move to Site panel from the DOM', () => {
      useDeviceStore.setState({
        sites: [
          { id: 'g1', organization_id: 'org-1', name: 'Site 1', created_at: '', updated_at: '' },
          { id: 'g2', organization_id: 'org-1', name: 'Site 2', created_at: '', updated_at: '' },
        ],
      });
      renderDetail();
      expect(screen.queryByText('Move to Site')).toBeNull();
    });

    it('keeps the device commands a member may issue', () => {
      renderDetail();
      expect(screen.getByRole('button', { name: /restart/i })).toBeInTheDocument();
      expect(screen.getByRole('button', { name: /enter maintenance/i })).toBeInTheDocument();
    });
  });
});
