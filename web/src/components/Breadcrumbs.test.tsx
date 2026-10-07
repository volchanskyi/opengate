import { render, screen } from '@testing-library/react';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { createMemoryRouter, RouterProvider } from 'react-router';
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
  });

  it('renders nothing on root path', () => {
    const { container } = renderAt('/');
    expect(container.querySelector('nav')).toBeNull();
  });

  it('renders devices breadcrumb at /devices (last segment, no link)', () => {
    renderAt('/devices');
    expect(screen.getByText('Dashboard')).toBeInTheDocument();
    const devicesText = screen.getByText('Devices');
    expect(devicesText.tagName).toBe('SPAN');
  });

  it('renders settings breadcrumb at /settings (last segment, no link)', () => {
    renderAt('/settings');
    const node = screen.getByText('Settings');
    expect(node.tagName).toBe('SPAN');
  });

  it('renders the investigations crumb at /investigations (last segment, no link)', () => {
    renderAt('/investigations');
    const node = screen.getByText('Investigations');
    expect(node.tagName).toBe('SPAN');
  });

  it('links back to the queue from inside a room, and names the room by its leading block', () => {
    const router = createMemoryRouter(
      [{ path: 'investigations/:id', element: <Breadcrumbs /> }],
      { initialEntries: ['/investigations/6f2b9c31-1111-2222-3333-444455556666'] },
    );
    render(<RouterProvider router={router} />);

    const queue = screen.getByText('Investigations');
    expect(queue.tagName).toBe('A');
    expect(queue.getAttribute('href')).toBe('/investigations');
    expect(screen.getByText('6f2b9c31')).toBeInTheDocument();
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
    const router = createMemoryRouter(
      [{ path: 'sessions/:token', element: <Breadcrumbs /> }],
      { initialEntries: ['/sessions/abc'] },
    );
    render(<RouterProvider router={router} />);
    const sessionsLink = screen.getByText('Sessions');
    expect(sessionsLink.tagName).toBe('A');
    expect(sessionsLink.getAttribute('href')).toBe('/sessions');
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
    renderAt('/security/sites');
    expect(screen.queryByText('security')).toBeNull();
  });

  it('renders empty when no recognized segments', () => {
    const { container } = renderAt('/totally-unknown');
    expect(container.querySelector('nav')).toBeNull();
  });

  it('names a device by the hostname its page handed the route', () => {
    const router = createMemoryRouter(
      [{ path: 'devices/:id', element: <Breadcrumbs /> }],
      { initialEntries: [{ pathname: '/devices/d1', state: { crumb: 'web-01' } }] },
    );
    render(<RouterProvider router={router} />);
    const devicesLink = screen.getByText('Devices');
    expect(devicesLink.tagName).toBe('A');
    expect(devicesLink.getAttribute('href')).toBe('/devices');
    expect(screen.getByText('web-01')).toBeInTheDocument();
  });

  it('renders /devices/<id> with the raw id until the page names it', () => {
    const router = createMemoryRouter(
      [{ path: 'devices/:id', element: <Breadcrumbs /> }],
      { initialEntries: ['/devices/raw-id'] },
    );
    render(<RouterProvider router={router} />);
    expect(screen.getByText('raw-id')).toBeInTheDocument();
  });

  it('names a room by the label its page handed the route', () => {
    const router = createMemoryRouter(
      [{ path: 'investigations/:id', element: <Breadcrumbs /> }],
      { initialEntries: [{ pathname: '/investigations/6f2b9c31-1111-2222-3333-444455556666', state: { crumb: 'cpu-saturated' } }] },
    );
    render(<RouterProvider router={router} />);
    expect(screen.getByText('cpu-saturated')).toBeInTheDocument();
    expect(screen.queryByText('6f2b9c31')).toBeNull();
  });

  it('Dashboard link always points to / and is rendered as an anchor', () => {
    renderAt('/devices');
    const dash = screen.getByText('Dashboard');
    expect(dash.tagName).toBe('A');
    expect(dash.getAttribute('href')).toBe('/');
  });

  it('uses ">" character as the separator between crumbs', () => {
    renderAt('/audit/foo');
    const nav = document.querySelector('nav')!;
    expect(nav.textContent).toMatch(/Dashboard\s*>\s*Audit Log/);
  });

  it('Settings non-last segment links to /settings (not the deeper path)', () => {
    renderAt('/settings/security');
    const settings = screen.getByText('Settings');
    expect(settings.tagName).toBe('A');
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

  it('does not take a page label for a segment outside a named section', () => {
    const router = createMemoryRouter(
      [{ path: 'audit/:id', element: <Breadcrumbs /> }],
      { initialEntries: [{ pathname: '/audit/d1', state: { crumb: 'web-01' } }] },
    );
    render(<RouterProvider router={router} />);
    expect(screen.queryByText('web-01')).toBeNull();
  });
});
