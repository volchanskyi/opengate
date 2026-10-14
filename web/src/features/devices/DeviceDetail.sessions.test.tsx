import { screen } from '@testing-library/react';
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { useDeviceStore } from './state/device-store';
import { useSessionStore } from '../session';
import { mockDevice, renderDetail, seedDeviceDetailStores } from './DeviceDetail.testkit';

vi.mock('../../lib/api', () => ({
  api: {
    GET: vi.fn().mockResolvedValue({ data: undefined, error: { error: 'mock' }, response: { status: 404 } }),
    POST: vi.fn().mockResolvedValue({ data: { token: 'tok', relay_url: 'ws://localhost' }, error: undefined }),
    DELETE: vi.fn().mockResolvedValue({ error: undefined }),
  },
}));

vi.mock('../investigations', () => ({
  DeviceIncidentsStrip: ({ deviceId }: { deviceId: string }) => (
    <div data-testid="incidents-strip">{deviceId}</div>
  ),
}));

vi.mock('./DeviceMetrics', () => ({
  DeviceMetrics: ({ deviceId, onViewLogs }: { deviceId: string; onViewLogs?: (f: number, t: number) => void }) => (
    <div data-testid="device-metrics">
      {deviceId}
      <button type="button" onClick={() => onViewLogs?.(1000, 4600)}>mock-view-logs</button>
    </div>
  ),
}));

describe('DeviceDetail — sessions', () => {
  beforeEach(seedDeviceDetailStores);

  afterEach(() => {
    vi.useRealTimers();
  });

  it('lists who holds the host and since when, and never a session token', () => {
    renderDetail();
    expect(screen.getByText('Active Sessions (1)')).toBeInTheDocument();
    expect(screen.getAllByText(/^Session open — Alice, since \d{1,2}:\d{2}/).length).toBeGreaterThan(0);
    expect(document.body.textContent).not.toMatch(/tok/);
  });

  it('offers to open a free host’s session in its own tab', () => {
    renderDetail();
    expect(screen.getByRole('button', { name: 'Open session' })).toBeEnabled();
  });

  it('shows the holder in the header of a host another technician holds', () => {
    useDeviceStore.setState({
      selectedDevice: { ...mockDevice, session: { holder_id: 'u2', holder_name: 'Alice', started_at: '2026-10-12T14:02:00Z' } },
    });
    renderDetail();
    expect(screen.getByRole('button', { name: 'Open session' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'End session' })).toBeInTheDocument();
  });

  it('Active Sessions heading hidden when sessions array is empty', () => {
    useSessionStore.setState({ sessions: [] });
    renderDetail();
    expect(screen.queryByText(/Active Sessions/)).not.toBeInTheDocument();
  });

  it('the 30s poll re-reads the session list so ended sessions drop out', () => {
    const fetchSessionsFn = vi.fn();
    useSessionStore.setState({ ...useSessionStore.getState(), fetchSessions: fetchSessionsFn });

    renderDetail();
    expect(fetchSessionsFn).toHaveBeenCalledTimes(1);
    vi.advanceTimersByTime(30_000);
    expect(fetchSessionsFn).toHaveBeenCalledTimes(2);
    expect(fetchSessionsFn).toHaveBeenLastCalledWith('d1');
  });
});
