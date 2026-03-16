import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { useDeviceStore } from '../../state/device-store';
import { useSessionStore } from '../../state/session-store';
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
  last_seen: '2026-01-01T00:00:00Z',
  created_at: '2025-12-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
};

describe('DeviceDetail', () => {
  beforeEach(() => {
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
      deleteDevice: vi.fn(),
    });
    useSessionStore.setState({
      sessions: [{ token: 'tok1', device_id: 'd1', user_id: 'u1', created_at: '' }],
      isLoading: false,
      error: null,
      fetchSessions: vi.fn(),
      createSession: vi.fn().mockResolvedValue({ token: 'new-tok', relay_url: 'ws://localhost' }),
    });
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
    const user = userEvent.setup();
    renderDetail();

    await user.click(screen.getByText('Delete Device'));
    expect(screen.getByText('Confirm Delete')).toBeInTheDocument();
  });

  it('navigates back on back button', async () => {
    const user = userEvent.setup();
    renderDetail();

    await user.click(screen.getByText(/Back to devices/));
    expect(screen.getByText('Device List')).toBeInTheDocument();
  });
});
