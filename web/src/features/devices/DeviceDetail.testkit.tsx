// Shared setup for the DeviceDetail suites, which are split by concern:
// hardware, power and AMT, sites and customers, sessions, agent lifecycle, and
// the page itself. The fixtures, the two render helpers and the store seeding
// live here so a change to any of them reaches every suite at once.
//
// The `vi.mock` calls stay in each suite. Vitest hoists them above every import
// in the file that declares them, so a mock registered here would run after the
// importing file's own imports had already resolved.
import { render, screen, fireEvent } from '@testing-library/react';
import { vi } from 'vitest';
import { createMemoryRouter, RouterProvider } from 'react-router';
import type { components } from '../../types/api';
import { useDeviceStore } from './state/device-store';
import { useSessionStore } from '../session';
import { useUpdateStore } from './state/update-store';
import { useAuthStore } from '../../state/auth-store';
import { DeviceDetail } from './DeviceDetail';

export function renderDetail() {
  const router = createMemoryRouter(
    [
      { path: '/devices/:id', element: <DeviceDetail /> },
      { path: '/devices', element: <p>Device List</p> },
    ],
    { initialEntries: ['/devices/d1'] },
  );
  return render(<RouterProvider router={router} />);
}

/**
 * Render, then open the collapsed-by-default Hardware section. Located by
 * accessible name, which pins the decorative caret as `aria-hidden` — otherwise
 * the toggle answers to "▶ Hardware" instead of "Hardware".
 */
export function renderDetailWithHardware() {
  const result = renderDetail();
  fireEvent.click(screen.getByRole('button', { name: 'Hardware' }));
  return result;
}

export const mockDevice = {
  id: 'd1',
  organization_id: 'org-1', site_id: 'g1',
  hostname: 'test-host',
  os: 'linux',
  agent_version: '1.0.0',
  status: 'online' as const,
  capabilities: [],
  last_seen: '2026-01-01T00:00:00Z',
  created_at: '2025-12-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
};

export type PowerAction = components['schemas']['AMTPowerRequest']['action'];

export const newerManifest = { version: '2.0.0', os: 'linux', arch: 'amd64', url: 'https://example.com/agent', sha256: 'abc', signature: 'sig', created_at: '2026-01-01T00:00:00Z' };

/** Puts the selected device in the linked-and-online AMT state power actions need. */
export function setLinkedAmtDevice(sendPowerAction: (uuid: string, action: PowerAction) => Promise<boolean>) {
  useDeviceStore.setState({
    selectedDevice: { ...mockDevice, amt: { available: true, status: 'online' as const, uuid: 'amt-1' } },
    sendPowerAction,
  });
}

/** Sets the signed-in user's admin flag; delete and site-move are admin-only. */
export function seedUser(isAdmin: boolean) {
  useAuthStore.setState({
    user: { id: 'u1', email: 'a@b.com', display_name: 'A', is_admin: isAdmin, created_at: '', updated_at: '' },
  });
}

/** The stores every DeviceDetail suite starts from. */
export function seedDeviceDetailStores() {
  vi.useFakeTimers();
  vi.clearAllMocks();
  seedUser(true);
  useDeviceStore.setState({
    selectedDevice: mockDevice,
    isLoading: false,
    error: null,
    devices: [],
    sites: [],
    selectedSiteId: null,
    fetchDevice: vi.fn(),
    refreshDevice: vi.fn(),
    fetchSites: vi.fn(),
    fetchHardware: vi.fn(),
    deleteDevice: vi.fn(),
    upgradeAgent: vi.fn().mockResolvedValue(true),
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
}
