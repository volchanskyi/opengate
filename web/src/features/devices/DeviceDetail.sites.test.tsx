import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { useDeviceStore } from './state/device-store';
import { useOrganizationStore } from '../organizations';
import { useToastStore } from '../../lib/feedback/toast-store';
import { renderDetail, seedDeviceDetailStores } from './DeviceDetail.testkit';

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

describe('DeviceDetail — sites and customers', () => {
  beforeEach(seedDeviceDetailStores);

  afterEach(() => {
    vi.useRealTimers();
  });

  it('handleMoveGroup moves device to new site', async () => {
    vi.useRealTimers();
    const user = userEvent.setup();
    const updateGroupFn = vi.fn().mockResolvedValue(true);
    useDeviceStore.setState({
      sites: [
        { id: 'g1', organization_id: 'org-1', name: 'Site 1', created_at: '', updated_at: '' },
        { id: 'g2', organization_id: 'org-1', name: 'Site 2', created_at: '', updated_at: '' },
      ],
      updateDeviceSite: updateGroupFn,
    });
    useToastStore.setState({ toasts: [] });

    renderDetail();

    // Select new site from the "Move to Site" dropdown (not the logs filter dropdown)
    const groupSelect = screen.getByDisplayValue('Select site...');
    await user.selectOptions(groupSelect, 'g2');
    await user.click(screen.getByText('Move'));

    expect(updateGroupFn).toHaveBeenCalledWith('d1', 'g2');
    const toasts = useToastStore.getState().toasts;
    expect(toasts.some((t) => t.message.includes('moved to new site'))).toBe(true);
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

  it('handleMoveGroup shows failure toast on error', async () => {
    vi.useRealTimers();
    const user = userEvent.setup();
    const updateGroupFn = vi.fn().mockResolvedValue(false);
    useDeviceStore.setState({
      sites: [
        { id: 'g1', organization_id: 'org-1', name: 'Site 1', created_at: '', updated_at: '' },
        { id: 'g2', organization_id: 'org-1', name: 'Site 2', created_at: '', updated_at: '' },
      ],
      updateDeviceSite: updateGroupFn,
    });
    useToastStore.setState({ toasts: [] });

    renderDetail();
    const groupSelect = screen.getByDisplayValue('Select site...');
    await user.selectOptions(groupSelect, 'g2');
    await user.click(screen.getByText('Move'));

    const toasts = useToastStore.getState().toasts;
    expect(toasts.some((t) => t.message.includes('Failed to move device'))).toBe(true);
  });

  it('Move to Site section hidden when sites.length === 0', () => {
    useDeviceStore.setState({ sites: [] });
    renderDetail();
    expect(screen.queryByText('Move to Site')).not.toBeInTheDocument();
  });

  it('Move to Site section hidden when sites.length === 1', () => {
    useDeviceStore.setState({
      sites: [{ id: 'g1', organization_id: 'org-1', name: 'Only', created_at: '', updated_at: '' }],
    });
    renderDetail();
    expect(screen.queryByText('Move to Site')).not.toBeInTheDocument();
  });

  it('Move to Site dropdown excludes the device current site', () => {
    useDeviceStore.setState({
      sites: [
        { id: 'g1', organization_id: 'org-1', name: 'Site 1', created_at: '', updated_at: '' },
        { id: 'g2', organization_id: 'org-1', name: 'Site 2', created_at: '', updated_at: '' },
        { id: 'g3', organization_id: 'org-1', name: 'Site 3', created_at: '', updated_at: '' },
      ],
    });
    renderDetail();
    const select = screen.getByDisplayValue('Select site...') as HTMLSelectElement;
    const optionLabels = Array.from(select.options).map((o) => o.textContent ?? '');
    expect(optionLabels).toEqual(['Select site...', 'Site 2', 'Site 3']);
    expect(optionLabels).not.toContain('Site 1');
  });

  it('handleMoveGroup is a no-op when no site is selected', async () => {
    vi.useRealTimers();
    const user = userEvent.setup();
    const updateGroupFn = vi.fn();
    useDeviceStore.setState({
      sites: [
        { id: 'g1', organization_id: 'org-1', name: 'Site 1', created_at: '', updated_at: '' },
        { id: 'g2', organization_id: 'org-1', name: 'Site 2', created_at: '', updated_at: '' },
      ],
      updateDeviceSite: updateGroupFn,
    });
    renderDetail();
    const moveBtn = screen.getByText('Move') as HTMLButtonElement;
    expect(moveBtn.disabled).toBe(true);
    await user.click(moveBtn);
    expect(updateGroupFn).not.toHaveBeenCalled();
  });

  it('handleMoveGroup clears the dropdown selection after a successful move', async () => {
    vi.useRealTimers();
    const user = userEvent.setup();
    const updateGroupFn = vi.fn().mockResolvedValue(true);
    useDeviceStore.setState({
      sites: [
        { id: 'g1', organization_id: 'org-1', name: 'Site 1', created_at: '', updated_at: '' },
        { id: 'g2', organization_id: 'org-1', name: 'Site 2', created_at: '', updated_at: '' },
      ],
      updateDeviceSite: updateGroupFn,
    });
    renderDetail();

    const select = screen.getByDisplayValue('Select site...') as HTMLSelectElement;
    await user.selectOptions(select, 'g2');
    expect(select.value).toBe('g2');

    const moveBtn = screen.getByText('Move') as HTMLButtonElement;
    expect(moveBtn.disabled).toBe(false);

    await user.click(moveBtn);

    // After a successful move, selectedSiteId is reset to '' — so the Move button is disabled again.
    // (A mutation that swaps `setSelectedSiteId('')` for any truthy literal leaves the button enabled.)
    const moveBtnAfter = screen.getByText('Move') as HTMLButtonElement;
    expect(moveBtnAfter.disabled).toBe(true);
  });
});
