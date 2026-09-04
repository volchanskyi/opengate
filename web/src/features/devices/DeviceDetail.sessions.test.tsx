import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createMemoryRouter, RouterProvider, useLocation } from 'react-router';
import { useDeviceStore } from './state/device-store';
import { useSessionStore } from '../session';
import { useToastStore } from '../../lib/feedback/toast-store';
import { DeviceDetail } from './DeviceDetail';
import { mockDevice, renderDetail, seedDeviceDetailStores } from './DeviceDetail.testkit';

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

describe('DeviceDetail — sessions', () => {
  beforeEach(seedDeviceDetailStores);

  afterEach(() => {
    vi.useRealTimers();
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

  it('Active Sessions heading hidden when sessions array is empty', () => {
    useSessionStore.setState({ sessions: [] });
    renderDetail();
    expect(screen.queryByText(/Active Sessions/)).not.toBeInTheDocument();
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

  it('the 30s poll re-reads the session list so ended sessions drop out', () => {
    const fetchSessionsFn = vi.fn();
    useSessionStore.setState({ ...useSessionStore.getState(), fetchSessions: fetchSessionsFn });

    renderDetail();
    expect(fetchSessionsFn).toHaveBeenCalledTimes(1); // mount

    vi.advanceTimersByTime(30_000);
    expect(fetchSessionsFn).toHaveBeenCalledTimes(2);
    expect(fetchSessionsFn).toHaveBeenLastCalledWith('d1');
  });
});
