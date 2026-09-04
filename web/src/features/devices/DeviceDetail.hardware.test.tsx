import { screen, act, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { useDeviceStore } from './state/device-store';
import { mockDevice, renderDetail, renderDetailWithHardware, seedDeviceDetailStores } from './DeviceDetail.testkit';

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

describe('DeviceDetail — hardware and inventory', () => {
  beforeEach(seedDeviceDetailStores);

  afterEach(() => {
    vi.useRealTimers();
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

  it('shows N/A for the Site ID when the device is not in a site', () => {
    useDeviceStore.setState({ selectedDevice: { ...mockDevice, organization_id: 'org-1', site_id: '' } });
    renderDetail();
    expect(screen.getByText('Site ID').nextElementSibling?.textContent).toBe('N/A');
  });

  it('shows the site id when the device belongs to a site', () => {
    useDeviceStore.setState({ selectedDevice: { ...mockDevice, organization_id: 'org-1', site_id: 'g-42' } });
    renderDetail();
    expect(screen.getByText('Site ID').nextElementSibling?.textContent).toBe('g-42');
  });

  it('shows N/A for the all-zeros placeholder Site ID', () => {
    useDeviceStore.setState({ selectedDevice: { ...mockDevice, organization_id: 'org-1', site_id: '00000000-0000-0000-0000-000000000000' } });
    renderDetail();
    expect(screen.getByText('Site ID').nextElementSibling?.textContent).toBe('N/A');
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

  it('the interface list carries its heading as an accessible name', () => {
    // A short interface name — `lo` is on every Linux host — is a substring of
    // plenty of unrelated text on this page, so a reader looking for one has to
    // be able to ask inside the list rather than across the document. The
    // accessible name is what makes that possible.
    useDeviceStore.setState({
      hardware: {
        device_id: 'd1', cpu_model: 'cpu', cpu_cores: 1,
        ram_total_mb: 1, disk_free_mb: 0, disk_total_mb: 0,
        updated_at: '2026-01-01T00:00:00Z',
        network_interfaces: [
          { name: 'lo', mac: '00:00:00:00:00:00', ipv4: ['127.0.0.1'], ipv6: [] },
        ],
      },
    });
    renderDetailWithHardware();
    const list = screen.getByRole('list', { name: 'Network Interfaces' });
    expect(within(list).getByText(/^lo:/)).toBeInTheDocument();
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
    expect(screen.getByText('RAM').nextElementSibling?.textContent).toBe('32.0 GB');
    // Free before total, and neither reading the other: a disk with 100 GB left
    // of 500 must not read as a full one, nor an empty one.
    expect(screen.getByText('Disk').nextElementSibling?.textContent).toBe(
      '100 GB free / 500 GB',
    );
    expect(screen.getByText(/eth0/)).toBeInTheDocument();
    expect(screen.getByText(/00:11:22:33:44:55/)).toBeInTheDocument();
  });
});
