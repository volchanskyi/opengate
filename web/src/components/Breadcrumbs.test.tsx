import { render, screen } from '@testing-library/react';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { useDeviceStore } from '../state/device-store';
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

  it('renders devices breadcrumb', () => {
    renderAt('/devices');
    expect(screen.getByText('Dashboard')).toBeInTheDocument();
    expect(screen.getByText('Devices')).toBeInTheDocument();
  });

  it('renders settings breadcrumb', () => {
    renderAt('/settings');
    expect(screen.getByText('Settings')).toBeInTheDocument();
  });

  it('renders setup breadcrumb', () => {
    renderAt('/setup');
    expect(screen.getByText('Add Device')).toBeInTheDocument();
  });

  it('renders profile breadcrumb', () => {
    renderAt('/profile');
    expect(screen.getByText('Profile')).toBeInTheDocument();
  });

  it('renders audit breadcrumb', () => {
    renderAt('/audit');
    expect(screen.getByText('Audit Log')).toBeInTheDocument();
  });
});
