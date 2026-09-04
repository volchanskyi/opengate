import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { useDeviceStore } from './state/device-store';
import { useToastStore } from '../../lib/feedback/toast-store';
import { mockDevice, renderDetail, seedDeviceDetailStores, setLinkedAmtDevice } from './DeviceDetail.testkit';

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

describe('DeviceDetail — power and Intel AMT', () => {
  beforeEach(seedDeviceDetailStores);

  afterEach(() => {
    vi.useRealTimers();
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
});
