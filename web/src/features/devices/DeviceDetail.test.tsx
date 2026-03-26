import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { useDeviceStore } from '../../state/device-store';
import { useSessionStore } from '../../state/session-store';
import { useAMTStore } from '../../state/amt-store';
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

describe('DeviceDetail', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.clearAllMocks();
    // Override fetchDevice/fetchSessions to no-ops so they don't overwrite pre-set state
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

  it('has start session button', () => {
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

    // Initial fetch on mount
    expect(fetchDeviceFn).toHaveBeenCalledTimes(1);

    // Advance 30s — should trigger second fetch
    vi.advanceTimersByTime(30_000);
    expect(fetchDeviceFn).toHaveBeenCalledTimes(2);

    // Advance another 30s — third fetch
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
});
