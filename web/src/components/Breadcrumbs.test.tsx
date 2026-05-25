import { render, screen } from '@testing-library/react';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { useDeviceStore } from '../features/devices/state/device-store';
import { Breadcrumbs } from './Breadcrumbs';

function renderAt(path: string) {
  const router = createMemoryRouter(
    [{ path: '*', element: <Breadcrumbs /> }],
    { initialEntries: [path] },
  );
  return render(<RouterProvider router={router} />);
}

describe('Breadcrumbs', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useDeviceStore.setState({ selectedDevice: null });
  });

  it('renders nothing on root path', () => {
    const { container } = renderAt('/');
    expect(container.querySelector('nav')).toBeNull();
  });

  it('renders devices breadcrumb at /devices (last segment, no link)', () => {
    renderAt('/devices');
    expect(screen.getByText('Dashboard')).toBeInTheDocument();
    // The Devices crumb is last → rendered as a span (no link).
    const devicesText = screen.getByText('Devices');
    expect(devicesText.tagName).toBe('SPAN');
  });

  it('renders settings breadcrumb at /settings (last segment, no link)', () => {
    renderAt('/settings');
    const node = screen.getByText('Settings');
    expect(node.tagName).toBe('SPAN');
  });

  it('renders setup breadcrumb at /setup', () => {
    renderAt('/setup');
    expect(screen.getByText('Add Device')).toBeInTheDocument();
  });

  it('renders profile breadcrumb at /profile', () => {
    renderAt('/profile');
    expect(screen.getByText('Profile')).toBeInTheDocument();
  });

  it('renders audit breadcrumb at /audit (last)', () => {
    renderAt('/audit');
    const node = screen.getByText('Audit Log');
    expect(node.tagName).toBe('SPAN');
  });

  // Two-segment paths exercise the !isLast branch and pin the link href.
  it('renders /audit/foo with Audit Log linked to /audit', () => {
    renderAt('/audit/foo');
    const link = screen.getByText('Audit Log');
    expect(link.tagName).toBe('A');
    expect(link.getAttribute('href')).toBe('/audit');
  });

  it('renders /users/u1 with Users linked to /users', () => {
    renderAt('/users/u1');
    const link = screen.getByText('Users');
    expect(link.tagName).toBe('A');
    expect(link.getAttribute('href')).toBe('/users');
  });

  it('renders /updates/x with Agent Settings linked to /updates', () => {
    renderAt('/updates/x');
    const link = screen.getByText('Agent Settings');
    expect(link.tagName).toBe('A');
    expect(link.getAttribute('href')).toBe('/updates');
  });

  it('renders /sessions/abc as Session label (params.token branch)', () => {
    // The Sessions list page is not a real route; this hits both the 'sessions'
    // segment with a `next` (non-last) and the params.token segment.
    const router = createMemoryRouter(
      [{ path: 'sessions/:token', element: <Breadcrumbs /> }],
      { initialEntries: ['/sessions/abc'] },
    );
    render(<RouterProvider router={router} />);
    // 'sessions' (not last) → Sessions linked
    const sessionsLink = screen.getByText('Sessions');
    expect(sessionsLink.tagName).toBe('A');
    expect(sessionsLink.getAttribute('href')).toBe('/sessions');
    // 'abc' === params.token → Session label
    expect(screen.getByText('Session')).toBeInTheDocument();
  });

  it('renders /sessions as last segment with Session label', () => {
    renderAt('/sessions');
    expect(screen.getByText('Session')).toBeInTheDocument();
  });

  it('renders /permissions with Permissions label', () => {
    renderAt('/permissions');
    expect(screen.getByText('Permissions')).toBeInTheDocument();
  });

  it('skips security segment and shows next-level label', () => {
    renderAt('/security/groups');
    // 'security' is intentionally skipped — only the next segment renders.
    // 'groups' is not in the recognized list, so nothing else shows.
    // Just confirm no 'security' label leaked in.
    expect(screen.queryByText('security')).toBeNull();
  });

  it('renders empty when no recognized segments', () => {
    const { container } = renderAt('/totally-unknown');
    // No recognized segments → null (kills the empty-crumbs return-null path).
    expect(container.querySelector('nav')).toBeNull();
  });

  it('renders /devices/<id> with hostname when selectedDevice is loaded', () => {
    useDeviceStore.setState({
      selectedDevice: { id: 'd1', group_id: 'g1', hostname: 'web-01', os: 'linux', agent_version: '', capabilities: [], status: 'online', last_seen: '', created_at: '', updated_at: '' },
    });
    const router = createMemoryRouter(
      [{ path: 'devices/:id', element: <Breadcrumbs /> }],
      { initialEntries: ['/devices/d1'] },
    );
    render(<RouterProvider router={router} />);
    // Devices segment is non-last → link to /devices.
    const devicesLink = screen.getByText('Devices');
    expect(devicesLink.tagName).toBe('A');
    expect(devicesLink.getAttribute('href')).toBe('/devices');
    // Device id segment shows hostname.
    expect(screen.getByText('web-01')).toBeInTheDocument();
  });

  it('renders /devices/<id> with raw id when no selectedDevice', () => {
    useDeviceStore.setState({ selectedDevice: null });
    const router = createMemoryRouter(
      [{ path: 'devices/:id', element: <Breadcrumbs /> }],
      { initialEntries: ['/devices/raw-id'] },
    );
    render(<RouterProvider router={router} />);
    expect(screen.getByText('raw-id')).toBeInTheDocument();
  });

  it('Dashboard link always points to / and is rendered as an anchor', () => {
    renderAt('/devices');
    const dash = screen.getByText('Dashboard');
    expect(dash.tagName).toBe('A');
    expect(dash.getAttribute('href')).toBe('/');
  });

  it('uses ">" character as the separator between crumbs', () => {
    renderAt('/audit/foo');
    // Two separators: "Dashboard > Audit Log > foo-but-foo-is-unrecognized" → really one for Dashboard→Audit.
    const nav = document.querySelector('nav')!;
    expect(nav.textContent).toMatch(/Dashboard\s*>\s*Audit Log/);
  });

  it('Settings non-last segment links to /settings (not the deeper path)', () => {
    renderAt('/settings/security');
    const settings = screen.getByText('Settings');
    expect(settings.tagName).toBe('A');
    // The Settings crumb pins its href to '/settings' explicitly — kills the StringLiteral mutant
    // that swapped the href to "".
    expect(settings.getAttribute('href')).toBe('/settings');
  });

  it('Devices non-last segment links to /devices (not the deeper path)', () => {
    const router = createMemoryRouter(
      [{ path: 'devices/:id', element: <Breadcrumbs /> }],
      { initialEntries: ['/devices/some-id'] },
    );
    render(<RouterProvider router={router} />);
    const link = screen.getByText('Devices');
    expect(link.tagName).toBe('A');
    expect(link.getAttribute('href')).toBe('/devices');
  });

  it('does not treat a device-id segment outside the devices/* path as a hostname', () => {
    useDeviceStore.setState({
      selectedDevice: { id: 'd1', group_id: 'g1', hostname: 'web-01', os: 'linux', agent_version: '', capabilities: [], status: 'online', last_seen: '', created_at: '', updated_at: '' },
    });
    // The guard `crumbs.some((c) => c.label === 'Devices')` ensures the hostname swap only
    // applies under /devices/*. Here params.id matches but the path is /audit/d1 — no Devices crumb yet.
    const router = createMemoryRouter(
      [{ path: 'audit/:id', element: <Breadcrumbs /> }],
      { initialEntries: ['/audit/d1'] },
    );
    render(<RouterProvider router={router} />);
    // 'd1' segment must NOT render as 'web-01' (kills the guard mutant).
    expect(screen.queryByText('web-01')).toBeNull();
  });
});
